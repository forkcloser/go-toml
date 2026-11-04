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

	path := writeFile(t, unformatted, 0o640)

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

	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
		t.Fatalf("the file's mode is %v, want it kept at 0640", info.Mode().Perm())
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
