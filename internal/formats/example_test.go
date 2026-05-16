package formats_test

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
)

// echoFormat is a minimal Format that emits one Event per line of
// input. Real formats extract structured events from noisier input,
// but the contract (init-time registration, cheap Detect, streaming
// Parse, channel close on EOF or cancellation) is the same.
type echoFormat struct{}

func (echoFormat) Name() string { return "echo" }

func (echoFormat) Detect(sample []byte) event.Confidence {
	// Claim every input; a real format would inspect sample for
	// markers and return a low confidence on no match.
	if len(sample) == 0 {
		return 0
	}
	return 1.0
}

func (echoFormat) Parse(ctx context.Context, r io.Reader, _ formats.ParseOpts) (<-chan event.Event, error) {
	out := make(chan event.Event)
	go func() {
		defer close(out)
		buf, err := io.ReadAll(r)
		if err != nil {
			return
		}
		for _, line := range bytes.Split(buf, []byte("\n")) {
			if len(line) == 0 {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case out <- event.Event{
				Severity: event.SeverityInfo,
				Kind:     "line",
				Title:    string(line),
				Body:     []string{string(line)},
				Count:    1,
			}:
			}
		}
	}()
	return out, nil
}

// Example demonstrates the minimum viable Format implementation:
// a Name, a Detect that scores an input sample, and a Parse that
// streams Events on a channel and closes it on EOF.
func Example() {
	var f formats.Format = echoFormat{}
	ctx := context.Background()
	in := bytes.NewBufferString("one\ntwo\n")
	ch, _ := f.Parse(ctx, in, formats.ParseOpts{})
	for ev := range ch {
		fmt.Println(ev.Title)
	}
	// Output:
	// one
	// two
}
