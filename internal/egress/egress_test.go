package egress

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The egress proxy lets CONNECT through to allowed hosts only.
func TestEgressProxyAllowsOnlyListedHosts(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	defer target.Close()
	targetHost := strings.TrimPrefix(target.URL, "http://")

	allowed := httptest.NewServer(NewProxy([]string{"127.0.0.1"}))
	defer allowed.Close()
	denied := httptest.NewServer(NewProxy([]string{"api.anthropic.com"}))
	defer denied.Close()

	if code := connectVia(t, allowed.URL, targetHost); code != 200 {
		t.Fatalf("allowed CONNECT answered %d", code)
	}
	if code := connectVia(t, denied.URL, targetHost); code != 403 {
		t.Fatalf("denied CONNECT answered %d", code)
	}
	// Plain HTTP through the proxy obeys the same list.
	proxyURL, _ := url.Parse(denied.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	resp, err := client.Get(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("plain HTTP to a denied host answered %d", resp.StatusCode)
	}
	proxyURL, _ = url.Parse(allowed.URL)
	client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	resp, err = client.Get(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "ok" {
		t.Fatalf("allowed plain HTTP got %q", body)
	}
}

func connectVia(t *testing.T, proxy, host string) int {
	u, _ := url.Parse(proxy)
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", host, host)
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode
}
