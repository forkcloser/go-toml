package toml_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forkcloser/go-toml"
)

// TestOrderGolden holds key ordering to an input and the output it must give, in
// testdata/order: <name>.toml, with <name>.schema.json, formats to
// <name>.formatted.toml. Every input is valid against its schema.
func TestOrderGolden(t *testing.T) {
	t.Parallel()

	schemas, err := filepath.Glob(filepath.Join("testdata", "order", "*.schema.json"))
	if err != nil || len(schemas) == 0 {
		t.Fatalf("list the golden files: %v", err)
	}

	for _, schemaPath := range schemas {
		base := strings.TrimSuffix(schemaPath, ".schema.json")

		t.Run(filepath.Base(base), func(t *testing.T) {
			t.Parallel()

			schema, err := toml.LoadSchema(schemaPath)
			if err != nil {
				t.Fatalf("LoadSchema: %v", err)
			}

			source, err := os.ReadFile(base + ".toml")
			if err != nil {
				t.Fatalf("read: %v", err)
			}

			want, err := os.ReadFile(base + ".formatted.toml")
			if err != nil {
				t.Fatalf("read the expected output: %v", err)
			}

			doc, err := toml.Parse(source)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			if err := doc.Validate(schema); err != nil {
				t.Fatalf("Validate: %v", err)
			}

			if got := formatted(t, source, toml.WithSchema(schema)); !bytes.Equal(got, want) {
				t.Fatalf("Format:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// writeSchema writes a schema into a directory of its own and returns its path.
func writeSchema(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write the schema: %v", err)
	}

	return path
}

func loadSchema(t *testing.T, content string) *toml.Schema {
	t.Helper()

	schema, err := toml.LoadSchema(writeSchema(t, content))
	if err != nil {
		t.Fatalf("LoadSchema: %v", err)
	}

	return schema
}

// violation is what a test expects of one: where, and a part of its message.
type violation struct {
	line, column int
	key          string
	msg          string
}

// TestValidate holds each violation to the place that writes the value: the key of
// a key-value, a table's header, an array's element, a table defined only through
// the keys it holds at its first one, and the document's start for what is missing
// from the root.
func TestValidate(t *testing.T) {
	t.Parallel()

	schema := loadSchema(t, `{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"required": ["name"],
		"properties": {
			"name": { "type": "string" },
			"port": { "type": "integer", "maximum": 65535 },
			"ratio": { "type": "number" },
			"when": { "type": "string", "pattern": "^1979-05-27T07:32:00" },
			"day": { "type": "string", "pattern": "^\\d{4}-\\d{2}-\\d{2}$" },
			"server": {
				"type": "object",
				"required": ["host"],
				"properties": { "host": { "type": "string" } },
				"additionalProperties": false
			},
			"tags": { "type": "array", "items": { "type": "string" } },
			"bin": { "type": "array", "items": { "type": "object", "properties": { "path": { "type": "string" } } } },
			"dep": { "anyOf": [{ "type": "string" }, { "type": "object", "required": ["version"] }] }
		}
	}`)

	cases := []struct {
		name   string
		source string
		want   []violation
	}{
		{name: "valid", source: "name = \"n\"\nport = 80\nratio = 1\nwhen = 1979-05-27T07:32:00Z\nday = 1979-05-27\n"},
		{
			name:   "missing from the root",
			source: "port = 80\n",
			want:   []violation{{1, 1, "", "missing property 'name'"}},
		},
		{
			name:   "a key-value",
			source: "name = \"n\"\n  port = 70000\n",
			want:   []violation{{2, 3, "port", "maximum"}},
		},
		{
			name:   "an inline table's key",
			source: "name = \"n\"\nserver = { host = 1 }\n",
			want:   []violation{{2, 12, "server.host", "want string"}},
		},
		{
			name:   "a header",
			source: "name = \"n\"\n[server]\nport = 1\n",
			want: []violation{
				{2, 1, "server", "missing property 'host'"},
				{3, 1, "server.port", "not allowed"},
			},
		},
		{
			name:   "a dotted key's part",
			source: "name = \"n\"\nserver.host = true\n",
			want:   []violation{{2, 8, "server.host", "want string"}},
		},
		{
			name:   "an array's element",
			source: "name = \"n\"\ntags = [\"a\", 2]\n",
			want:   []violation{{2, 14, "tags.1", "want string"}},
		},
		{
			name:   "an array of tables' element",
			source: "name = \"n\"\n[[bin]]\npath = \"a\"\n[[bin]]\npath = 1\n",
			want:   []violation{{5, 1, "bin.1.path", "want string"}},
		},
		{
			name:   "a table defined by what it holds",
			source: "name = \"n\"\n[server.inner]\n",
			want: []violation{
				{2, 1, "server", "missing property 'host'"},
				{2, 1, "server.inner", "not allowed"},
			},
		},
		{
			name:   "anyOf says each branch",
			source: "name = \"n\"\ndep = { path = \"x\" }\n",
			want:   []violation{{2, 1, "dep", "'anyOf' failed: got object, want string; missing property 'version'"}},
		},
		{
			name:   "a date-time is a string",
			source: "name = \"n\"\nwhen = 2000-01-01T00:00:00Z\nday = 07:32:00\n",
			want: []violation{
				{2, 1, "when", "does not match pattern"},
				{3, 1, "day", "does not match pattern"},
			},
		},
		{
			name:   "nan is not a JSON number",
			source: "name = \"n\"\nratio = nan\n",
			want:   []violation{{2, 1, "ratio", "invalid jsonType"}},
		},
		{
			name:   "an escaped key",
			source: "name = \"n\"\nserver = { \"ho\\u0073t\" = 1 }\n",
			want:   []violation{{2, 12, "server.host", "want string"}},
		},
		{
			name:   "columns count characters",
			source: "name = \"é\"\nserver = { \"é\" = 1, host = \"h\" }\n",
			want:   []violation{{2, 12, "server.é", "not allowed"}},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			doc, err := toml.Parse([]byte(test.source))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			err = doc.Validate(schema)
			if test.want == nil {
				if err != nil {
					t.Fatalf("Validate = %v, want nil", err)
				}

				return
			}

			violations, ok := errors.AsType[toml.Violations](err)
			if !ok || !errors.Is(err, toml.ErrViolation) {
				t.Fatalf("Validate = %v, want violations", err)
			}

			if len(violations) != len(test.want) {
				t.Fatalf("Validate gave %d violations, want %d:\n%v", len(violations), len(test.want), err)
			}

			for i, want := range test.want {
				got := violations[i]
				if got.Line != want.line || got.Column != want.column || strings.Join(got.Key, ".") != want.key ||
					!strings.Contains(got.Msg, want.msg) {
					t.Errorf("violation %d = %v, want line %d, column %d, key %q, saying %q",
						i, got, want.line, want.column, want.key, want.msg)
				}
			}
		})
	}
}

// TestLoadSchemaRefuses holds LoadSchema to errors that say what is wrong, and to
// never fetching a schema.
func TestLoadSchemaRefuses(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"not JSON":            `{`,
		"not a schema":        `{"type": 1}`,
		"a remote reference":  `{"$ref": "https://json.schemastore.org/cargo.json"}`,
		"an unknown strategy": `{"x-tombi-table-keys-order": "random"}`,
		"an unknown group":    `{"x-tombi-table-keys-order": {"keys": "ascending"}}`,
		"schema order outside properties": `{
			"x-tombi-table-keys-order": {"additionalProperties": "schema"}
		}`,
		"an annotation that is neither": `{"x-tombi-table-keys-order": 1}`,
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := toml.LoadSchema(writeSchema(t, content)); !errors.Is(err, toml.ErrSchema) {
				t.Fatalf("LoadSchema = %v, want an error wrapping ErrSchema", err)
			}
		})
	}

	t.Run("a missing file", func(t *testing.T) {
		t.Parallel()

		_, err := toml.LoadSchema(filepath.Join(t.TempDir(), "absent.json"))
		if !errors.Is(err, fs.ErrNotExist) || errors.Is(err, toml.ErrSchema) {
			t.Fatalf("LoadSchema = %v, want the error reading the file", err)
		}
	})

	t.Run("a missing referenced file", func(t *testing.T) {
		t.Parallel()

		_, err := toml.LoadSchema(writeSchema(t, `{"$ref": "absent.json"}`))
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("LoadSchema = %v, want the error reading the file", err)
		}
	})
}

// TestSchemaDirective holds the directive to the document's first line, and to its
// exact spelling.
func TestSchemaDirective(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"#:schema ./a.json\n":      "./a.json",
		"#:schema  a b.json  \n":   "a b.json",
		"#:schema\n":               "",
		"#:schemata a.json\n":      "",
		"a = 1\n#:schema a.json\n": "",
		" #:schema a.json\n":       "",
	}

	for source, want := range cases {
		doc, err := toml.Parse([]byte(source))
		if err != nil {
			t.Fatalf("Parse(%q): %v", source, err)
		}

		if got, ok := doc.SchemaDirective(); got != want || ok != (want != "") {
			t.Errorf("SchemaDirective of %q = %q, %t, want %q", source, got, ok, want)
		}
	}
}
