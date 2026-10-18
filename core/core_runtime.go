package core

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	corelog "github.com/SnishaperTeam/snishaper-core/log"

	"github.com/SnishaperTeam/snishaper-core/certmanager"
	coreconfig "github.com/SnishaperTeam/snishaper-core/config"
	"github.com/SnishaperTeam/snishaper-core/netiface"
	"github.com/SnishaperTeam/snishaper-core/proxy"
	"github.com/SnishaperTeam/snishaper-core/singtun"
	"github.com/SnishaperTeam/snishaper-core/subscription"
	"flowweaver/common"
)

// ProductName is the user-visible product name. It decides the TUN adapter
// name, the generated CA subject and the stale-adapter cleanup scope, so the
// core library must never label FlowWeaver's artefacts as its own reference
// product.
const ProductName = "FlowWeaver"

// tunDebugPrefix 是 core singtun 给 debug 级日志加的前缀。
//
// 前缀必须与 snishaper-core/singtun/logger.go 的 Handle 保持一致：
// debug 级写 "[sing-tun:debug] "，info 写 "[sing-tun] "，warn/error 分别写
// "[sing-tun:warn] " 和 "[sing-tun:error] "。
const tunDebugPrefix = "[sing-tun:debug]"

// tunLogDebugEnabled 控制每连接级别的 TUN 详细日志，默认关闭。
//
// 这些日志在正常使用时每条 TCP 连接会打一到三行（TCP flow / CONNECT
// request / CONNECT response），每次 DNS 查询还会再打一行（fake-ip
// allocated），足以把有用的错误信息淹没在 5000 条环形缓冲里。
// 通过环境变量 FLOWWEAVER_TUN_DEBUG=1 打开。
//
// 之所以在 App 层过滤而不是改 core：snishaper-core 是共享库，它的
// logfHandler.Enabled 刻意对所有级别返回 true（"logf 管道本身不过滤"），
// 因为参考产品 SniShaper 从来没有这个开关。FlowWeaver 迁移前有，所以由
// FlowWeaver 在自己的边界上重新施加，而不是 fork core。
var tunLogDebugEnabled = os.Getenv("FLOWWEAVER_TUN_DEBUG") == "1"

// shouldEmitTunLog 判断一条 singtun 日志是否应当进入 App 日志管道。
//
// 只丢弃 debug 级（每连接）日志；info/warn/error 必须始终可见，否则隧道
// 出问题时就无从排查。
func shouldEmitTunLog(message string, debugEnabled bool) bool {
	if debugEnabled {
		return true
	}
	return !strings.HasPrefix(message, tunDebugPrefix)
}

// logBufPool provides reusable byte buffers for log formatting
// to reduce memory allocation and GC pressure in high-frequency logging.
var logBufPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 0, 256)
		return &buf
	},
}

type coreRuntime struct {
	mu                sync.RWMutex
	execPath          string
	execDir           string
	certPath          string
	ruleManager       *proxy.RuleManager
	subStore          *subscription.Store
	proxyServer       *proxy.ProxyServer
	nativeTUN         *singtun.Manager
	certManager       *certmanager.CertManager
	logBuffer         *common.RingLogWriter
	logCaptureMu      sync.RWMutex
	logCaptureEnabled bool
	proxyOpMu         sync.Mutex
	tunOpMu           sync.Mutex
	tunStateMu        sync.RWMutex
	tunStarting       bool
	tunStartErr       string
	routeEventsMu     sync.Mutex
	routeEvents       []RouteEvent
	rulesWatchStop    func()
	// ifaces holds the outbound interface cache. TUN start/stop invalidate it,
	// because the interface list only becomes correct once our own adapter
	// exists (or is gone).
	ifaces *netiface.Selector
}

