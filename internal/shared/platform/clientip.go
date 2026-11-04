package platform

import (
	"net/http"
	"net/netip"
	"strings"
)

// ClientIPResolver finds the address a request really came from.
type ClientIPResolver struct {
	trusted []netip.Prefix
}

func NewClientIPResolver(trusted []netip.Prefix) ClientIPResolver {
	return ClientIPResolver{trusted: trusted}
}

// Resolve returns the TCP peer unless that peer is a trusted proxy. Then it walks X-Forwarded-For
// from the right, because only the entries appended by trusted hops can be believed, and returns
// the first address that is not itself trusted.
func (c ClientIPResolver) Resolve(r *http.Request) netip.Addr {
	peer := remoteAddr(r)
	if !peer.IsValid() || !c.isTrusted(peer) {
		return peer
	}
	client := peer
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return client
		}
		client = hop.Unmap()
		if !c.isTrusted(client) {
			return client
		}
	}
	return client
}

func (c ClientIPResolver) isTrusted(addr netip.Addr) bool {
	for _, prefix := range c.trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func remoteAddr(r *http.Request) netip.Addr {
	addrPort, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	return addrPort.Addr().Unmap()
}
