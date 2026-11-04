package edit

import (
	"bytes"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/forkcloser/go-toml/internal/order"
	"github.com/forkcloser/go-toml/internal/syntax"
)

// Exact returns the value written exactly at path: a key-value's value, or an
// array's element, at any depth of inline tables and arrays.
func (d *Document) Exact(path []string) (*syntax.Node, bool) {
	for _, current := range d.sections {
		for _, written := range current.pairs {
			if !hasPrefix(path, written.path) {
				continue
			}

			if len(path) == len(written.path) {
				return written.node.Value(), true
			}

			if found, ok := inside(written.node.Value(), path[len(written.path):]); ok {
				return found, true
			}
		}
	}

	return nil, false
}

// inside returns the value at rest within the collection value.
func inside(value *syntax.Node, rest []string) (*syntax.Node, bool) {
	if value.Kind != syntax.KindArray && value.Kind != syntax.KindInlineTable {
		return nil, false
	}

	for i, element := range elements(value) {
		relative := elementPath(value, i, element)
		if !hasPrefix(rest, relative) {
			continue
		}

		target := element
		if element.Kind == syntax.KindKeyValue {
			target = element.Value()
		}

		if len(rest) == len(relative) {
			return target, true
		}

		if found, ok := inside(target, rest[len(relative):]); ok {
			return found, true
		}
	}

	return nil, false
}

// Own returns the section path's own header opens, the first if several do, and
// the splice writing text over its lines, the comments above its header included,
// up to the blank lines after its last.
func (d *Document) Own(path []string, text string) (int, Splice, bool) {
	for i, current := range d.sections {
		if current.header != nil && slices.Equal(current.path, path) {
			return i, Splice{
				Start: d.lines[current.first].start,
				End:   d.lineStart(d.lastContent(current)),
				Text:  text,
			}, true
		}
	}

	return 0, Splice{}, false
}

// Replace returns the splice writing text over node.
func (d *Document) Replace(node *syntax.Node, text string) Splice {
	start, end := d.span(node)

	return Splice{Start: start, End: end, Text: text}
}

// Place returns the indent level of the line node starts on, two spaces a level,
// and the 0-based character column it starts at.
func (d *Document) Place(node *syntax.Node) (indent, column int) {
	start, _ := d.span(node)
	lineStart := bytes.LastIndexByte(d.src[:start], '\n') + 1
	prefix := d.src[lineStart:start]

	spaces := len(prefix) - len(bytes.TrimLeft(prefix, " \t"))

	return spaces / 2, utf8.RuneCount(prefix)
}

// Delete returns the splices removing everything that defines path or a value under
// it: the sections of its tables with the comments above their headers, its
// key-values with the comments above them, its elements of inline tables and arrays.
// It returns none when nothing does. The section at index keep, if any, is left to
// the caller.
func (d *Document) Delete(path []string, keep int) []Splice {
	removed := map[int]bool{}

	var splices []Splice

	for index, current := range d.sections {
		switch {
		case index == keep:
		case current.header != nil && hasPrefix(current.path, path):
			mark(removed, current.first, current.end)
		default:
			splices = append(splices, d.deletePairs(current, path, removed)...)
		}
	}

	return append(splices, d.deleteLines(removed)...)
}

// deletePairs marks the lines of a section's key-values under path, and returns the
// splices removing what is under path inside the others' values.
func (d *Document) deletePairs(current section, path []string, removed map[int]bool) []Splice {
	var splices []Splice

	for _, kv := range current.pairs {
		switch {
		case hasPrefix(kv.path, path):
			mark(removed, kv.comment, kv.line+1)
		case hasPrefix(path, kv.path):
			splices = append(splices, d.deleteInside(kv.node.Value(), path[len(kv.path):])...)
		}
	}

	return splices
}

func mark(removed map[int]bool, from, to int) {
	for i := from; i < to; i++ {
		removed[i] = true
	}
}

// deleteLines returns the splices removing the lines marked, each run of them with
// the blank lines that would otherwise begin or end the document.
func (d *Document) deleteLines(removed map[int]bool) []Splice {
	var splices []Splice

	for i := 0; i < len(d.lines); i++ {
		if !removed[i] {
			continue
		}

		end := i
		for end < len(d.lines) && removed[end] {
			end++
		}

		start, stop := d.widen(i, end)
		splices = append(splices, Splice{Start: d.lineStart(start), End: d.lineStart(stop)})
		i = end - 1
	}

	return merge(splices)
}

