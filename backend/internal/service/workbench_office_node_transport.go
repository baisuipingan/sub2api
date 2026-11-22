package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const officeNodeIdentityHeader = "X-Office-Node-ID"

var (
	officeNodeVPNRange          = netip.MustParsePrefix("100.64.0.0/10")
	officeNodeMetadataAddresses = [...]netip.Addr{
		netip.MustParseAddr("100.100.100.200"),
		netip.MustParseAddr("fd00:ec2::254"),
	}
)

func normalizeOfficeNodeOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 512 || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") ||
		u.User != nil || u.Opaque != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") {
		return "", ErrOfficeNodeInvalid
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if ip, err := netip.ParseAddr(host); err == nil {
		if !officeNodeAllowedAddress(ip, u.Scheme == "https") {
			return "", ErrOfficeNodeInvalid
		}
		host = ip.Unmap().String()
	} else if net.ParseIP(host) != nil || host == "" || strings.IndexFunc(host, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.')
	}) >= 0 || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return "", ErrOfficeNodeInvalid
	}
	port := u.Port()
	if port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", ErrOfficeNodeInvalid
		}
		port = strconv.Itoa(number)
		if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
			port = ""
		}
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return u.Scheme + "://" + host, nil
}

func officeNodeAllowedAddress(addr netip.Addr, allowPublic bool) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || addr.Zone() != "" || !addr.IsGlobalUnicast() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	for _, reserved := range officeNodeMetadataAddresses {
		if addr == reserved {
			return false
		}
	}
	return addr.IsPrivate() || officeNodeVPNRange.Contains(addr) || allowPublic && officePublicWebIP(addr.AsSlice())
}

func officeNodeDial(allowPublic bool) func(context.Context, string, string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, ErrOfficeNodeInvalid
		}
		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(addresses) == 0 || len(addresses) > 64 {
			return nil, ErrOfficeNodeUnavailable
		}
		for _, ip := range addresses {
			if !officeNodeAllowedAddress(ip, allowPublic) {
				return nil, ErrOfficeNodeInvalid
			}
		}
		// Dial the address checked above, while preserving the URL hostname for HTTPS verification.
		for _, ip := range addresses {
			connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return connection, nil
			}
		}
		return nil, ErrOfficeNodeUnavailable
	}
}

func newOfficeNodeHTTP(allowPublic bool) *http.Client {
	return &http.Client{
		Timeout:       35 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			DialContext: officeNodeDial(allowPublic), TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 32 * time.Second,
			MaxConnsPerHost: 16, MaxIdleConns: 32, MaxIdleConnsPerHost: 8, IdleConnTimeout: 45 * time.Second,
		},
	}
}

var (
	officeNodeHTTPS       = newOfficeNodeHTTP(true)
	officeNodePrivateHTTP = newOfficeNodeHTTP(false)
)

func (s *OfficeAgentService) officeNodeRequest(ctx context.Context, node OfficeNode, method, endpoint string, body []byte, limit int64) (int, []byte, error) {
	status, data, _, err := s.sendOfficeNodeRequest(ctx, node, method, endpoint, body, limit)
	return status, data, err
}

func (s *OfficeAgentService) sendOfficeNodeRequest(ctx context.Context, node OfficeNode, method, endpoint string, body []byte, limit int64) (int, []byte, string, error) {
	origin, err := normalizeOfficeNodeOrigin(node.URL)
	if err != nil {
		return 0, nil, "", err
	}
	token, err := s.officeNodeCredential(ctx, node)
	if err != nil {
		return 0, nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, method, origin+endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, "", ErrOfficeNodeInvalid
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if node.WorkerIdentity != "" {
		req.Header.Set(officeNodeIdentityHeader, node.WorkerIdentity)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	client := officeNodeHTTPS
	if req.URL.Scheme == "http" {
		client = officeNodePrivateHTTP
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, "", ErrOfficeNodeUnavailable
	}
	defer resp.Body.Close()
	actualIdentity := resp.Header.Get(officeNodeIdentityHeader)
	if resp.StatusCode == http.StatusUnauthorized || !officeIdentifier(actualIdentity, 128) {
		return 0, nil, "", ErrOfficeNodeUnavailable
	}
	if node.WorkerIdentity != "" && actualIdentity != node.WorkerIdentity {
		return 0, nil, "", ErrOfficeNodeIdentity
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return 0, nil, "", ErrOfficeNodeUnavailable
	}
	return resp.StatusCode, data, actualIdentity, nil
}
