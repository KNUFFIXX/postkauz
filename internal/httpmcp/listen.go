package httpmcp

import (
	"errors"
	"fmt"
	"net"
	"net/netip"

	"github.com/KNUFFIXX/postkauz/internal/config"
)

// Tailnet-Adressen von Tailscale (CGNAT-Bereich und ULA-Präfix): Nur hier
// darf der Modus key außerhalb von Loopback lauschen (ADR-010 Punkt 4,
// Rückfrage R1).
var tailnetPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("fd7a:115c:a1e0::/48"),
}

// ErrListenNotAllowed: Die Adresse aus [http] listen ist nicht erlaubt.
var ErrListenNotAllowed = errors.New("http listen address not allowed")

// CheckListen prüft die Adresse aus [http] listen vor dem Start (E9):
// Loopback ist immer erlaubt; jede andere Adresse nur mit
// allow_non_loopback = true, im Modus key zusätzlich nur eine
// Tailnet-Adresse (nie 0.0.0.0 oder ::).
func CheckListen(h config.HTTP) error {
	host, _, err := net.SplitHostPort(h.Listen)
	if err != nil {
		return fmt.Errorf("%w: %q is not host:port", ErrListenNotAllowed, h.Listen)
	}
	if host == "localhost" {
		return nil
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("%w: %q is not an IP address", ErrListenNotAllowed, host)
	}
	ip = ip.Unmap()
	if ip.IsLoopback() {
		return nil
	}
	if !h.AllowNonLoopback {
		return fmt.Errorf("%w: %s is not a loopback address; set [http] allow_non_loopback = true to allow it (put TLS and a private network in front, docs/betrieb.md)",
			ErrListenNotAllowed, h.Listen)
	}
	if h.Auth == config.HTTPAuthKey && !isTailnet(ip) {
		return fmt.Errorf("%w: with [http] auth = \"key\" only loopback or Tailscale addresses (100.64.0.0/10, fd7a:115c:a1e0::/48) are allowed, not %s",
			ErrListenNotAllowed, h.Listen)
	}
	return nil
}

func isTailnet(ip netip.Addr) bool {
	for _, p := range tailnetPrefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
