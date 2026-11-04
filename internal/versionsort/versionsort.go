// Package versionsort compares strings by the Rust style guide's version sorting
// (https://doc.rust-lang.org/nightly/style-guide/#sorting), as rustfmt implements it:
// x8 before x16, an underscore before anything else.
package versionsort

import "strings"

// chunk is a maximal run of a string: one underscore, ASCII digits, or anything
// else.
type chunk struct {
	text       string
	numeric    bool
	underscore bool
}

func chunks(text string) []chunk {
	result := make([]chunk, 0, len(text))

	for text != "" {
		end := 1

		switch {
		case text[0] == '_':
			result = append(result, chunk{text: "_", underscore: true})
			text = text[1:]

			continue
		case isDigit(text[0]):
			for end < len(text) && isDigit(text[end]) {
				end++
			}

			result = append(result, chunk{text: text[:end], numeric: true})
		default:
			for end < len(text) && text[end] != '_' && !isDigit(text[end]) {
				end++
			}

			result = append(result, chunk{text: text[:end]})
		}

		text = text[end:]
	}

	return result
}

func isDigit(char byte) bool {
	return char >= '0' && char <= '9'
}

// Compare returns -1, 0 or +1 as a sorts before, with or after b.
func Compare(a, b string) int {
	left, right := chunks(a), chunks(b)

	// zeros is the first tie broken by leading zeros: -1 when a had more, so sorts
	// first.
	zeros := 0

	for i := range min(len(left), len(right)) {
		if order := compareChunks(left[i], right[i], &zeros); order != 0 {
			return order
		}
	}

	if len(left) != len(right) {
		return sign(len(left) - len(right))
	}

	return zeros
}

// compareChunks compares two chunks in the same place; two numbers equal but for
// their leading zeros compare equal, the first such tie kept in zeros.
func compareChunks(ours, theirs chunk, zeros *int) int {
	switch {
	case ours.underscore && theirs.underscore:
		return 0
	case ours.underscore:
		return -1
	case theirs.underscore:
		return +1
	case ours.numeric && theirs.numeric:
		if order := compareNumbers(ours.text, theirs.text); order != 0 {
			return order
		}

		if *zeros == 0 && len(ours.text) != len(theirs.text) {
			*zeros = sign(len(theirs.text) - len(ours.text))
		}

		return 0
	default:
		return strings.Compare(ours.text, theirs.text)
	}
}

// compareNumbers compares two runs of digits by value, however long.
func compareNumbers(ours, theirs string) int {
	ours, theirs = strings.TrimLeft(ours, "0"), strings.TrimLeft(theirs, "0")

	if len(ours) != len(theirs) {
		return sign(len(ours) - len(theirs))
	}

	return strings.Compare(ours, theirs)
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return +1
	default:
		return 0
	}
}