// widen extends a run of removed lines over the blank lines that would be left at
// the end or the start of the document.
func (d *Document) widen(start, end int) (from, to int) {
	switch {
	case end == len(d.lines):
		for start > 0 && d.lines[start-1].blank() {
			start--
		}
	case start == 0:
		for end < len(d.lines) && d.lines[end].blank() {
			end++
		}
	}

	return start, end
}

// deleteInside returns the splices removing what defines rest within a collection.
func (d *Document) deleteInside(value *syntax.Node, rest []string) []Splice {
	if value.Kind != syntax.KindArray && value.Kind != syntax.KindInlineTable {
		return nil
	}

	var (
		splices []Splice
		gone    []int
	)

	all := elements(value)

	for i, element := range all {
		relative := elementPath(value, i, element)

		switch {
		case hasPrefix(relative, rest):
			gone = append(gone, i)
		case hasPrefix(rest, relative):
			target := element
			if element.Kind == syntax.KindKeyValue {
				target = element.Value()
			}

			splices = append(splices, d.deleteInside(target, rest[len(relative):])...)
		}
	}

	return append(splices, d.removeElements(value, all, gone)...)
}

// removeElements returns the splices removing the elements at the indices gone of a
// collection: the lines they stand on alone, with the comment lines above them, or
// themselves and the comma that separates them from a kept element.
func (d *Document) removeElements(collection *syntax.Node, all []*syntax.Node, gone []int) []Splice {
	if len(gone) == 0 {
		return nil
	}

	if len(gone) == len(all) {
		empty := "[]"
		if collection.Kind == syntax.KindInlineTable {
			empty = "{}"
		}

		return []Splice{d.Replace(collection, empty)}
	}

	var splices []Splice

	for _, i := range gone {
		if start, end, ok := d.ownLine(collection, all[i]); ok {
			splices = append(splices, Splice{Start: start, End: end})

			continue
		}

		start, end := d.span(all[i])

		if next := nextKept(i, len(all), gone); next < len(all) {
			end, _ = d.span(all[next])
		} else if previous := previousKept(i, gone); previous >= 0 {
			_, start = d.span(all[previous])
		}

		splices = append(splices, Splice{Start: start, End: end})
	}

	return merge(splices)
}

func nextKept(i, count int, gone []int) int {
	for next := i + 1; next < count; next++ {
		if !slices.Contains(gone, next) {
			return next
		}
	}

	return count
}

func previousKept(i int, gone []int) int {
	for previous := i - 1; previous >= 0; previous-- {
		if !slices.Contains(gone, previous) {
			return previous
		}
	}

	return -1
}

// merge joins overlapping and touching splices that remove text.
func merge(splices []Splice) []Splice {
	slices.SortFunc(splices, func(a, b Splice) int { return a.Start - b.Start })

	result := make([]Splice, 0, len(splices))

	for _, splice := range splices {
		if last := len(result) - 1; last >= 0 && splice.Start <= result[last].End {
			result[last].End = max(result[last].End, splice.End)

			continue
		}

		result = append(result, splice)
	}

	return result
}

// ownLine returns the range of the lines an element of a collection stands on
// alone: from the comment lines directly above it to its line's end.
func (d *Document) ownLine(collection, element *syntax.Node) (start, end int, ok bool) {
	collectionStart, collectionEnd := d.span(collection)
	elementStart, elementEnd := d.span(element)

	lineStart := bytes.LastIndexByte(d.src[collectionStart:elementStart], '\n')
	if lineStart < 0 {
		return 0, 0, false
	}

	lineStart += collectionStart + 1

	lineEnd := bytes.IndexByte(d.src[elementEnd:collectionEnd], '\n')
	if lineEnd < 0 {
		return 0, 0, false
	}

	lineEnd += elementEnd + 1

	if comma := d.commaAfter(collection, element); comma >= lineEnd {
		return 0, 0, false
	}

	before := strings.TrimSpace(string(d.src[lineStart:elementStart]))
	after := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(d.src[elementEnd:lineEnd])), ","))

	if before != "" || (after != "" && !strings.HasPrefix(after, "#")) {
		return 0, 0, false
	}

	for lineStart > collectionStart {
		previous := bytes.LastIndexByte(d.src[collectionStart:lineStart-1], '\n')
		if previous < 0 {
			break
		}

		above := strings.TrimSpace(string(d.src[collectionStart+previous+1 : lineStart-1]))
		if !strings.HasPrefix(above, "#") {
			break
		}

		lineStart = collectionStart + previous + 1
	}

	return lineStart, lineEnd, true
}

