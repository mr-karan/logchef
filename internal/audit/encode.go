package audit

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"unicode/utf8"
)

const (
	redacted = "[redacted]"
	// maxStringBytes bounds one string value and maxQueryTextBytes one query
	// text; maxPayloadBytes bounds changes and detail together.
	maxStringBytes    = 1024
	maxQueryTextBytes = 8 * 1024
	maxPayloadBytes   = 16 * 1024
	truncatedKey      = "truncated"
)

// deniedKeyParts mark a key whose value is a credential unless Secret set it.
var deniedKeyParts = []string{
	"password", "passwd", "secret", "token", "api_key", "authorization",
	"cookie", "hash", "headers", "credential", "dsn",
}

func deniedKey(key string) bool {
	key = strings.ToLower(key)
	for _, part := range deniedKeyParts {
		if strings.Contains(key, part) {
			return true
		}
	}
	return false
}

// encoded is the serialized changes and detail of one event, and the number
// of values the serializer replaced.
type encoded struct {
	changes, detail json.RawMessage
	redactions      int
}

type entry struct {
	key string
	raw []byte
}

// encodeEvent runs the serializer that every event passes: it diffs Before
// and After, replaces denied keys and URLs, truncates strings, and drops the
// keys that do not fit the payload cap.
func encodeEvent(e Event) encoded {
	var enc encoded
	changes := enc.diff(e.Before, e.After)
	var detail []entry
	if e.Detail != nil {
		for _, key := range e.Detail.order {
			detail = append(detail, entry{key, enc.marshal(enc.detailValue(key, e.Detail.fields[key]))})
		}
	}

	// Two pairs of braces, plus room for the truncation marker.
	budget := maxPayloadBytes - 4 - len(`,"truncated":true`)
	truncated := false
	keep := func(entries []entry) []entry {
		var kept []entry
		for _, en := range entries {
			// The encoded key, a colon, the value and a comma.
			size := len(jsonString(en.key)) + 1 + len(en.raw) + 1
			if truncated || size > budget {
				truncated = true
				continue
			}
			budget -= size
			kept = append(kept, en)
		}
		return kept
	}
	changes = keep(changes)
	detail = keep(detail)
	if truncated {
		detail = append(detail, entry{truncatedKey, []byte("true")})
	}
	enc.changes = object(changes)
	enc.detail = object(detail)
	return enc
}

func (enc *encoded) diff(before, after *Fields) []entry {
	if before == nil {
		before = NewFields()
	}
	if after == nil {
		after = NewFields()
	}
	keys := append([]string(nil), before.order...)
	for _, key := range after.order {
		if _, ok := before.fields[key]; !ok {
			keys = append(keys, key)
		}
	}

	var out []entry
	for _, key := range keys {
		b, inBefore := before.fields[key]
		a, inAfter := after.fields[key]
		if b.kind == kindSecret || a.kind == kindSecret {
			// An absent secret and an empty one are the same: not set.
			if b.set == a.set && b.sum == a.sum || !b.set && !a.set {
				continue
			}
			out = append(out, entry{key, []byte(`{"secret":true,"changed":true}`)})
			continue
		}
		if inBefore && inAfter && reflect.DeepEqual(b.value, a.value) {
			continue
		}
		change := map[string]any{}
		if inBefore {
			change["from"] = enc.value(key, b.value)
		}
		if inAfter {
			change["to"] = enc.value(key, a.value)
		}
		out = append(out, entry{key, enc.marshal(change)})
	}
	return out
}

func (enc *encoded) detailValue(key string, f field) any {
	if f.kind == kindSecret {
		return map[string]bool{"secret": true}
	}
	return enc.value(key, f.value)
}

// value applies the serializer rules to one recorded value.
func (enc *encoded) value(key string, v any) any {
	if v == nil {
		return nil
	}
	if deniedKey(key) {
		enc.redactions++
		return redacted
	}
	switch v := v.(type) {
	case queryText:
		return truncateTo(string(v), maxQueryTextBytes)
	case string:
		return enc.text(v)
	case []string:
		out := make([]string, len(v))
		for i, s := range v {
			out[i] = enc.text(s)
		}
		return out
	}
	return v
}

func (enc *encoded) text(s string) string {
	if origin, ok := absoluteURLOrigin(s); ok && origin != s {
		enc.redactions++
		s = origin
	}
	return truncate(s)
}

func truncate(s string) string {
	return truncateTo(s, maxStringBytes)
}

func truncateTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n - len("...")
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

func (enc *encoded) marshal(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		// Values are plain types built by this package. A float NaN or Inf is
		// the one thing JSON cannot hold; drop the value, not the event.
		enc.redactions++
		return []byte(`"` + redacted + `"`)
	}
	return raw
}

// jsonString encodes s as a JSON string. Encoding a string cannot fail.
func jsonString(s string) []byte {
	raw, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return raw
}

func object(entries []entry) json.RawMessage {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, en := range entries {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(jsonString(en.key))
		buf.WriteByte(':')
		buf.Write(en.raw)
	}
	buf.WriteByte('}')
	return buf.Bytes()
}
