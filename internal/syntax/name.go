package syntax

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Names returns the parts of a key, KindKey, as the decoder reads them: quotes
// removed, escapes decoded.
func (n *Node) Names() []string {
	names := make([]string, 0, len(n.Children))

	for _, part := range n.Children {
		if name, ok := Name(part); ok {
			names = append(names, name)
		}
	}

	return names
}

// Name returns what a child of a key names, if it is one of the key's parts rather
// than a dot or whitespace.
func Name(part *Node) (string, bool) {
	switch part.Kind { //nolint:exhaustive // the kinds that can occur here are named; default is every other by design
	case KindBareKey:
		return string(part.Text), true
	case KindLiteralString:
		return string(part.Text[1 : len(part.Text)-1]), true
	case KindBasicString:
		return unescape(string(part.Text[1 : len(part.Text)-1])), true
	default:
		return "", false
	}
}

// unescape decodes the escapes of a basic string's content, which the decoder has
// accepted: every escape is one TOML 1.1 defines.
func unescape(content string) string {
	if !strings.Contains(content, `\`) {
		return content
	}

	var out strings.Builder

	for i := 0; i < len(content); i++ {
		if content[i] != '\\' {
			out.WriteByte(content[i])

			continue
		}

		i++

		switch content[i] {
		case 'b':
			out.WriteByte('\b')
		case 't':
			out.WriteByte('\t')
		case 'n':
			out.WriteByte('\n')
		case 'f':
			out.WriteByte('\f')
		case 'r':
			out.WriteByte('\r')
		case 'e':
			out.WriteByte('\x1b')
		case 'x':
			out.WriteRune(hex(content[i+1 : i+3]))
			i += 2
		case 'u':
			out.WriteRune(hex(content[i+1 : i+5]))
			i += 4
		case 'U':
			out.WriteRune(hex(content[i+1 : i+9]))
			i += 8
		default:
			out.WriteByte(content[i])
		}
	}

	return out.String()
}

func hex(digits string) rune {
	value, err := strconv.ParseInt(digits, 16, 32)
	if err != nil {
		return utf8.RuneError
	}

	return rune(value) // #nosec G115 -- ParseInt with bit size 32 keeps the value in a rune's range
}
