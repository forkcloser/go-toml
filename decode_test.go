package toml_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/forkcloser/go-toml"
)

type inner struct {
	On bool `toml:"on"`
}

type target struct {
	Name  string   `toml:"name"`
	Count int8     `toml:"count"`
	Ratio float64  `toml:"ratio"`
	Tags  []string `toml:"tags"`
	Inner inner    `toml:"inner"`
}

func TestDecode(t *testing.T) {
	t.Parallel()

	src := []byte("name = \"x\"\ncount = 3\nratio = 1\ntags = [\"a\", \"b\"]\n[inner]\non = true\n")

	var got target
	if err := toml.Decode(src, &got); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if got.Name != "x" || got.Count != 3 || got.Ratio != 1 || !slices.Equal(got.Tags, []string{"a", "b"}) ||
		!got.Inner.On {
		t.Fatalf("Decode = %+v", got)
	}

	doc, err := toml.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var fromDocument target
	if err := doc.Decode(&fromDocument); err != nil {
		t.Fatalf("Document.Decode: %v", err)
	}

	if fromDocument.Name != got.Name || fromDocument.Count != got.Count || !slices.Equal(fromDocument.Tags, got.Tags) {
		t.Fatalf("Document.Decode = %+v, Decode = %+v", fromDocument, got)
	}
}

func TestDecodeRefuses(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		input        string
		sentinel     error
		line, column int
		key          []string
	}{
		"unknown key":          {"name = \"x\"\nextra = 1\n", toml.ErrDecode, 2, 1, []string{"extra"}},
		"unknown nested key":   {"[inner]\noff = true\n", toml.ErrDecode, 2, 1, []string{"inner", "off"}},
		"integer into string":  {"name = 1\n", toml.ErrDecode, 1, 8, []string{"name"}},
		"integer out of range": {"count = 300\n", toml.ErrDecode, 1, 9, []string{"count"}},
		"string into bool":     {"[inner]\non = \"yes\"\n", toml.ErrDecode, 2, 6, []string{"inner", "on"}},
		"invalid TOML":         {"name = \n", toml.ErrInvalid, 1, 8, nil},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var got target

			err := toml.Decode([]byte(tc.input), &got)
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("Decode(%q) = %v, want an error wrapping %v", tc.input, err, tc.sentinel)
			}

			var located *toml.Error
			if !errors.As(err, &located) {
				t.Fatalf("Decode(%q) = %v, want a *toml.Error", tc.input, err)
			}

			if located.Line != tc.line || located.Column != tc.column || !slices.Equal(located.Key, tc.key) {
				t.Fatalf("Decode(%q) at line %d, column %d, key %v; want line %d, column %d, key %v",
					tc.input, located.Line, located.Column, located.Key, tc.line, tc.column, tc.key)
			}
		})
	}
}
