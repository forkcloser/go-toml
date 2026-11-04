package toml_test

import (
	"bytes"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	gotoml "github.com/pelletier/go-toml/v2"
	tomltest "github.com/toml-lang/toml-test/v2"

	"github.com/forkcloser/go-toml"
)

// seed adds every TOML case of toml-test to the corpus.
func seed(f *testing.F) {
	f.Helper()

	cases := tomltest.TestCases()

	err := fs.WalkDir(cases, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".toml") || strings.HasPrefix(path, "encoder") {
			return err
		}

		data, err := fs.ReadFile(cases, path)
		if err == nil {
			f.Add(data)
		}

		return err
	})
	if err != nil {
		f.Fatalf("seed the corpus from toml-test: %v", err)
	}
}

// FuzzParse holds the parser to the decoder it delegates validity to: a document the
// decoder accepts parses, and a parsed document's bytes are its input.
func FuzzParse(f *testing.F) {
	seed(f)

	f.Fuzz(func(t *testing.T, input []byte) {
		var reference map[string]any

		accepted := gotoml.Unmarshal(input, &reference) == nil

		doc, err := toml.Parse(input)
		if accepted != (err == nil) {
			t.Fatalf("go-toml accepted %t, Parse returned %v, for %q", accepted, err, input)
		}

		if err == nil && !bytes.Equal(doc.Bytes(), input) {
			t.Fatalf("the round trip changed the document:\n%q\nbecame\n%q", input, doc.Bytes())
		}
	})
}

// fuzzSchema orders every table's keys, descending, and wants every value a table:
// it reorders what is written in order, and every other value breaks it.
func fuzzSchema(f *testing.F) *toml.Schema {
	f.Helper()

	schema, err := toml.LoadSchema(filepath.Join("testdata", "fuzz.schema.json"))
	if err != nil {
		f.Fatalf("LoadSchema: %v", err)
	}

	return schema
}

// FuzzFormat holds Format, on every document go-toml accepts, with and without a
// schema ordering keys, to succeeding (Format checks the meaning is unchanged) and
// to being idempotent.
func FuzzFormat(f *testing.F) {
	seed(f)

	schema := fuzzSchema(f)

	f.Fuzz(func(t *testing.T, input []byte) {
		doc, err := toml.Parse(input)
		if err != nil {
			return
		}

		for _, opts := range [][]toml.Option{nil, {toml.WithSchema(schema)}} {
			once, err := doc.Format(opts...)
			if err != nil {
				t.Fatalf("Format(%q) with %d options: %v", input, len(opts), err)
			}

			again, err := toml.Parse(once)
			if err != nil {
				t.Fatalf("Parse of the formatted %q: %v", once, err)
			}

			twice, err := again.Format(opts...)
			if err != nil {
				t.Fatalf("Format of the formatted %q: %v", once, err)
			}

			if !bytes.Equal(once, twice) {
				t.Fatalf("Format with %d options is not idempotent on %q:\n%q\nthen\n%q", len(opts), input, once, twice)
			}
		}
	})
}

// FuzzValidate holds Validate, on every document go-toml accepts, to placing each
// violation in the document: every value that is not a table breaks the schema.
func FuzzValidate(f *testing.F) {
	seed(f)

	schema := fuzzSchema(f)

	f.Fuzz(func(t *testing.T, input []byte) {
		doc, err := toml.Parse(input)
		if err != nil {
			return
		}

		err = doc.Validate(schema)
		if err == nil {
			return
		}

		violations, ok := errors.AsType[toml.Violations](err)
		if !ok {
			t.Fatalf("Validate(%q) = %v, want violations", input, err)
		}

		lines := bytes.Count(input, []byte("\n")) + 1

		for _, violation := range violations {
			if violation.Line < 1 || violation.Line > lines || violation.Column < 1 {
				t.Fatalf("Validate(%q) placed %v outside the document", input, violation)
			}
		}
	})
}
