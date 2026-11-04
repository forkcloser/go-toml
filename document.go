package toml

import (
	"bytes"
	"errors"
	"strconv"
	"strings"

	gotoml "github.com/pelletier/go-toml/v2"

	"github.com/forkcloser/go-toml/internal/position"
	"github.com/forkcloser/go-toml/internal/syntax"
)

// LocalDate is a TOML local date, as decoded into an untyped value ([any] or a map).
type LocalDate = gotoml.LocalDate

// LocalTime is a TOML local time, as decoded into an untyped value.
type LocalTime = gotoml.LocalTime

// LocalDateTime is a TOML local date-time, as decoded into an untyped value. An
// offset date-time decodes as a [time.Time].
type LocalDateTime = gotoml.LocalDateTime

// Document is a parsed TOML document, every byte of it kept.
type Document struct {
	root *syntax.Node
}

// Parse parses src, which must be TOML 1.1. The document keeps its own copy of src.
func Parse(src []byte) (*Document, error) {
	src = bytes.Clone(src)

	if err := validate(src); err != nil {
		return nil, err
	}

	root, err := syntax.Parse(src)
	if err != nil {
		return nil, internal(err)
	}

	return &Document{root: root}, nil
}

// Bytes returns the document as text: the input to Parse, byte for byte, for a
// document that has not been changed.
func (d *Document) Bytes() []byte {
	var buf bytes.Buffer

	d.root.Write(&buf)

	return buf.Bytes()
}

// SchemaDirective returns the schema the document names, as written, if its first
// line is a "#:schema <location>" comment. Where the location is relative to is the
// caller's to say: the document's own file, for tomlfmt.
func (d *Document) SchemaDirective() (string, bool) {
	return syntax.Directive(d.root)
}

// Decode reads the document into value, as [Decode] does.
func (d *Document) Decode(value any) error {
	return Decode(d.Bytes(), value)
}

// Decode reads src, which must be TOML 1.1, into value, a pointer. A key value has no field
// for is an error, as is a value its field cannot hold: an integer into a string or
// a boolean, an integer out of its field's range. An integer is accepted into a
// float field.
func Decode(src []byte, value any) error {
	if err := validate(src); err != nil {
		return err
	}

	decoder := gotoml.NewDecoder(bytes.NewReader(src))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(value); err != nil {
		return wrap(src, err, ErrDecode)
	}

	return nil
}

func validate(src []byte) error {
	var discard map[string]any

	if err := gotoml.Unmarshal(src, &discard); err != nil {
		return wrap(src, err, ErrInvalid)
	}

	return nil
}

// wrap turns a go-toml error about src into an [Error] wrapping sentinel.
func wrap(src []byte, err, sentinel error) error {
	if strict, ok := errors.AsType[*gotoml.StrictMissingError](err); ok && len(strict.Errors) > 0 {
		first := strict.Errors[0]
		line, column := first.Position()
		column = position.Column(src, line, column)
		msg := "unknown key"

		if more := len(strict.Errors) - 1; more > 0 {
			msg = "unknown key, and " + plural(more, "other")
		}

		return &Error{Err: ErrDecode, Line: line, Column: column, Key: first.Key(), Msg: msg}
	}

	if decode, ok := errors.AsType[*gotoml.DecodeError](err); ok {
		line, byteColumn := decode.Position()

		return &Error{
			Err:    sentinel,
			Line:   line,
			Column: position.Column(src, line, byteColumn),
			Key:    decode.Key(),
			Msg:    position.NameCharacter(strings.TrimPrefix(decode.Error(), "toml: "), src, line, byteColumn),
		}
	}

	return &Error{Err: sentinel, Msg: strings.TrimPrefix(err.Error(), "toml: ")}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}

	return strconv.Itoa(n) + " " + word + "s"
}

// internal turns the syntax tree failing on a document the decoder accepted into an
// [Error] wrapping [ErrInternal]: a bug here, never the input's fault.
func internal(err error) error {
	location, ok := errors.AsType[*syntax.Error](err)
	if !ok {
		return &Error{Err: ErrInternal, Msg: err.Error()}
	}

	return &Error{
		Err:    ErrInternal,
		Line:   location.Line,
		Column: location.Column,
		Msg:    "the syntax tree failed on a document the decoder accepted: " + location.Msg,
	}
}
