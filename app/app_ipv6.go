package app

import (
	"fmt"
	"net"
	"time"
)

// checkIPv6Available reports whether any up interface carries a usable IPv6
// address beyond link-local/loopback. ULA (fc00::/7) addresses count: many
// ISPs and routers (NAT66 / NPTv6 / NAT64 deployments) provide IPv6
// connectivity through ULA prefixes, so an interface with only ULA is still
// an IPv6-enabled network — it must not be reported as IPv4-only. Whether
// global addresses are actually reachable is a runtime concern handled by the
// dialer (orderIPsByDNSMode), not by this capability check.
func (a *App) checkIPv6Available() bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP
			if ip.To4() == nil && !ip.IsLinkLocalUnicast() && !ip.IsLoopback() && !ip.IsUnspecified() {
				return true
			}
		}
	}
	return false
}

func (a *App) cachedIPv6Available(maxAge time.Duration) bool {
	a.ipv6Mu.Lock()
	defer a.ipv6Mu.Unlock()
	if !a.ipv6At.IsZero() && time.Since(a.ipv6At) < maxAge {
		return a.ipv6Value
	}
	value := a.checkIPv6Available()
	a.ipv6Value = value
	a.ipv6At = time.Now()
	return value
}

// GetIPv6Available returns whether the current network has usable IPv6.
func (a *App) GetIPv6Available() bool {
	return a.cachedIPv6Available(time.Minute)
}

// RefreshIPv6Check re-checks IPv6 availability, notifies the frontend and
// returns the current result.
func (a *App) RefreshIPv6Check() bool {
	available := a.checkIPv6Available()
	a.ipv6Mu.Lock()
	a.ipv6Value = available
	a.ipv6At = time.Now()
	a.ipv6Mu.Unlock()
	a.appendLog(fmt.Sprintf("[network] IPv6 availability check: %v", available))
	a.emitFrontendState()
	return available
}

// startIPv6Monitor re-checks IPv6 availability every 10 minutes while the
// app runs. Runs until shutdown; use RefreshIPv6Check from Settings for an
// immediate re-check.
func (a *App) startIPv6Monitor() {
	a.runSafeAsync("ipv6 monitor", func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				a.RefreshIPv6Check()
			case <-a.ctx.Done():
				return
			}
		}
	})
}