func newCoreRuntime() (*coreRuntime, error) {
	execPath, err := os.Executable()
	if err != nil {
		return nil, err
	}
	execDir := filepath.Dir(execPath)
	settingsPath := common.ConfigSettingsPath(execDir)
	rulesPath := common.ConfigRulesPath(execDir)

	ruleManager := proxy.NewRuleManager(settingsPath, rulesPath,
		proxy.WithLogger(corelog.Std),
		proxy.WithRouterDebug(os.Getenv("FLOWWEAVER_ROUTER_DEBUG") == "1"),
		proxy.WithProductName(ProductName),
	)
	if err := ruleManager.LoadConfig(); err != nil {
		return nil, err
	}

	// The core process serves the actual traffic, so it must honour the active
	// subscription exactly like the GUI does.
	subStore := subscription.NewStore(
		common.ConfigSubscriptionPath(execDir),
		nil,
		subscription.WithProductName(ProductName),
	)
	ruleManager.SetSubscriptionStore(subStore)

	port := ruleManager.GetListenPort()
	if port == "" {
		port = "8080"
	}

	r := &coreRuntime{
		execPath:    execPath,
		execDir:     execDir,
		certPath:    common.ConfigCertDir(execDir),
		ruleManager: ruleManager,
		subStore:    subStore,
		proxyServer: proxy.NewProxyServer("127.0.0.1:"+port,
			proxy.WithLogger(corelog.Std),
			proxy.WithProductName(ProductName),
		),
		ifaces:    netiface.NewSelector(),
		logBuffer: common.NewRingLogWriter(5000),
	}
	// Initialize nativeTUN with DoH resolver
	// 日志经 appendTunLog 过滤：core 不过滤级别，每连接 debug 日志默认在这里丢弃。
	r.nativeTUN = singtun.NewManagerFor(coreconfig.Brand{Name: ProductName},
		r.proxyServer.GetDoHResolver(), r.appendTunLog)
	// TUN UDP traffic must be able to use the bound subscription node.
	r.nativeTUN.SetUDPRelay(r.proxyServer)

	if err := r.start(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *coreRuntime) start() error {
	r.setupLogger()
	var err error
	r.certManager, err = certmanager.InitCertManagerFor(coreconfig.Brand{Name: ProductName}, r.certPath)
	if err != nil {
		r.appendLog("[core] Failed to init cert manager: " + err.Error())
	}
	r.proxyServer.SetRuleManager(r.ruleManager)
	r.proxyServer.SetSubscriptionDialer(r.subStore)
	r.proxyServer.UpdateCloudflareIPPool(r.ruleManager.GetCloudflareConfig().PreferredIPs)
	r.proxyServer.SetCertGenerator(r.certManager)
	r.proxyServer.SetLogCallback(r.appendLog)
	r.ruleManager.InitAutoRouter(r.proxyServer.GetDoHResolver())

	r.ruleManager.SetRouteEventCallback(func(domain, mode string) {
		r.routeEventsMu.Lock()
		defer r.routeEventsMu.Unlock()
		r.routeEvents = append(r.routeEvents, RouteEvent{Domain: domain, Mode: mode})
		if len(r.routeEvents) > 200 {
			// Create new slice to release underlying array memory
			newSlice := make([]RouteEvent, 100)
			copy(newSlice, r.routeEvents[len(r.routeEvents)-100:])
			r.routeEvents = newSlice
		}
	})

	if stop, err := r.ruleManager.WatchRulesFile(func() {
		if err := r.reloadConfig(); err != nil {
			r.appendLog("[core] rules file changed but reload failed: " + err.Error())
			return
		}
		r.appendLog("[core] rules file changed, reloaded automatically")
	}); err != nil {
		r.appendLog("[core] rules file watch unavailable: " + err.Error())
	} else {
		r.rulesWatchStop = stop
	}

	r.appendLog("[core] runtime ready")
	return nil
}

func (r *coreRuntime) shutdown() {
	if r.rulesWatchStop != nil {
		r.rulesWatchStop()
		r.rulesWatchStop = nil
	}
	if r.nativeTUN != nil {
		//进程退出路径用 Shutdown：这是唯一允许终态关闭 Handler 的地方，
		// 可确保 TUN 停止后残留派发的连接也无法再生效。
		_ = r.nativeTUN.Shutdown()
	}
	_ = r.proxyServer.Stop()
	r.appendLog("[core] runtime stopped")
}

func (r *coreRuntime) reloadConfig() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.ruleManager.LoadConfig(); err != nil {
		return err
	}
	// subscriptions.json is written by the GUI process (activate / switch /
	// node selection). The file watcher only covers the rules file, so the
	// subscription store must be re-read here explicitly — otherwise the core
	// keeps routing with the stale subscription state forever.
	if r.subStore != nil {
		if err := r.subStore.Reload(); err != nil {
			r.appendLog("[core] subscription store reload failed: " + err.Error())
		}
		// SetSubscriptionStore re-applies the active subscription's rules on
		// the freshly loaded rule set (and releases stale WG dialers).
		r.ruleManager.SetSubscriptionStore(r.subStore)
	}
	port := r.ruleManager.GetListenPort()
	if port == "" {
		port = "8080"
	}
	if err := r.proxyServer.SetListenAddr("127.0.0.1:" + port); err != nil {
		return err
	}
	r.proxyServer.SetRuleManager(r.ruleManager)
	r.proxyServer.SetSubscriptionDialer(r.subStore)
	r.proxyServer.UpdateCloudflareIPPool(r.ruleManager.GetCloudflareConfig().PreferredIPs)
	r.proxyServer.SetCertGenerator(r.certManager)
	r.ruleManager.InitAutoRouter(r.proxyServer.GetDoHResolver())
	// nativeTUN doesn't need restart on config reload
	r.appendLog("[core] config reloaded")
	return nil
}

