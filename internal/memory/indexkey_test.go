package memory

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"time"
)

// adversarial covers every character class that broke the old ':'-separated
// schema, plus the new schema's own sentinel and kind bytes.
var adversarial = []string{
	"plain",
	"team:platform",
	"pkg:mypkg:Symbol",
	"srv:standards:1730000000",
	"implements:io.Reader",
	"100%",
	"with space",
	"with\ttab",
	"with\nnewline",
	"_x",
	"_x\x01domain",
	"t", "c", "g",
	"日本語",
	"emoji🙂",
	strings.Repeat("a", 512),
	"",
}

func TestIndexKey_RoundTrip(t *testing.T) {
	kinds := []byte{kindTime, kindCategory, kindTag}
	for _, dom := range []string{DomainMemories, DomainStandards, "custom-domain"} {
		for _, kind := range kinds {
			for _, val := range adversarial {
				for _, rk := range adversarial {
					if rk == "" {
						continue // a record key is never empty
					}
					key, err := encodeIndexKey(dom, kind, val, rk)
					if err != nil {
						t.Fatalf("encode(%q,%c,%q,%q): %v", dom, kind, val, rk, err)
					}
					gotDom, gotKind, gotVal, gotRK, ok := decodeIndexKey(key)
					if !ok {
						t.Fatalf("decode failed for %q/%c/%q/%q", dom, kind, val, rk)
					}
					if gotDom != dom || gotKind != kind || gotVal != val || gotRK != rk {
						t.Errorf("round-trip mismatch:\n want %q %c %q %q\n  got %q %c %q %q",
							dom, kind, val, rk, gotDom, gotKind, gotVal, gotRK)
					}
				}
			}
		}
	}
}

func TestIndexKey_RejectsNUL(t *testing.T) {
	bad := "has\x00nul"
	if _, err := encodeIndexKey(bad, kindTag, "v", "k"); err == nil {
		t.Error("expected error for NUL in domain")
	}
	if _, err := encodeIndexKey("d", kindTag, bad, "k"); err == nil {
		t.Error("expected error for NUL in value")
	}
	if _, err := encodeIndexKey("d", kindTag, "v", bad); err == nil {
		t.Error("expected error for NUL in record key")
	}
	for name, call := range map[string]func() error{
		"domain":   func() error { return ValidateIndexable(bad, "k", "c", nil) },
		"key":      func() error { return ValidateIndexable("d", bad, "c", nil) },
		"category": func() error { return ValidateIndexable("d", "k", bad, nil) },
		"tag":      func() error { return ValidateIndexable("d", "k", "c", []string{"ok", bad}) },
	} {
		if err := call(); err == nil {
			t.Errorf("ValidateIndexable: expected error for NUL in %s", name)
		}
	}
	if err := ValidateIndexable("d", "k", "c", []string{"a", "b"}); err != nil {
		t.Errorf("expected clean input to validate, got %v", err)
	}
}

// TestIndexKey_NoNULOutsideSeparators is the invariant the schema rests on.
func TestIndexKey_NoNULOutsideSeparators(t *testing.T) {
	for _, val := range adversarial {
		key, err := encodeIndexKey(DomainMemories, kindTag, val, "rk")
		if err != nil {
			t.Fatal(err)
		}
		if n := bytes.Count(key, []byte{indexSep}); n != 4 {
			t.Errorf("expected exactly 4 separators in %q, got %d", key, n)
		}
	}
}

// TestIndexKey_PrefixDoesNotBleed pins that tag "foo" cannot match "foobar".
func TestIndexKey_PrefixDoesNotBleed(t *testing.T) {
	foo, err := encodeIndexKey(DomainMemories, kindTag, "foo", "k1")
	if err != nil {
		t.Fatal(err)
	}
	foobar, err := encodeIndexKey(DomainMemories, kindTag, "foobar", "k2")
	if err != nil {
		t.Fatal(err)
	}
	p := indexPrefix(DomainMemories, kindTag, "foo")
	if !bytes.HasPrefix(foo, p) {
		t.Error("exact tag should match its own prefix")
	}
	if bytes.HasPrefix(foobar, p) {
		t.Errorf("prefix %q bled into %q", p, foobar)
	}
}

