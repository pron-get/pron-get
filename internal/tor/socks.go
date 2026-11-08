// Package tor routes all network traffic through a Tor SOCKS5 proxy.
//
// The SOCKS client is deliberately tiny and dependency free: it always sends
// hostnames to the proxy (never resolves them locally), so DNS lookups happen
// inside Tor and cannot leak.
package tor

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// Dialer opens TCP connections through a SOCKS5 proxy. Username/password are
// used by Tor for circuit isolation (IsolateSOCKSAuth): connections with
// different credentials never share a circuit.
type Dialer struct {
	ProxyAddr string

	mu       sync.Mutex
	user     string
	password string
}

// NewDialer returns a Dialer for the proxy at addr with fresh isolation
// credentials.
func NewDialer(addr string) *Dialer {
	d := &Dialer{ProxyAddr: addr}
	d.Renew()
	return d
}

// Renew switches to new isolation credentials, so new connections are made on
// a fresh circuit (and usually a different exit).
func (d *Dialer) Renew() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.user = randomToken()
	d.password = randomToken()
}

func (d *Dialer) creds() (string, string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.user, d.password
}

// DialContext connects to addr ("host:port") through the proxy.
func (d *Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("socks5: unsupported network %q", network)
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("socks5: bad port %q", portStr)
	}

	var nd net.Dialer
	conn, err := nd.DialContext(ctx, "tcp", d.ProxyAddr)
	if err != nil {
		return nil, fmt.Errorf("tor proxy %s unreachable: %w", d.ProxyAddr, err)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(2 * time.Minute)
	}
	conn.SetDeadline(deadline)

	user, pass := d.creds()
	if err := handshake(conn, host, port, user, pass); err != nil {
		conn.Close()
		return nil, fmt.Errorf("tor connect %s: %w", addr, err)
	}
	conn.SetDeadline(time.Time{})
	return conn, nil
}

func handshake(conn net.Conn, host string, port int, user, pass string) error {
	method := byte(0x00)
	if user != "" {
		method = 0x02
	}
	if _, err := conn.Write([]byte{5, 1, method}); err != nil {
		return err
	}
	var resp [2]byte
	if _, err := io.ReadFull(conn, resp[:]); err != nil {
		return err
	}
	if resp[0] != 5 || resp[1] != method {
		return errors.New("proxy refused authentication method")
	}
	if method == 0x02 {
		if len(user) > 255 || len(pass) > 255 {
			return errors.New("credentials too long")
		}
		msg := []byte{1, byte(len(user))}
		msg = append(msg, user...)
		msg = append(msg, byte(len(pass)))
		msg = append(msg, pass...)
		if _, err := conn.Write(msg); err != nil {
			return err
		}
		if _, err := io.ReadFull(conn, resp[:]); err != nil {
			return err
		}
		if resp[1] != 0 {
			return errors.New("proxy rejected credentials")
		}
	}

	req := []byte{5, 1, 0}
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			req = append(req, 1)
			req = append(req, ip4...)
		} else {
			req = append(req, 4)
			req = append(req, ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			return errors.New("hostname too long")
		}
		req = append(req, 3, byte(len(host)))
		req = append(req, host...)
	}
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	if _, err := conn.Write(req); err != nil {
		return err
	}

	var hdr [4]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return err
	}
	if hdr[1] != 0 {
		return replyError(hdr[1])
	}
	var skip int
	switch hdr[3] {
	case 1:
		skip = 4 + 2
	case 4:
		skip = 16 + 2
	case 3:
		var l [1]byte
		if _, err := io.ReadFull(conn, l[:]); err != nil {
			return err
		}
		skip = int(l[0]) + 2
	default:
		return fmt.Errorf("bad reply address type %d", hdr[3])
	}
	_, err := io.CopyN(io.Discard, conn, int64(skip))
	return err
}

// ReplyError is a SOCKS5 failure reported by the proxy.
type ReplyError byte

func replyError(code byte) error { return ReplyError(code) }

func (e ReplyError) Error() string {
	// 0xF0–0xF7 are Tor's ExtendedErrors for onion services.
	msgs := map[byte]string{
		1: "general failure", 2: "not allowed", 3: "network unreachable",
		4: "host unreachable", 5: "connection refused", 6: "TTL expired (circuit timed out)",
		7: "command not supported", 8: "address type not supported (SafeSocks?)",
		0xF0: "onion service descriptor not found", 0xF1: "onion service descriptor invalid",
		0xF2: "onion service introduction failed", 0xF3: "onion service rendezvous failed",
		0xF4: "onion service needs client auth", 0xF5: "onion service client auth wrong",
		0xF6: "invalid onion address", 0xF7: "onion service introduction timed out",
	}
	if m, ok := msgs[byte(e)]; ok {
		return "socks5: " + m
	}
	return fmt.Sprintf("socks5: error 0x%02x", byte(e))
}
