package output

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/pipeline"
)

func TestJSONSink_Goldens(t *testing.T) {
	for _, c := range loadCases(t, "json") {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			var buf bytes.Buffer
			s := &JSONSink{
				Writer:        &buf,
				NoFooter:      c.NoFooter,
				FormatName:    c.FormatName,
				InputLines:    c.InputLines,
				Streaming:     c.Streaming,
				EstimatorName: c.EstimatorName,
				ExitCode:      c.ExitCode,
			}
			feedSink(t, s, c.Events)
			ext := "json"
			if c.Streaming {
				ext = "ndjson"
			}
			goldenCompare(t, "json", ext, c, buf.Bytes())
		})
	}
}

// TestJSONSink_SchemaVersionMatchesDoc cross-checks output.SchemaVersion
// against the "Current schema version" line in docs/formats/SCHEMA.md.
// Catches the case where one was bumped without the other.
func TestJSONSink_SchemaVersionMatchesDoc(t *testing.T) {
	path := findSchemaDoc(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	needle := fmt.Sprintf("Current schema version: `%d`", SchemaVersion)
	if !strings.Contains(string(raw), needle) {
		t.Fatalf("SCHEMA.md does not contain %q; either output.SchemaVersion or the doc has drifted", needle)
	}
}

func TestJSONSink_BatchProducesSingleObject(t *testing.T) {
	evs := []event.Event{
		simpleEvent("error", "a"),
		simpleEvent("warn", "b"),
	}
	var buf bytes.Buffer
	s := &JSONSink{Writer: &buf, FormatName: "pytest"}
	feedSink(t, s, evs)
	var got struct {
		SchemaVersion int             `json:"schema_version"`
		Format        string          `json:"format"`
		Events        []event.Event   `json:"events"`
		Summary       json.RawMessage `json:"summary"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if got.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version=%d want %d", got.SchemaVersion, SchemaVersion)
	}
	if got.Format != "pytest" {
		t.Errorf("format=%q want pytest", got.Format)
	}
	if len(got.Events) != len(evs) {
		t.Errorf("events len=%d want %d", len(got.Events), len(evs))
	}
}

func TestJSONSink_StreamingProducesNDJSON(t *testing.T) {
	evs := []event.Event{
		simpleEvent("error", "a"),
		simpleEvent("warn", "b"),
	}
	var buf bytes.Buffer
	s := &JSONSink{Writer: &buf, FormatName: "pytest", Streaming: true}
	feedSink(t, s, evs)
	lines := bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n"))
	if got, want := len(lines), len(evs)+1; got != want {
		t.Fatalf("ndjson line count=%d want %d\noutput:\n%s", got, want, buf.String())
	}
	for i, line := range lines {
		var obj map[string]any
		if err := json.Unmarshal(line, &obj); err != nil {
			t.Fatalf("line %d is not JSON: %v\nline=%s", i, err, line)
		}
		if obj["schema_version"].(float64) != float64(SchemaVersion) {
			t.Errorf("line %d schema_version mismatch", i)
		}
		if i < len(evs) {
			if _, ok := obj["event"]; !ok {
				t.Errorf("line %d missing event key", i)
			}
			if _, ok := obj["summary"]; ok {
				t.Errorf("line %d should not have summary key", i)
			}
		} else {
			if _, ok := obj["summary"]; !ok {
				t.Errorf("trailer line missing summary key")
			}
			if _, ok := obj["event"]; ok {
				t.Errorf("trailer line should not have event key")
			}
		}
	}
}

func TestJSONSink_MetadataSortedKeys(t *testing.T) {
	ev := simpleEvent("error", "x")
	ev.Metadata = map[string]string{"c": "3", "a": "1", "b": "2"}
	var buf bytes.Buffer
	s := &JSONSink{Writer: &buf, FormatName: "pytest", Streaming: true}
	feedSink(t, s, []event.Event{ev})
	// Find the event line and look for the metadata block.
	idx := bytes.Index(buf.Bytes(), []byte(`"metadata":{`))
	if idx == -1 {
		t.Fatalf("expected metadata block: %s", buf.String())
	}
	frag := string(buf.Bytes()[idx : idx+40])
	posA := strings.Index(frag, `"a"`)
	posB := strings.Index(frag, `"b"`)
	posC := strings.Index(frag, `"c"`)
	if posA >= posB || posB >= posC {
		t.Errorf("metadata keys not in sorted order: %s", frag)
	}
}

func TestJSONSink_NullLocationMarshalsAsNull(t *testing.T) {
	ev := simpleEvent("error", "x")
	ev.Location = nil
	var buf bytes.Buffer
	s := &JSONSink{Writer: &buf, FormatName: "pytest"}
	feedSink(t, s, []event.Event{ev})
	// Indented batch output has "location": null with a space; ndjson
	// has "location":null. Accept either.
	if !bytes.Contains(buf.Bytes(), []byte(`"location": null`)) &&
		!bytes.Contains(buf.Bytes(), []byte(`"location":null`)) {
		t.Fatalf("expected location null in output:\n%s", buf.String())
	}
}

func TestJSONSink_NoFooterIsNoOp(t *testing.T) {
	evs := []event.Event{simpleEvent("error", "x")}
	var withFooter bytes.Buffer
	withSink := &JSONSink{Writer: &withFooter, FormatName: "pytest"}
	feedSink(t, withSink, evs)
	var withoutFooter bytes.Buffer
	withoutSink := &JSONSink{Writer: &withoutFooter, FormatName: "pytest", NoFooter: true}
	feedSink(t, withoutSink, evs)
	if !bytes.Equal(withFooter.Bytes(), withoutFooter.Bytes()) {
		t.Errorf("NoFooter should be a no-op for JSONSink; outputs differ\nwith:\n%s\nwithout:\n%s",
			withFooter.String(), withoutFooter.String())
	}
}

func TestJSONSink_NilWriterErrors(t *testing.T) {
	s := &JSONSink{}
	ch := make(chan event.Event)
	close(ch)
	if err := s.Sink(context.Background(), ch); err == nil {
		t.Fatalf("expected error for nil Writer")
	}
}

func TestJSONSink_FooterReflectsCounters(t *testing.T) {
	counters := &pipeline.BudgetCounters{
		EventsDroppedBudget: 5,
		EstimatedTokens:     200,
	}
	var buf bytes.Buffer
	s := &JSONSink{
		Writer:     &buf,
		FormatName: "pytest",
		InputLines: 99,
		Counters:   counters,
		ExitCode:   3,
	}
	feedSink(t, s, []event.Event{simpleEvent("error", "x")})
	var got struct {
		Summary summary `json:"summary"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}
	if got.Summary.EventsDroppedBudget != 5 {
		t.Errorf("events_dropped_budget=%d want 5", got.Summary.EventsDroppedBudget)
	}
	if got.Summary.EstimatedTokens != 200 {
		t.Errorf("estimated_tokens=%d want 200", got.Summary.EstimatedTokens)
	}
	if got.Summary.ExitCode != 3 {
		t.Errorf("exit_code=%d want 3", got.Summary.ExitCode)
	}
	if got.Summary.InputLines != 99 {
		t.Errorf("input_lines=%d want 99", got.Summary.InputLines)
	}
}

// TestJSONSink_LineSourceWinsOverStaticInputLines proves the CLI's
// LineCounter wiring contract: a LineSource installed at Run time
// supersedes the static InputLines value set at construction. The
// JSON summary records the LineSource value.
func TestJSONSink_LineSourceWinsOverStaticInputLines(t *testing.T) {
	var buf bytes.Buffer
	s := &JSONSink{
		Writer:     &buf,
		FormatName: "pytest",
		InputLines: 1, // stale fallback
		LineSource: FixedLineSource(123),
	}
	feedSink(t, s, []event.Event{simpleEvent("error", "x")})
	var got struct {
		Summary summary `json:"summary"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, buf.String())
	}
	if got.Summary.InputLines != 123 {
		t.Errorf("input_lines=%d want 123 (LineSource); InputLines field was 1", got.Summary.InputLines)
	}
}

// TestJSONSink_SummarySchemaMatchesDoc verifies that every JSON tag on
// the summary struct appears in docs/formats/SCHEMA.md's Summary field
// reference. Parallel to TestEvent_JSONSchemaMatchesDoc; catches the
// case where a counter is added to BudgetCounters and threaded into
// the wire summary without a SCHEMA.md row to match.
func TestJSONSink_SummarySchemaMatchesDoc(t *testing.T) {
	path := findSchemaDoc(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	doc := string(raw)
	ty := reflect.TypeOf(summary{})
	for i := 0; i < ty.NumField(); i++ {
		f := ty.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		needle := "`" + name + "`"
		if !strings.Contains(doc, needle) {
			t.Errorf("summary.%s: JSON tag %q (looking for %s) not found in SCHEMA.md; struct and doc have drifted",
				f.Name, name, needle)
		}
	}
}

// findSchemaDoc walks up from the test's working directory to locate
// docs/formats/SCHEMA.md. Mirrors the helper in event_test.go.
func findSchemaDoc(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(dir, "docs", "formats", "SCHEMA.md")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate docs/formats/SCHEMA.md")
	return ""
}