func (r *coreRuntime) reloadCertificate() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	cm, err := certmanager.InitCertManagerFor(coreconfig.Brand{Name: ProductName}, r.certPath)
	if err != nil {
		return err
	}
	r.certManager = cm
	r.proxyServer.SetCertGenerator(r.certManager)
	r.proxyServer.ClearCertCache()
	r.appendLog("[core] certificate reloaded")
	return nil
}

func (r *coreRuntime) setupLogger() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetOutput(io.MultiWriter(r.logBuffer, os.Stdout))
}

func (r *coreRuntime) appendLog(message string) {
	if r.logBuffer == nil {
		r.logBuffer = common.NewRingLogWriter(500)
	}
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return
	}

	// Get buffer from pool to reduce allocation
	buf := logBufPool.Get().(*[]byte)
	*buf = (*buf)[:0] // Reset buffer

	// Write timestamp directly to buffer
	*buf = time.Now().AppendFormat(*buf, "2006/01/02 15:04:05.000000")
	*buf = append(*buf, ' ')
	*buf = append(*buf, trimmed...)
	if !strings.HasSuffix(trimmed, "\n") {
		*buf = append(*buf, '\n')
	}

	_, _ = r.logBuffer.Write(*buf)
	logBufPool.Put(buf) // Return to pool
}

// appendTunLog 是注入给 singtun.NewManagerFor 的日志回调。
//
// 它在 appendLog 之前施加级别过滤：core 的 logf 管道对所有级别都放行，
// 每连接 debug 日志必须由 FlowWeaver 自己按 FLOWWEAVER_TUN_DEBUG 决定去留。
func (r *coreRuntime) appendTunLog(message string) {
	if !shouldEmitTunLog(message, tunLogDebugEnabled) {
		return
	}
	r.appendLog(message)
}

func (r *coreRuntime) isLogCaptureEnabled() bool {
	r.logCaptureMu.RLock()
	defer r.logCaptureMu.RUnlock()
	return r.logCaptureEnabled
}

func (r *coreRuntime) startLogCapture() {
	r.logCaptureMu.Lock()
	r.logCaptureEnabled = true
	r.logCaptureMu.Unlock()
	if r.logBuffer != nil {
		r.logBuffer.Clear()
	}
	r.appendLog("[core] log capture started")
}

func (r *coreRuntime) stopLogCapture() {
	r.appendLog("[core] log capture stopping")
	r.logCaptureMu.Lock()
	r.logCaptureEnabled = false
	r.logCaptureMu.Unlock()
}

func (r *coreRuntime) recentLogs(limit int) string {
	if r.logBuffer == nil {
		return ""
	}
	return strings.Join(r.logBuffer.Snapshot(limit), "\n")
}

func (r *coreRuntime) clearLogs() {
	if r.logBuffer != nil {
		r.logBuffer.Clear()
	}
	r.appendLog("[core] logs cleared")
}

func (r *coreRuntime) startProxy() error {
	r.proxyOpMu.Lock()
	defer r.proxyOpMu.Unlock()

	if r.proxyServer.IsRunning() {
		return nil
	}

	originalPort := r.getListenPort()
	if originalPort == 0 {
		originalPort = 8080
	}
	availablePort, err := proxy.EnsurePortAvailable(originalPort, []string{"flowweaver", "usque"})
	if err != nil {
		availablePort = originalPort
	}
	if availablePort != originalPort {
		if err := r.setListenPort(availablePort); err != nil {
			return err
		}
	}
	if err := r.proxyServer.Start(); err != nil {
		return err
	}
	addr := r.proxyServer.GetListenAddr()
	if err := waitForListen(addr, 2*time.Second); err != nil {
		_ = r.proxyServer.Stop()
		return fmt.Errorf("proxy started but not listening on %s: %w", addr, err)
	}
	r.appendLog("[core] proxy started")
	return nil
}

