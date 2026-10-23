package datasource

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"testing"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

func TestSourceAdminDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, detail string
		err          error
	}{
		{"authentication", "authentication failed", &ch.Exception{Code: 516, Message: "private authentication detail"}},
		{"refused", "connection refused", &net.OpError{Op: "dial", Addr: &net.TCPAddr{Port: 9000}, Err: syscall.ECONNREFUSED}},
		{"TLS", "TLS: certificate verification failed", &tls.CertificateVerificationError{}},
		{"DDL", "Table already exists. Choose another name.", &ch.Exception{Code: 57, Message: "Table already exists.\n Choose another name."}},
		{"bounded DDL", strings.Repeat("x", 240) + "...", &ch.Exception{Code: 57, Message: strings.Repeat("x", 300)}},
		{"driver", "", errors.New("unexpected driver packet from example.invalid:9000")},
		{"backend", "", &ch.Exception{Code: 210, Message: "network error from example.invalid:9000"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validation := &ValidationError{Field: "connection", Message: "Cannot validate source", Err: fmt.Errorf("connect: %w", tc.err)}
			want := validation.Message
			if tc.detail != "" {
				want += ": " + tc.detail
			}
			if got := validation.AdminMessage(); got != want {
				t.Fatalf("message=%q, want %q", got, want)
			}
		})
	}
}
