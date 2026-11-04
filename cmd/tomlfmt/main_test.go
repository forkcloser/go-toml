package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// asCommand, set in the environment, makes the test binary run as tomlfmt: the
// tests drive the command through a process, as a user does.
const asCommand = "TOMLFMT_TEST_AS_COMMAND"

func TestMain(m *testing.M) {
	if os.Getenv(asCommand) == "1" {
		main()
	}

	m.Run()
}

const (
	unformatted = "a=1\n[t]\nb  =  2\n"
	canonical   = "a = 1\n\n[t]\nb = 2\n"
)

type result struct {
	code           int
	stdout, stderr string
}

func tomlfmt(t *testing.T, stdin string, args ...string) result {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), os.Args[0], args...)

	cmd.Env = append(os.Environ(), asCommand+"=1")
	cmd.Stdin = strings.NewReader(stdin)

	var stdout, stderr bytes.Buffer

	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()

	code := 0

	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("run tomlfmt: %v", err)
	}

	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func writeFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(content)
}

func TestStandardInput(t *testing.T) {
	t.Parallel()

	got := tomlfmt(t, unformatted)
	if got.code != exitDone || got.stdout != canonical || got.stderr != "" {
		t.Fatalf("tomlfmt < input = %+v, want exit 0 and %q", got, canonical)
	}
}

func TestFileInPlace(t *testing.T) {
	t.Parallel()

	// Group-writable: the bit a common umask (022) clears from a file made anew.
	path := writeFile(t, unformatted, 0o664)

	if err := os.Chmod(path, 0o664); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}

	if got := tomlfmt(t, "", path); got.code != exitDone || got.stdout != "" || got.stderr != "" {
		t.Fatalf("tomlfmt %s = %+v, want exit 0 and no output", path, got)
	}

	if content := readFile(t, path); content != canonical {
		t.Fatalf("the file holds %q, want %q", content, canonical)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}

	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o664 {
		t.Fatalf("the file's mode is %v, want it kept at 0664", info.Mode().Perm())
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("the directory holds %d entries (%v), want the file alone", len(entries), err)
	}
}

func TestCheck(t *testing.T) {
	t.Parallel()

	path := writeFile(t, unformatted, 0o644)

	got := tomlfmt(t, "", "--check", path)
	if got.code != exitDifferent || !strings.Contains(got.stdout, "--- "+path+"\n") ||
		!strings.Contains(got.stdout, "-a=1\n+a = 1\n") {
		t.Fatalf("tomlfmt --check %s = %+v, want exit 1 and the diff", path, got)
	}

	if content := readFile(t, path); content != unformatted {
		t.Fatalf("--check changed the file to %q", content)
	}

	formatted := writeFile(t, canonical, 0o644)
	if got := tomlfmt(t, "", "--check", formatted); got.code != exitDone || got.stdout != "" {
		t.Fatalf("tomlfmt --check on a formatted file = %+v, want exit 0 and no output", got)
	}
}

func TestInvalidInput(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "a = 1\na = 2\n", 0o644)

	got := tomlfmt(t, "", path)
	if got.code != exitInvalid || !strings.Contains(got.stderr, path+": invalid TOML: line 2") {
		t.Fatalf("tomlfmt on a duplicate key = %+v, want exit 2 naming the file and line", got)
	}
}

func TestMissingFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "absent.toml")

	if got := tomlfmt(t, "", path); got.code != exitIO || !strings.Contains(got.stderr, "absent.toml") {
		t.Fatalf("tomlfmt on a missing file = %+v, want exit 3 naming it", got)
	}
}

func TestUsage(t *testing.T) {
	t.Parallel()

	if got := tomlfmt(t, "", "--nonsense"); got.code != exitInvalid || !strings.Contains(got.stderr, "usage: tomlfmt") {
		t.Fatalf("tomlfmt --nonsense = %+v, want exit 2 and the usage", got)
	}
}

func TestWorstCodeWins(t *testing.T) {
	t.Parallel()

	invalid := writeFile(t, "a =\n", 0o644)
	missing := filepath.Join(t.TempDir(), "absent.toml")
	fine := writeFile(t, unformatted, 0o644)

	if got := tomlfmt(t, "", invalid, missing, fine); got.code != exitIO {
		t.Fatalf("tomlfmt on an invalid, a missing and a fine file = exit %d, want 3", got.code)
	}

	if content := readFile(t, fine); content != canonical {
		t.Fatalf("the fine file holds %q: a failure on another file must not stop it", content)
	}
}

