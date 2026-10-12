package middleware

import (
	"net"
	"net/url"
	"slices"
	"strings"
)

// OriginPolicy decides whether a browser origin may call the API and open
// sockets. One policy, shared by CORS and the websocket upgrade, so the two
// can never disagree.
type OriginPolicy struct {
	allowed []string
	// Outside production, any origin on a private network is accepted. The game
	// is played with phones on the home wifi, reaching this machine by its LAN
	// address — an address nobody can know in advance to put in a config file.
	// Production lists its origins exactly.
	allowPrivateNetwork bool
}

func NewOriginPolicy(allowed []string, allowPrivateNetwork bool) OriginPolicy {
	return OriginPolicy{allowed: allowed, allowPrivateNetwork: allowPrivateNetwork}
}

// AnyOrigin in the allowed list opens the API to every origin. Meant for
// getting a deployment working; list the real origins once they are known.
const AnyOrigin = "*"

func (p OriginPolicy) Allows(origin string) bool {
	if slices.Contains(p.allowed, AnyOrigin) || slices.Contains(p.allowed, origin) {
		return true
	}
	if !p.allowPrivateNetwork {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	return isPrivateHost(parsed.Hostname())
}

func isPrivateHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".local") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}
