package toml

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
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
)

// Error is a failure at a place in a document.
type Error struct {
	// Err is ErrInvalid, ErrDecode or ErrInternal.
	Err error
	// Line and Column are 1-based, Column counting characters; both are zero when the
	// failure has no single place.
	Line   int
	Column int
	// Key is the dotted key the failure concerns, when there is one.
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

// lineStart returns the offset of the first byte of the 1-based line in src, and
// whether src has that line.
func lineStart(src []byte, line int) (int, bool) {
	start := 0

	for range line - 1 {
		next := bytes.IndexByte(src[start:], '\n')
		if next < 0 {
			return 0, false
		}

		start += next + 1
	}

	return start, true
}

// characterColumn turns a 1-based byte column on a 1-based line of src into a
// 1-based character column; the decoder counts bytes.
func characterColumn(src []byte, line, byteColumn int) int {
	start, ok := lineStart(src, line)
	if !ok {
		return byteColumn
	}

	end := min(start+byteColumn-1, len(src))

	return utf8.RuneCount(src[start:end]) + 1
}

// nameCharacter corrects the character a decoder message names at a 1-based line
// and byte column of src. The decoder formats the offending byte, not the character
// it starts, so a multi-byte character is named as its first byte read as Latin-1
// ("é" as U+00C3 'Ã'); a message about a single-byte character is left as it is.
func nameCharacter(msg string, src []byte, line, byteColumn int) string {
	start, ok := lineStart(src, line)
	if !ok {
		return msg
	}

	offset := start + byteColumn - 1
	if offset < 0 || offset >= len(src) || src[offset] < utf8.RuneSelf {
		return msg
	}

	character, size := utf8.DecodeRune(src[offset:])
	if character == utf8.RuneError && size <= 1 {
		return msg
	}

	return strings.Replace(msg, fmt.Sprintf("%#U", rune(src[offset])), fmt.Sprintf("%#U", character), 1)
}