// AddElement returns the splices adding element, an array's value or an inline
// table's key-value as text, to a collection: before the element at index before,
// or after the last when before is past them; on a line of its own when the
// collection holds one element a line.
func (d *Document) AddElement(collection *syntax.Node, element string, before int) []Splice {
	all := elements(collection)

	if len(all) == 0 {
		if bytes.ContainsAny(d.text(collection), "#") {
			start, _ := d.span(collection.Children[0])

			return []Splice{{Start: start + 1, End: start + 1, Text: "\n  " + element + ",\n"}}
		}

		if collection.Kind == syntax.KindInlineTable {
			return []Splice{d.Replace(collection, "{ "+element+" }")}
		}

		return []Splice{d.Replace(collection, "["+element+"]")}
	}

	if before < len(all) {
		if start, _, ok := d.ownLine(collection, all[before]); ok {
			indent, _ := d.Place(all[before])

			return []Splice{{Start: start, End: start, Text: strings.Repeat("  ", indent) + element + ",\n"}}
		}

		start, _ := d.span(all[before])

		return []Splice{{Start: start, End: start, Text: element + ", "}}
	}

	last := all[len(all)-1]
	_, lastEnd := d.span(last)
	comma := d.commaAfter(collection, last)

	if _, end, ok := d.ownLine(collection, last); ok {
		indent, _ := d.Place(last)
		splices := []Splice{{Start: end, End: end, Text: strings.Repeat("  ", indent) + element + ",\n"}}

		if comma < 0 {
			splices = append(splices, Splice{Start: lastEnd, End: lastEnd, Text: ","})
		}

		return splices
	}

	if comma >= 0 {
		return []Splice{{Start: comma + 1, End: comma + 1, Text: " " + element + ","}}
	}

	return []Splice{{Start: lastEnd, End: lastEnd, Text: ", " + element}}
}

// commaAfter returns the offset of the comma after element in a collection, or -1.
func (d *Document) commaAfter(collection, element *syntax.Node) int {
	index := slices.Index(collection.Children, element)

	for _, child := range collection.Children[index+1:] {
		switch child.Kind { //nolint:exhaustive // the kinds that can occur here are named; default is every other by design
		case syntax.KindComma:
			start, _ := d.span(child)

			return start
		case syntax.KindWhitespace, syntax.KindNewline, syntax.KindComment:
		default:
			return -1
		}
	}

	return -1
}

func (d *Document) text(n *syntax.Node) []byte {
	start, end := d.span(n)

	return d.src[start:end]
}

// Container is where a table's new key-value goes.
type Container struct {
	// Collection is an inline table holding the table, or an array, to add to.
	Collection *syntax.Node
	// Section is the index of the section to add a key-value to, with Prefix the
	// part of the key naming the table within it: empty in the table's own section,
	// the dotted keys' first parts in a section defining it by dotted keys.
	Section int
	Prefix  []string
	// Implied is set when the table is defined only by the headers of tables under
	// it: a header of its own goes before the first of them, at section Section.
	Implied bool
}

// ContainerOf returns where a key-value of the table at path goes. path must name
// a table or an array the document holds.
func (d *Document) ContainerOf(path []string) Container {
	if value, ok := d.Exact(path); ok {
		return Container{Collection: value}
	}

	for i, current := range d.sections {
		if slices.Equal(current.path, path) {
			return Container{Section: i}
		}
	}

	for i, current := range d.sections {
		if found, ok := dotted(current, path); ok {
			found.Section = i

			return found
		}
	}

	for i, current := range d.sections {
		if current.header != nil && hasPrefix(current.path, path) {
			return Container{Section: i, Implied: true}
		}
	}

	return Container{Section: 0, Prefix: path}
}

