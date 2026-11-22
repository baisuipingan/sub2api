package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"time"
)

// Resolve once, validate every answer, then dial the numeric address. A separate
// hostname check followed by a second DNS lookup permits DNS rebinding.
type officeWebDialer struct {
	lookup func(context.Context, string) ([]net.IPAddr, error)
	dial   func(context.Context, string, string) (net.Conn, error)
}

var officeWebExcludedNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

func officePublicWebIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.Zone() != "" {
		return false
	}
	if addr.Is6() && !netip.MustParsePrefix("2000::/3").Contains(addr) {
		return false
	}
	for _, block := range officeWebExcludedNetworks {
		if block.Contains(addr) {
			return false
		}
	}
	return true
}

func (d officeWebDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || (port != "80" && port != "443") {
		return nil, ErrOfficeInvalid
	}
	answers, err := d.lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(answers) == 0 || len(answers) > 64 {
		return nil, ErrOfficeInvalid
	}
	for _, answer := range answers {
		if answer.Zone != "" || !officePublicWebIP(answer.IP) {
			return nil, ErrOfficeInvalid
		}
	}
	var last error
	for _, answer := range answers {
		conn, dialErr := d.dial(ctx, network, net.JoinHostPort(answer.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		last = dialErr
	}
	return nil, last
}

var officeWebClient = func() *http.Client {
	dialer := officeWebDialer{lookup: net.DefaultResolver.LookupIPAddr, dial: (&net.Dialer{Timeout: 5 * time.Second}).DialContext}
	return &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			DialContext: dialer.DialContext, TLSHandshakeTimeout: 5 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second, MaxConnsPerHost: 4,
			MaxIdleConns: 16, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("redirect limit")
			}
			return officeReadURL(req.URL.String())
		},
	}
}()
