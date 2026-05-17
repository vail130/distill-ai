package gotest_test

import (
	"strings"
	"testing"
)

// TestGotest_ParseExtractsFrames — a panic block produces an Event
// whose Frames slice has at least one entry with the bottom user-
// code frame's File and Line. The Vendor flag is left false; the
// pipeline's CollapseStage re-classifies later.
func TestGotest_ParseExtractsFrames(t *testing.T) {
	input := `panic: runtime error: nil pointer dereference

goroutine 1 [running]:
example.com/proj.fetch(...)
	/home/u/proj/fetch.go:21
example.com/proj.run()
	/home/u/proj/run.go:10 +0x1a
main.main()
	/home/u/proj/main.go:5 +0x0c
exit status 2
`
	got := parseAll(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1", len(got))
	}
	ev := got[0]
	if len(ev.Frames) == 0 {
		t.Fatalf("frames empty; expected at least one frame")
	}
	files := make(map[string]bool)
	for _, fr := range ev.Frames {
		files[fr.File] = true
	}
	for _, want := range []string{
		"/home/u/proj/fetch.go",
		"/home/u/proj/run.go",
		"/home/u/proj/main.go",
	} {
		if !files[want] {
			t.Errorf("expected frame file %q; got files=%v", want, files)
		}
	}
	// Location should point at the first user frame (none of the
	// frames above are runtime / testing, so the first frame wins).
	if ev.Location == nil {
		t.Fatalf("location nil; want first user frame")
	}
	if !strings.HasSuffix(ev.Location.File, "fetch.go") {
		t.Errorf("location.file = %q; want first user frame fetch.go", ev.Location.File)
	}
}

// TestGotest_ParseFramesSkipRuntime — panics whose top frames are
// runtime / testing helpers skip them when populating Location.
func TestGotest_ParseFramesSkipRuntime(t *testing.T) {
	input := `panic: oops

goroutine 7 [running]:
runtime.gopanic({0x1, 0x2})
	/usr/local/go/src/runtime/panic.go:884 +0x21
testing.tRunner.func1(0xc000)
	/usr/local/go/src/testing/testing.go:1234 +0x1a
example.com/proj.TestX(...)
	/home/u/proj/x_test.go:9
exit status 2
`
	got := parseAll(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1", len(got))
	}
	if got[0].Location == nil {
		t.Fatalf("location nil")
	}
	if !strings.HasSuffix(got[0].Location.File, "x_test.go") {
		t.Errorf("location.file = %q; want x_test.go (user frame)", got[0].Location.File)
	}
}

// TestGotest_ParseRaceCondition — a real race-detector report
// emits one Event with Kind="race_condition", Body retains the
// full block including dividers, and Frames are extracted from
// both contained goroutine stacks.
func TestGotest_ParseRaceCondition(t *testing.T) {
	input := `==================
WARNING: DATA RACE
Write at 0x00c0000a0008 by goroutine 7:
  example.com/proj.set(...)
      /home/u/proj/race.go:10 +0x1a

Previous read at 0x00c0000a0008 by goroutine 8:
  example.com/proj.get(...)
      /home/u/proj/race.go:15 +0x2b

Goroutine 7 (running) created at:
  example.com/proj.run(...)
      /home/u/proj/run.go:7 +0x0c
==================
FAIL
exit status 66
FAIL	example.com/proj	0.123s
`
	got := parseAll(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1: %+v", len(got), got)
	}
	ev := got[0]
	if ev.Kind != "race_condition" {
		t.Errorf("kind = %q, want race_condition", ev.Kind)
	}
	if ev.Title != "WARNING: DATA RACE" {
		t.Errorf("title = %q, want canonical race header", ev.Title)
	}
	if ev.Metadata["race_goroutines"] != "2" {
		t.Errorf("metadata.race_goroutines = %q, want \"2\"", ev.Metadata["race_goroutines"])
	}
	if len(ev.Frames) == 0 {
		t.Errorf("frames empty; expected entries from both stacks")
	}
}

// TestGotest_ParseJSONMode — `go test -json` output dispatches to
// the JSON scanner. Each `fail` Action emits a test_failure Event
// whose Body is assembled from the corresponding `output` Actions.
func TestGotest_ParseJSONMode(t *testing.T) {
	input := strings.Join([]string{
		`{"Time":"2024-01-01T00:00:00Z","Action":"run","Package":"example.com/proj","Test":"TestX"}`,
		`{"Time":"2024-01-01T00:00:00Z","Action":"output","Package":"example.com/proj","Test":"TestX","Output":"=== RUN   TestX\n"}`,
		`{"Time":"2024-01-01T00:00:00Z","Action":"output","Package":"example.com/proj","Test":"TestX","Output":"    x_test.go:1: nope\n"}`,
		`{"Time":"2024-01-01T00:00:00Z","Action":"output","Package":"example.com/proj","Test":"TestX","Output":"--- FAIL: TestX (0.01s)\n"}`,
		`{"Time":"2024-01-01T00:00:00Z","Action":"fail","Package":"example.com/proj","Test":"TestX","Elapsed":0.01}`,
		`{"Time":"2024-01-01T00:00:00Z","Action":"fail","Package":"example.com/proj","Elapsed":0.01}`,
	}, "\n") + "\n"
	got := parseAll(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1: %+v", len(got), got)
	}
	ev := got[0]
	if ev.Kind != "test_failure" {
		t.Errorf("kind = %q, want test_failure", ev.Kind)
	}
	if ev.Metadata["test_id"] != "TestX" {
		t.Errorf("test_id = %q, want TestX", ev.Metadata["test_id"])
	}
	if ev.Metadata["package"] != "example.com/proj" {
		t.Errorf("package = %q, want example.com/proj", ev.Metadata["package"])
	}
	if ev.Location == nil || ev.Location.File != "x_test.go" || ev.Location.Line != 1 {
		t.Errorf("location = %+v; want x_test.go:1", ev.Location)
	}
	bodyJoined := strings.Join(ev.Body, "\n")
	if strings.Contains(bodyJoined, "=== RUN") {
		t.Errorf("body leaked === RUN framing: %q", bodyJoined)
	}
	if strings.Contains(bodyJoined, "--- FAIL") {
		t.Errorf("body leaked --- FAIL framing: %q", bodyJoined)
	}
}

// TestGotest_ParseJSONBuildFailure — build errors arrive as Output
// actions with Test == "" and the `path:line:col:` shape. They
// emit one build_failure Event per matched line.
func TestGotest_ParseJSONBuildFailure(t *testing.T) {
	input := strings.Join([]string{
		`{"Time":"2024-01-01T00:00:00Z","Action":"output","Package":"example.com/proj","Output":"foo.go:42:7: undefined: bar\n"}`,
		`{"Time":"2024-01-01T00:00:00Z","Action":"output","Package":"example.com/proj","Output":"FAIL\texample.com/proj [build failed]\n"}`,
		`{"Time":"2024-01-01T00:00:00Z","Action":"fail","Package":"example.com/proj","Elapsed":0}`,
	}, "\n") + "\n"
	got := parseAll(t, input)
	if len(got) != 1 {
		t.Fatalf("got %d events; want 1: %+v", len(got), got)
	}
	if got[0].Kind != "build_failure" {
		t.Errorf("kind = %q, want build_failure", got[0].Kind)
	}
	if got[0].Title != "undefined: bar" {
		t.Errorf("title = %q", got[0].Title)
	}
	if got[0].Metadata["package"] != "example.com/proj" {
		t.Errorf("metadata.package = %q", got[0].Metadata["package"])
	}
}
