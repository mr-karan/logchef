package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"reflect"
	"time"
)

type fieldKind int

const (
	kindValue fieldKind = iota
	kindSecret
)

type field struct {
	kind fieldKind
	// value is nil, string, bool, int64, uint64, float64, []string or
	// digestValue. A secret has no value.
	value any
	// set and sum describe a secret: whether it is non-empty, and its digest
	// for the equality check. Neither is serialized.
	set bool
	sum [sha256.Size]byte
}

type digestValue struct {
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

// Fields is a redacted snapshot of one object. Only this package builds one:
// a view names each recorded field, so a field that no view names is never in
// an event.
type Fields struct {
	order  []string
	fields map[string]field
}

// NewFields returns an empty snapshot.
func NewFields() *Fields {
	return &Fields{fields: map[string]field{}}
}

func (f *Fields) put(key string, v field) *Fields {
	if _, ok := f.fields[key]; !ok {
		f.order = append(f.order, key)
	}
	f.fields[key] = v
	return f
}

// Set records a plain value: a string, bool, integer, float, []string or
// time.Time, a named type of one of these, or a pointer to one (nil records
// null). Any other type is a bug in a view and panics, because stringifying
// an arbitrary value could leak a field that no view named.
func (f *Fields) Set(key string, value any) *Fields {
	return f.put(key, field{kind: kindValue, value: plainValue(key, reflect.ValueOf(value))})
}

func plainValue(key string, v reflect.Value) any {
	if !v.IsValid() {
		return nil
	}
	if t, ok := reflect.TypeAssert[time.Time](v); ok {
		return t.UTC().Format(time.RFC3339Nano)
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		return plainValue(key, v.Elem())
	case reflect.String:
		return v.String()
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint()
	case reflect.Float32, reflect.Float64:
		return v.Float()
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.String {
			out := make([]string, v.Len())
			for i := range out {
				out[i] = v.Index(i).String()
			}
			return out
		}
	default:
	}
	panic(fmt.Sprintf("audit: field %q has unsupported type %s", key, v.Type()))
}

// Secret records whether a credential is set and whether it changed, never
// its value.
func (f *Fields) Secret(key, value string) *Fields {
	return f.put(key, field{kind: kindSecret, set: value != "", sum: sha256.Sum256([]byte(value))})
}

// Digest records query text or SQL as its SHA-256 and byte length, so a
// literal inside it never reaches an event.
func (f *Fields) Digest(key, text string) *Fields {
	sum := sha256.Sum256([]byte(text))
	return f.put(key, field{kind: kindValue, value: digestValue{SHA256: hex.EncodeToString(sum[:]), Bytes: len(text)}})
}

// URL records the scheme and host of a URL and drops the user info, the path
// and the query string. A value that is not an absolute URL is redacted.
func (f *Fields) URL(key, raw string) *Fields {
	return f.put(key, field{kind: kindValue, value: urlOrigin(raw)})
}

func urlOrigin(raw string) string {
	if raw == "" {
		return ""
	}
	if origin, ok := absoluteURLOrigin(raw); ok {
		return origin
	}
	return redacted
}

func absoluteURLOrigin(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	return u.Scheme + "://" + u.Host, true
}
