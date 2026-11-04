package toml_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gotoml "github.com/pelletier/go-toml/v2"

	"github.com/forkcloser/go-toml"
)

type editCase struct {
	name   string
	source string
	edit   func(*toml.Document) error
	want   string
}

func runEdits(t *testing.T, cases []editCase) {
	t.Helper()

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			doc, err := toml.Parse([]byte(test.source))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			if err := test.edit(doc); err != nil {
				t.Fatalf("edit: %v", err)
			}

			if got := string(doc.Bytes()); got != test.want {
				t.Fatalf("edit gave\n%s\nwant\n%s", got, test.want)
			}
		})
	}
}

// TestSet holds Set to changing the bytes of the value it replaces and nothing
// else, and to writing a new key-value where its table is.
func TestSet(t *testing.T) {
	t.Parallel()

	runEdits(t, []editCase{
		{
			name:   "replaces a value, keeping the key and comments",
			source: "# above\n'name'   =   \"old\"   # trailing\nnext = 1\n",
			edit:   func(d *toml.Document) error { return d.Set(toml.Key("name"), "new") },
			want:   "# above\n'name'   =   \"new\"   # trailing\nnext = 1\n",
		},
		{
			name:   "replaces a value in a section",
			source: "[tools.fmt]\nversion = \"v1\"\n\n[tools.query]\nversion = \"v2\"\n",
			edit:   func(d *toml.Document) error { return d.Set(toml.Key("tools", "query", "version"), "v3") },
			want:   "[tools.fmt]\nversion = \"v1\"\n\n[tools.query]\nversion = \"v3\"\n",
		},
		{
			name:   "replaces a value inside an inline table",
			source: "ext = { default = \"tar.gz\", windows = \"zip\" }\n",
			edit:   func(d *toml.Document) error { return d.Set(toml.Key("ext", "windows"), "7z") },
			want:   "ext = { default = \"tar.gz\", windows = \"7z\" }\n",
		},
		{
			name:   "replaces an array's element",
			source: "list = [1, 2, 3]\n",
			edit:   func(d *toml.Document) error { return d.Set(toml.Key("list").Index(1), 20) },
			want:   "list = [1, 20, 3]\n",
		},
		{
			name:   "adds a key after the section's last",
			source: "[a]\nx = 1\n# dangling\n\n[b]\ny = 2\n",
			edit:   func(d *toml.Document) error { return d.Set(toml.Key("a", "z"), true) },
			want:   "[a]\nx = 1\nz = true\n# dangling\n\n[b]\ny = 2\n",
		},
		{
			name:   "adds a key to the root, above the first header",
			source: "#:schema s.json\n[a]\nx = 1\n",
			edit:   func(d *toml.Document) error { return d.Set(toml.Key("top"), "t") },
			want:   "#:schema s.json\ntop = \"t\"\n\n[a]\nx = 1\n",
		},
		{
			name:   "adds a key to an empty document",
			source: "",
			edit:   func(d *toml.Document) error { return d.Set(toml.Key("a", "b"), 1) },
			want:   "a.b = 1\n",
		},
		{
			name:   "adds a key to a file without a final line ending",
			source: "a = 1",
			edit:   func(d *toml.Document) error { return d.Set(toml.Key("b"), 2) },
			want:   "a = 1\nb = 2",
		},
		{
			name:   "adds a dotted key next to its siblings",
			source: "[t]\na.x = 1\nother = 2\n",
			edit:   func(d *toml.Document) error { return d.Set(toml.Key("t", "a", "y"), 3) },
			want:   "[t]\na.x = 1\na.y = 3\nother = 2\n",
		},
		{
			name:   "adds a key to an inline table",
			source: "ext = { default = \"tar.gz\" }\n",
			edit:   func(d *toml.Document) error { return d.Set(toml.Key("ext", "windows"), "zip") },
			want:   "ext = { default = \"tar.gz\", windows = \"zip\" }\n",
		},
		{
			name:   "adds a key to a multi-line inline table",
			source: "t = {\n  a = 1,\n  b = 2\n}\n",
			edit:   func(d *toml.Document) error { return d.Set(toml.Key("t", "c"), 3) },
			want:   "t = {\n  a = 1,\n  b = 2,\n  c = 3,\n}\n",
		},
		{
			name:   "gives a table known only by its subtables a header",
			source: "[a.b]\nx = 1\n",
			edit:   func(d *toml.Document) error { return d.Set(toml.Key("a", "y"), 2) },
			want:   "[a]\ny = 2\n\n[a.b]\nx = 1\n",
		},
		{
			name:   "adds a key to an array of tables' element",
			source: "[[bin]]\nname = \"a\"\n\n[[bin]]\nname = \"b\"\n",
			edit:   func(d *toml.Document) error { return d.Set(toml.Key("bin").Index(0).Key("path"), "x") },
			want:   "[[bin]]\nname = \"a\"\npath = \"x\"\n\n[[bin]]\nname = \"b\"\n",
		},
		{
			name:   "appends to an inline array by index",
			source: "list = [1, 2]\n",
			edit:   func(d *toml.Document) error { return d.Set(toml.Key("list").Index(2), 3) },
			want:   "list = [1, 2, 3]\n",
		},
		{
			name:   "writes a table inline, in the canonical spelling",
			source: "",
			edit: func(d *toml.Document) error {
				return d.Set(
					toml.Key("source"),
					map[string]any{"repository": "acme/fmt", "id": 81234567, "tag": `v"1"`},
				)
			},
			want: "source = { id = 81234567, repository = \"acme/fmt\", tag = \"v\\\"1\\\"\" }\n",
		},
		{
			name:   "writes a struct's fields in their order",
			source: "",
			edit: func(d *toml.Document) error {
				type signer struct {
					Workflow string `toml:"workflow"`
					Runners  string `toml:"runners"`
				}

				return d.Set(toml.Key("signer"), signer{Workflow: "release.yaml", Runners: "hosted"})
			},
			want: "signer = { workflow = \"release.yaml\", runners = \"hosted\" }\n",
		},
		{
			name:   "writes floats, dates and quoted keys canonically",
			source: "",
			edit: func(d *toml.Document) error {
				return d.Set(toml.Key("darwin/arm64"), map[string]any{
					"big":  1e300,
					"one":  1.0,
					"when": time.Date(1979, time.May, 27, 7, 32, 0, 0, time.UTC),
					"day":  toml.LocalDate{Year: 2020, Month: 1, Day: 2},
				})
			},
			want: "\"darwin/arm64\" = { big = 1e+300, day = 2020-01-02, one = 1.0, when = 1979-05-27T07:32:00Z }\n",
		},
		{
			name:   "breaks a long value as the canonical layout does",
			source: "",
			edit: func(d *toml.Document) error {
				return d.Set(toml.Key("material"), []string{
					"toolchain-v2.1.0.darwin-arm64.tar.gz.minisig", "toolchain-v2.1.0.linux-amd64.tar.gz.minisig",
				})
			},
			want: "material = [\n  \"toolchain-v2.1.0.darwin-arm64.tar.gz.minisig\",\n" +
				"  \"toolchain-v2.1.0.linux-amd64.tar.gz.minisig\",\n]\n",
		},
		{
			name:   "replaces a table written by dotted keys",
			source: "a.x = 1\na.y = 2\nb = 3\n",
			edit:   func(d *toml.Document) error { return d.Set(toml.Key("a"), map[string]any{"z": 1}) },
			want:   "b = 3\na = { z = 1 }\n",
		},
	})
}

