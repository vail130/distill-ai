package gitlabci_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/detect"
	"github.com/vail130/distill-ai/internal/envelope"
	"github.com/vail130/distill-ai/internal/envelope/gitlabci"
	"github.com/vail130/distill-ai/internal/event"
	_ "github.com/vail130/distill-ai/internal/formats/gotest" // detection round-trip
	"github.com/vail130/distill-ai/internal/testutil"
)

func TestGitLab_RegisteredAtInit(t *testing.T) {
	got, ok := envelope.Get(gitlabci.Name)
	if !ok {
		t.Fatalf("envelope.Get(%q) not found; init() side effect failed", gitlabci.Name)
	}
	if got.Name() != gitlabci.Name {
		t.Errorf("Name() = %q, want %q", got.Name(), gitlabci.Name)
	}
}

func TestGitLab_DetectSectionMarker(t *testing.T) {
	cases := []struct {
		name, sample string
		want         event.Confidence
	}{
		{"start-with-cr", "section_start:1700000000:build\r\nfoo\n", 1.0},
		{"start-without-cr", "section_start:1700000000:build\nfoo\n", 1.0},
		{"end-with-cr", "foo\nsection_end:1700000000:build\r\n", 1.0},
		{"mixed-case-name", "section_start:1700000000:BuildSection\n", 1.0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := gitlabci.Stripper{}.Detect([]byte(tc.sample))
			if got != tc.want {
				t.Errorf("Detect(%q) = %v, want %v", tc.sample, got, tc.want)
			}
		})
	}
}

func TestGitLab_DetectRunnerBanner(t *testing.T) {
	// Five distinct CSI sequences + the runner banner → 0.8.
	sample := "" +
		"Running with gitlab-runner 16.0.0 (abcdef12)\n" +
		"\x1b[31mError\x1b[0m \x1b[32mmsg\x1b[0m \x1b[33mmore\x1b[0m\n" +
		"\x1b[34mblue\x1b[0m \x1b[35mmagenta\x1b[0m\n"
	got := gitlabci.Stripper{}.Detect([]byte(sample))
	if got != 0.8 {
		t.Errorf("Detect = %v, want 0.8", got)
	}
}

func TestGitLab_DetectNegativeOnPlainLogs(t *testing.T) {
	got := gitlabci.Stripper{}.Detect([]byte("hello\nworld\n"))
	if got != 0 {
		t.Errorf("Detect on plain text = %v, want 0", got)
	}
}

func TestGitLab_DetectBannerWithoutDenseANSI(t *testing.T) {
	// Banner is present but only one CSI sequence — below the
	// fuzzy threshold of 5.
	sample := "Running with gitlab-runner 16.0.0\n\x1b[31mone color\x1b[0m\n"
	got := gitlabci.Stripper{}.Detect([]byte(sample))
	if got != 0 {
		t.Errorf("Detect = %v, want 0 (insufficient CSI density)", got)
	}
}

func TestGitLab_StripSectionMarkers(t *testing.T) {
	input := "" +
		"section_start:1700000000:build\r\n" +
		"compiling foo\n" +
		"section_start:1700000001:subsection\r\n" +
		"sub-line\n" +
		"section_end:1700000001:subsection\r\n" +
		"compiling bar\n" +
		"section_end:1700000000:build\r\n"
	want := "compiling foo\nsub-line\ncompiling bar\n"
	cleaned, signals := stripAll(t, input)
	if cleaned != want {
		t.Errorf("cleaned = %q, want %q", cleaned, want)
	}
	if len(signals) != 0 {
		t.Errorf("expected 0 signals, got %d", len(signals))
	}
}

func TestGitLab_StripCRLF(t *testing.T) {
	input := "line one\r\nline two\r\nline three\n"
	want := "line one\nline two\nline three\n"
	cleaned, _ := stripAll(t, input)
	if cleaned != want {
		t.Errorf("cleaned = %q, want %q", cleaned, want)
	}
}

func TestGitLab_JobFailureSignalInsideSection(t *testing.T) {
	input := "" +
		"section_start:1700000000:run_tests\r\n" +
		"FAIL: TestFoo\n" +
		"ERROR: Job failed: exit code 2\n" +
		"section_end:1700000000:run_tests\r\n"
	cleaned, signals := stripAll(t, input)
	if cleaned != "FAIL: TestFoo\n" {
		t.Errorf("cleaned = %q; only the body line should survive", cleaned)
	}
	if len(signals) != 1 {
		t.Fatalf("got %d signals, want 1", len(signals))
	}
	s := signals[0]
	if s.Kind != envelope.KindEnvelopeStepFailure {
		t.Errorf("Kind = %q, want %q", s.Kind, envelope.KindEnvelopeStepFailure)
	}
	if s.Severity != event.SeverityError {
		t.Errorf("Severity = %q, want %q", s.Severity, event.SeverityError)
	}
	if s.Metadata["exit_code"] != "2" {
		t.Errorf("Metadata[\"exit_code\"] = %q, want %q", s.Metadata["exit_code"], "2")
	}
	if s.Metadata["step"] != "run_tests" {
		t.Errorf("Metadata[\"step\"] = %q, want %q", s.Metadata["step"], "run_tests")
	}
	if s.Title != "run_tests" {
		t.Errorf("Title = %q, want %q", s.Title, "run_tests")
	}
}

