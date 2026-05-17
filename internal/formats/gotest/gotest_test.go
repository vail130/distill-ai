package gotest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/event"
	"github.com/vail130/distill-ai/internal/formats"
	_ "github.com/vail130/distill-ai/internal/formats/gotest"
)

// TestGotest_DetectFailMarker — the canonical default-reporter
// failure header anchors detection at the highest confidence.
func TestGotest_DetectFailMarker(t *testing.T) {
	f, ok := formats.Get("gotest")
	if !ok {
		t.Fatal("gotest not registered")
	}
	sample := []byte("--- FAIL: TestLogin (0.02s)\n")
	if got := f.Detect(sample); got != event.Confidence(1.0) {
		t.Errorf("Detect(--- FAIL:) = %v, want 1.0", got)
	}
}

// TestGotest_DetectFailPackage — the per-package summary line with
// a Go-package-shaped token anchors at the highest confidence.
func TestGotest_DetectFailPackage(t *testing.T) {
	f, _ := formats.Get("gotest")
	sample := []byte("FAIL\tgithub.com/vail130/distill-ai/internal/event\t1.234s\n")
	if got := f.Detect(sample); got != event.Confidence(1.0) {
		t.Errorf("Detect(FAIL\\tpkg) = %v, want 1.0", got)
	}
}

// TestGotest_DetectRunHeader — the `-v` reporter's per-test header
// anchors at the highest confidence.
func TestGotest_DetectRunHeader(t *testing.T) {
	f, _ := formats.Get("gotest")
	sample := []byte("=== RUN   TestFoo\n")
	if got := f.Detect(sample); got != event.Confidence(1.0) {
		t.Errorf("Detect(=== RUN) = %v, want 1.0", got)
	}
}

// TestGotest_DetectFailRequiresPackageToken — a bare `FAIL` line
// without a package-shaped token must not raise the score. Prevents
// unrelated tools (mocha's `FAIL`, generic CI prose) from claiming
// gotest output.
func TestGotest_DetectFailRequiresPackageToken(t *testing.T) {
	f, _ := formats.Get("gotest")
	for _, sample := range []string{
		"FAIL: rebooting node\n",
		"some FAIL state was reached\n",
		"FAIL\trebooting\n", // single-segment token, no dots or slashes
	} {
		got := f.Detect([]byte(sample))
		if got == event.Confidence(1.0) {
			t.Errorf("Detect(%q) = %v; want < 1.0 (no package token)", sample, got)
		}
	}
}

// TestGotest_DetectGoroutineDump — a goroutine dump plus a Go file
// reference scores at the fuzzy confidence, catching bare panics
// from `go run` without test scaffolding.
func TestGotest_DetectGoroutineDump(t *testing.T) {
	f, _ := formats.Get("gotest")
	sample := []byte(`panic: runtime error: index out of range

goroutine 1 [running]:
main.main()
	/home/u/proj/main.go:42 +0x1a
`)
	if got := f.Detect(sample); got != event.Confidence(0.8) {
		t.Errorf("Detect(goroutine + .go:N) = %v, want 0.8", got)
	}
}

// TestGotest_DetectGoroutineWithoutGoFile — a goroutine dump alone,
// without any `.go:N` reference, does not raise the score. Keeps
// the fuzzy match from claiming non-Go runtime dumps that happen
// to use the word "goroutine".
func TestGotest_DetectGoroutineWithoutGoFile(t *testing.T) {
	f, _ := formats.Get("gotest")
	sample := []byte("goroutine 1 [running]:\nsome lines without go file refs\n")
	if got := f.Detect(sample); got != 0 {
		t.Errorf("Detect(goroutine without .go:N) = %v, want 0", got)
	}
}

// TestGotest_DetectNegative — unrelated formats (Python traceback,
// JSON logs) score 0.
func TestGotest_DetectNegative(t *testing.T) {
	f, _ := formats.Get("gotest")
	for _, sample := range []string{
		`Traceback (most recent call last):
  File "foo.py", line 1, in <module>
ValueError: nope
`,
		`{"level":"error","msg":"boom"}` + "\n",
		"Hello, world.\n",
		"",
	} {
		if got := f.Detect([]byte(sample)); got != 0 {
			t.Errorf("Detect(%q) = %v, want 0", sample, got)
		}
	}
}

// TestGotest_RegisteredAtInit — the side-effect import registers
// the format under the name "gotest".
func TestGotest_RegisteredAtInit(t *testing.T) {
	f, ok := formats.Get("gotest")
	if !ok {
		t.Fatal("formats.Get(\"gotest\") returned !ok")
	}
	if f.Name() != "gotest" {
		t.Errorf("Name() = %q, want \"gotest\"", f.Name())
	}
}

// TestGotest_ParseEmptyStub — M10.1 ships the stub Parse that
// closes the channel immediately. M10.2 fills it in. The early
// stub lets the autodetect → parse path work end-to-end while
// the scanner is still under construction.
func TestGotest_ParseEmptyStub(t *testing.T) {
	f, _ := formats.Get("gotest")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := f.Parse(ctx, strings.NewReader("--- FAIL: TestX (0.01s)\n"), formats.ParseOpts{})
	if err != nil {
		t.Fatalf("Parse err = %v, want nil", err)
	}
	count := 0
	for range ch {
		count++
	}
	if count != 0 {
		t.Errorf("got %d events from stub Parse; want 0", count)
	}
}
