// Command tomlfmt formats TOML 1.1 files in the canonical layout.
//
//	tomlfmt [--check] [--schema <file>] [<file>…]
//	tomlfmt --version
//
// With files, each is formatted in place, atomically; with none, standard input is
// formatted to standard output. --check writes nothing: it prints the unified diff of
// each file whose formatted form differs, and exits 1 if any does.
//
// A JSON Schema, given by --schema or named by a "#:schema <path>" directive on a
// file's first line (relative to the file; --schema wins), validates the file and
// orders its tables' keys by its x-tombi-table-keys-order annotations. A file that
// breaks its schema is reported, every violation with its line, column and key, and
// left as it is. A schema is a local file, never fetched.
//
// Exit codes: 0 done, 1 differences found by --check, 2 invalid input or usage, 3 an
// I/O error, 4 a schema violation.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/forkcloser/xos"

	"github.com/forkcloser/go-toml"
	"github.com/forkcloser/go-toml/internal/diff"
)

const (
	exitDone = iota
	exitDifferent
	exitInvalid
	exitIO
	exitViolation
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
		fmt.Fprintln(stderr, "usage: tomlfmt [--check] [--schema <file>] [<file>…] | --version")
		flags.PrintDefaults()
	}

	check := flags.Bool(
		"check",
		false,
		"write nothing; print the diff of each file that is not formatted, and exit 1 if any",
	)
	schemaPath := flags.String(
		"schema",
		"",
		"validate each file against this JSON Schema, and order keys by it, whatever schema the file names",
	)
	printVersion := flags.Bool("version", false, "print the version and exit")

	if err := flags.Parse(args); err != nil {
		return exitInvalid
	}

	if *printVersion {
		fmt.Fprintln(stdout, "tomlfmt", version)

		return exitDone
	}

	job := &formatter{stdout: stdout, stderr: stderr, check: *check, schemas: map[string]*toml.Schema{}}

	if *schemaPath != "" {
		var code int
		if job.given, code = job.load(*schemaPath); code != exitDone {
			return code
		}
	}

	if flags.NArg() == 0 {
		return job.stream(stdin)
	}

	code := exitDone

	for _, path := range flags.Args() {
		code = worst(code, job.file(path))
	}

	return code
}

// formatter formats one file after another, loading each schema once.
type formatter struct {
	stdout, stderr io.Writer
	check          bool
	// given is the schema --schema names, if any.
	given *toml.Schema
	// schemas are those directives named, by path.
	schemas map[string]*toml.Schema
}

// worst keeps the more serious of two exit codes.
func worst(a, b int) int {
	return max(a, b)
}

func (f *formatter) stream(stdin io.Reader) int {
	source, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(f.stderr, "tomlfmt: read standard input: %v\n", err)

		return exitIO
	}

	formatted, code := f.format("<stdin>", ".", source)
	if code != exitDone {
		return code
	}

	if f.check {
		return report("<stdin>", source, formatted, f.stdout)
	}

	if _, err := f.stdout.Write(formatted); err != nil {
		fmt.Fprintf(f.stderr, "tomlfmt: write standard output: %v\n", err)

		return exitIO
	}

	return exitDone
}

func (f *formatter) file(path string) int {
	source, err := xos.ReadFile(path)
	if err != nil {
		fmt.Fprintf(f.stderr, "tomlfmt: %v\n", err)

		return exitIO
	}

	formatted, code := f.format(path, filepath.Dir(path), source)
	if code != exitDone {
		return code
	}

	if f.check {
		return report(path, source, formatted, f.stdout)
	}

	if bytes.Equal(source, formatted) {
		return exitDone
	}

	if err := replace(path, formatted); err != nil {
		fmt.Fprintf(f.stderr, "tomlfmt: %s: %v\n", path, err)

		return exitIO
	}

	return exitDone
}

// format returns source formatted, validated and ordered by its schema if it has
// one; a directive's path is relative to dir.
func (f *formatter) format(name, dir string, source []byte) ([]byte, int) {
	doc, err := toml.Parse(source)
	if err != nil {
		return f.fail(name, err, exitInvalid)
	}

	schema, code := f.schemaOf(name, dir, doc)
	if code != exitDone {
		return nil, code
	}

	var opts []toml.Option

	if schema != nil {
		if violations := doc.Validate(schema); violations != nil {
			return f.fail(name, violations, exitViolation)
		}

		opts = append(opts, toml.WithSchema(schema))
	}

	formatted, err := doc.Format(opts...)
	if err != nil {
		return f.fail(name, err, exitInvalid)
	}

	return formatted, exitDone
}

// fail reports err about the named input, one line per violation, and returns code.
func (f *formatter) fail(name string, err error, code int) ([]byte, int) {
	if violations, ok := errors.AsType[toml.Violations](err); ok {
		for _, violation := range violations {
			fmt.Fprintf(f.stderr, "tomlfmt: %s: %v\n", name, violation)
		}

		return nil, code
	}

	fmt.Fprintf(f.stderr, "tomlfmt: %s: %v\n", name, err)

	return nil, code
}

// schemaOf returns the schema for a document: --schema's, else the one its directive
// names, else none.
func (f *formatter) schemaOf(name, dir string, doc *toml.Document) (*toml.Schema, int) {
	if f.given != nil {
		return f.given, exitDone
	}

	location, ok := doc.SchemaDirective()
	if !ok {
		return nil, exitDone
	}

	if strings.Contains(location, "://") {
		fmt.Fprintf(
			f.stderr,
			"tomlfmt: %s: the #:schema directive names %s: tomlfmt never fetches a schema\n",
			name,
			location,
		)

		return nil, exitInvalid
	}

	location = filepath.FromSlash(location)
	if !filepath.IsAbs(location) {
		location = filepath.Join(dir, location)
	}

	if schema, loaded := f.schemas[location]; loaded {
		return schema, exitDone
	}

	schema, code := f.load(location)
	if code == exitDone {
		f.schemas[location] = schema
	}

	return schema, code
}

func (f *formatter) load(path string) (*toml.Schema, int) {
	schema, err := toml.LoadSchema(path)
	if err == nil {
		return schema, exitDone
	}

	fmt.Fprintf(f.stderr, "tomlfmt: %v\n", err)

	if errors.Is(err, toml.ErrSchema) {
		return nil, exitInvalid
	}

	return nil, exitIO
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
