package irc

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Conn is a low-level IRC connection. It is safe to call Write concurrently.
type Conn struct {
	nc        net.Conn
	br        *bufio.Reader
	bw        *bufio.Writer
	wmu       sync.Mutex
	closed    atomic.Bool
	closeOnce sync.Once
}

// DialOptions controls how Dial establishes a connection.
type DialOptions struct {
	Addr          string // host:port
	UseTLS        bool
	TLSSkipVerify bool
	TLSServerName string
	TLSCertFile   string // path to PEM client certificate (for SASL EXTERNAL / CertFP)
	TLSKeyFile    string // path to PEM private key; defaults to TLSCertFile if empty
	Timeout       time.Duration
}

// Dial opens a connection to an IRC server, optionally with TLS.
func Dial(opts DialOptions) (*Conn, error) {
	if opts.Addr == "" {
		return nil, errors.New("empty address")
	}
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}

	dialer := &net.Dialer{Timeout: opts.Timeout}

	var nc net.Conn
	var err error
	if opts.UseTLS {
		host, _, splitErr := net.SplitHostPort(opts.Addr)
		if splitErr != nil {
			host = opts.Addr
		}
		serverName := opts.TLSServerName
		if serverName == "" {
			serverName = host
		}
		tlsConf := &tls.Config{
			ServerName:         serverName,
			InsecureSkipVerify: opts.TLSSkipVerify, //#nosec G402 -- user-controlled flag
			MinVersion:         tls.VersionTLS12,
		}
		if opts.TLSCertFile != "" {
			keyFile := opts.TLSKeyFile
			if keyFile == "" {
				keyFile = opts.TLSCertFile
			}
			cert, certErr := tls.LoadX509KeyPair(opts.TLSCertFile, keyFile)
			if certErr != nil {
				return nil, fmt.Errorf("loading client certificate: %w", certErr)
			}
			tlsConf.Certificates = []tls.Certificate{cert}
		}
		nc, err = tls.DialWithDialer(dialer, "tcp", opts.Addr, tlsConf)
	} else {
		nc, err = dialer.Dial("tcp", opts.Addr)
	}
	if err != nil {
		return nil, err
	}

	return &Conn{
		nc: nc,
		br: bufio.NewReaderSize(nc, 4096),
		bw: bufio.NewWriterSize(nc, 4096),
	}, nil
}

// WriteRaw writes a raw IRC line. CRLF is appended automatically.
func (c *Conn) WriteRaw(line string) error {
	if c.closed.Load() {
		return errors.New("connection closed")
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := fmt.Fprintf(c.bw, "%s\r\n", line); err != nil {
		return err
	}
	return c.bw.Flush()
}

// ReadMessage reads the next line and parses it.
func (c *Conn) ReadMessage() (*Message, error) {
	line, err := c.br.ReadString('\n')
	if err != nil {
		return nil, err
	}
	return ParseMessage(line)
}

// Close shuts down the connection.
func (c *Conn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		err = c.nc.Close()
	})
	return err
}

// RemoteAddr returns the remote network address.
func (c *Conn) RemoteAddr() string {
	if c.nc == nil {
		return ""
	}
	return c.nc.RemoteAddr().String()
}
