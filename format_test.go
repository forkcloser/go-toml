package toml_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tomltest "github.com/toml-lang/toml-test/v2"

	"github.com/forkcloser/go-toml"
)

// formatted parses input, formats it, and checks the result is a fixed point.
func formatted(t *testing.T, input []byte) []byte {
	t.Helper()

	doc, err := toml.Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	once, err := doc.Format()
	if err != nil {
		t.Fatalf("Format: %v\ninput:\n%s", err, input)
	}

	again, err := toml.Parse(once)
	if err != nil {
		t.Fatalf("Parse of the formatted document: %v\nformatted:\n%s", err, once)
	}

	twice, err := again.Format()
	if err != nil {
		t.Fatalf("Format of the formatted document: %v", err)
	}

	if !bytes.Equal(once, twice) {
		t.Fatalf("Format is not idempotent:\nonce:\n%s\ntwice:\n%s", once, twice)
	}

	return once
}

// TestFormatGolden holds each layout rule to an input and the output it must give, in
// testdata/format: <name>.toml formats to <name>.formatted.toml.
func TestFormatGolden(t *testing.T) {
	t.Parallel()

	inputs, err := filepath.Glob(filepath.Join("testdata", "format", "*.toml"))
	if err != nil {
		t.Fatalf("list the golden files: %v", err)
	}

	for _, input := range inputs {
		if strings.HasSuffix(input, ".formatted.toml") {
			continue
		}

		name := strings.TrimSuffix(filepath.Base(input), ".toml")

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source, err := os.ReadFile(input)
			if err != nil {
				t.Fatalf("read: %v", err)
			}

			want, err := os.ReadFile(strings.TrimSuffix(input, ".toml") + ".formatted.toml")
			if err != nil {
				t.Fatalf("read the expected output: %v", err)
			}

			if got := formatted(t, source); !bytes.Equal(got, want) {
				t.Fatalf("Format:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// TestFormatSuite formats every valid case of toml-test: the result must mean the same
// (Format checks it) and be a fixed point.
func TestFormatSuite(t *testing.T) {
	t.Parallel()

	cases := tomltest.TestCases()

	err := fs.WalkDir(cases, "valid", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".toml") {
			return err
		}

		input, err := fs.ReadFile(cases, path)
		if err != nil {
			return err
		}

		if _, err := toml.Parse(input); err != nil {
			return nil //nolint:nilerr // a case of a TOML version this package does not read
		}

		t.Run(path, func(t *testing.T) {
			t.Parallel()

			formatted(t, input)
		})

		return nil
	})
	if err != nil {
		t.Fatalf("walk the suite: %v", err)
	}
}

// TestFormatWhitespace covers what golden files cannot hold without an editor or a
// linter undoing it: tabs, trailing spaces, CRLF line endings, and the end of input.
func TestFormatWhitespace(t *testing.T) {
	t.Parallel()

	cases := map[string]struct{ input, want string }{
		"empty":                  {"", ""},
		"blank lines only":       {"\n\n\n", ""},
		"only a comment":         {"# c", "# c\n"},
		"final newline added":    {"a = 1", "a = 1\n"},
		"trailing blank lines":   {"a = 1\n\n\n", "a = 1\n"},
		"tabs":                   {"\ta\t=\t1\t#\tc\n", "a = 1 #\tc\n"},
		"trailing spaces":        {"a = 1   \n[t]   \n", "a = 1\n\n[t]\n"},
		"crlf":                   {"a = 1\r\n# c\r\n[t]\r\n", "a = 1\n\n# c\n[t]\n"},
		"crlf in a string, kept": {"a = \"\"\"x\r\ny\"\"\"\r\n", "a = \"\"\"x\r\ny\"\"\"\n"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := string(formatted(t, []byte(tc.input))); got != tc.want {
				t.Fatalf("Format(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
