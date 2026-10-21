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
