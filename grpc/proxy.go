package grpc

import (
	"net/url"
	"strings"

	"golang.org/x/net/http/httpproxy"
)

const dnsSchemeNoAuthority = "dns:///"

// ProxyProofTarget takes the host and if there is no proxy in place in the environment at all or
// if there is a proxy, but we should still use it for the given url, the returned host will include
// the dns:// prefix.
//
// Note if this is used to avoid traffic going via a proxy then DNS based load balancing is disabled.
//
// A Note On Proxies.
//
// NO_PROXY domain names can't be honored with dns scheme because of https://github.com/grpc/grpc-go/issues/3401
//
// The following doc... https://github.com/grpc/grpc/blob/master/doc/naming.md suggests that if no scheme prefix
// is used then the dns is used automatically, but this is a bit misleading.
//
// The dns scheme does a lookup for any SRV and multiple A records amongst other things, to set up a list of servers.
// At this point the server addresses are all in the form ip:port. The thing is - nothing is actually connected at
// this point, connections are made lazily. Using the dns scheme supports client side per request load balancing.
//
// If the dns scheme is not used then in fact go grpc uses a 'passthrough' scheme, which defers name resolution to the
// standard (dns) resolver at connection (dial) time passing through the server name, resolving to one IP.
// This obviously does not support client side per call balancing.
//
// The detection of a proxy only happens during dialing, and it is the proxy environment variables that indicate
// whether a proxy should be used.
//
// In the 'passthrough' case because we have the full domain during dialing the client can compare this with the list
// of domains in the NO_PROXY list. However, in the full dns scheme case we only have a list of IPs which don't match
// anything in the NO_PROXY list - so they are duly proxied.
// host should be the host part not including scheme, or have the dns scheme in which case the authority should not
//be present
func ProxyProofTarget(host string) string {
	strippedHost := strings.TrimPrefix(host, dnsSchemeNoAuthority)
	// We only check proxy safety for hosts that have asked for dns load balancing
	if strippedHost == host {
		return host
	}
	if goodProxySettings(strippedHost) {
		return dnsSchemeNoAuthority + strippedHost
	}
	return strippedHost
}

// goodProxySettings returns true if there is no proxy in place in the environment at all or
// if there is a proxy, and we should still use it for the given url. This is needed to
// solve CIRCLE-25181.
func goodProxySettings(host string) bool {
	url, _ := url.Parse("https://" + host)

	conf := httpproxy.FromEnvironment()
	proxy, _ := conf.ProxyFunc()(url)
	// if the environment suggests we should proxy this url then we can use the dns scheme
	// as the ip's will be proxied.
	if proxy != nil {
		return true
	}
	// getting here means the environment suggests we should not proxy. Now we need to
	// check if that is because of the no proxy list.
	conf.NoProxy = ""
	proxy, _ = conf.ProxyFunc()(url)

	// If there is no proxy configured in the environment at all
	// then we are safe. Otherwise the proxy exists because we are ignoring no proxy.
	// In this case we can not use dns scheme (as it ignores no proxy).
	return proxy == nil
}
