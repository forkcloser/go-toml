package toml_test

import (
	"bytes"
	"io/fs"
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

// FuzzFormat holds Format, on every document go-toml accepts, to succeeding (Format
// checks the meaning is unchanged) and to being idempotent.
func FuzzFormat(f *testing.F) {
	seed(f)

	f.Fuzz(func(t *testing.T, input []byte) {
		doc, err := toml.Parse(input)
		if err != nil {
			return
		}

		once, err := doc.Format()
		if err != nil {
			t.Fatalf("Format(%q): %v", input, err)
		}

		again, err := toml.Parse(once)
		if err != nil {
			t.Fatalf("Parse of the formatted %q: %v", once, err)
		}

		twice, err := again.Format()
		if err != nil {
			t.Fatalf("Format of the formatted %q: %v", once, err)
		}

		if !bytes.Equal(once, twice) {
			t.Fatalf("Format is not idempotent on %q:\n%q\nthen\n%q", input, once, twice)
		}
	})
}
