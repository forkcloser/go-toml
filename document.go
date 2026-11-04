package toml

import (
	"bytes"
	"errors"
	"strconv"
	"strings"

	gotoml "github.com/pelletier/go-toml/v2"
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
	root *node
}

// Parse parses src, which must be TOML 1.1. The document keeps its own copy of src.
func Parse(src []byte) (*Document, error) {
	src = bytes.Clone(src)

	if err := validate(src); err != nil {
		return nil, err
	}

	root, err := parseTree(src)
	if err != nil {
		return nil, err
	}

	return &Document{root: root}, nil
}

// Bytes returns the document as text: the input to Parse, byte for byte, for a
// document that has not been changed.
func (d *Document) Bytes() []byte {
	var buf bytes.Buffer

	d.root.write(&buf)

	return buf.Bytes()
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
		column = characterColumn(src, line, column)
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
			Column: characterColumn(src, line, byteColumn),
			Key:    decode.Key(),
			Msg:    nameCharacter(strings.TrimPrefix(decode.Error(), "toml: "), src, line, byteColumn),
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