// TestSetTable holds SetTable to writing sections where they belong, and to
// replacing a table in its place.
func TestSetTable(t *testing.T) {
	t.Parallel()

	tool := map[string]any{"source": "github:acme/jq", "version": "v1.7"}

	runEdits(t, []editCase{
		{
			name:   "adds a section after its parent's",
			source: "schema = 1\n\n[tools.fmt]\nversion = \"v1\"\n\n[tools.fmt.resolved]\nx = 1\n\n[locations]\ntmp = \"t\"\n",
			edit:   func(d *toml.Document) error { return d.SetTable(toml.Key("tools", "jq"), tool) },
			want: "schema = 1\n\n[tools.fmt]\nversion = \"v1\"\n\n[tools.fmt.resolved]\nx = 1\n\n" +
				"[tools.jq]\nsource = \"github:acme/jq\"\nversion = \"v1.7\"\n\n[locations]\ntmp = \"t\"\n",
		},
		{
			name:   "adds a section at the end",
			source: "a = 1\n",
			edit:   func(d *toml.Document) error { return d.SetTable(toml.Key("tools", "jq"), tool) },
			want:   "a = 1\n\n[tools.jq]\nsource = \"github:acme/jq\"\nversion = \"v1.7\"\n",
		},
		{
			name:   "adds a section to an empty document",
			source: "",
			edit:   func(d *toml.Document) error { return d.SetTable(toml.Key("t"), map[string]any{}) },
			want:   "[t]\n",
		},
		{
			name: "replaces a section in its place, with the tables under it",
			source: "# about fmt\n[tools.fmt]\nversion = \"v1\"\n\n[tools.fmt.resolved]\nx = 1\n\n" +
				"[tools.query]\nversion = \"v2\"\n",
			edit: func(d *toml.Document) error { return d.SetTable(toml.Key("tools", "fmt"), tool) },
			want: "[tools.fmt]\nsource = \"github:acme/jq\"\nversion = \"v1.7\"\n\n[tools.query]\nversion = \"v2\"\n",
		},
		{
			name:   "replaces an array of tables' element",
			source: "[[bin]]\nname = \"a\"\n\n[[bin]]\nname = \"b\"\n\n[[bin]]\nname = \"c\"\n",
			edit:   func(d *toml.Document) error { return d.SetTable(toml.Key("bin").Index(1), map[string]any{"name": "B"}) },
			want:   "[[bin]]\nname = \"a\"\n\n[[bin]]\nname = \"B\"\n\n[[bin]]\nname = \"c\"\n",
		},
		{
			name:   "adds a subtable inside an array of tables' element",
			source: "[[bin]]\nname = \"a\"\n\n[[bin]]\nname = \"b\"\n",
			edit: func(d *toml.Document) error {
				return d.SetTable(toml.Key("bin").Index(0).Key("env"), map[string]any{"X": "1"})
			},
			want: "[[bin]]\nname = \"a\"\n\n[bin.env]\nX = \"1\"\n\n[[bin]]\nname = \"b\"\n",
		},
		{
			name:   "rewrites a table written inline as a section",
			source: "t = { a = 1 }\nu = 2\n",
			edit:   func(d *toml.Document) error { return d.SetTable(toml.Key("t"), map[string]any{"b": 2}) },
			want:   "u = 2\n\n[t]\nb = 2\n",
		},
	})
}

