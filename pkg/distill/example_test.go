package distill_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vail130/distill-ai/pkg/distill"
)

// exampleFixture is a tiny gotest-fail sample inlined so the
// examples don't depend on testdata files.
const exampleFixture = `=== RUN   TestExample
    example_test.go:1: failed assertion
--- FAIL: TestExample (0.00s)
FAIL	example.com/m	0.001s
FAIL
`

// ExampleDistill_text demonstrates the minimum invocation: distil
// stdin to stdout in the default text format. Most library callers
// use this shape — pass an io.Reader, get encoded output via an
// io.Writer, ignore the channel.
func ExampleDistill_text() {
	out := &bytes.Buffer{}
	events, summary, err := distill.Distill(context.Background(),
		strings.NewReader(exampleFixture),
		distill.Options{Writer: out, Format: "gotest"},
	)
	if err != nil {
		fmt.Println("setup error:", err)
		return
	}
	// Drain the Event channel even though we don't need
	// programmatic access — otherwise the internal teeing sink
	// blocks waiting for someone to read it.
	for range events { //nolint:revive // discarding events
	}
	summary.Wait()
	fmt.Println("emitted:", summary.EventsEmitted)
	// Output: emitted: 1
}

// ExampleDistill_jsonBatch distils to a bytes.Buffer using the
// batch JSON encoder, parses the resulting JSON, and prints the
// summary's exit_code field. Shows how a library caller can read
// the encoder's own summary trailer rather than relying on the
// Summary struct.
func ExampleDistill_jsonBatch() {
	out := &bytes.Buffer{}
	events, summary, err := distill.Distill(context.Background(),
		strings.NewReader(exampleFixture),
		distill.Options{
			Writer: out,
			Format: "gotest",
			Output: distill.OutputJSON,
		},
	)
	if err != nil {
		fmt.Println("setup error:", err)
		return
	}
	for range events { //nolint:revive // discarding events
	}
	summary.Wait()
	// The library Summary and the JSON encoder's trailer agree.
	var batch struct {
		Summary struct {
			ExitCode int `json:"exit_code"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(out.Bytes(), &batch); err != nil {
		fmt.Println("parse error:", err)
		return
	}
	fmt.Println("summary.exit_code:", batch.Summary.ExitCode)
	fmt.Println("ExitCodeFromSummary:", distill.ExitCodeFromSummary(summary))
	// Output:
	// summary.exit_code: 0
	// ExitCodeFromSummary: 0
}

// ExampleDistill_jsonStreaming consumes events as they arrive in
// the OutputJSONStreaming (ndjson) mode. Each line of out is one
// JSON object; the consumer parses them incrementally rather than
// waiting for EOF. The Event channel is drained in parallel — for
// most programmatic consumers, reading the Event channel directly
// is simpler than parsing ndjson, but the JSON path matters when
// the consumer is a different language or another tool.
func ExampleDistill_jsonStreaming() {
	out := &bytes.Buffer{}
	events, summary, err := distill.Distill(context.Background(),
		strings.NewReader(exampleFixture),
		distill.Options{
			Writer: out,
			Format: "gotest",
			Output: distill.OutputJSONStreaming,
		},
	)
	if err != nil {
		fmt.Println("setup error:", err)
		return
	}
	// Count Events as they arrive on the channel.
	channelCount := 0
	for range events {
		channelCount++
	}
	summary.Wait()
	fmt.Println("channel saw:", channelCount)
	fmt.Println("summary says:", summary.EventsEmitted)
	// Output:
	// channel saw: 1
	// summary says: 1
}

// ExampleDistill_explicitFormat bypasses autodetection by setting
// Options.Format. Useful when the caller knows the source format
// and wants to skip the detection step (saves a 4 KiB read on the
// input).
func ExampleDistill_explicitFormat() {
	out := &bytes.Buffer{}
	events, summary, err := distill.Distill(context.Background(),
		strings.NewReader(exampleFixture),
		distill.Options{
			Writer: out,
			Format: "gotest", // explicit, no detect
		},
	)
	if err != nil {
		fmt.Println("setup error:", err)
		return
	}
	for range events { //nolint:revive // discarding events
	}
	summary.Wait()
	fmt.Println("ok, emitted:", summary.EventsEmitted)
	// Output: ok, emitted: 1
}

// ExampleDistill_contextCancellation shows how to cancel a Distill
// run early. The pipeline drains, the channel closes, the Summary
// populates with whatever the pipeline observed at cancellation
// time. Useful for callers with a timeout or a parent context that
// might be cancelled by the user.
func ExampleDistill_contextCancellation() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &bytes.Buffer{}
	events, summary, err := distill.Distill(ctx,
		strings.NewReader(exampleFixture),
		distill.Options{Writer: out, Format: "gotest"},
	)
	if err != nil {
		fmt.Println("setup error:", err)
		return
	}
	// Cancel after the first Event.
	for range events {
		cancel()
		break
	}
	// Drain whatever's left so the pipeline goroutine exits.
	for range events { //nolint:revive // discarding events
	}
	summary.Wait()
	fmt.Println("done")
	// Output: done
}
