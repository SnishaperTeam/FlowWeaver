package app

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"flowweaver/pkg/subscription"
)

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

// AddSubscription downloads and registers a Clash subscription.
func (a *App) AddSubscription(name, url string) (SubscriptionInfo, error) {
	entry, err := a.ruleManager.AddSubscription(name, url)
	if err != nil {
		return SubscriptionInfo{}, err
	}
	store := a.ruleManager.GetSubscriptionStore()
	return toSubscriptionInfo(entry, store.ActiveID()), nil
}

// UpdateSubscription re-downloads a subscription.
func (a *App) UpdateSubscription(id string) (SubscriptionInfo, error) {
	entry, err := a.ruleManager.UpdateSubscription(id)
	if err != nil {
		return SubscriptionInfo{}, err
	}
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
	a.emitEvent("app:rules_changed", map[string]interface{}{"source": "subscription"})
	a.emitFrontendState()
	return nil
}

// SelectSubscriptionNode stores the selected node for a subscription group.
func (a *App) SelectSubscriptionNode(subscriptionID, group, node string) error {
	return a.ruleManager.SelectSubscriptionNode(subscriptionID, group, node)
}

// GetActiveSubscriptionID returns the active subscription id.
func (a *App) GetActiveSubscriptionID() string {
	store := a.ruleManager.GetSubscriptionStore()
	if store == nil {
		return subscription.DefaultSubscriptionID
	}
	return store.ActiveID()
}

// TestSubscriptionNode measures the latency of one node by dialing its server.
func (a *App) TestSubscriptionNode(server string, port int) (int, error) {
	target := net.JoinHostPort(strings.TrimSpace(server), strconv.Itoa(port))
	if strings.TrimSpace(server) == "" || port <= 0 {
		return 0, errors.New("invalid node address")
	}

	start := time.Now()
	conn, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		return 0, err
	}
	_ = conn.Close()
	return int(time.Since(start).Milliseconds()), nil
}
