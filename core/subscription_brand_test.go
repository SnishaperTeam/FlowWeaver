package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SnishaperTeam/snishaper-core/subscription"
)

// 订阅下载的 User-Agent 里必须出现 FlowWeaver，而不是 core 的参考产品名。
//
// 这条用例跨模块边界打真实 HTTP 请求：core 若把产品名写死（或丢掉
// WithProductName 的处理），这里就会失败。订阅源按 User-Agent 分流的场景
// 下，UA 写错会把 FlowWeaver 的流量记到别的产品名下。
func TestSubscriptionUserAgentCarriesProductName(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("proxies: []\n"))
	}))
	defer srv.Close()

	if _, _, err := subscription.Fetch(srv.URL, "", subscription.WithProductName(ProductName)); err != nil {
		t.Fatalf("fetch: %v", err)
	}

	const want = "clash-verge/v2.0 " + ProductName
	if got != want {
		t.Errorf("subscription User-Agent = %q, want %q", got, want)
	}
}
