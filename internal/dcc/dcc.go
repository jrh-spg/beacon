// Package dcc implements client-to-client direct connections (DCC SEND and
// DCC CHAT) over plain TCP.
//
// The classic DCC spec encodes the offerer's IPv4 address as a base-10
// uint32 in network byte order, embedded inside a CTCP message:
//
//	\x01DCC SEND <filename> <ip-int> <port> <size>\x01
//	\x01DCC CHAT chat <ip-int> <port>\x01
//
// This package handles parsing, the address conversion, file send/receive,
// and listening for an inbound chat connection. NAT/firewall traversal is
// out of scope — DCC requires that the offerer be reachable from the
// receiver, which usually means port forwarding or running on a host with
// a public address.
package dcc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Kind is the DCC sub-protocol.
type Kind int

const (
	// KindSend is a file offer.
	KindSend Kind = iota + 1
	// KindChat is a peer chat session offer.
	KindChat
)

// String returns a human-readable name for the kind.
func (k Kind) String() string {
	switch k {
	case KindSend:
		return "SEND"
	case KindChat:
		return "CHAT"
	}
	return "?"
}

// Offer represents one parsed CTCP DCC payload, with the offerer's IPv4
// address decoded into a net.IP.
type Offer struct {
	From     string
	Kind     Kind
	Filename string
	Size     int64
	IP       net.IP
	Port     uint16
}

// ParseCTCP parses a CTCP DCC payload. The argument is the portion of the
// CTCP body that follows the "DCC " prefix (e.g. "SEND foo 12345 6000 4096"
// or "CHAT chat 12345 6000").
func ParseCTCP(from, body string) (*Offer, error) {
	parts := strings.Fields(body)
	if len(parts) < 4 {
		return nil, errors.New("malformed DCC offer")
	}
	kind := strings.ToUpper(parts[0])
	switch kind {
	case "SEND":
		// SEND <filename> <ip-int> <port> [<size>]
		filename := parts[1]
		ip, err := decodeIP(parts[2])
		if err != nil {
			return nil, fmt.Errorf("DCC SEND: %w", err)
		}
		port, err := parsePort(parts[3])
		if err != nil {
			return nil, fmt.Errorf("DCC SEND: %w", err)
		}
		var size int64
		if len(parts) >= 5 {
			size, _ = strconv.ParseInt(parts[4], 10, 64)
		}
		return &Offer{
			From: from, Kind: KindSend,
			Filename: filename, Size: size,
			IP: ip, Port: port,
		}, nil
	case "CHAT":
		// CHAT chat <ip-int> <port>
		ip, err := decodeIP(parts[2])
		if err != nil {
			return nil, fmt.Errorf("DCC CHAT: %w", err)
		}
		port, err := parsePort(parts[3])
		if err != nil {
			return nil, fmt.Errorf("DCC CHAT: %w", err)
		}
		return &Offer{From: from, Kind: KindChat, IP: ip, Port: port}, nil
	}
	return nil, fmt.Errorf("unsupported DCC subtype %q", kind)
}

func parsePort(s string) (uint16, error) {
	p, err := strconv.ParseUint(s, 10, 16)
	if err != nil {
		return 0, fmt.Errorf("bad port %q", s)
	}
	if p == 0 {
		return 0, errors.New("port must be > 0")
	}
	return uint16(p), nil
}

// EncodeIP turns an IPv4 address into the base-10 uint32 form used by DCC.
func EncodeIP(ip net.IP) (string, error) {
	v4 := ip.To4()
	if v4 == nil {
		return "", errors.New("DCC is IPv4-only")
	}
	return strconv.FormatUint(uint64(binary.BigEndian.Uint32(v4)), 10), nil
}

func decodeIP(s string) (net.IP, error) {
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("bad ip int %q", s)
	}
	b := make(net.IP, 4)
	binary.BigEndian.PutUint32(b, uint32(n))
	return b, nil
}