func TestVersion(t *testing.T) {
	t.Parallel()

	if got := tomlfmt(t, "", "--version"); got.code != exitDone || got.stdout != "tomlfmt dev\n" {
		t.Fatalf("tomlfmt --version = %+v, want exit 0 and \"tomlfmt dev\"", got)
	}
}

// orderSchema wants name a string, and orders the root's keys ascending.
const orderSchema = `{
	"$schema": "https://json-schema.org/draft/2020-12/schema",
	"type": "object",
	"x-tombi-table-keys-order": "ascending",
	"properties": { "name": { "type": "string" } }
}`

// writeBeside writes a file named name in dir.
func writeBeside(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	return path
}

func TestSchemaOrders(t *testing.T) {
	t.Parallel()

	schema := writeBeside(t, t.TempDir(), "schema.json", orderSchema)
	path := writeFile(t, "name = \"n\"\nb = 1\na = 2\n", 0o644)

	if got := tomlfmt(t, "", "--schema", schema, path); got.code != exitDone || got.stderr != "" {
		t.Fatalf("tomlfmt --schema = %+v, want exit 0", got)
	}

	if content, want := readFile(t, path), "a = 2\nb = 1\nname = \"n\"\n"; content != want {
		t.Fatalf("the file holds %q, want %q", content, want)
	}
}

func TestSchemaViolation(t *testing.T) {
	t.Parallel()

	schema := writeBeside(t, t.TempDir(), "schema.json", orderSchema)
	source := "b = 1\nname = 1\n"
	path := writeFile(t, source, 0o644)

	for _, args := range [][]string{{"--schema", schema, path}, {"--check", "--schema", schema, path}} {
		got := tomlfmt(t, "", args...)
		if got.code != exitViolation ||
			!strings.Contains(
				got.stderr,
				path+": does not match the schema: line 2, column 1, key name: got number, want string",
			) {
			t.Fatalf("tomlfmt %v = %+v, want exit 4 naming the file, line, column and key", args, got)
		}

		if content := readFile(t, path); content != source {
			t.Fatalf("a file breaking its schema was changed to %q", content)
		}
	}
}

// TestSchemaDirective holds the directive to its path, relative to the file, and
// --schema to winning over it.
func TestSchemaDirective(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeBeside(t, dir, "schema.json", orderSchema)

	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	path := writeBeside(t, nested, "config.toml", "#:schema ../schema.json\nb = 1\na = 2\n")

	if got := tomlfmt(t, "", path); got.code != exitDone {
		t.Fatalf("tomlfmt on a file naming its schema = %+v, want exit 0", got)
	}

	if content, want := readFile(t, path), "#:schema ../schema.json\na = 2\nb = 1\n"; content != want {
		t.Fatalf("the file holds %q, want %q", content, want)
	}

	unordered := writeBeside(t, dir, "unordered.json", `{"x-tombi-table-keys-order": "descending"}`)

	if got := tomlfmt(t, "", "--schema", unordered, path); got.code != exitDone {
		t.Fatalf("tomlfmt --schema over a directive = %+v, want exit 0", got)
	}

	if content, want := readFile(t, path), "#:schema ../schema.json\nb = 1\na = 2\n"; content != want {
		t.Fatalf("--schema did not win over the directive: the file holds %q, want %q", content, want)
	}
}

func TestSchemaNeverFetched(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "#:schema https://json.schemastore.org/cargo.json\na = 1\n", 0o644)

	if got := tomlfmt(t, "", path); got.code != exitInvalid || !strings.Contains(got.stderr, "never fetches") {
		t.Fatalf("tomlfmt on a directive naming a URL = %+v, want exit 2 saying it never fetches", got)
	}
}

func TestSchemaFaults(t *testing.T) {
	t.Parallel()

	path := writeFile(t, "a = 1\n", 0o644)
	invalid := writeBeside(t, t.TempDir(), "schema.json", `{"type": 1}`)
	missing := filepath.Join(t.TempDir(), "absent.json")

	if got := tomlfmt(
		t,
		"",
		"--schema",
		invalid,
		path,
	); got.code != exitInvalid ||
		!strings.Contains(got.stderr, "invalid JSON Schema") {
		t.Fatalf("tomlfmt with an invalid schema = %+v, want exit 2", got)
	}

	if got := tomlfmt(
		t,
		"",
		"--schema",
		missing,
		path,
	); got.code != exitIO ||
		!strings.Contains(got.stderr, "absent.json") {
		t.Fatalf("tomlfmt with a missing schema = %+v, want exit 3 naming it", got)
	}
}
