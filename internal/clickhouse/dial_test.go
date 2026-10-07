package clickhouse

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"log/slog"
	"math/big"
	"net"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

// silentServer accepts TCP connections and never answers, like a hung
// ClickHouse.
func silentServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = conn.Close() })
		}
	}()
	return ln.Addr().String()
}

// A caller deadline shorter than DialTimeout must end a stuck handshake.
// Before the dial wrapper, this call took the full 10 s DialTimeout.
func TestPingHonorsContextOnSilentServer(t *testing.T) {
	t.Parallel()
	client, err := NewClient(ClientOptions{Host: silentServer(t)}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := client.Ping(ctx, "", ""); err == nil {
		t.Fatal("ping to a silent server succeeded")
	}
	if elapsed := time.Since(started); elapsed > 800*time.Millisecond {
		t.Fatalf("ping took %s with a 200 ms deadline", elapsed)
	}
}

// After the driver clears the deadline (the end of its handshake), the end
// of the dial context must not affect the connection, because the pool
// reuses it for later callers.
func TestHandshakeConnIgnoresContextAfterHandshake(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			accepted <- conn
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	conn, err := newDialContext(nil, time.Second)(ctx, ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	peer := <-accepted
	t.Cleanup(func() { _ = peer.Close() })

	if err := conn.SetDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	cancel()
	time.Sleep(50 * time.Millisecond)

	if _, err := peer.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err != nil {
		t.Fatalf("read after the dial context ended: %v", err)
	}
}

// During the handshake, the end of the dial context fails blocked reads,
// even after the driver set its own longer deadline.
func TestHandshakeConnExpiresWithContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	conn, err := newDialContext(nil, time.Second)(ctx, silentServer(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	started := time.Now()
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("read from a silent server succeeded")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("read took %s after the context ended", elapsed)
	}

	// A deadline set after the context ended is replaced by an expired one.
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	started = time.Now()
	if _, err := conn.Read(make([]byte, 1)); err == nil || time.Since(started) > 100*time.Millisecond {
		t.Fatalf("read after a late deadline: err=%v after %s", err, time.Since(started))
	}
}

// closingProxy forwards TCP traffic to upstream and can close every
// connection it carries, as a ClickHouse restart or an idle timeout does.
type closingProxy struct {
	addr  string
	mu    sync.Mutex
	conns []net.Conn
}

func newClosingProxy(t *testing.T, upstream string, tlsCfg *tls.Config) *closingProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if tlsCfg != nil {
		ln = tls.NewListener(ln, tlsCfg)
	}
	t.Cleanup(func() { _ = ln.Close() })
	p := &closingProxy{addr: ln.Addr().String()}
	t.Cleanup(p.closeAll)
	go func() {
		for {
			client, err := ln.Accept()
			if err != nil {
				return
			}
			server, err := net.Dial("tcp", upstream)
			if err != nil {
				_ = client.Close()
				continue
			}
			p.mu.Lock()
			p.conns = append(p.conns, client, server)
			p.mu.Unlock()
			go func() { _, _ = io.Copy(server, client); _ = server.Close() }()
			go func() { _, _ = io.Copy(client, server); _ = client.Close() }()
		}
	}()
	return p
}

func (p *closingProxy) closeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, conn := range p.conns {
		_ = conn.Close()
	}
	p.conns = nil
}

// selfSignedTLS returns a server certificate for 127.0.0.1 and a client
// config that trusts only it.
func selfSignedTLS(t *testing.T) (server, client *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "logchef-test"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	server = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	client = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	return server, client
}

// A pooled connection whose peer closed while idle must be detected by the
// driver's socket check and replaced, not reused into an EOF. That check
// needs the dialed connection to expose its socket (syscall.Conn, or a
// *tls.Conn over one).
func TestPooledConnectionSurvivesIdlePeerClose(t *testing.T) {
	upstream := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if upstream == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	serverTLS, clientTLS := selfSignedTLS(t)
	for _, tc := range []struct {
		name      string
		serverTLS *tls.Config
		clientTLS *tls.Config
	}{
		{"plaintext", nil, nil},
		{"tls", serverTLS, clientTLS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proxy := newClosingProxy(t, upstream, tc.serverTLS)
			dial := newDialContext(tc.clientTLS, 5*time.Second)

			raw, err := dial(context.Background(), proxy.addr)
			if err != nil {
				t.Fatal(err)
			}
			inspected := raw
			if tlsConn, ok := raw.(*tls.Conn); ok {
				inspected = tlsConn.NetConn()
			} else if tc.clientTLS != nil {
				t.Fatalf("TLS dial returned %T, want *tls.Conn", raw)
			}
			if _, ok := inspected.(syscall.Conn); !ok {
				t.Fatalf("dialed connection %T hides its socket from the driver", inspected)
			}
			_ = raw.Close()

			conn, err := ch.Open(&ch.Options{
				Addr:        []string{proxy.addr},
				Auth:        ch.Auth{Username: "default"},
				DialTimeout: 5 * time.Second,
				DialContext: dial,
				TLS:         tc.clientTLS,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if err := conn.Ping(ctx); err != nil {
				t.Fatalf("first ping: %v", err)
			}
			proxy.closeAll()
			time.Sleep(100 * time.Millisecond)
			if err := conn.Ping(ctx); err != nil {
				t.Fatalf("ping after the idle connection was closed: %v", err)
			}
		})
	}
}

// The dial timeout covers the TLS handshake, not only the TCP connect, as
// tls.Dialer's Timeout did. A peer that accepts and never speaks TLS must
// fail at the dial timeout even when the caller allows far longer.
func TestTLSDialTimeoutCoversHandshake(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	started := time.Now()
	conn, err := newDialContext(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}, 150*time.Millisecond)(ctx, silentServer(t))
	if conn != nil {
		_ = conn.Close()
	}
	if elapsed := time.Since(started); err == nil || elapsed > 600*time.Millisecond {
		t.Fatalf("TLS dial to a silent peer: err=%v after %s, want an error near 150 ms", err, elapsed)
	}
}

// The driver's native handshake runs after the dial returns. It must stay
// bound to the caller's context, not to the dial budget that ends at return,
// so a connection that dialed successfully is usable.
func TestDialBudgetDoesNotExpireTheConnection(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		if conn, err := ln.Accept(); err == nil {
			accepted <- conn
		}
	}()
	conn, err := newDialContext(nil, 50*time.Millisecond)(context.Background(), ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	peer := <-accepted
	t.Cleanup(func() { _ = peer.Close() })
	time.Sleep(100 * time.Millisecond)
	if _, err := peer.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(make([]byte, 1)); err != nil {
		t.Fatalf("read after the dial budget ended: %v", err)
	}
}
