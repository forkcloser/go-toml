// Package position converts the decoder's places in a document, a 1-based line and
// byte column, into what an error reports.
package position

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"
)

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

// Column turns a 1-based byte column on a 1-based line of src into a
// 1-based character column; the decoder counts bytes.
func Column(src []byte, line, byteColumn int) int {
	start, ok := lineStart(src, line)
	if !ok {
		return byteColumn
	}

	end := min(start+byteColumn-1, len(src))

	return utf8.RuneCount(src[start:end]) + 1
}

// NameCharacter corrects the character a decoder message names at a 1-based line
// and byte column of src. The decoder formats the offending byte, not the character
// it starts, so a multi-byte character is named as its first byte read as Latin-1
// ("é" as U+00C3 'Ã'); a message about a single-byte character is left as it is.
func NameCharacter(msg string, src []byte, line, byteColumn int) string {
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
