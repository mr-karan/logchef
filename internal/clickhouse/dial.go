package clickhouse

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync"
	"syscall"
	"time"
)

// expiredDeadline is a deadline in the past. Setting it makes blocked and
// future I/O on a connection fail at once.
var expiredDeadline = time.Unix(1, 0)

// newDialContext returns a clickhouse-go DialContext that dials with ctx and
// ties the connection's deadline to ctx until the driver finishes its
// handshake. clickhouse-go's handshake replaces any deadline with
// now+DialTimeout and does not watch ctx, so without this a server that
// accepts and never answers holds every caller for DialTimeout, whatever the
// caller's own deadline. After the handshake the connection behaves normally,
// so a pooled connection does not depend on the context that dialed it.
//
// The driver checks an idle connection for a closed socket before reusing it
// (conn_check.go). That check needs a syscall.Conn, or a *tls.Conn whose
// NetConn is one. So for TLS the wrapper sits under tls.Client, and the
// wrapper exposes the TCP socket through SyscallConn.
func newDialContext(tlsCfg *tls.Config, timeout time.Duration) func(ctx context.Context, addr string) (net.Conn, error) {
	dialer := &net.Dialer{}
	return func(ctx context.Context, addr string) (net.Conn, error) {
		// One budget covers the TCP connect and the TLS handshake, as
		// tls.Dialer applies its Timeout. The driver's own handshake comes
		// after this and is bound to ctx through handshakeConn, not to this
		// budget, which ends when the dial returns.
		dialCtx := ctx
		if timeout > 0 {
			var cancel context.CancelFunc
			dialCtx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		raw, err := dialer.DialContext(dialCtx, "tcp", addr)
		if err != nil {
			return nil, err
		}
		conn := newHandshakeConn(ctx, raw)
		if tlsCfg == nil {
			return conn, nil
		}
		cfg := tlsCfg.Clone()
		if cfg.ServerName == "" {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				_ = conn.Close()
				return nil, err
			}
			cfg.ServerName = host
		}
		tlsConn := tls.Client(conn, cfg)
		if err := tlsConn.HandshakeContext(dialCtx); err != nil {
			_ = tlsConn.Close()
			return nil, err
		}
		return tlsConn, nil
	}
}

// handshakeConn expires its deadline when ctx ends during the handshake. The
// driver clears the deadline (SetDeadline with the zero time) when the
// handshake ends; from then on the connection passes calls through.
type handshakeConn struct {
	net.Conn
	// ctxErr is the dial context's Err method.
	ctxErr func() error
	stop   func() bool

	mu        sync.Mutex
	handshake bool
}

func newHandshakeConn(ctx context.Context, conn net.Conn) *handshakeConn {
	c := &handshakeConn{Conn: conn, ctxErr: ctx.Err, handshake: true}
	c.stop = context.AfterFunc(ctx, c.expire)
	return c
}

func (c *handshakeConn) expire() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.handshake {
		_ = c.Conn.SetDeadline(expiredDeadline)
	}
}

func (c *handshakeConn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.handshake {
		if t.IsZero() {
			c.handshake = false
			c.stop()
		} else if c.ctxErr() != nil {
			t = expiredDeadline
		}
	}
	return c.Conn.SetDeadline(t)
}

// SyscallConn exposes the TCP socket, so the driver can inspect an idle
// pooled connection for a peer close.
func (c *handshakeConn) SyscallConn() (syscall.RawConn, error) {
	sc, ok := c.Conn.(syscall.Conn)
	if !ok {
		return nil, errors.ErrUnsupported
	}
	return sc.SyscallConn()
}

func (c *handshakeConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.handshake && !t.IsZero() && c.ctxErr() != nil {
		t = expiredDeadline
	}
	return c.Conn.SetReadDeadline(t)
}
