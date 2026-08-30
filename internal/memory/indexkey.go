// Package memory provides functionality for the memory subsystem.
package memory

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

// Secondary index key schema (0006-MADR).
//
//	"_x" 0x00 <domain> 0x00 <kind> 0x00 <value> 0x00 <record key>  ->  (empty)
//
// Domain leads every key, so each query is a single prefix scan and an entire
// domain's indexes can be dropped with one prefix sweep. The value is empty:
// the record key is already the final component, so storing it again wastes
// space and forces a value read.
//
// 0x00 separates components and may not appear inside one. Components are
// rejected at the write boundary rather than escaped — rejection is verifiable,
// escaping is another parser to get wrong.
const (
	indexSentinel = "_x"

	kindTime     byte = 't'
	kindCategory byte = 'c'
	kindTag      byte = 'g'
)

const indexSep = 0x00

// errIndexNUL reports a component that cannot be encoded.
type errIndexNUL struct {
	component string
	value     string
}

func (e *errIndexNUL) Error() string {
	return fmt.Sprintf("index %s %q contains a NUL byte, which is reserved as the index key separator", e.component, e.value)
}

// validateIndexComponent rejects any value that would corrupt a key.
func validateIndexComponent(component, value string) error {
	if strings.IndexByte(value, indexSep) >= 0 {
		return &errIndexNUL{component: component, value: value}
	}
	return nil
}

// ValidateIndexable checks every component of a record that reaches an index
// key. Callers must invoke it before any write so a rejected record leaves no
// partial index behind.
func ValidateIndexable(domain, key, category string, tags []string) error {
	if err := validateIndexComponent("domain", domain); err != nil {
		return err
	}
	if err := validateIndexComponent("key", key); err != nil {
		return err
	}
	if err := validateIndexComponent("category", category); err != nil {
		return err
	}
	for _, t := range tags {
		if err := validateIndexComponent("tag", t); err != nil {
			return err
		}
	}
	return nil
}

// encodeTimeValue renders an instant as a fixed-width, NUL-free index component.
//
// Width is fixed at 16 hex digits so lexicographic order equals numeric order
// across the whole uint64 range, rather than only while the current epoch
// happens to produce equal-length output. Raw big-endian binary would be more
// compact but cannot be used: every realistic UnixNano contains NUL bytes, which
// collide with the separator.
func encodeTimeValue(t time.Time) string {
	return fmt.Sprintf("%016x", uint64(t.UnixNano())) //nolint:gosec // wraparound is intentional; ordering is preserved within an epoch
}

// encodeIndexKey builds a full index entry key.
func encodeIndexKey(domain string, kind byte, value, recordKey string) ([]byte, error) {
	if err := validateIndexComponent("domain", domain); err != nil {
		return nil, err
	}
	if err := validateIndexComponent("value", value); err != nil {
		return nil, err
	}
	if err := validateIndexComponent("key", recordKey); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString(indexSentinel)
	b.WriteByte(indexSep)
	b.WriteString(domain)
	b.WriteByte(indexSep)
	b.WriteByte(kind)
	b.WriteByte(indexSep)
	b.WriteString(value)
	b.WriteByte(indexSep)
	b.WriteString(recordKey)
	return b.Bytes(), nil
}

// indexPrefix builds a scan prefix.
//
// Passing a non-empty value appends the trailing separator so that a scan for
// tag "foo" cannot also match "foobar". An empty value scans every value of that
// kind; an empty kind (0) scans every kind in the domain, which is what makes
// domain teardown a single sweep.
func indexPrefix(domain string, kind byte, value string) []byte {
	var b bytes.Buffer
	b.WriteString(indexSentinel)
	b.WriteByte(indexSep)
	b.WriteString(domain)
	b.WriteByte(indexSep)
	if kind == 0 {
		return b.Bytes()
	}
	b.WriteByte(kind)
	b.WriteByte(indexSep)
	if value == "" {
		return b.Bytes()
	}
	b.WriteString(value)
	b.WriteByte(indexSep)
	return b.Bytes()
}

// indexSentinelPrefix matches every index key in the store.
func indexSentinelPrefix() []byte {
	return []byte{indexSentinel[0], indexSentinel[1], indexSep}
}

// isIndexKey reports whether a raw Badger key belongs to the secondary index.
func isIndexKey(key []byte) bool {
	return bytes.HasPrefix(key, indexSentinelPrefix())
}

// decodeIndexKey splits an index key back into its components.
func decodeIndexKey(key []byte) (domain string, kind byte, value, recordKey string, ok bool) {
	if !isIndexKey(key) {
		return "", 0, "", "", false
	}
	parts := bytes.SplitN(key, []byte{indexSep}, 5)
	if len(parts) != 5 {
		return "", 0, "", "", false
	}
	if len(parts[2]) != 1 {
		return "", 0, "", "", false
	}
	return string(parts[1]), parts[2][0], string(parts[3]), string(parts[4]), true
}
