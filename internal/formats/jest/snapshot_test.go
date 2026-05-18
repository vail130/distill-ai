package jest_test

import (
	"strings"
	"testing"
)

// TestJest_ParseSnapshotMismatch — a file-backed snapshot block
// promotes the Event Kind to snapshot_mismatch, captures the
// snapshot name from the `Snapshot name: ...` line, and sets
// snapshot_kind="file".
func TestJest_ParseSnapshotMismatch(t *testing.T) {
	input := "FAIL src/render.test.js\n" +
		"  ● Render › renders header\n" +
		"\n" +
		"    expect(received).toMatchSnapshot()\n" +
		"\n" +
		"    Snapshot name: `Render renders header 1`\n" +
		"\n" +
		"    - Snapshot  - 1\n" +
		"    + Received  + 1\n" +
		"\n" +
		"      Object {\n" +
		"    -   \"a\": 1,\n" +
		"    +   \"a\": 2,\n" +
		"      }\n" +
		"\n" +
		"      at Object.<anonymous> (src/render.test.js:8:21)\n" +
		"\n" +
		"Test Suites: 1 failed, 1 total\n"
	events := parseString(t, input)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	ev := events[0]
	if ev.Kind != "snapshot_mismatch" {
		t.Errorf("Kind = %q, want snapshot_mismatch", ev.Kind)
	}
	if ev.Title != "Snapshot mismatch: Render renders header 1" {
		t.Errorf("Title = %q, want Snapshot mismatch: Render renders header 1", ev.Title)
	}
	if ev.Metadata["snapshot_kind"] != "file" {
		t.Errorf("snapshot_kind = %q, want file", ev.Metadata["snapshot_kind"])
	}
	if ev.Metadata["snapshot_truncated"] != "" {
		t.Errorf("snapshot_truncated = %q, want empty",
			ev.Metadata["snapshot_truncated"])
	}
	// The diff lines must survive intact in Body.
	wantBody := []string{"- Snapshot  - 1", "+ Received  + 1",
		`-   "a": 1,`, `+   "a": 2,`}
	for _, want := range wantBody {
		found := false
		for _, line := range ev.Body {
			if strings.Contains(line, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Body missing diff line %q; got %v", want, ev.Body)
		}
	}
}

// TestJest_ParseInlineSnapshotMismatch — an inline snapshot block
// uses the generic Title (no per-call name printed by jest) and
// sets snapshot_kind="inline".
func TestJest_ParseInlineSnapshotMismatch(t *testing.T) {
	input := `FAIL src/render.test.js
  ● Render › renders inline

    expect(received).toMatchInlineSnapshot()

    Expected:
      "expected"
    Received:
      "actual"

      at Object.<anonymous> (src/render.test.js:8:21)

Test Suites: 1 failed, 1 total
`
	events := parseString(t, input)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	ev := events[0]
	if ev.Kind != "snapshot_mismatch" {
		t.Errorf("Kind = %q, want snapshot_mismatch", ev.Kind)
	}
	if ev.Title != "Snapshot mismatch" {
		t.Errorf("Title = %q, want generic Snapshot mismatch", ev.Title)
	}
	if ev.Metadata["snapshot_kind"] != "inline" {
		t.Errorf("snapshot_kind = %q, want inline", ev.Metadata["snapshot_kind"])
	}
}

// TestJest_ParseSnapshotMaxLines — a snapshot block exceeding the
// maxSnapshotLines cap (200) is truncated. The last accepted Body
// entry is the sentinel and metadata.snapshot_truncated is set.
func TestJest_ParseSnapshotMaxLines(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("FAIL src/render.test.js\n")
	sb.WriteString("  ● Render › big snapshot\n\n")
	sb.WriteString("    expect(received).toMatchSnapshot()\n\n")
	sb.WriteString("    Snapshot name: `Render big snapshot 1`\n\n")
	// 250 diff lines is well over the 200 cap (Body also includes
	// the header lines above, so the cap fires faster than 250
	// raw diff lines — the test only needs to overflow the cap by
	// any margin to exercise the truncation branch).
	for i := 0; i < 250; i++ {
		sb.WriteString("    +   line ")
		sb.WriteString(strings.Repeat("x", 5))
		sb.WriteByte('\n')
	}
	sb.WriteString("      at Object.<anonymous> (src/render.test.js:8:21)\n")
	sb.WriteString("Test Suites: 1 failed, 1 total\n")
	events := parseString(t, sb.String())
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	ev := events[0]
	if len(ev.Body) > 200 {
		t.Errorf("Body length %d exceeds maxSnapshotLines=200", len(ev.Body))
	}
	if last := ev.Body[len(ev.Body)-1]; last != "... [snapshot truncated]" {
		t.Errorf("Body[last] = %q, want sentinel", last)
	}
	if ev.Metadata["snapshot_truncated"] != "true" {
		t.Errorf("snapshot_truncated = %q, want true",
			ev.Metadata["snapshot_truncated"])
	}
}

// TestJest_ParseSnapshotAndOrdinaryFailure — a fixture containing
// one snapshot mismatch and one ordinary failure emits two
// Events with the correct distinct Kinds, in source order.
func TestJest_ParseSnapshotAndOrdinaryFailure(t *testing.T) {
	input := "FAIL src/mix.test.js\n" +
		"  ● Mix › snapshot test\n" +
		"\n" +
		"    expect(received).toMatchSnapshot()\n" +
		"\n" +
		"    Snapshot name: `Mix snapshot test 1`\n" +
		"\n" +
		"    - Snapshot\n" +
		"    + Received\n" +
		"\n" +
		"      at src/mix.test.js:5:5\n" +
		"\n" +
		"  ● Mix › ordinary test\n" +
		"\n" +
		"    Error: nope\n" +
		"\n" +
		"      at src/mix.test.js:15:5\n" +
		"\n" +
		"Test Suites: 1 failed, 1 total\n"
	events := parseString(t, input)
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Kind != "snapshot_mismatch" {
		t.Errorf("events[0].Kind = %q, want snapshot_mismatch", events[0].Kind)
	}
	if events[1].Kind != "test_failure" {
		t.Errorf("events[1].Kind = %q, want test_failure", events[1].Kind)
	}
	// Ensure the snapshot's diff doesn't leak into the next
	// Event's body (a state-machine regression risk).
	for _, line := range events[1].Body {
		if strings.Contains(line, "Snapshot") {
			t.Errorf("events[1].Body leaked snapshot line: %q", line)
		}
	}
}
