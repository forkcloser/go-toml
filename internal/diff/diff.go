// Package diff writes the unified diff of two texts, line by line.
package diff

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
)

const (
	// context is how many unchanged lines surround each change.
	context = 3
	// maxCells bounds the search's memory: past it, the diff replaces every line
	// rather than searching for the shortest edit.
	maxCells = 1 << 22
)

type operation byte

const (
	equal  operation = ' '
	remove operation = '-'
	insert operation = '+'
)

type edit struct {
	op   operation
	text string
}

// Unified returns the unified diff turning before into after, its headers naming
// them, or nothing when they are equal.
func Unified(beforeName, afterName string, before, after []byte) []byte {
	edits := shortest(lines(before), lines(after))
	if !slices.ContainsFunc(edits, func(e edit) bool { return e.op != equal }) {
		return nil
	}

	var out bytes.Buffer

	fmt.Fprintf(&out, "--- %s\n+++ %s\n", beforeName, afterName)

	for _, h := range hunks(edits) {
		h.write(&out, edits)
	}

	return out.Bytes()
}

// lines splits text into lines, each keeping its line ending.
func lines(text []byte) []string {
	result := make([]string, 0, bytes.Count(text, []byte("\n"))+1)

	for len(text) > 0 {
		end := bytes.IndexByte(text, '\n') + 1
		if end == 0 {
			end = len(text)
		}

		result = append(result, string(text[:end]))
		text = text[end:]
	}

	return result
}

// shortest is Myers' algorithm: the shortest edit script from a to b. Its names are
// the paper's: d edits so far, diagonal k, x into a and y into b.
//
//nolint:varnamelen // the algorithm's own notation reads better than long names here
func shortest(a, b []string) []edit {
	n, m := len(a), len(b)
	offset := n + m
	frontier := make([]int, 2*offset+2)

	var trace [][]int

	for d := 0; d <= n+m; d++ {
		if (d+1)*len(frontier) > maxCells {
			return replaceAll(a, b)
		}

		trace = append(trace, slices.Clone(frontier))

		for k := -d; k <= d; k += 2 {
			var x int
			if down(frontier, offset, d, k) {
				x = frontier[offset+k+1]
			} else {
				x = frontier[offset+k-1] + 1
			}

			x = follow(a, b, x, x-k)
			frontier[offset+k] = x

			if x >= n && x-k >= m {
				return backtrack(trace, a, b, offset)
			}
		}
	}

	return replaceAll(a, b)
}

// replaceAll is the edit script that removes every line of before and inserts every
// line of after.
func replaceAll(before, after []string) []edit {
	edits := make([]edit, 0, len(before)+len(after))

	for _, text := range before {
		edits = append(edits, edit{op: remove, text: text})
	}

	for _, text := range after {
		edits = append(edits, edit{op: insert, text: text})
	}

	return edits
}

// follow moves along the diagonal from x, y while a and b agree, and returns where x
// ends.
//
//nolint:varnamelen // the algorithm's own notation, as in shortest
func follow(a, b []string, x, y int) int {
	for x < len(a) && y < len(b) && a[x] == b[y] {
		x++
		y++
	}

	return x
}

// down reports whether the path to diagonal k after d edits comes from diagonal k+1,
// an insertion, rather than from k-1, a removal.
func down(frontier []int, offset, d, k int) bool {
	return k == -d || (k != d && frontier[offset+k-1] < frontier[offset+k+1])
}

// backtrack walks the trace back from the end, collecting the edits of the path.
//
//nolint:varnamelen // the algorithm's own notation, as in shortest
func backtrack(trace [][]int, a, b []string, offset int) []edit {
	edits := make([]edit, 0, len(a)+len(b))

	x, y := len(a), len(b)

	for d, frontier := range slices.Backward(trace) {
		k := x - y

		previous := k - 1
		if down(frontier, offset, d, k) {
			previous = k + 1
		}

		previousX := frontier[offset+previous]
		previousY := previousX - previous

		for x > previousX && y > previousY {
			edits = append(edits, edit{op: equal, text: a[x-1]})
			x--
			y--
		}

		if d == 0 {
			break
		}

		if x == previousX {
			edits = append(edits, edit{op: insert, text: b[y-1]})
			y--
		} else {
			edits = append(edits, edit{op: remove, text: a[x-1]})
			x--
		}
	}

	slices.Reverse(edits)

	return edits
}

// hunk is a run of edits, changes with their context, by index into the edits.
type hunk struct {
	start, end int
}

func hunks(edits []edit) []hunk {
	var result []hunk

	for i := 0; i < len(edits); i++ {
		if edits[i].op == equal {
			continue
		}

		end := hunkEnd(edits, i)
		result = append(result, hunk{start: max(i-context, 0), end: end})
		i = end - 1
	}

	return result
}

// hunkEnd returns where the hunk holding the change at i ends: after the context
// following its last change, where a run of unchanged lines too long to join the
// next change begins.
func hunkEnd(edits []edit, i int) int {
	end := i

	for end < len(edits) {
		if edits[end].op != equal {
			end++

			continue
		}

		run := end
		for run < len(edits) && edits[run].op == equal {
			run++
		}

		if run == len(edits) || run-end > 2*context {
			return min(end+context, len(edits))
		}

		end = run
	}

	return end
}

func (h hunk) write(out *bytes.Buffer, edits []edit) {
	beforeLine, afterLine := 1, 1

	for _, e := range edits[:h.start] {
		if e.op != insert {
			beforeLine++
		}

		if e.op != remove {
			afterLine++
		}
	}

	beforeCount, afterCount := 0, 0

	for _, e := range edits[h.start:h.end] {
		if e.op != insert {
			beforeCount++
		}

		if e.op != remove {
			afterCount++
		}
	}

	fmt.Fprintf(out, "@@ -%s +%s @@\n", span(beforeLine, beforeCount), span(afterLine, afterCount))

	for _, e := range edits[h.start:h.end] {
		out.WriteByte(byte(e.op))
		out.WriteString(e.text)

		if e.text == "" || e.text[len(e.text)-1] != '\n' {
			out.WriteString("\n\\ No newline at end of file\n")
		}
	}
}

// span is a hunk header's range: the line a range starts at, and how many lines it
// holds; an empty range starts at the line before it.
func span(line, count int) string {
	switch count {
	case 0:
		return fmt.Sprintf("%d,0", line-1)
	case 1:
		return strconv.Itoa(line)
	default:
		return fmt.Sprintf("%d,%d", line, count)
	}
}