// dotted returns where, in a section, dotted keys define the table at path: the
// section's key-values, or an inline table of one of their values.
func dotted(current section, path []string) (Container, bool) {
	if !hasPrefix(path, current.path) {
		return Container{}, false
	}

	for _, written := range current.pairs {
		if len(written.path) > len(path) && hasPrefix(written.path, path) {
			return Container{Prefix: path[len(current.path):]}, true
		}

		if len(written.path) < len(path) && hasPrefix(path, written.path) {
			if collection, prefix, ok := within(written.node.Value(), path[len(written.path):]); ok {
				return Container{Collection: collection, Prefix: prefix}, true
			}
		}
	}

	return Container{}, false
}

// within returns where, inside value, the table or array at rest is written: the
// collection that is it, or the inline table whose dotted keys define it, with the
// part of those keys naming it.
func within(value *syntax.Node, rest []string) (*syntax.Node, []string, bool) {
	if value.Kind != syntax.KindArray && value.Kind != syntax.KindInlineTable {
		return nil, nil, false
	}

	if len(rest) == 0 {
		return value, nil, true
	}

	for i, element := range elements(value) {
		relative := elementPath(value, i, element)

		target := element
		if element.Kind == syntax.KindKeyValue {
			target = element.Value()
		}

		switch {
		case hasPrefix(rest, relative):
			return within(target, rest[len(relative):])
		case value.Kind == syntax.KindInlineTable && hasPrefix(relative, rest):
			return value, rest, true
		}
	}

	return nil, nil, false
}

// AddPair returns the splice adding a key-value, given as its canonical lines, to a
// section: among the pairs whose key starts with prefix, before the one sort places
// after key, or after the last of them; after the last key-value of the section
// when none does.
func (d *Document) AddPair(sectionIndex int, prefix []string, key, text string, sort order.Sorter) Splice {
	current := d.sections[sectionIndex]
	table := append(slices.Clone(current.path), prefix...)

	var siblings []pair

	for _, kv := range current.pairs {
		if hasPrefix(kv.path, table) && len(kv.path) > len(table) {
			siblings = append(siblings, kv)
		}
	}

	if next, ok := following(siblings, len(table), key, table, sort); ok {
		return Splice{Start: d.lines[next.comment].start, End: d.lines[next.comment].start, Text: text}
	}

	after := current.head
	if current.header == nil {
		after = -1
	}

	switch {
	case len(siblings) > 0:
		after = siblings[len(siblings)-1].line
	case len(current.pairs) > 0:
		after = current.pairs[len(current.pairs)-1].line
	case current.header == nil && len(d.lines) > 0 && d.lines[0].comment && strings.HasPrefix(d.lineText(0), "#:schema"):
		after = 0
	}

	offset := d.lineStart(after + 1)
	if after < 0 {
		offset = 0
	}

	if offset == len(d.src) && !d.endsLine() {
		text = "\n" + strings.TrimSuffix(text, "\n")
	}

	if current.header == nil && after+1 < len(d.lines) && after+1 >= d.lastContent(current) &&
		!d.lines[after+1].blank() {
		text += "\n"
	}

	return Splice{Start: offset, End: offset, Text: text}
}

func (d *Document) lineText(i int) string {
	return strings.TrimSpace(string(d.src[d.lines[i].start:d.lines[i].end]))
}

// following returns the first sibling, by the order sort gives the keys at depth,
// that goes after key.
func following(siblings []pair, depth int, key string, table []string, sort order.Sorter) (pair, bool) {
	if sort == nil || len(siblings) == 0 {
		return pair{}, false
	}

	var keys []string

	for _, kv := range siblings {
		if !slices.Contains(keys, kv.path[depth]) {
			keys = append(keys, kv.path[depth])
		}
	}

	sorted := sort(table, append(slices.Clone(keys), key))

	position := slices.Index(sorted, key)
	if position < 0 {
		return pair{}, false
	}

	for _, next := range sorted[position+1:] {
		for _, kv := range siblings {
			if kv.path[depth] == next {
				return kv, true
			}
		}
	}

	return pair{}, false
}

