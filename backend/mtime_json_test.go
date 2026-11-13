package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// Nanosecond mtimes are beyond 2^53; they must travel as JSON strings so a
// JavaScript client can echo them back without rounding.
func TestFileDocMtimeIsString(t *testing.T) {
	const ns = int64(1778000000123456789)
	b, err := json.Marshal(FileDoc{Mtime: ns})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"mtime":"1778000000123456789"`) {
		t.Fatalf("mtime not encoded as string: %s", b)
	}
}
