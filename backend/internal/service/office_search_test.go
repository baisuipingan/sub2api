package service

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOfficeWebReadRejectsUnsafeURLAndIP(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "https://user:pass@example.com/", "http://example.com:4000/", "https://example.com/#secret", "//example.com/a"} {
		require.ErrorIs(t, officeReadURL(raw), ErrOfficeInvalid, raw)
	}
	for _, raw := range []string{"https://example.com/", "http://example.com:80/a?q=1", "https://example.com:443/"} {
		require.NoError(t, officeReadURL(raw), raw)
	}
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "172.16.0.1", "192.168.1.1", "100.64.0.1", "198.18.0.1", "240.0.0.1", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "2001:db8::1", "2002:7f00:1::1", "64:ff9b::7f00:1"} {
		require.False(t, officePublicWebIP(net.ParseIP(raw)), raw)
	}
	for _, raw := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		require.True(t, officePublicWebIP(net.ParseIP(raw)), raw)
	}
}

func TestOfficeWebReadPinsDNSBeforeDial(t *testing.T) {
	ctx := context.Background()
	lookupCount, dialCount := 0, 0
	dialFailure := errors.New("test numeric dial")
	dialer := officeWebDialer{
		lookup: func(context.Context, string) ([]net.IPAddr, error) {
			lookupCount++
			return []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}, nil
		},
		dial: func(_ context.Context, network, address string) (net.Conn, error) {
			dialCount++
			require.Equal(t, "tcp", network)
			require.Equal(t, "1.1.1.1:443", address)
			return nil, dialFailure
		},
	}
	_, err := dialer.DialContext(ctx, "tcp", "source.example:443")
	require.ErrorIs(t, err, dialFailure)
	require.Equal(t, 1, lookupCount)
	require.Equal(t, 1, dialCount)
	// A mixed public/private answer must never dial either address, even when
	// the first result looks safe. No second resolution can switch the target.
	dialer.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}, {IP: net.ParseIP("127.0.0.1")}}, nil
	}
	_, err = dialer.DialContext(ctx, "tcp", "mixed.example:443")
	require.ErrorIs(t, err, ErrOfficeInvalid)
	require.Equal(t, 1, dialCount)
	_, err = dialer.DialContext(ctx, "tcp", "source.example:2375")
	require.ErrorIs(t, err, ErrOfficeInvalid)
	require.Equal(t, 1, dialCount)
}
