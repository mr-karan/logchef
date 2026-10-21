package clickhouse

import (
	"errors"
	"fmt"
	"testing"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

func TestPublicQueryErrorResourceLimits(t *testing.T) {
	for _, code := range []int32{158, 307, 396, 241} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			cause := &ch.Exception{Code: code, Message: "resource limit diagnostic"}
			err := publicQueryError(fmt.Errorf("query failed: %w", cause))
			diagnostic, public := errors.AsType[*QueryError](err)
			if want := code != 241; public != want {
				t.Fatalf("code %d public=%t, want %t", code, public, want)
			}
			if public && diagnostic.Message != cause.Message {
				t.Fatalf("message=%q, want %q", diagnostic.Message, cause.Message)
			}
			if !errors.Is(err, cause) {
				t.Fatal("original driver cause is missing")
			}
		})
	}
}

func TestPublicQueryErrorDistributedDiagnostics(t *testing.T) {
	for _, code := range []int32{16, 34, 35, 36, 42, 43, 46, 47, 50, 53, 59, 60, 62, 63, 69, 70, 80, 81, 158, 184, 215, 307, 386, 396} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			for _, tc := range []struct {
				name, message string
				public        bool
			}{
				{"remote", "Received from shard.example:9000. DB::Exception: limit exceeded. While executing Remote. Stack trace:\n0. DB::Exception", true},
				{"stack", "limit exceeded. Stack trace:\n0. DB::Exception", true},
				{"host port", "failure on shard.example:9000", false},
				{"ipv4", "failure on 192.0.2.1", false},
				{"ipv6", "failure on [2001:db8::1]", false},
				{"bare ipv6", "failure on 2001:db8::1", false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					cause := &ch.Exception{Code: code, Message: tc.message}
					err := publicQueryError(cause)
					diagnostic, public := errors.AsType[*QueryError](err)
					if public != tc.public {
						t.Fatalf("public=%t, want %t", public, tc.public)
					}
					if public && diagnostic.Message != "limit exceeded." {
						t.Fatalf("message=%q, want cleaned diagnostic", diagnostic.Message)
					}
					if !errors.Is(err, cause) {
						t.Fatal("original cause missing")
					}
				})
			}
		})
	}
}