func (r *coreRuntime) stopProxy() error {
	r.proxyOpMu.Lock()
	defer r.proxyOpMu.Unlock()
	var errs []error
	if err := r.proxyServer.Stop(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return errorsJoin(errs...)
	}
	r.appendLog("[core] proxy stopped")
	return nil
}

func (r *coreRuntime) startTUN() (err error) {
	// 串行化 start/stop：core_api 的 TUN 请求都在各自的 goroutine 里执行，
	// 没有互斥时连点会并发进入 singtun.Manager 的锁，表现为界面长时间无响应。
	r.tunOpMu.Lock()
	defer r.tunOpMu.Unlock()

	r.setTUNStartState(true, nil)
	defer func() {
		r.setTUNStartState(false, err)
	}()

	if !isProcessElevated() {
		err = fmt.Errorf("TUN requires administrator privileges on Windows; please restart FlowWeaver as administrator")
		return err
	}
	if !r.proxyServer.IsRunning() {
		r.appendLog("[core] proxy not running, starting proxy before TUN")
		if err = r.startProxy(); err != nil {
			return fmt.Errorf("start proxy before TUN: %w", err)
		}
	}
	if r.nativeTUN == nil {
		err = fmt.Errorf("native TUN manager is not initialized")
		return err
	}
	listenPort := r.currentListenPort()
	if listenPort == "" {
		err = fmt.Errorf("proxy listen port is empty")
		return err
	}
	tunCfg := r.ruleManager.GetTUNConfig()
	r.appendLog("[core] startTUN: calling nativeTUN.Start with proxy=" + "127.0.0.1:" + listenPort)
	proxyAddr := "127.0.0.1:" + listenPort
	if err = r.nativeTUN.Start(tunCfg, proxyAddr); err != nil {
		r.appendLog("[core] startTUN: nativeTUN.Start failed: " + err.Error())
		return err
	}
	r.appendLog("[core] native sing-tun started, status=" + fmt.Sprintf("%v", r.nativeTUN.Status().Running))
	// 通知 ProxyServer 启用 TUN 模式，出站连接绑物理网卡。
	// 出站网卡在 TUN 启动之后才配置：此时网卡列表已包含 FlowWeaver 自己的虚拟
	// 网卡，必须让 netiface 重新扫描，否则可能把 TUN 自己选成出站网卡。
	// 注意 SetTUNMode 必须在 Start 成功之后：Start 失败时 TUN 并不存在，
	// 此时打开 TUN 模式会让所有出站连接去找一张并不存在的网卡。
	r.proxyServer.SetTUNMode(true)
	r.ifaces.Invalidate()
	if binding, selErr := r.ifaces.Select(netiface.FamilyIPv4, tunCfg.InterfaceConfigFor(coreconfig.Brand{Name: ProductName}), r.appendLog); selErr == nil {
		r.appendLog("[core] TUN outbound interface: " + binding.Describe())
	}
	// TUN 数据面自检：通过 TUN 发送 DNS 查询，验证 gvisor 栈正常工作。
	// 解决 gvisor 数据面静默失效时（网卡存在但流量不通）无任何错误日志的问题。
	if err := verifyTUNDataPlane(singtun.TUNGateway4(), 2*time.Second); err != nil {
		r.appendLog("[error] TUN data plane check failed: " + err.Error())
	} else {
		r.appendLog("[core] TUN data plane check passed")
	}
	return nil
}

// verifyTUNDataPlane 通过 TUN 接口发送 DNS 查询（绑定 TUN 自身地址作为源），
// 验证 gvisor 数据面是否正常。查询会被 gvisor 劫持并由 Handler 返回 fake-ip 响应。
func verifyTUNDataPlane(tunIP string, timeout time.Duration) error {
	msg := new(dns.Msg)
	msg.Id = dns.Id()
	msg.RecursionDesired = true
	msg.Question = []dns.Question{{Name: "example.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET}}
	query, err := msg.Pack()
	if err != nil {
		return err
	}

	laddr, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(tunIP, "0"))
	if err != nil {
		return err
	}
	conn, err := net.DialUDP("udp4", laddr, &net.UDPAddr{IP: net.ParseIP("1.1.1.1"), Port: 53})
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.Write(query); err != nil {
		return err
	}

	deadline := time.Now().Add(timeout)
	buf := make([]byte, 512)
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(deadline)
		n, err := conn.Read(buf)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				break
			}
			return err
		}
		resp := new(dns.Msg)
		if err := resp.Unpack(buf[:n]); err != nil || resp.Id != msg.Id {
			continue
		}
		return nil
	}
	return fmt.Errorf("no DNS response through TUN within %v (data plane may be down)", timeout)
}