// AddSection returns the splice adding a section, its header and body as canonical
// lines, for the table at path: under its parent, before the first sibling table
// sort places after it, or after the last section under the parent, or of the
// nearest ancestor that has one; at the end of the document when none has.
func (d *Document) AddSection(path []string, text string, sort order.Sorter) Splice {
	parent := path[:len(path)-1]

	if next, ok := d.nextSibling(parent, path[len(path)-1], sort); ok {
		return d.sectionSplice(d.lines[d.sections[next].first].start, text)
	}

	for ancestor := len(parent); ancestor >= 0; ancestor-- {
		last := -1

		for i, current := range d.sections {
			if (current.header != nil || ancestor == 0) && hasPrefix(current.path, parent[:ancestor]) {
				last = i
			}
		}

		if last >= 0 {
			return d.sectionSplice(d.lineStart(d.lastContent(d.sections[last])), text)
		}
	}

	return d.sectionSplice(len(d.src), text)
}

// nextSibling returns the section of the first table under parent that sort places
// after key: where a section for key goes.
func (d *Document) nextSibling(parent []string, key string, sort order.Sorter) (int, bool) {
	if sort == nil {
		return 0, false
	}

	var siblings []string

	firsts := map[string]int{}

	for i, current := range d.sections {
		if current.header == nil || len(current.path) <= len(parent) || !hasPrefix(current.path, parent) {
			continue
		}

		if name := current.path[len(parent)]; !slices.Contains(siblings, name) {
			firsts[name] = i
			siblings = append(siblings, name)
		}
	}

	if len(siblings) == 0 {
		return 0, false
	}

	sorted := sort(parent, append(slices.Clone(siblings), key))

	position := slices.Index(sorted, key)
	if position < 0 {
		return 0, false
	}

	for _, next := range sorted[position+1:] {
		if i, ok := firsts[next]; ok {
			return i, true
		}
	}

	return 0, false
}

// SectionBefore returns the splice adding a section before the section at index.
func (d *Document) SectionBefore(index int, text string) Splice {
	return d.sectionSplice(d.lines[d.sections[index].first].start, text)
}

// sectionSplice inserts a section at offset, the start of a line or the end of the
// document, with a blank line between it and what is above and below it.
func (d *Document) sectionSplice(offset int, text string) Splice {
	if offset == len(d.src) && !d.endsLine() {
		text = "\n" + text
	}

	before := d.src[:offset]
	if len(bytes.TrimSpace(before)) > 0 && !bytes.HasSuffix(before, []byte("\n\n")) {
		text = "\n" + text
	}

	after := d.src[offset:]
	if len(bytes.TrimSpace(after)) > 0 && !bytes.HasPrefix(after, []byte("\n")) {
		text += "\n"
	}

	return Splice{Start: offset, End: offset, Text: text}
}

// ArrayEnd returns the splice adding a section, an array of tables' element, after
// the last section of the array at path.
func (d *Document) ArrayEnd(path []string, text string) (Splice, bool) {
	last := -1

	for i, current := range d.sections {
		if current.header != nil && len(current.path) > len(path) && hasPrefix(current.path, path) {
			last = i
		}
	}

	if last < 0 {
		return Splice{}, false
	}

	return d.sectionSplice(d.lineStart(d.lastContent(d.sections[last])), text), true
}

// Sections reports whether a section's header writes path or a table under it.
func (d *Document) Sections(path []string) bool {
	for _, current := range d.sections {
		if current.header != nil && hasPrefix(current.path, path) {
			return true
		}
	}

	return false
}

// Count returns how many elements a collection holds.
func Count(collection *syntax.Node) int {
	return len(elements(collection))
}

// InlineBefore returns the index of the element of an inline table a new key-value
// goes before: among the elements whose key starts with prefix, the first sort
// places after key, the table at table; past the elements when none does.
func InlineBefore(
	collection *syntax.Node,
	prefix []string,
	key string,
	table []string,
	sort order.Sorter,
) int {
	all := elements(collection)
	if sort == nil {
		return len(all)
	}

	var keys []string

	for _, element := range all {
		names := element.Key().Names()
		if len(names) > len(prefix) && hasPrefix(names, prefix) && !slices.Contains(keys, names[len(prefix)]) {
			keys = append(keys, names[len(prefix)])
		}
	}

	sorted := sort(table, append(slices.Clone(keys), key))

	position := slices.Index(sorted, key)
	if position < 0 {
		return len(all)
	}

	for _, next := range sorted[position+1:] {
		for i, element := range all {
			names := element.Key().Names()
			if len(names) > len(prefix) && hasPrefix(names, prefix) && names[len(prefix)] == next {
				return i
			}
		}
	}

	return len(all)
}
