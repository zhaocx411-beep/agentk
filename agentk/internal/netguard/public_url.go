package netguard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// ValidatePublicHTTPURL rejects targets that resolve to non-public network addresses.
func ValidatePublicHTTPURL(ctx context.Context, raw string) (*url.URL, error) {
	target, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || target.Host == "" ||
		(target.Scheme != "http" && target.Scheme != "https") {
		return nil, fmt.Errorf("URL must be an absolute HTTP(S) URL")
	}
	if target.User != nil {
		return nil, fmt.Errorf("URL userinfo is not allowed")
	}
	host := target.Hostname()
	if host == "" {
		return nil, fmt.Errorf("URL host is required")
	}
	var ips []net.IP
	if literal := net.ParseIP(host); literal != nil {
		ips = []net.IP{literal}
	} else {
		resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve URL host %q: %w", host, err)
		}
		for _, item := range resolved {
			ips = append(ips, item.IP)
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("URL host %q resolved to no addresses", host)
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, fmt.Errorf("URL host %q resolves to non-public address %s", host, ip)
		}
	}
	return target, nil
}

func publicIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsUnspecified()
}

// PublicDialContext resolves once, validates every answer, then dials one of
// those exact IPs. This closes the DNS-rebinding gap between URL validation
// and http.Transport's later connection.
func PublicDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("split dial address %q: %w", address, err)
	}
	var ips []net.IP
	if literal := net.ParseIP(host); literal != nil {
		ips = []net.IP{literal}
	} else {
		resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve dial host %q: %w", host, err)
		}
		for _, item := range resolved {
			ips = append(ips, item.IP)
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("dial host %q resolved to no addresses", host)
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, fmt.Errorf("dial host %q resolves to non-public address %s", host, ip)
		}
	}
	dialer := &net.Dialer{}
	var lastErr error
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("dial public host %q: %w", host, lastErr)
}

// PublicRedirectPolicy validates every redirect target and preserves the standard 10-hop limit.
func PublicRedirectPolicy(previous func(*http.Request, []*http.Request) error) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if _, err := ValidatePublicHTTPURL(req.Context(), req.URL.String()); err != nil {
			return err
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}
}