func (r *coreRuntime) stopTUN() error {
	// 与 startTUN 共用一把锁，保证同一时刻只有一个 TUN 生命周期操作在跑。
	r.tunOpMu.Lock()
	defer r.tunOpMu.Unlock()

	r.setTUNStartState(false, nil)
	if r.nativeTUN == nil {
		return fmt.Errorf("native TUN manager is not initialized")
	}
	// 顺序要求：必须先停 TUN 再退出 TUN 模式。
	// 反过来的话，设备还在、默认路由还指向隧道，但出站 socket 已经不再绑物理网卡，
	// 这段时间内所有出站流量都会被 TUN 重新捕获，形成自激回环。
	if err := r.nativeTUN.Stop(); err != nil {
		return err
	}
	// 通知 ProxyServer 退出 TUN 模式
	r.proxyServer.SetTUNMode(false)
	// TUN 网卡已消失，重新扫描网卡列表，避免下次出站仍引用已释放的接口索引。
	r.ifaces.Invalidate()
	r.appendLog("[core] native sing-tun stopped")
	return nil
}

func (r *coreRuntime) getTUNStatus() proxy.TUNStatus {
	var status proxy.TUNStatus
	if r.nativeTUN != nil {
		status = r.nativeTUN.Status()
	} else {
		status = proxy.TUNStatus{
			Supported: false,
			Enabled:   false,
			Running:   false,
			Message:   "Native TUN is not initialized",
		}
	}

	r.tunStateMu.RLock()
	starting := r.tunStarting
	startErr := strings.TrimSpace(r.tunStartErr)
	r.tunStateMu.RUnlock()

	if status.Running {
		status.Enabled = true
		return status
	}
	status.Enabled = false
	if starting {
		return status
	}
	if startErr != "" {
		status.Message = startErr
	}
	return status
}

func (r *coreRuntime) setTUNStartState(starting bool, err error) {
	r.tunStateMu.Lock()
	defer r.tunStateMu.Unlock()
	r.tunStarting = starting
	if starting {
		r.tunStartErr = ""
		return
	}
	if err != nil {
		r.tunStartErr = strings.TrimSpace(err.Error())
		return
	}
	r.tunStartErr = ""
}

func (r *coreRuntime) failTUNStart(err error) {
	r.setTUNStartState(false, err)
	if err != nil {
		r.appendLog("[core] TUN panic: " + err.Error())
		r.appendLog(string(debug.Stack()))
	}
}

func (r *coreRuntime) getListenPort() int {
	addr := r.proxyServer.GetListenAddr()
	var port int
	fmt.Sscanf(addr, "127.0.0.1:%d", &port)
	return port
}

func (r *coreRuntime) currentListenPort() string {
	addr := strings.TrimSpace(r.proxyServer.GetListenAddr())
	if addr != "" {
		if _, port, err := net.SplitHostPort(addr); err == nil && strings.TrimSpace(port) != "" {
			return strings.TrimSpace(port)
		}
	}
	return strings.TrimSpace(r.ruleManager.GetListenPort())
}

func (r *coreRuntime) setListenPort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid port number: %d", port)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	if err := r.proxyServer.SetListenAddr(addr); err != nil {
		return err
	}
	r.ruleManager.SetListenPort(fmt.Sprintf("%d", port))
	return r.ruleManager.SaveConfig()
}

func waitForListen(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("timeout")
	}
	return lastErr
}

func errorsJoin(errs ...error) error {
	var filtered []error
	for _, err := range errs {
		if err != nil {
			filtered = append(filtered, err)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	if len(filtered) == 1 {
		return filtered[0]
	}
	msgs := make([]string, 0, len(filtered))
	for _, err := range filtered {
		msgs = append(msgs, err.Error())
	}
	return errors.New(strings.Join(msgs, "; "))
}

func (r *coreRuntime) popRouteEvents() []RouteEvent {
	r.routeEventsMu.Lock()
	defer r.routeEventsMu.Unlock()
	if len(r.routeEvents) == 0 {
		return nil
	}
	events := r.routeEvents
	r.routeEvents = nil
	return events
}
