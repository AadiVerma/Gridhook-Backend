package httpx

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"syscall"

	"gridhook.dev/connector-backend/internal/secrets"
)

var ErrBlockedAddress = errors.New("httpx: destination address is not permitted")

func guardDial(allowPrivate bool) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, _ syscall.RawConn) error {
		switch network {
		case "tcp", "tcp4", "tcp6":
		default:
			return fmt.Errorf("%w: network %q", ErrBlockedAddress, network)
		}

		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("%w: unparseable address", ErrBlockedAddress)
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("%w: unresolvable address", ErrBlockedAddress)
		}
		return checkIP(ip, allowPrivate)
	}
}

func checkIP(ip net.IP, allowPrivate bool) error {

	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("%w: link-local address %s", ErrBlockedAddress, ip)
	}
	if ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return fmt.Errorf("%w: reserved address %s", ErrBlockedAddress, ip)
	}

	if isUniqueLocalV6(ip) && !allowPrivate {
		return fmt.Errorf("%w: unique-local address %s", ErrBlockedAddress, ip)
	}
	if allowPrivate {
		return nil
	}
	if ip.IsLoopback() || ip.IsPrivate() {
		return fmt.Errorf("%w: private address %s", ErrBlockedAddress, ip)
	}
	return nil
}

func isUniqueLocalV6(ip net.IP) bool {
	v6 := ip.To16()
	return ip.To4() == nil && v6 != nil && v6[0]&0xfe == 0xfc
}

func SanitizeError(err error) error {
	if err == nil {
		return nil
	}
	if urlErr, ok := errors.AsType[*url.Error](err); ok {
		return &url.Error{
			Op:  urlErr.Op,
			URL: secrets.SanitizeURL(urlErr.URL),
			Err: urlErr.Err,
		}
	}
	return err
}
