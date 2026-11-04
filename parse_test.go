package toml_test

import (
	"errors"
	"testing"

	"github.com/forkcloser/go-toml"
)

// TestParseRoundTripAtEndOfInput covers what toml-test's valid cases do not: a
// document that ends in a value, with no line ending after it.
func TestParseRoundTripAtEndOfInput(t *testing.T) {
	t.Parallel()

	const input = "a = 1"

	doc, err := toml.Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got := string(doc.Bytes()); got != input {
		t.Fatalf("the round trip changed the document: %q became %q", input, got)
	}
}

func TestParseKeepsItsOwnCopy(t *testing.T) {
	t.Parallel()

	input := []byte("a = 1\n")

	doc, err := toml.Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	input[4] = '2'

	if got := string(doc.Bytes()); got != "a = 1\n" {
		t.Fatalf("the document changed with the caller's slice: %q", got)
	}
}

func TestParseRefusesInvalidInput(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		input        string
		line, column int
	}{
		"duplicate key":        {"a = 1\na = 2\n", 2, 1},
		"missing value":        {"a =\n", 1, 4},
		"unterminated string":  {"a = \"x\n", 1, 7},
		"bare carriage return": {"a = 1\r", 1, 6},
		"table redefined":      {"[t]\n[t]\n", 2, 2},
		"column in characters": {"s = \"ééé\" x\n", 1, 11},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := toml.Parse([]byte(tc.input))
			if !errors.Is(err, toml.ErrInvalid) {
				t.Fatalf("Parse(%q) = %v, want an error wrapping ErrInvalid", tc.input, err)
			}

			var located *toml.Error
			if !errors.As(err, &located) {
				t.Fatalf("Parse(%q) = %v, want a *toml.Error", tc.input, err)
			}

			if located.Line != tc.line || located.Column != tc.column {
				t.Fatalf("Parse(%q) at line %d, column %d; want line %d, column %d",
					tc.input, located.Line, located.Column, tc.line, tc.column)
			}
		})
	}
}
