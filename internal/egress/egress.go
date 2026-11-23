// Package egress is the allow-list HTTP proxy that sandboxed pipeline containers reach the
// internet through.
package egress

import (
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// NewProxy returns the allow-list proxy a pipeline container reaches the internet
// through (docs/features/07 "Sandbox", 16): HTTP CONNECT (HTTPS) and plain HTTP, only to
// the hosts the listing's declared upstreams need. Everything else gets 403. The
// container sits on an --internal network whose only way out is this proxy, so the
// allow-list is enforced, not advisory.
func NewProxy(allow []string) http.Handler {
	allowed := map[string]bool{}
	for _, h := range allow {
		allowed[strings.ToLower(strings.TrimSpace(h))] = true
	}
	permit := func(hostport string) bool {
		host := hostport
		if h, _, err := net.SplitHostPort(hostport); err == nil {
			host = h
		}
		return allowed[strings.ToLower(host)]
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			if !permit(r.Host) {
				log.Printf("[egress] denied CONNECT %s", r.Host)
				http.Error(w, "host not allowed", http.StatusForbidden)
				return
			}
			upstream, err := net.DialTimeout("tcp", r.Host, 15*time.Second)
			if err != nil {
				http.Error(w, "upstream unreachable", http.StatusBadGateway)
				return
			}
			hj, ok := w.(http.Hijacker)
			if !ok {
				upstream.Close()
				http.Error(w, "no hijack", http.StatusInternalServerError)
				return
			}
			client, _, err := hj.Hijack()
			if err != nil {
				upstream.Close()
				return
			}
			_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
			go pipe(upstream, client)
			go pipe(client, upstream)
			return
		}
		if r.URL.Host == "" || !permit(r.URL.Host) {
			log.Printf("[egress] denied %s %s", r.Method, r.URL.Host)
			http.Error(w, "host not allowed", http.StatusForbidden)
			return
		}
		out, err := http.NewRequest(r.Method, r.URL.String(), r.Body)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		out.Header = r.Header.Clone()
		out.Header.Del("Proxy-Connection")
		resp, err := http.DefaultTransport.RoundTrip(out)
		if err != nil {
			http.Error(w, "upstream unreachable", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})
}

func pipe(dst io.WriteCloser, src io.ReadCloser) {
	defer dst.Close()
	defer src.Close()
	_, _ = io.Copy(dst, src)
}