func TestIndexKey_PrefixGranularity(t *testing.T) {
	key, err := encodeIndexKey(DomainMemories, kindTag, "project:x", "k")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		prefix []byte
	}{
		{"whole domain", indexPrefix(DomainMemories, 0, "")},
		{"domain+kind", indexPrefix(DomainMemories, kindTag, "")},
		{"domain+kind+value", indexPrefix(DomainMemories, kindTag, "project:x")},
	} {
		if !bytes.HasPrefix(key, tc.prefix) {
			t.Errorf("%s: %q does not prefix %q", tc.name, tc.prefix, key)
		}
	}
	// A different domain must not be caught by either the teardown prefix or a
	// same-kind, same-value prefix.
	other, err := encodeIndexKey(DomainSessions, kindTag, "project:x", "k")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(other, indexPrefix(DomainMemories, 0, "")) {
		t.Error("domain teardown prefix leaked across domains")
	}
	if bytes.HasPrefix(other, indexPrefix(DomainMemories, kindTag, "project:x")) {
		t.Error("domain+kind+value prefix leaked across domains")
	}
	if !bytes.HasPrefix(other, indexPrefix(DomainSessions, kindTag, "project:x")) {
		t.Error("sessions key not matched by its own domain prefix")
	}
}

// TestIndexKey_TimeOrdering pins that lexicographic order equals numeric order.
// The previous schema used variable-width hex, which held only while every value
// was 16 digits — true for roughly 2006 to 2554 and false either side.
func TestIndexKey_TimeOrdering(t *testing.T) {
	instants := []time.Time{
		time.Unix(0, 0),
		time.Unix(0, 108000000000000),
		time.Unix(0, 1152939600000000000),
		time.Unix(0, 1788066000000000000),
		time.Date(2554, 7, 21, 0, 0, 0, 0, time.UTC),
	}
	var prev string
	for i, inst := range instants {
		got := encodeTimeValue(inst)
		if len(got) != 16 {
			t.Errorf("width not fixed for %v: %q is %d chars", inst, got, len(got))
		}
		if i > 0 && prev >= got {
			t.Errorf("ordering broken: %q (%v) not < %q (%v)", prev, instants[i-1], got, inst)
		}
		prev = got
	}
	if w := len(encodeTimeValue(time.Unix(0, math.MaxInt64))); w != 16 {
		t.Errorf("MaxInt64 width %d, want 16", w)
	}
	// A zero time.Time yields a negative UnixNano; it must still encode to a
	// fixed width rather than a '-'-prefixed string that misorders.
	z := encodeTimeValue(time.Time{})
	if len(z) != 16 || strings.ContainsAny(z, "-") {
		t.Errorf("zero time encoded as %q, want 16 NUL-free hex chars", z)
	}
}

func TestIndexKey_DecodeRejectsGarbage(t *testing.T) {
	for _, bad := range [][]byte{
		[]byte("not-an-index-key"),
		[]byte("_idx:domain:memories:k"),
		{'_', 'x', indexSep, 'd'},
		append([]byte("_x"), indexSep, 'd', indexSep, 't', 'o', 'o', 'l', 'o', 'n', 'g'),
	} {
		if _, _, _, _, ok := decodeIndexKey(bad); ok {
			t.Errorf("decode accepted garbage: %q", bad)
		}
	}
}

func TestIndexKey_IsIndexKey(t *testing.T) {
	k, err := encodeIndexKey(DomainMemories, kindTime, encodeTimeValue(time.Now()), "rec")
	if err != nil {
		t.Fatal(err)
	}
	if !isIndexKey(k) {
		t.Error("index key not recognised")
	}
	for _, notIdx := range []string{"rec", "pkg:a:b", "_xnot", "_idx:t:0:k"} {
		if isIndexKey([]byte(notIdx)) {
			t.Errorf("record key %q misidentified as an index key", notIdx)
		}
	}
}