func TestAppend(t *testing.T) {
	t.Parallel()

	runEdits(t, []editCase{
		{
			name:   "adds an array of tables' element after the last",
			source: "[[include]]\npath = \"a\"\n\n[[include]]\npath = \"b\"\n\n[locations]\nx = 1\n",
			edit:   func(d *toml.Document) error { return d.Append(toml.Key("include"), map[string]any{"path": "c"}) },
			want:   "[[include]]\npath = \"a\"\n\n[[include]]\npath = \"b\"\n\n[[include]]\npath = \"c\"\n\n[locations]\nx = 1\n",
		},
		{
			name:   "starts an array of tables",
			source: "a = 1\n",
			edit:   func(d *toml.Document) error { return d.Append(toml.Key("include"), map[string]any{"path": "c"}) },
			want:   "a = 1\n\n[[include]]\npath = \"c\"\n",
		},
		{
			name:   "adds to an inline array",
			source: "pass-env = [\"A\", \"B\"] # vars\n",
			edit:   func(d *toml.Document) error { return d.Append(toml.Key("pass-env"), "C") },
			want:   "pass-env = [\"A\", \"B\", \"C\"] # vars\n",
		},
		{
			name:   "adds to a multi-line array",
			source: "a = [\n  1,\n  2, # two\n]\n",
			edit:   func(d *toml.Document) error { return d.Append(toml.Key("a"), 3) },
			want:   "a = [\n  1,\n  2, # two\n  3,\n]\n",
		},
		{
			name:   "adds to an empty array",
			source: "a = []\n",
			edit:   func(d *toml.Document) error { return d.Append(toml.Key("a"), 1) },
			want:   "a = [1]\n",
		},
		{
			name:   "starts an array of values",
			source: "",
			edit:   func(d *toml.Document) error { return d.Append(toml.Key("a"), "x") },
			want:   "a = [\"x\"]\n",
		},
	})
}

