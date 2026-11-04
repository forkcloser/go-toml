package toml_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"runtime"
	"strconv"
	"testing"
	"time"

	tomltest "github.com/toml-lang/toml-test/v2"

	"github.com/forkcloser/go-toml"
)

// suiteDecoder is the decoder toml-test's runner drives: it parses each case,
// requires the round trip to be exact, and answers with the decoded values in the
// suite's tagged JSON.
type suiteDecoder struct{}

func (suiteDecoder) Cmd() []string {
	return []string{"github.com/forkcloser/go-toml"}
}

// Run answers a parse or decode failure as the case's output, which is what the suite
// expects of an invalid case, and keeps the error result for failures of the harness.
//
//revive:disable-next-line:function-result-limit // the signature is tomltest.Parser's
func (suiteDecoder) Run(_ context.Context, input string) (int, string, bool, error) {
	doc, parseErr := toml.Parse([]byte(input))
	if parseErr != nil {
		return 0, parseErr.Error(), true, nil //nolint:nilerr // a parse failure is the case's output, not the harness's
	}

	if got := string(doc.Bytes()); got != input {
		return 0, "", false, fmt.Errorf("%w:\n%q\nbecame\n%q", errRoundTrip, input, got)
	}

	var values map[string]any
	if decodeErr := doc.Decode(&values); decodeErr != nil {
		return 0, decodeErr.Error(), true, nil //nolint:nilerr // a decode failure is the case's output, not the harness's
	}

	tagged, err := tag(values)
	if err != nil {
		return 0, "", false, err
	}

	out, err := json.Marshal(tagged)
	if err != nil {
		return 0, "", false, fmt.Errorf("encode the tagged values: %w", err)
	}

	return 0, string(out), false, nil
}

var (
	errUntaggable = errors.New("no tagged form for value")
	errRoundTrip  = errors.New("the round trip changed the document")
)

// tag gives each value the type the suite's JSON names it by.
func tag(value any) (any, error) {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))

		for key, item := range v {
			tagged, err := tag(item)
			if err != nil {
				return nil, err
			}

			out[key] = tagged
		}

		return out, nil
	case []any:
		out := make([]any, len(v))

		for i, item := range v {
			tagged, err := tag(item)
			if err != nil {
				return nil, err
			}

			out[i] = tagged
		}

		return out, nil
	case string:
		return typed("string", v), nil
	case bool:
		return typed("bool", strconv.FormatBool(v)), nil
	case int64:
		return typed("integer", strconv.FormatInt(v, 10)), nil
	case float64:
		return typed("float", formatFloat(v)), nil
	case time.Time:
		return typed("datetime", v.Format(time.RFC3339Nano)), nil
	case toml.LocalDateTime:
		return typed("datetime-local", v.String()), nil
	case toml.LocalDate:
		return typed("date-local", v.String()), nil
	case toml.LocalTime:
		return typed("time-local", v.String()), nil
	default:
		return nil, fmt.Errorf("%w: %T", errUntaggable, value)
	}
}

func typed(name, value string) map[string]string {
	return map[string]string{"type": name, "value": value}
}

func formatFloat(f float64) string {
	switch {
	case math.IsNaN(f):
		return "nan"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	default:
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
}

func TestCompliance(t *testing.T) {
	t.Parallel()

	runner := tomltest.NewRunner(tomltest.Runner{
		Decoder:  suiteDecoder{},
		Version:  "1.1.0",
		Parallel: runtime.NumCPU(),
	})

	results, err := runner.Run()
	if err != nil {
		t.Fatalf("run the suite: %v", err)
	}

	if results.PassedValid == 0 || results.PassedInvalid == 0 {
		t.Fatalf("the suite ran no cases: %+v", results)
	}

	for _, result := range results.Tests {
		if !result.Failed() {
			continue
		}

		t.Errorf("%s: %s\ninput:\n%s\noutput:\n%s\nwant:\n%s",
			result.Path, result.Failure, result.Input, result.Output, result.Want)
	}

	t.Logf("valid: %d passed, %d failed; invalid: %d passed, %d failed; skipped: %d",
		results.PassedValid, results.FailedValid, results.PassedInvalid, results.FailedInvalid, results.Skipped)
}
