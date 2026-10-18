package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/SnishaperTeam/snishaper-core/subscription"
)

const nodeTestTimeout = 5 * time.Second
const serviceTestTimeout = 5 * time.Second

// SubscriptionInfo is the frontend-facing shape of a subscription.
type SubscriptionInfo struct {
	ID         string                        `json:"id"`
	Name       string                        `json:"name"`
	URL        string                        `json:"url"`
	Builtin    bool                          `json:"builtin"`
	Active     bool                          `json:"active"`
	UpdatedAt  int64                         `json:"updated_at"`
	Upload     int64                         `json:"upload"`
	Download   int64                         `json:"download"`
	Total      int64                         `json:"total"`
	Left       int64                         `json:"left"`
	ExpireTime int64                         `json:"expire_time"`
	NodeCount  int                           `json:"node_count"`
	RuleCount  int                           `json:"rule_count"`
	Groups     []subscription.Group          `json:"groups"`
	Nodes      []SubscriptionNodeInfo        `json:"nodes"`
	Selection  map[string]string             `json:"selection"`
	Error      string                        `json:"error"`
}

type SubscriptionNodeInfo struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Server   string            `json:"server"`
	Port     int               `json:"port"`
	ServerName string          `json:"server_name,omitempty"`
	UDP      bool              `json:"udp"`
	Options  map[string]string `json:"options,omitempty"`
	// Supported reports whether the node's protocol can actually be dialled.
	// Unsupported protocols still show in the list but cannot carry traffic.
	Supported bool `json:"supported"`
	// UDPSupported reports whether the node can relay UDP as well as TCP.
	UDPSupported bool `json:"udp_supported"`
}

func toSubscriptionInfo(entry *subscription.Entry, activeID string) SubscriptionInfo {
	info := SubscriptionInfo{
		ID:         entry.ID,
		Name:       entry.Name,
		URL:        entry.URL,
		Builtin:    entry.Builtin,
		Active:     entry.ID == activeID,
		UpdatedAt:  entry.UpdatedAt,
		Upload:     entry.UserInfo.Upload,
		Download:   entry.UserInfo.Download,
		Total:      entry.UserInfo.Total,
		Left:       entry.UserInfo.Left(),
		ExpireTime: entry.UserInfo.ExpireTime,
		NodeCount:  entry.NodeCount,
		RuleCount:  entry.RuleCount,
		Groups:     entry.Groups,
		Nodes:      toNodeInfos(entry.Nodes),
		Selection:  entry.Selection,
		Error:      entry.Error,
	}
	if info.Groups == nil {
		info.Groups = []subscription.Group{}
	}
	if info.Nodes == nil {
		info.Nodes = []SubscriptionNodeInfo{}
	}
	if info.Selection == nil {
		info.Selection = map[string]string{}
	}
	return info
}

func toNodeInfos(nodes []subscription.Node) []SubscriptionNodeInfo {
	out := make([]SubscriptionNodeInfo, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, SubscriptionNodeInfo{
			Name:       n.Name,
			Type:       n.Type,
			Server:     n.Server,
			Port:       n.Port,
			ServerName: n.ServerName,
			UDP:        n.UDP,
			Options:      n.Options,
			Supported:    n.Supported(),
			UDPSupported: n.UDPSupported(),
		})
	}
	return out
}

// GetSubscriptions returns all subscriptions including the built-in default.
func (a *App) GetSubscriptions() []SubscriptionInfo {
	store := a.ruleManager.GetSubscriptionStore()
	if store == nil {
		return nil
	}
	activeID := store.ActiveID()
	entries := store.List()
	out := make([]SubscriptionInfo, 0, len(entries))
	for _, e := range entries {
		out = append(out, toSubscriptionInfo(e, activeID))
	}
	return out
}

// syncFetchProxy points subscription downloads at the running local proxy so
// subscription domains that are unreachable directly still update (Clash
// updates through its own running proxy the same way). Direct stays as the
// fallback inside Fetch.
func (a *App) syncFetchProxy() {
	store := a.ruleManager.GetSubscriptionStore()
	if store == nil {
		return
	}
	if a.IsProxyRunning() {
		store.SetFetchProxy(fmt.Sprintf("http://127.0.0.1:%d", a.GetListenPort()))
	} else {
		store.SetFetchProxy("")
	}
}

// syncSubscriptionToCore pushes subscription state changes to the core
// process. The core's file watcher covers only the rules file, while
// activate / switch / node-selection write subscriptions.json — without an
// explicit reload the core keeps routing with the stale subscription state.
// The in-process proxy shares this process's rule manager and needs no push.
func (a *App) syncSubscriptionToCore() {
	if a.core != nil {
		a.core.ReloadIfRunning()
	}
}

// AddSubscription downloads and registers a Clash subscription.
func (a *App) AddSubscription(name, url string) (SubscriptionInfo, error) {
	a.syncFetchProxy()
	entry, err := a.ruleManager.AddSubscription(name, url)
	if err != nil {
		return SubscriptionInfo{}, err
	}
	a.syncSubscriptionToCore()
	store := a.ruleManager.GetSubscriptionStore()
	return toSubscriptionInfo(entry, store.ActiveID()), nil
}

// UpdateSubscription re-downloads a subscription.
func (a *App) UpdateSubscription(id string) (SubscriptionInfo, error) {
	a.syncFetchProxy()
	entry, err := a.ruleManager.UpdateSubscription(id)
	if err != nil {
		return SubscriptionInfo{}, err
	}
	a.syncSubscriptionToCore()
	store := a.ruleManager.GetSubscriptionStore()
	return toSubscriptionInfo(entry, store.ActiveID()), nil
}