func TestDelete(t *testing.T) {
	t.Parallel()

	runEdits(t, []editCase{
		{
			name:   "removes a key-value with the comments above it",
			source: "a = 1\n# about b\nb = 2 # trailing\nc = 3\n",
			edit:   func(d *toml.Document) error { return d.Delete(toml.Key("b")) },
			want:   "a = 1\nc = 3\n",
		},
		{
			name: "removes a table's sections, and the tables under it",
			source: "[tools.fmt]\nv = 1\n\n# about query\n[tools.query]\nv = 2\n\n[tools.query.resolved]\nx = 1\n\n" +
				"[locations]\ny = 1\n",
			edit: func(d *toml.Document) error { return d.Delete(toml.Key("tools", "query")) },
			want: "[tools.fmt]\nv = 1\n\n[locations]\ny = 1\n",
		},
		{
			name:   "removes the last section without leaving a blank line",
			source: "[a]\nx = 1\n\n[b]\ny = 2\n",
			edit:   func(d *toml.Document) error { return d.Delete(toml.Key("b")) },
			want:   "[a]\nx = 1\n",
		},
		{
			name:   "removes an array of tables' element",
			source: "[[bin]]\nn = 1\n\n[[bin]]\nn = 2\n\n[bin.env]\nx = 1\n\n[[bin]]\nn = 3\n",
			edit:   func(d *toml.Document) error { return d.Delete(toml.Key("bin").Index(1)) },
			want:   "[[bin]]\nn = 1\n\n[[bin]]\nn = 3\n",
		},
		{
			name:   "removes every dotted key of a table",
			source: "a.x = 1\nb = 2\na.y = 3\n",
			edit:   func(d *toml.Document) error { return d.Delete(toml.Key("a")) },
			want:   "b = 2\n",
		},
		{
			name:   "removes a dotted key, the implicit table with it",
			source: "a.x = 1\nb = 2\n",
			edit:   func(d *toml.Document) error { return d.Delete(toml.Key("a", "x")) },
			want:   "b = 2\n",
		},
		{
			name:   "removes an inline table's member",
			source: "t = { a = 1, b = 2, c = 3 }\n",
			edit:   func(d *toml.Document) error { return d.Delete(toml.Key("t", "b")) },
			want:   "t = { a = 1, c = 3 }\n",
		},
		{
			name:   "removes an inline table's last member",
			source: "t = { a = 1, b = 2 }\n",
			edit:   func(d *toml.Document) error { return d.Delete(toml.Key("t", "b")) },
			want:   "t = { a = 1 }\n",
		},
		{
			name:   "removes an inline table's only member",
			source: "t = { a = 1 }\n",
			edit:   func(d *toml.Document) error { return d.Delete(toml.Key("t", "a")) },
			want:   "t = {}\n",
		},
		{
			name:   "removes a multi-line array's element with its comment",
			source: "a = [\n  1,\n  # about two\n  2, # two\n  3,\n]\n",
			edit:   func(d *toml.Document) error { return d.Delete(toml.Key("a").Index(1)) },
			want:   "a = [\n  1,\n  3,\n]\n",
		},
		{
			name:   "removes the only root key above a header",
			source: "a = 1\n\n[t]\nx = 1\n",
			edit:   func(d *toml.Document) error { return d.Delete(toml.Key("a")) },
			want:   "[t]\nx = 1\n",
		},
	})
}

