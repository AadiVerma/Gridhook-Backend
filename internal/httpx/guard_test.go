package httpx

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
)

func TestCheckIP(t *testing.T) {
	cases := []struct {
		name         string
		ip           string
		allowPrivate bool
		wantBlocked  bool
	}{

		{"aws/gcp/azure metadata", "169.254.169.254", true, true},
		{"link-local range", "169.254.1.1", true, true},
		{"ipv6 link-local", "fe80::1", true, true},

		{"unspecified", "0.0.0.0", true, true},
		{"multicast", "224.0.0.1", true, true},

		{"loopback allowed when configured", "127.0.0.1", true, false},
		{"loopback blocked when not", "127.0.0.1", false, true},
		{"rfc1918 10/8 allowed", "10.0.0.1", true, false},
		{"rfc1918 10/8 blocked", "10.0.0.1", false, true},
		{"rfc1918 192.168 blocked", "192.168.1.1", false, true},
		{"rfc1918 172.16 blocked", "172.16.0.1", false, true},
		{"ipv6 unique-local blocked", "fd00::1", false, true},

		{"public v4", "93.184.216.34", false, false},
		{"public v6", "2606:2800:220:1:248:1893:25c8:1946", false, false},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/allowPrivate=%v", tc.name, tc.allowPrivate), func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("bad test IP %q", tc.ip)
			}
			err := checkIP(ip, tc.allowPrivate)
			blocked := err != nil
			if blocked != tc.wantBlocked {
				t.Errorf("checkIP(%s, allowPrivate=%v) blocked=%v (err=%v), want blocked=%v",
					tc.ip, tc.allowPrivate, blocked, err, tc.wantBlocked)
			}
			if blocked && !errors.Is(err, ErrBlockedAddress) {
				t.Errorf("checkIP error = %v, want it to wrap ErrBlockedAddress", err)
			}
		})
	}
}

func TestCheckIP_MetadataBlockedEvenWhenPrivateAllowed(t *testing.T) {
	for _, addr := range []string{"169.254.169.254", "fe80::1"} {
		if err := checkIP(net.ParseIP(addr), true); err == nil {
			t.Errorf("checkIP(%s, allowPrivate=true) = nil, want it blocked", addr)
		}
	}
}

func TestGuardDial_RejectsNonTCP(t *testing.T) {
	control := guardDial(true)
	if err := control("udp", "8.8.8.8:53", nil); err == nil {
		t.Error("guardDial permitted a udp dial")
	}
}

func TestGuardDial_RejectsMetadataAddress(t *testing.T) {
	control := guardDial(true)
	if err := control("tcp", "169.254.169.254:80", nil); !errors.Is(err, ErrBlockedAddress) {
		t.Errorf("guardDial on the metadata endpoint = %v, want ErrBlockedAddress", err)
	}
}

func TestGuardDial_AllowsPublicAddress(t *testing.T) {
	control := guardDial(false)
	if err := control("tcp", "93.184.216.34:443", nil); err != nil {
		t.Errorf("guardDial on a public address = %v, want nil", err)
	}
}

func TestGuardDial_ChecksResolvedAddressNotHostname(t *testing.T) {
	control := guardDial(false)
	if err := control("tcp", "10.0.0.5:443", nil); !errors.Is(err, ErrBlockedAddress) {
		t.Errorf("guardDial = %v, want the resolved private address blocked", err)
	}
}

func TestSanitizeError_RedactsURLCredentials(t *testing.T) {
	original := &url.Error{
		Op:  "Get",
		URL: "https://api.example.com/v1/users?api_key=live_SECRET123&page=2",
		Err: errors.New("connection refused"),
	}

	got := SanitizeError(original).Error()
	if strings.Contains(got, "live_SECRET123") {
		t.Errorf("SanitizeError leaked the API key: %s", got)
	}
	if !strings.Contains(got, "page=2") {
		t.Errorf("SanitizeError dropped benign context: %s", got)
	}
	if !strings.Contains(got, "connection refused") {
		t.Errorf("SanitizeError dropped the underlying cause: %s", got)
	}
}

func TestSanitizeError_PassesThroughNonURLErrors(t *testing.T) {
	if got := SanitizeError(nil); got != nil {
		t.Errorf("SanitizeError(nil) = %v, want nil", got)
	}

	plain := errors.New("some other failure")
	if got := SanitizeError(plain); !errors.Is(got, plain) {
		t.Errorf("SanitizeError(%v) = %v, want the original error", plain, got)
	}
}

func TestSanitizeError_PreservesErrorChain(t *testing.T) {
	cause := errors.New("dial timeout")
	sanitized := SanitizeError(&url.Error{Op: "Get", URL: "https://x/?token=abc", Err: cause})

	if !errors.Is(sanitized, cause) {
		t.Error("SanitizeError broke the error chain; errors.Is no longer finds the cause")
	}
}
