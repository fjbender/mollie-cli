// Package input provides utilities for reading structured JSON from stdin.
//
// When stdin is a TTY (interactive use), all Read* functions return nil so
// the caller falls through to its normal flag / config-default path without
// any change in behaviour.
//
// When stdin is redirected (e.g. mollie payments create < request.json), the
// package reads and parses a single JSON object.  Each top-level key is
// exposed as a json.RawMessage so the caller can extract only the fields it
// cares about using the typed helper functions (Str, Bool, Int64, Amount, …).
//
// # Precedence — from highest to lowest authority
//
//  1. Explicit CLI flags   (cmd.Flags().Changed returns true)
//  2. stdin JSON           (ReadStdin returns a non-nil map)
//  3. Stored config defaults (applyCreateDefaults)
//  4. Zero / empty value
//
// # Milestone-2 note
//
// A future bulk-create milestone will allow piping a JSON *array* of request
// objects.  ReadStdin currently rejects arrays with an actionable error so
// users get a clear message instead of a confusing parse failure.  When bulk
// is implemented the array branch in ReadStdin will be expanded into a
// separate ReadStdinBatch function; single-object callers will not need to
// change.
package input

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/mattn/go-isatty"
)

// ReadStdin reads a single JSON object from stdin when stdin is not a terminal.
//
// It returns (nil, nil) when:
//   - stdin is a TTY (interactive use), or
//   - stdin is empty after trimming whitespace.
//
// It returns (map, nil) when stdin contained a valid JSON object.
//
// It returns (nil, err) on I/O or parse errors, or when stdin contains a JSON
// array (reserved for the upcoming bulk-create milestone).
func ReadStdin() (map[string]json.RawMessage, error) {
	if isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd()) {
		return nil, nil
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("reading stdin: %w", err)
	}

	// Skip leading whitespace to find the first meaningful byte.
	first := firstNonSpace(data)
	if first == 0 {
		return nil, nil // empty input → behave as if no stdin was provided
	}

	if first == '[' {
		return nil, fmt.Errorf(
			"JSON array detected on stdin: bulk create is not yet supported; " +
				"pipe a single JSON object instead",
		)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing JSON from stdin: %w", err)
	}
	return m, nil
}

// firstNonSpace returns the first non-whitespace byte in data, or 0 if data
// contains only whitespace (or is empty).
func firstNonSpace(data []byte) byte {
	for _, b := range data {
		if b != ' ' && b != '\t' && b != '\n' && b != '\r' {
			return b
		}
	}
	return 0
}

// Str extracts a string value from a JSON field map.
// Returns ("", false) when m is nil, when key is absent, or when the value is
// not a JSON string.
func Str(m map[string]json.RawMessage, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	raw, ok := m[key]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// Bool extracts a bool value from a JSON field map.
// Returns (false, false) when m is nil, when key is absent, or when the value
// is not a JSON boolean.
func Bool(m map[string]json.RawMessage, key string) (bool, bool) {
	if m == nil {
		return false, false
	}
	raw, ok := m[key]
	if !ok {
		return false, false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false, false
	}
	return b, true
}

// Int64 extracts an int64 value from a JSON field map.
// Returns (0, false) when m is nil, when key is absent, or when the value is
// not a JSON number representable as int64.
func Int64(m map[string]json.RawMessage, key string) (int64, bool) {
	if m == nil {
		return 0, false
	}
	raw, ok := m[key]
	if !ok {
		return 0, false
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, false
	}
	return n, true
}

// Amount extracts the "value" and "currency" sub-fields from a nested Mollie
// amount object within a JSON field map, e.g.:
//
//	{ "amount": { "value": "10.00", "currency": "EUR" } }
//
// Returns ("", "", false) when m is nil, when key is absent, or when the
// nested object cannot be parsed.
func Amount(m map[string]json.RawMessage, key string) (value, currency string, ok bool) {
	if m == nil {
		return "", "", false
	}
	raw, exists := m[key]
	if !exists {
		return "", "", false
	}
	var a struct {
		Value    string `json:"value"`
		Currency string `json:"currency"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", "", false
	}
	return a.Value, a.Currency, true
}

// RawJSON returns the raw JSON bytes for a key as a string.
// This is particularly useful for metadata fields, which the CLI stores as a
// raw JSON string before passing to parseMetadata.
// Returns ("", false) when m is nil or when key is absent.
func RawJSON(m map[string]json.RawMessage, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	raw, ok := m[key]
	if !ok {
		return "", false
	}
	return string(raw), true
}
