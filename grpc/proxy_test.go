package grpc

import (
	"os"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestGoodProxySettings(t *testing.T) {
	// N.B. can not be run in parallel, as we mess with the operational environment
	// related to proxy settings.
	const (
		proxyEnvKey   = "https_proxy"
		noProxyEnvKey = "no_proxy"
		hostName      = "hostname.com"
	)
	// reset all the proxy related env vars (they will be restored on cleanup)
	for _, k := range []string{
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"NO_PROXY",
		"http_proxy",
		proxyEnvKey,
		noProxyEnvKey,
	} {
		t.Setenv(k, "")
	}

	for _, tc := range []struct {
		name     string
		proxy    string
		noProxy  string
		expected bool
	}{
		{
			name:     "no proxy",
			proxy:    "",
			noProxy:  "",
			expected: true,
		},
		{
			name:     "mismatched proxy",
			proxy:    "no-match.com",
			noProxy:  "",
			expected: true,
		},
		{
			name:     "matched proxy",
			proxy:    hostName,
			noProxy:  "",
			expected: true,
		},
		{
			name:     "matched proxy matched no proxy",
			proxy:    hostName,
			noProxy:  hostName,
			expected: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_ = os.Setenv(proxyEnvKey, tc.proxy)
			_ = os.Setenv(noProxyEnvKey, tc.noProxy)
			assert.Check(t, cmp.Equal(tc.expected, goodProxySettings(hostName)))
		})
	}
}