// DeleteSubscription removes a subscription.
func (a *App) DeleteSubscription(id string) error {
	store := a.ruleManager.GetSubscriptionStore()
	wasActive := store != nil && store.ActiveID() == id

	if err := a.ruleManager.DeleteSubscription(id); err != nil {
		return err
	}
	a.syncSubscriptionToCore()
	if wasActive {
		a.emitEvent("app:rules_changed", map[string]interface{}{"source": "subscription"})
		a.emitFrontendState()
	}
	return nil
}

// ActivateSubscription switches the active subscription and reloads rules.
func (a *App) ActivateSubscription(id string) error {
	if err := a.ruleManager.ActivateSubscription(id); err != nil {
		return err
	}
	a.appendLog("Subscription activated: " + id)
	a.syncSubscriptionToCore()
	a.emitEvent("app:rules_changed", map[string]interface{}{"source": "subscription"})
	a.emitFrontendState()
	return nil
}

// SelectSubscriptionNode stores the selected node for a subscription group.
func (a *App) SelectSubscriptionNode(subscriptionID, group, node string) error {
	if err := a.ruleManager.SelectSubscriptionNode(subscriptionID, group, node); err != nil {
		return err
	}
	a.syncSubscriptionToCore()
	return nil
}

// GetActiveSubscriptionID returns the active subscription id.
func (a *App) GetActiveSubscriptionID() string {
	store := a.ruleManager.GetSubscriptionStore()
	if store == nil {
		return subscription.DefaultSubscriptionID
	}
	return store.ActiveID()
}

// nodeTestURL is the Clash Verge default URL-test target: a 204 endpoint that
// measures pure connectivity without downloading a body.
const nodeTestURL = "https://www.gstatic.com/generate_204"

// TestSubscriptionNodeByID measures the URL-test delay of one subscription
// node the way Clash Verge does: the request to the test URL leaves through
// the node itself (the node's server resolves the domain), so the number is
// the real usable latency — server dial + tunnel + TLS + response — instead
// of a bare TCP handshake to the server address.
func (a *App) TestSubscriptionNodeByID(subscriptionID, nodeName string) (int, error) {
	store := a.ruleManager.GetSubscriptionStore()
	if store == nil {
		return 0, errors.New("subscription store is not initialized")
	}
	node, ok := store.NodeByID(strings.TrimSpace(subscriptionID), strings.TrimSpace(nodeName))
	if !ok {
		return 0, fmt.Errorf("node %q not found in subscription %q", nodeName, subscriptionID)
	}

	// Subscription nodes carry their raw Clash config and run the core's own
	// URL test (adapter.Proxy.URLTest — the exact code behind mihomo's
	// /proxies/:name/delay endpoint that Clash Verge calls), so protocol and
	// transport handling match Clash instead of the legacy hand-rolled
	// dialers, which time out on modern node configs (vmess ws, REALITY...).
	if node.Raw != nil {
		ctx, cancel := context.WithTimeout(context.Background(), nodeTestTimeout)
		defer cancel()
		delay, err := node.URLTest(ctx, nodeTestURL)
		if err != nil {
			return 0, err
		}
		return int(delay), nil
	}

	// Legacy path for nodes without raw config (hand-constructed entries).
	client := subscription.NodeHTTPClient(node, nodeTestTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), nodeTestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nodeTestURL, nil)
	if err != nil {
		return 0, err
	}

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := int(time.Since(start).Milliseconds())
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return 0, fmt.Errorf("timeout: %w", err)
		}
		return 0, err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("http status %d", resp.StatusCode)
	}
	return elapsed, nil
}

// TestServiceLatency measures how long one HTTPS service takes to answer,
// with Clash Verge semantics: the request leaves through the currently
// selected subscription node when one applies (the node's server resolves
// the domain, so polluted local DNS cannot skew the number), then falls back
// to the running local proxy and finally to a direct request.
func (a *App) TestServiceLatency(serviceURL string) (int64, error) {
	u, err := url.Parse(strings.TrimSpace(serviceURL))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return 0, errors.New("invalid service url, https:// required")
	}

	clients := make([]*http.Client, 0, 3)
	if store := a.ruleManager.GetSubscriptionStore(); store != nil {
		if node, ok := store.ActiveNode(); ok {
			clients = append(clients, subscription.NodeHTTPClient(node, serviceTestTimeout))
		}
	}
	if a.IsProxyRunning() {
		proxyURL, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", a.GetListenPort()))
		if err != nil {
			return 0, err
		}
		clients = append(clients, &http.Client{
			Timeout: serviceTestTimeout,
			Transport: &http.Transport{
				Proxy:           http.ProxyURL(proxyURL),
				TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			},
		})
	}
	clients = append(clients, &http.Client{
		Timeout: serviceTestTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	})

	var errs []error
	for _, client := range clients {
		elapsed, err := probeService(client, u)
		if err == nil {
			return elapsed, nil
		}
		errs = append(errs, err)
	}
	err = errors.Join(errs...)
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return 0, fmt.Errorf("timeout: %w", err)
	}
	return 0, err
}

// probeService runs one GET and reports the time to the response headers.
// Any HTTP response proves the exit path works, so status codes are not
// treated as failures (a 403 from a bot-walled service still measures the
// path latency); only transport errors fall through to the next client.
func probeService(client *http.Client, u *url.URL) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), serviceTestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, err
	}

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return elapsed, nil
}
