package crypto

import (
	"errors"
	"context"
	"net"
	"net/http"
	"net/url"
	"time"
)

// IsPrivateIP returns true if the provided IP is within private, loopback, link-local, or otherwise non-public ranges.
func IsPrivateIP(ip net.IP) bool {
	privateCIDRs := []string{
		"127.0.0.0/8",   // loopback
		"10.0.0.0/8",    // private
		"172.16.0.0/12", // private
		"192.168.0.0/16",// private
		"169.254.0.0/16",// link-local
		"::1/128",       // IPv6 loopback
		"fc00::/7",      // IPv6 unique local addresses
	}
	for _, cidr := range privateCIDRs {
		_, block, _ := net.ParseCIDR(cidr)
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// IsSafeWebhookURL validates that a webhook URL is acceptable:
// - must be valid URL
// - must use https
// - must resolve to public IPs (no private/loopback/link-local)
func IsSafeWebhookURL(raw string) error {
	if raw == "" {
		return errors.New("url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "https" {
		return errors.New("only https scheme allowed for webhooks")
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("invalid host")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if IsPrivateIP(ip) {
			return errors.New("destination resolves to private/internal IP")
		}
	}
	return nil
}

// NewRestrictedHTTPClient returns an HTTP client that:
// - blocks connections to private/internal IP ranges at dial time
// - performs DNS resolution per request (mitigates DNS rebinding)
// - enforces short timeouts and disables redirects
func NewRestrictedHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			// address is "host:port"
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				// Best-effort; if split fails, block
				return nil, errors.New("invalid address")
			}
			ips, err := net.LookupIP(host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if IsPrivateIP(ip) {
					return nil, errors.New("blocked connection to internal IP")
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
		},
		// Reasonable limits
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	client := &http.Client{
		Timeout: timeout,
		Transport: transport,
		// Do not follow redirects automatically; avoid pivoting to internal addresses via Location.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return client
}

