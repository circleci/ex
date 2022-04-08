package httpclient_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fwdProxy struct {
	URL        string
	ProxiedURL string
}

func startFwdProxy(t *testing.T) *fwdProxy {
	p := &fwdProxy{}
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodConnect { // tunnelling not supported
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			} else {
				p.handleHTTP(w, r)
			}
		}))
	p.URL = server.URL
	t.Cleanup(server.Close)
	return p
}

func (p *fwdProxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	p.ProxiedURL = r.URL.String()

	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = localhostDialler()
	t.Proxy = nil

	resp, err := t.RoundTrip(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()
	for k, vv := range w.Header() {
		for _, v := range vv {
			resp.Header.Add(k, v)
		}
	}

	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
