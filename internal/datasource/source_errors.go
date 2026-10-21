package datasource

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"strings"
	"syscall"

	ch "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/mr-karan/logchef/internal/clickhouse"
)

// AdminMessage adds actionable connection diagnostics for source administrators.
func (e *ValidationError) AdminMessage() string {
	detail := sourceErrorDetail(e.Err)
	if detail == "" {
		return e.Message
	}
	return e.Message + ": " + detail
}

func sourceErrorDetail(err error) string {
	if exception, ok := errors.AsType[*ch.Exception](err); ok {
		switch exception.Code {
		case 192, 193, 194, 516:
			return "authentication failed"
		case 497:
			return "access denied"
		case 57, 58, 82, 119, 122:
			return cleanSourceDiagnostic(exception.Message)
		default:
			if queryErr, ok := errors.AsType[*clickhouse.QueryError](err); ok {
				return cleanSourceDiagnostic(queryErr.Message)
			}
			return ""
		}
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "connection refused"
	}
	if _, ok := errors.AsType[*tls.CertificateVerificationError](err); ok {
		return "TLS: certificate verification failed"
	}
	if _, ok := errors.AsType[x509.UnknownAuthorityError](err); ok {
		return "TLS: certificate authority is not trusted"
	}
	if _, ok := errors.AsType[x509.HostnameError](err); ok {
		return "TLS: certificate does not match the server name"
	}
	if _, ok := errors.AsType[tls.RecordHeaderError](err); ok {
		return "TLS: invalid handshake response"
	}
	return ""
}

func cleanSourceDiagnostic(message string) string {
	runes := []rune(strings.Join(strings.Fields(message), " "))
	if len(runes) > 240 {
		return string(runes[:240]) + "..."
	}
	return string(runes)
}