// Listener wraps a TCP listener and remembers the chosen port.
type Listener struct {
	L    *net.TCPListener
	Port uint16
}

// Close releases the listener.
func (l *Listener) Close() error {
	if l == nil || l.L == nil {
		return nil
	}
	return l.L.Close()
}

// Listen opens an IPv4 TCP listener on an ephemeral port for use as the
// receiver-facing endpoint of a DCC offer.
func Listen() (*Listener, error) {
	l, err := net.ListenTCP("tcp4", &net.TCPAddr{Port: 0})
	if err != nil {
		return nil, err
	}
	return &Listener{L: l, Port: uint16(l.Addr().(*net.TCPAddr).Port)}, nil
}

// Accept blocks for up to timeout waiting for the receiver to connect.
func (l *Listener) Accept(timeout time.Duration) (net.Conn, error) {
	if err := l.L.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	return l.L.Accept()
}

// SendFile streams f over c, optionally invoking progress as bytes flow.
// It also drains 4-byte ack counters the receiver may send back.
func SendFile(c net.Conn, f *os.File, total int64, progress func(sent, total int64)) error {
	defer c.Close()
	defer f.Close()
	buf := make([]byte, 32*1024)
	var sent int64

	// Drain acks in the background so the kernel buffer doesn't stall.
	ackDone := make(chan struct{})
	go func() {
		defer close(ackDone)
		ack := make([]byte, 4)
		for {
			if _, err := io.ReadFull(c, ack); err != nil {
				return
			}
		}
	}()

	for {
		if err := c.SetWriteDeadline(time.Now().Add(60 * time.Second)); err != nil {
			return err
		}
		n, rerr := f.Read(buf)
		if n > 0 {
			if _, werr := c.Write(buf[:n]); werr != nil {
				return werr
			}
			sent += int64(n)
			if progress != nil {
				progress(sent, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	_ = c.(*net.TCPConn).CloseWrite()
	select {
	case <-ackDone:
	case <-time.After(5 * time.Second):
	}
	return nil
}

// Receive dials ip:port, writes to dest until EOF or the expected byte
// count is reached, and ACKs running totals back to the sender (some
// clients require ACKs to continue).
func Receive(ip net.IP, port uint16, dest string, expected int64, progress func(got, total int64)) error {
	addr := net.JoinHostPort(ip.String(), strconv.Itoa(int(port)))
	c, err := net.DialTimeout("tcp", addr, 30*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()

	if mkErr := os.MkdirAll(parentDir(dest), 0o755); mkErr != nil {
		return mkErr
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, 32*1024)
	var got int64
	for {
		if err := c.SetReadDeadline(time.Now().Add(120 * time.Second)); err != nil {
			return err
		}
		n, rerr := c.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			got += int64(n)
			if progress != nil {
				progress(got, expected)
			}
			var ack [4]byte
			binary.BigEndian.PutUint32(ack[:], uint32(got))
			_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
			_, _ = c.Write(ack[:])
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
		if expected > 0 && got >= expected {
			break
		}
	}
	return nil
}

func parentDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i]
		}
	}
	return "."
}

// DialChat connects to ip:port for a CHAT session.
func DialChat(ip net.IP, port uint16) (net.Conn, error) {
	addr := net.JoinHostPort(ip.String(), strconv.Itoa(int(port)))
	return net.DialTimeout("tcp", addr, 30*time.Second)
}

// HumanSize formats a byte count as "<n>B/<KiB/MiB/GiB>".
func HumanSize(n int64) string {
	const k = 1024
	switch {
	case n < k:
		return fmt.Sprintf("%dB", n)
	case n < k*k:
		return fmt.Sprintf("%.1fKiB", float64(n)/k)
	case n < k*k*k:
		return fmt.Sprintf("%.1fMiB", float64(n)/(k*k))
	default:
		return fmt.Sprintf("%.2fGiB", float64(n)/(k*k*k))
	}
}