// TestEditRefuses holds every edit to its errors, and to leaving the document as it
// was when it fails.
func TestEditRefuses(t *testing.T) {
	t.Parallel()

	const source = "name = \"n\"\nlist = [1]\nt = { a = 1 }\n\n[s]\nx = 1\n\n[[bin]]\nn = 1\n"

	cases := []struct {
		name string
		edit func(*toml.Document) error
		want error
	}{
		{
			"a key under a string",
			func(d *toml.Document) error { return d.Set(toml.Key("name", "x"), 1) },
			toml.ErrConflict,
		},
		{
			"a key under an array",
			func(d *toml.Document) error { return d.Set(toml.Key("list", "x"), 1) },
			toml.ErrConflict,
		},
		{
			"an index under a table",
			func(d *toml.Document) error { return d.Set(toml.Key("s").Index(0), 1) },
			toml.ErrConflict,
		},
		{
			"an element past the end",
			func(d *toml.Document) error { return d.Set(toml.Key("list").Index(5), 1) },
			toml.ErrNotFound,
		},
		{
			"a section's table set as a value",
			func(d *toml.Document) error { return d.Set(toml.Key("s"), 1) },
			toml.ErrConflict,
		},
		{"a section under an inline table", func(d *toml.Document) error {
			return d.SetTable(toml.Key("t", "u"), map[string]any{})
		}, toml.ErrConflict},
		{
			"a table that is not one",
			func(d *toml.Document) error { return d.SetTable(toml.Key("u"), 1) },
			toml.ErrConflict,
		},
		{
			"a value to an array of tables",
			func(d *toml.Document) error { return d.Append(toml.Key("bin"), 1) },
			toml.ErrConflict,
		},
		{
			"appending to a string",
			func(d *toml.Document) error { return d.Append(toml.Key("name"), 1) },
			toml.ErrConflict,
		},
		{"deleting nothing", func(d *toml.Document) error { return d.Delete(toml.Key("absent")) }, toml.ErrNotFound},
		{"nil", func(d *toml.Document) error { return d.Set(toml.Key("z"), nil) }, toml.ErrValue},
		{
			"an unsigned integer past int64",
			func(d *toml.Document) error { return d.Set(toml.Key("z"), ^uint64(0)) },
			toml.ErrValue,
		},
		{"the root", func(d *toml.Document) error { return d.Delete(toml.Path{}) }, toml.ErrConflict},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			doc, err := toml.Parse([]byte(source))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			if err := test.edit(doc); !errors.Is(err, test.want) {
				t.Fatalf("edit = %v, want an error wrapping %v", err, test.want)
			}

			if got := string(doc.Bytes()); got != source {
				t.Fatalf("a failed edit changed the document to\n%s", got)
			}
		})
	}
}

