package core

import (
	"os"
	"testing"
)

// 这三个测试从迁移前的 pkg/singtun/logging_test.go 移植而来。原包
// pkg/singtun 已删除、改用 snishaper-core/singtun，但 core 的 logf 管道
// 刻意不对级别过滤（见 singtun/logger.go 的 logfHandler.Enabled），
// 所以每连接日志的抑制逻辑改由 core 包内的 shouldEmitTunLog 承担。

// 迁移前每条 TCP 连接都会打这几行；默认必须静默，否则 TUN 日志会被淹没。
var perConnectionTunLogs = []string{
	tunDebugPrefix + " debug: TCP flow source=1.2.3.4:1 destination=5.6.7.8:443 resolved=example.com",
	tunDebugPrefix + " debug: CONNECT request request=\"CONNECT example.com:443 HTTP/1.1\"",
	tunDebugPrefix + " debug: CONNECT response status=\"HTTP/1.1 200 OK\"",
	tunDebugPrefix + " debug: fake-ip allocated domain=example.com fake_ip=198.18.0.5 qtype=1",
	tunDebugPrefix + " debug: udp relay: idle beyond ttl, releasing session",
	// 目标为 IP 的连接（浏览器很常见）每次都会嗅探 SNI。core 侧已把该行
	// 从 info 降到 debug（见 snishaper-core singtun/handler.go），这里同时
	// 守住前缀契约：一旦 core 把它改回 info，本用例即失败。
	tunDebugPrefix + " SNI sniffed sni=example.com previous=93.184.216.34",
}

// TestTunPerConnectionLogsSuppressedByDefault 锁定"默认不刷屏"这一行为。
func TestTunPerConnectionLogsSuppressedByDefault(t *testing.T) {
	for _, line := range perConnectionTunLogs {
		if shouldEmitTunLog(line, false) {
			t.Errorf("默认必须抑制每连接日志，却放行了 %q", line)
		}
	}
}

// TestTunPerConnectionLogsWhenDebugEnabled 确保开关能真正打开细节，
// 排障时仍然可用。
func TestTunPerConnectionLogsWhenDebugEnabled(t *testing.T) {
	for _, line := range perConnectionTunLogs {
		if !shouldEmitTunLog(line, true) {
			t.Errorf("调试模式下必须放行每连接日志，却丢弃了 %q", line)
		}
	}
}

// TestTunErrorLogsAlwaysEmit 守住更重要的另一半：失败必须始终可见，
// 否则隧道坏掉时无从排查。info 级（TUN 生命周期）同样不能被误伤。
func TestTunErrorLogsAlwaysEmit(t *testing.T) {
	alwaysVisible := []string{
		"[sing-tun] TUN started, running=true",
		"[sing-tun] Handler created proxy=127.0.0.1:18080",
		"[sing-tun:warn] failed to connect to proxy error=connection refused",
		"[sing-tun:warn] fake-ip has no domain mapping addr=198.18.0.5",
		"[sing-tun:error] failed to update interface finder error=boom",
	}
	for _, line := range alwaysVisible {
		if !shouldEmitTunLog(line, false) {
			t.Errorf("debug 关闭时也必须放行 %q", line)
		}
	}
}

// TestTunDebugSwitchDefaultsOff 保证环境变量未设置时默认是关闭的，
// 也就是"默认静默"这一契约不会因为误改默认值而失效。
func TestTunDebugSwitchDefaultsOff(t *testing.T) {
	if os.Getenv("FLOWWEAVER_TUN_DEBUG") == "1" {
		t.Skip("FLOWWEAVER_TUN_DEBUG=1 已在本机环境显式设置，跳过默认值断言")
	}
	if tunLogDebugEnabled {
		t.Fatal("FLOWWEAVER_TUN_DEBUG 未设置时 tunLogDebugEnabled 必须为 false")
	}
}
