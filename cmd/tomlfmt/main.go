// Command tomlfmt formats TOML 1.1 files in the canonical layout.
//
//	tomlfmt [--check] [<file>…]
//	tomlfmt --version
//
// With files, each is formatted in place, atomically; with none, standard input is
// formatted to standard output. --check writes nothing: it prints the unified diff of
// each file whose formatted form differs, and exits 1 if any does. Exit codes: 0 done,
// 1 differences found by --check, 2 invalid input or usage, 3 an I/O error.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/forkcloser/xos"

	"github.com/forkcloser/go-toml"
	"github.com/forkcloser/go-toml/internal/diff"
)

const (
	exitDone = iota
	exitDifferent
	exitInvalid
	exitIO
)

var errIO = errors.New("I/O")

// version is set by the release build.
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("tomlfmt", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: tomlfmt [--check] [<file>…] | --version")
		flags.PrintDefaults()
	}

	check := flags.Bool(
		"check",
		false,
		"write nothing; print the diff of each file that is not formatted, and exit 1 if any",
	)
	printVersion := flags.Bool("version", false, "print the version and exit")

	if err := flags.Parse(args); err != nil {
		return exitInvalid
	}

	if *printVersion {
		fmt.Fprintln(stdout, "tomlfmt", version)

		return exitDone
	}

	if flags.NArg() == 0 {
		return worst(exitDone, stream(stdin, stdout, stderr, *check))
	}

	code := exitDone

	for _, path := range flags.Args() {
		code = worst(code, file(path, stdout, stderr, *check))
	}

	return code
}

// worst keeps the more serious of two exit codes.
func worst(a, b int) int {
	return max(a, b)
}

func stream(stdin io.Reader, stdout, stderr io.Writer, check bool) int {
	source, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "tomlfmt: read standard input: %v\n", err)

		return exitIO
	}

	formatted, code := format("<stdin>", source, stderr)
	if code != exitDone {
		return code
	}

	if check {
		return report("<stdin>", source, formatted, stdout)
	}

	if _, err := stdout.Write(formatted); err != nil {
		fmt.Fprintf(stderr, "tomlfmt: write standard output: %v\n", err)

		return exitIO
	}

	return exitDone
}

func file(path string, stdout, stderr io.Writer, check bool) int {
	source, err := xos.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "tomlfmt: %v\n", err)

		return exitIO
	}

	formatted, code := format(path, source, stderr)
	if code != exitDone {
		return code
	}

	if check {
		return report(path, source, formatted, stdout)
	}

	if bytes.Equal(source, formatted) {
		return exitDone
	}

	if err := replace(path, formatted); err != nil {
		fmt.Fprintf(stderr, "tomlfmt: %s: %v\n", path, err)

		return exitIO
	}

	return exitDone
}

func format(name string, source []byte, stderr io.Writer) ([]byte, int) {
	doc, err := toml.Parse(source)
	if err == nil {
		var formatted []byte

		if formatted, err = doc.Format(); err == nil {
			return formatted, exitDone
		}
	}

	fmt.Fprintf(stderr, "tomlfmt: %s: %v\n", name, err)

	return nil, exitInvalid
}

func report(name string, source, formatted []byte, stdout io.Writer) int {
	patch := diff.Unified(name, name+" (formatted)", source, formatted)
	if patch == nil {
		return exitDone
	}

	if _, err := stdout.Write(patch); err != nil {
		return exitIO
	}

	return exitDifferent
}

// replace writes data over path atomically: a temporary file beside it, with path's
// mode, synced, then renamed over it.
func replace(path string, data []byte) error {
	info, err := xos.Stat(path)
	if err != nil {
		return fmt.Errorf("%w: %w", errIO, err)
	}

	temporary, err := xos.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("%w: %w", errIO, err)
	}

	name := temporary.Name()

	if err := write(temporary, data, info.Mode().Perm()); err != nil {
		_ = os.Remove(name)

		return err
	}

	if err := xos.Rename(name, path); err != nil {
		_ = os.Remove(name)

		return fmt.Errorf("%w: %w", errIO, err)
	}

	return nil
}

func write(temporary *os.File, data []byte, mode os.FileMode) error {
	_, err := temporary.Write(data)
	if err == nil {
		err = temporary.Chmod(mode)
	}

	if err == nil {
		err = temporary.Sync()
	}

	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}

	return err
}