// TestEditWithSchema holds edits to the schema's order: a new key-value, member or
// section goes where the order puts it among its siblings, and a new table's keys
// are in the order.
func TestEditWithSchema(t *testing.T) {
	t.Parallel()

	schema := loadSchema(t, `{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"x-tombi-table-keys-order": "schema",
		"properties": {
			"schema": {}, "platforms": {},
			"tools": {
				"type": "object",
				"x-tombi-table-keys-order": "ascending",
				"additionalProperties": {
					"type": "object",
					"x-tombi-table-keys-order": "schema",
					"properties": { "source": {}, "version": {}, "asset": {}, "ext": {
						"type": "object", "x-tombi-table-keys-order": "ascending"
					} }
				}
			},
			"include": { "type": "array", "items": {
				"type": "object", "x-tombi-table-keys-order": "schema", "properties": { "path": {}, "exclude": {} }
			} }
		}
	}`)

	with := toml.WithSchema(schema)

	runEdits(t, []editCase{
		{
			name:   "a key-value in its place",
			source: "schema = 1\n\n[tools.fmt]\nsource = \"s\"\nasset = \"a\"\n",
			edit:   func(d *toml.Document) error { return d.Set(toml.Key("tools", "fmt", "version"), "v1", with) },
			want:   "schema = 1\n\n[tools.fmt]\nsource = \"s\"\nversion = \"v1\"\nasset = \"a\"\n",
		},
		{
			name:   "a root key above the others",
			source: "# the platforms\nplatforms = []\n",
			edit:   func(d *toml.Document) error { return d.Set(toml.Key("schema"), 1, with) },
			want:   "schema = 1\n# the platforms\nplatforms = []\n",
		},
		{
			name:   "an inline member in its place",
			source: "[tools.fmt]\next = { a = 1, c = 3 }\n",
			edit:   func(d *toml.Document) error { return d.Set(toml.Key("tools", "fmt", "ext", "b"), 2, with) },
			want:   "[tools.fmt]\next = { a = 1, b = 2, c = 3 }\n",
		},
		{
			name:   "a section among its siblings, its keys in order",
			source: "[tools.fmt]\nsource = \"f\"\n\n[tools.query]\nsource = \"q\"\n",
			edit: func(d *toml.Document) error {
				return d.SetTable(
					toml.Key("tools", "jq"),
					map[string]any{"version": "v1", "source": "j", "zz": 1},
					with,
				)
			},
			want: "[tools.fmt]\nsource = \"f\"\n\n[tools.jq]\nsource = \"j\"\nversion = \"v1\"\nzz = 1\n\n" +
				"[tools.query]\nsource = \"q\"\n",
		},
		{
			name:   "an inline value's keys in order",
			source: "[tools.fmt]\nsource = \"f\"\n",
			edit: func(d *toml.Document) error {
				return d.Set(
					toml.Key("tools", "fmt", "ext"),
					map[string]any{"windows": "zip", "default": "tar.gz"},
					with,
				)
			},
			want: "[tools.fmt]\nsource = \"f\"\next = { default = \"tar.gz\", windows = \"zip\" }\n",
		},
		{
			name:   "an array of tables' element, its keys in order",
			source: "[[include]]\npath = \"a\"\n",
			edit: func(d *toml.Document) error {
				return d.Append(toml.Key("include"), map[string]any{"exclude": []string{"x"}, "path": "b"}, with)
			},
			want: "[[include]]\npath = \"a\"\n\n[[include]]\npath = \"b\"\nexclude = [\"x\"]\n",
		},
	})
}

// changed returns the part of before an edit replaced, and what replaced it: what
// lies between the longest prefix and suffix the two share.
func changed(before, after string) (removed, added string) {
	prefix := 0
	for prefix < len(before) && prefix < len(after) && before[prefix] == after[prefix] {
		prefix++
	}

	suffix := 0
	for suffix < len(before)-prefix && suffix < len(after)-prefix &&
		before[len(before)-1-suffix] == after[len(after)-1-suffix] {
		suffix++
	}

	return before[prefix : len(before)-suffix], after[prefix : len(after)-suffix]
}

