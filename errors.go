package toml

import (
	"errors"
	"fmt"
	"strings"

	"github.com/forkcloser/go-toml/internal/schema"
)

var (
	// ErrInvalid is wrapped by every error about input that is not TOML 1.1.
	ErrInvalid = errors.New("invalid TOML")
	// ErrDecode is wrapped by every error about a valid document that does not fit
	// the Go value it is decoded into.
	ErrDecode = errors.New("TOML does not fit the value")
	// ErrInternal is wrapped by an error that is a bug in this package, not a fault of
	// the input: the syntax tree failing on a document the decoder accepted.
	ErrInternal = errors.New("internal error in github.com/forkcloser/go-toml")
	// ErrSchema is wrapped by the error about a file [LoadSchema] cannot use: not a
	// JSON Schema, or with an annotation this package cannot apply.
	ErrSchema = schema.ErrInvalid
	// ErrViolation is wrapped by each of the [Violations] of a schema.
	ErrViolation = errors.New("does not match the schema")
)

// Error is a failure at a place in a document.
type Error struct {
	// Err is ErrInvalid, ErrDecode, ErrViolation or ErrInternal.
	Err error
	// Line and Column are 1-based, Column counting characters; both are zero when the
	// failure has no single place.
	Line   int
	Column int
	// Key is the dotted key the failure concerns, when there is one; for a
	// violation, an array's element is its index, in decimal.
	Key []string
	// Msg says what is wrong.
	Msg string
}

func (e *Error) Error() string {
	var text strings.Builder

	text.WriteString(e.Err.Error())

	if e.Line > 0 {
		fmt.Fprintf(&text, ": line %d, column %d", e.Line, e.Column)
	}

	if len(e.Key) > 0 {
		fmt.Fprintf(&text, ", key %s", strings.Join(e.Key, "."))
	}

	text.WriteString(": ")
	text.WriteString(e.Msg)

	return text.String()
}

func (e *Error) Unwrap() error {
	return e.Err
}
