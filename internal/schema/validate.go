package schema

import (
	"errors"
	"slices"
	"strings"
	"time"

	gotoml "github.com/pelletier/go-toml/v2"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// Violation is a value breaking the schema: where, by keys and array indices, and
// how.
type Violation struct {
	Path []string
	Msg  string
}

// Instance turns a decoded document into the JSON data model a schema describes: a
// date-time, a date or a time becomes its RFC 3339 text. Arrays and tables are
// copied, the document left as it is.
func Instance(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		table := make(map[string]any, len(typed))

		for key, member := range typed {
			table[key] = Instance(member)
		}

		return table
	case []any:
		array := make([]any, len(typed))

		for i, element := range typed {
			array[i] = Instance(element)
		}

		return array
	case time.Time:
		return typed.Format(time.RFC3339Nano)
	case gotoml.LocalDate:
		return typed.String()
	case gotoml.LocalTime:
		return typed.String()
	case gotoml.LocalDateTime:
		return typed.String()
	default:
		return value
	}
}

// Validate returns every violation of the schema by instance, which [Instance] made.
func (s *Schema) Validate(instance any) []Violation {
	err := s.root.Validate(instance)

	failure, ok := errors.AsType[*jsonschema.ValidationError](err)
	if !ok {
		return nil
	}

	var violations []Violation

	collect(failure, message.NewPrinter(language.English), &violations)

	return violations
}

// collect gathers the violations a failure holds: its causes, down to those that
// have none; an additional property each at its own key; a failed anyOf or oneOf as
// one violation, its branches' failures said in its message.
func collect(failure *jsonschema.ValidationError, printer *message.Printer, into *[]Violation) {
	switch failed := failure.ErrorKind.(type) {
	case *kind.AdditionalProperties:
		for _, property := range failed.Properties {
			*into = append(*into, Violation{
				Path: append(slices.Clone(failure.InstanceLocation), property),
				Msg:  "not allowed: the schema has no property for it",
			})
		}

		return
	case *kind.AnyOf, *kind.OneOf:
		if len(failure.Causes) == 0 {
			break
		}

		var branches []Violation

		for _, cause := range failure.Causes {
			collect(cause, printer, &branches)
		}

		reasons := make([]string, 0, len(branches))

		for _, branch := range branches {
			reason := branch.Msg
			if inner := branch.Path[len(failure.InstanceLocation):]; len(inner) > 0 {
				reason = strings.Join(inner, ".") + ": " + reason
			}

			reasons = append(reasons, reason)
		}

		*into = append(*into, Violation{
			Path: failure.InstanceLocation,
			Msg:  failure.ErrorKind.LocalizedString(printer) + ": " + strings.Join(reasons, "; "),
		})

		return
	}

	if len(failure.Causes) == 0 {
		*into = append(
			*into,
			Violation{Path: failure.InstanceLocation, Msg: failure.ErrorKind.LocalizedString(printer)},
		)

		return
	}

	for _, cause := range failure.Causes {
		collect(cause, printer, into)
	}
}