// TestEditRealFiles holds edits, on files shaped as real ones are (testdata/edit),
// to every byte outside the edit: setting any value changes its line only, removing
// one takes a single run of bytes away.
func TestEditRealFiles(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob(filepath.Join("testdata", "edit", "*.toml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("list the files: %v", err)
	}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			t.Parallel()

			source, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read: %v", err)
			}

			var decoded map[string]any
			if err := gotoml.Unmarshal(source, &decoded); err != nil {
				t.Fatalf("decode: %v", err)
			}

			var all []located

			paths(decoded, toml.Path{}, &all)

			for _, found := range all {
				path := found.path
				if isContainer(found.value) {
					continue
				}

				doc := parse(t, source)
				if err := doc.Set(path, "edited"); err != nil {
					t.Fatalf("Set(%s): %v", path, err)
				}

				if old, _ := changed(string(source), string(doc.Bytes())); strings.Contains(old, "\n") {
					t.Errorf("Set(%s) changed more than its line: %q", path, old)
				}

				doc = parse(t, source)
				if err := doc.Delete(path); err != nil {
					t.Fatalf("Delete(%s): %v", path, err)
				}

				if _, added := changed(string(source), string(doc.Bytes())); added != "" {
					t.Errorf("Delete(%s) wrote %q: it must only take bytes away, in one run", path, added)
				}
			}
		})
	}
}

func parse(t *testing.T, source []byte) *toml.Document {
	t.Helper()

	doc, err := toml.Parse(source)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	return doc
}

func isContainer(value any) bool {
	switch value.(type) {
	case map[string]any, []any:
		return true
	default:
		return false
	}
}

// TestEditsAreFormatted holds a document a program writes through edits, with a
// schema, to being what Format with the schema makes of it: a file written by a
// program and the same file formatted by tomlfmt are the same bytes.
func TestEditsAreFormatted(t *testing.T) {
	t.Parallel()

	schema, err := toml.LoadSchema(filepath.Join("testdata", "edit", "manifest.schema.json"))
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}

	with := toml.WithSchema(schema)
	doc := parse(t, nil)

	type resolved struct {
		Digest string `toml:"digest"`
		Size   int64  `toml:"size"`
	}

	edits := []func() error{
		func() error { return doc.Set(toml.Key("platforms"), []string{"darwin/arm64", "linux/amd64"}, with) },
		func() error { return doc.Set(toml.Key("schema"), 1, with) },
		func() error {
			return doc.SetTable(
				toml.Key("tools", "query"),
				map[string]any{"version": "v0.9.0", "source": "github:acme/query"},
				with,
			)
		},
		func() error {
			return doc.SetTable(toml.Key("tools", "fmt"), map[string]any{
				"provides": []string{"fmt"}, "source": "github:acme/fmt", "version": "v1.4.2",
				"ext": map[string]string{"windows": "zip", "default": "tar.gz"},
			}, with)
		},
		func() error {
			return doc.SetTable(toml.Key("tools", "fmt", "resolved"), map[string]any{
				"version":      "v1.4.2",
				"linux/amd64":  resolved{"sha256:0c9d", 5361664},
				"darwin/arm64": resolved{"sha256:4be1", 5242880},
			}, with)
		},
		func() error {
			return doc.Set(toml.Key("tools", "fmt", "asset"), "fmt_{version}_{os}_{arch}.{ext}", with)
		},
		func() error {
			return doc.Append(toml.Key("include"), map[string]any{"path": ".hermetic/base.toml"}, with)
		},
		func() error { return doc.Set(toml.Key("tools", "query", "version"), "v0.9.1", with) },
	}

	for i, apply := range edits {
		if err = apply(); err != nil {
			t.Fatalf("edit %d: %v", i, err)
		}
	}

	formatted, err := doc.Format(with)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}

	if !bytes.Equal(formatted, doc.Bytes()) {
		t.Fatalf("the edited document is not formatted:\n%s\nformatted:\n%s", doc.Bytes(), formatted)
	}

	want, err := os.ReadFile(filepath.Join("testdata", "edit", "manifest.written.toml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !bytes.Equal(doc.Bytes(), want) {
		t.Fatalf("the edits wrote\n%s\nwant\n%s", doc.Bytes(), want)
	}
}
