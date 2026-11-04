package toml

import (
	"math"
	"time"

	gotoml "github.com/pelletier/go-toml/v2"

	"github.com/forkcloser/go-toml/internal/format"
	"github.com/forkcloser/go-toml/internal/order"
)

// Option changes what [Document.Format] does.
type Option func(*options)

type options struct {
	schema *Schema
}

// WithSchema orders each table's keys by the schema's x-tombi-table-keys-order
// annotations: declared keys in the order the annotation gives, any other after them
// in the order written. Keys move with the comments on the lines directly above them
// and the one trailing them, within the lines between two blank lines, never past a
// table header; elements of arrays, and of arrays of tables, keep their order. A
// table no annotation covers keeps its keys as written.
func WithSchema(s *Schema) Option {
	return func(o *options) {
		o.schema = s
	}
}

// Format returns the document in the canonical layout: one space around "=", no
// indentation outside collections, one blank line before each table, runs of blank
// lines collapsed, arrays and inline tables on one line when they hold no comment,
// have no comma after their last element and fit in 100 columns, otherwise one
// element per line with a trailing comma,
// comments kept where they were, keys unquoted where TOML allows, values spelled as
// they were; keys in the order written, unless [WithSchema] orders them. The result
// is checked to mean what the document means; one that does not is an [ErrInternal]
// error, never returned.
func (d *Document) Format(opts ...Option) ([]byte, error) {
	var chosen options

	for _, opt := range opts {
		opt(&chosen)
	}

	root := d.root

	if chosen.schema != nil {
		instance, err := d.instance()
		if err != nil {
			return nil, err
		}

		root = order.Keys(root, chosen.schema.compiled.Sorter(instance))
	}

	formatted := format.Document(root)

	if err := sameMeaning(d.Bytes(), formatted); err != nil {
		return nil, err
	}

	return formatted, nil
}

func sameMeaning(original, formatted []byte) error {
	var before, after map[string]any

	if err := gotoml.Unmarshal(original, &before); err != nil {
		return wrap(original, err, ErrInvalid)
	}

	if err := gotoml.Unmarshal(formatted, &after); err != nil {
		return &Error{Err: ErrInternal, Msg: "formatting produced a document that is not TOML: " + err.Error()}
	}

	if !equal(before, after) {
		return &Error{Err: ErrInternal, Msg: "formatting changed what the document means"}
	}

	return nil
}

// equal compares decoded values, NaN equal to itself.
func equal(left, right any) bool {
	switch value := left.(type) {
	case map[string]any:
		other, ok := right.(map[string]any)

		return ok && equalTables(value, other)
	case []any:
		other, ok := right.([]any)

		return ok && equalArrays(value, other)
	case float64:
		other, ok := right.(float64)

		return ok && (value == other || math.IsNaN(value) && math.IsNaN(other))
	case time.Time:
		other, ok := right.(time.Time)

		return ok && value.Equal(other)
	default:
		return left == right
	}
}

func equalTables(left, right map[string]any) bool {
	if len(left) != len(right) {
		return false
	}

	for key, value := range left {
		other, found := right[key]
		if !found || !equal(value, other) {
			return false
		}
	}

	return true
}

func equalArrays(left, right []any) bool {
	if len(left) != len(right) {
		return false
	}

	for i := range left {
		if !equal(left[i], right[i]) {
			return false
		}
	}

	return true
}
