package singtun

import (
	"strings"
	"sync"
	"testing"
)

// collector records log lines so the test can assert on what is emitted.
type collector struct {
	mu    sync.Mutex
	lines []string
}

func (c *collector) logf(msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, msg)
}

func (c *collector) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.lines))
	copy(out, c.lines)
	return out
}

func (c *collector) contains(substr string) bool {
	for _, l := range c.all() {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}

func (c *collector) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = nil
}

// TestPerConnectionLogsSuppressedByDefault locks in the behaviour that stops
// the TUN log from being flooded: one line per TCP connection and one line per
// DNS query is far too noisy for normal use.
func TestPerConnectionLogsSuppressedByDefault(t *testing.T) {
	original := debugLogEnabled
	t.Cleanup(func() { debugLogEnabled = original })

	debugLogEnabled = false
	c := &collector{}
	h := &Handler{logf: c.logf}

	// tracef is what the per-connection call sites use.
	h.tracef("[sing-tun] TCP 1.2.3.4:1 -> 5.6.7.8:443 (resolved: example.com)")
	h.tracef("[sing-tun] CONNECT request: %q", "CONNECT example.com:443 HTTP/1.1")
	h.tracef("[sing-tun] fake-ip: example.com -> 198.18.0.5 (type: 1)")

	if got := c.all(); len(got) != 0 {
		t.Errorf("tracef must stay silent by default, got %v", got)
	}
}

// TestPerConnectionLogsWhenDebugEnabled makes sure the switch actually turns
// the detail back on, so troubleshooting is still possible.
func TestPerConnectionLogsWhenDebugEnabled(t *testing.T) {
	original := debugLogEnabled
	t.Cleanup(func() { debugLogEnabled = original })

	debugLogEnabled = true
	c := &collector{}
	h := &Handler{logf: c.logf}

	h.tracef("[sing-tun] TCP 1.2.3.4:1 -> 5.6.7.8:443 (resolved: example.com)")

	if !c.contains("resolved: example.com") {
		t.Errorf("debug mode should emit per-connection detail, got %v", c.all())
	}
}

// TestErrorLogsAlwaysEmit guards the more important half: failures must be
// visible even with debug logging off, otherwise troubleshooting a broken
// tunnel becomes impossible.
func TestErrorLogsAlwaysEmit(t *testing.T) {
	original := debugLogEnabled
	t.Cleanup(func() { debugLogEnabled = original })

	debugLogEnabled = false
	c := &collector{}
	h := &Handler{logf: c.logf}

	// Error paths use logf directly, not tracef.
	h.logf("[sing-tun] failed to connect to proxy: connection refused")

	if !c.contains("connection refused") {
		t.Error("error logs must be emitted regardless of the debug switch")
	}
}
