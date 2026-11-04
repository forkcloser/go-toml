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

// FuzzParse holds the parser to the decoder it delegates validity to: a document the
// decoder accepts parses, and a parsed document's bytes are its input.
func FuzzParse(f *testing.F) {
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
