// Package netguard restricts outbound HTTP requests to user-supplied URLs.
//
// Link-local (cloud metadata), unspecified, "this network" and multicast
// addresses are always refused. Loopback, RFC 1918, unique-local and CGNAT
// addresses are refused unless private endpoints are allowed. The check runs
// on the address actually dialed, so DNS rebinding and redirects cannot
// bypass it.
package netguard

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func alwaysBlocked(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 0 {
		return true
	}
	return ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast()
}

func private(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || cgnat.Contains(ip)
}

// CheckIP reports whether ip may be contacted.
func CheckIP(ip net.IP, allowPrivate bool) error {
	if alwaysBlocked(ip) {
		return fmt.Errorf("endpoint must not point to a link-local, unspecified, or multicast address")
	}
	if !allowPrivate && private(ip) {
		return fmt.Errorf("endpoint must not point to a private or loopback address")
	}
	return nil
}

// ValidateURL checks a user-supplied endpoint before it is stored or used.
// A failed DNS lookup is not an error here; the dial-time check still applies.
func ValidateURL(endpoint string, allowPrivate bool) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("invalid endpoint URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("endpoint must use http or https scheme, got %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("endpoint must have a hostname")
	}
	if ip := net.ParseIP(host); ip != nil {
		return CheckIP(ip, allowPrivate)
	}
	if !allowPrivate {
		lower := strings.ToLower(host)
		if lower == "localhost" || strings.HasSuffix(lower, ".local") || strings.HasSuffix(lower, ".internal") {
			return fmt.Errorf("endpoint must not point to a local hostname")
		}
	}
	ips, _ := net.LookupIP(host)
	for _, ip := range ips {
		if err := CheckIP(ip, allowPrivate); err != nil {
			return fmt.Errorf("endpoint %q resolves to a disallowed address: %w", host, err)
		}
	}
	return nil
}

func proxyConfigured() bool {
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if os.Getenv(k) != "" {
			return true
		}
	}
	return false
}

type transportKey struct {
	allowPrivate       bool
	insecureSkipVerify bool
}

var transports sync.Map

// Transport returns a shared transport that enforces CheckIP on every dial.
// When an HTTP proxy is configured the private-address check cannot see the
// real target, so only ValidateURL covers it; the always-blocked check still
// applies to the proxy connection.
func Transport(allowPrivate, insecureSkipVerify bool) *http.Transport {
	key := transportKey{allowPrivate, insecureSkipVerify}
	if t, ok := transports.Load(key); ok {
		return t.(*http.Transport)
	}
	dialAllowPrivate := allowPrivate || proxyConfigured()
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("unexpected dial address %q", address)
			}
			return CheckIP(ip, dialAllowPrivate)
		},
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = dialer.DialContext
	if insecureSkipVerify {
		t.Proxy = nil
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in via TLS_SKIP_VERIFY
	}
	actual, _ := transports.LoadOrStore(key, t)
	return actual.(*http.Transport)
}

// Client returns an HTTP client for user-supplied URLs.
func Client(timeout time.Duration, allowPrivate, insecureSkipVerify bool) *http.Client {
	return &http.Client{Timeout: timeout, Transport: Transport(allowPrivate, insecureSkipVerify)}
}
