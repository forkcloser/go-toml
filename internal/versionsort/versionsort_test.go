package versionsort_test

import (
	"slices"
	"testing"

	"github.com/forkcloser/go-toml/internal/versionsort"
)

// TestStyleGuideOrder holds Compare to the example the style guide gives, in the
// order it gives.
func TestStyleGuideOrder(t *testing.T) {
	t.Parallel()

	want := []string{
		"_ZYXW", "_abcd", "A2", "ABCD", "Z_YXW", "ZY_XW", "ZY_XW", "ZYXW", "ZYXW_", "a1", "abcd",
		"u_zzz", "u8", "u16", "u32", "u64", "u128", "u256", "ua", "usize", "uz", "v000", "v00",
		"v0", "v0s", "v00t", "v0u", "v001", "v01", "v1", "v009", "v09", "v9", "v010", "v10",
		"w005s09t", "w5s009t", "x64", "x86", "x86_32", "x86_64", "x86_128", "x87", "zyxw",
	}

	got := slices.Clone(want)
	slices.Reverse(got)
	slices.SortStableFunc(got, versionsort.Compare)

	if !slices.Equal(got, want) {
		t.Fatalf("version sort gave\n%q\nwant\n%q", got, want)
	}
}

func TestLongNumbers(t *testing.T) {
	t.Parallel()

	if versionsort.Compare("v99999999999999999999999", "v100000000000000000000000") != -1 {
		t.Fatal("a number past 64 bits must still compare by value")
	}
}
