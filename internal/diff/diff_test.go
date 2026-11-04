package diff_test

import (
	"strings"
	"testing"

	"github.com/forkcloser/go-toml/internal/diff"
)

func TestUnified(t *testing.T) {
	t.Parallel()

	cases := map[string]struct{ before, after, want string }{
		"equal": {"a\nb\n", "a\nb\n", ""},
		"one line changed": {
			"a\nb\nc\n", "a\nB\nc\n",
			"--- before\n+++ after\n@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n",
		},
		"added at the end": {
			"a\n", "a\nb\n",
			"--- before\n+++ after\n@@ -1 +1,2 @@\n a\n+b\n",
		},
		"removed everything": {
			"a\nb\n", "",
			"--- before\n+++ after\n@@ -1,2 +0,0 @@\n-a\n-b\n",
		},
		"no final newline": {
			"a", "a\n",
			"--- before\n+++ after\n@@ -1 +1 @@\n-a\n\\ No newline at end of file\n+a\n",
		},
		"two hunks": {
			"1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n", "x\n2\n3\n4\n5\n6\n7\n8\n9\ny\n",
			"--- before\n+++ after\n@@ -1,4 +1,4 @@\n-1\n+x\n 2\n 3\n 4\n@@ -7,4 +7,4 @@\n 7\n 8\n 9\n-10\n+y\n",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := string(diff.Unified("before", "after", []byte(tc.before), []byte(tc.after))); got != tc.want {
				t.Fatalf("Unified:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

// TestUnifiedPastTheBound checks that a diff too large to search still says what
// changed: every line replaced.
func TestUnifiedPastTheBound(t *testing.T) {
	t.Parallel()

	before := strings.Repeat("a\n", 3000)
	after := strings.Repeat("b\n", 3000)

	got := string(diff.Unified("before", "after", []byte(before), []byte(after)))
	if !strings.HasPrefix(got, "--- before\n+++ after\n@@ -1,3000 +1,3000 @@\n-a\n") ||
		strings.Count(got, "\n+b") != 3000 {
		t.Fatalf("Unified past the bound gave %d bytes starting %q", len(got), got[:min(len(got), 60)])
	}
}
