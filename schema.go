package toml

import (
	"cmp"
	"slices"
	"strings"

	gotoml "github.com/pelletier/go-toml/v2"

	"github.com/forkcloser/go-toml/internal/locate"
	"github.com/forkcloser/go-toml/internal/schema"
)

// Schema is a JSON Schema (draft 2020-12, or the draft its $schema names) for a TOML
// document: a document is validated against it, and its x-tombi-table-keys-order
// annotations order a table's keys when formatting. A document is read as JSON reads:
// a table is an object, a date-time, a date or a time a string, its RFC 3339 text;
// nan and inf are not JSON numbers, and break any schema.
type Schema struct {
	compiled *schema.Schema
}

// LoadSchema reads the schema at path, and the files its references name, relative
// to it. A schema is a local file: a reference to anything else is an error, never a
// download. A file that is not a JSON Schema, or an annotation this package cannot
// apply, is an error wrapping [ErrSchema]; a file that cannot be read, the error
// reading it.
func LoadSchema(path string) (*Schema, error) {
	compiled, err := schema.Load(path)
	if err != nil {
		return nil, err
	}

	return &Schema{compiled: compiled}, nil
}

// Violations is every place a document breaks its schema, in document order, each an
// [Error] wrapping [ErrViolation].
//
//nolint:errname // a list of violations, each an Error; "ViolationErrors" would say less
type Violations []*Error

func (v Violations) Error() string {
	lines := make([]string, 0, len(v))

	for _, violation := range v {
		lines = append(lines, violation.Error())
	}

	return strings.Join(lines, "\n")
}

func (v Violations) Unwrap() []error {
	errs := make([]error, 0, len(v))

	for _, violation := range v {
		errs = append(errs, violation)
	}

	return errs
}

// Validate checks the document against a schema. It returns nil when the document is
// valid, [Violations] when it is not, each at the key, header or element that
// writes the value.
func (d *Document) Validate(against *Schema) error {
	instance, err := d.instance()
	if err != nil {
		return err
	}

	found := against.compiled.Validate(instance)
	if len(found) == 0 {
		return nil
	}

	index := locate.New(d.root)
	violations := make(Violations, 0, len(found))

	for _, v := range found {
		line, column := index.Find(v.Path)
		violations = append(violations, &Error{Err: ErrViolation, Line: line, Column: column, Key: v.Path, Msg: v.Msg})
	}

	slices.SortStableFunc(violations, func(a, b *Error) int {
		return cmp.Or(cmp.Compare(a.Line, b.Line), cmp.Compare(a.Column, b.Column))
	})

	return violations
}

// instance is the document as a schema reads it.
func (d *Document) instance() (any, error) {
	var decoded map[string]any

	if err := gotoml.Unmarshal(d.Bytes(), &decoded); err != nil {
		return nil, wrap(d.Bytes(), err, ErrInvalid)
	}

	return schema.Instance(decoded), nil
}