func TestGitLab_JobFailureSignalOutsideSection(t *testing.T) {
	// When the runner emits the failure line after section_end
	// (the common case), step name is empty.
	input := "" +
		"section_start:1700000000:run_tests\r\n" +
		"some output\n" +
		"section_end:1700000000:run_tests\r\n" +
		"ERROR: Job failed: exit code 1\n"
	_, signals := stripAll(t, input)
	if len(signals) != 1 {
		t.Fatalf("got %d signals, want 1", len(signals))
	}
	s := signals[0]
	if s.Metadata["exit_code"] != "1" {
		t.Errorf("Metadata[\"exit_code\"] = %q, want \"1\"", s.Metadata["exit_code"])
	}
	if s.Metadata["step"] != "" {
		t.Errorf("Metadata[\"step\"] = %q, want \"\" (no section open)", s.Metadata["step"])
	}
}

// TestGitLab_StripPreservesInnerFormatBytes is the round-trip test:
// a real gotest run wrapped in GitLab CI sections must still detect
// as gotest after the stripper has had its turn.
func TestGitLab_StripPreservesInnerFormatBytes(t *testing.T) {
	wrapped := "" +
		"Running with gitlab-runner 16.0.0 (abcdef12)\r\n" +
		"section_start:1700000000:run_go_test\r\n" +
		"=== RUN   TestLogin\r\n" +
		"--- FAIL: TestLogin (0.02s)\r\n" +
		"    auth_test.go:42: expected 200, got 502\r\n" +
		"FAIL\r\n" +
		"FAIL\tgithub.com/example/m\t0.123s\r\n" +
		"section_end:1700000000:run_go_test\r\n" +
		"ERROR: Job failed: exit code 1\r\n"
	cleaned, _, stripper, err := envelope.Wrap(
		context.Background(),
		strings.NewReader(wrapped),
		envelope.Options{Choice: envelope.ChoiceAuto},
	)
	if err != nil {
		t.Fatalf("envelope.Wrap: %v", err)
	}
	if stripper.Name() != gitlabci.Name {
		t.Fatalf("Wrap picked %q, want %q", stripper.Name(), gitlabci.Name)
	}
	res, err := detect.Detect(context.Background(), cleaned, detect.Opts{})
	if err != nil {
		t.Fatalf("detect.Detect on cleaned bytes: %v", err)
	}
	if res.Format.Name() != "gotest" {
		t.Errorf("detector picked %q on cleaned bytes, want \"gotest\"; sample=%q",
			res.Format.Name(), string(res.Sample))
	}
	if res.Confidence < 1.0 {
		t.Errorf("detector confidence = %v on cleaned bytes, want 1.0", res.Confidence)
	}
}

func TestGitLab_StreamingStripsIncrementally(t *testing.T) {
	input := "" +
		"section_start:1700000000:build\r\n" +
		"line one\r\n" +
		"line two\r\n" +
		"section_end:1700000000:build\r\n"
	src := &testutil.SlowReader{
		Inner:      strings.NewReader(input),
		ChunkSize:  4,
		ChunkDelay: 20 * time.Millisecond,
	}
	cleaned, signals, err := gitlabci.Stripper{}.Strip(context.Background(), src)
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	go func() {
		for range signals { //nolint:revive // empty body intentional: discard.
		}
	}()
	buf := make([]byte, 16)
	deadline := time.Now().Add(150 * time.Millisecond)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("first cleaned byte did not arrive within 150ms")
		}
		n, err := cleaned.Read(buf)
		if n > 0 {
			return
		}
		if err != nil && err != io.EOF {
			t.Fatalf("cleaned.Read: %v", err)
		}
	}
}

func TestGitLab_StripContextCancellation(t *testing.T) {
	hang := &blockingReader{ch: make(chan struct{})}
	defer close(hang.ch)
	ctx, cancel := context.WithCancel(context.Background())
	cleaned, signals, err := gitlabci.Stripper{}.Strip(ctx, hang)
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	cancel()
	go func() { _, _ = io.ReadAll(cleaned) }()
	select {
	case _, ok := <-signals:
		if ok {
			t.Errorf("signal arrived after cancellation")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("signals channel did not close within 500ms after cancel")
	}
}

func stripAll(t *testing.T, input string) (cleaned string, signals []event.Event) {
	t.Helper()
	r, sigCh, err := gitlabci.Stripper{}.Strip(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range sigCh {
			signals = append(signals, ev)
		}
	}()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	<-done
	return string(out), signals
}

type blockingReader struct {
	ch chan struct{}
}

func (b *blockingReader) Read(_ []byte) (int, error) {
	<-b.ch
	return 0, io.EOF
}
