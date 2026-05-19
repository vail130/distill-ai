package dockercompose_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/detect"
	"github.com/vail130/distill-ai/internal/envelope"
	"github.com/vail130/distill-ai/internal/envelope/dockercompose"
	"github.com/vail130/distill-ai/internal/event"
	_ "github.com/vail130/distill-ai/internal/formats/gotest" // detection round-trip
	"github.com/vail130/distill-ai/internal/testutil"
)

func TestDockerCompose_RegisteredAtInit(t *testing.T) {
	got, ok := envelope.Get(dockercompose.Name)
	if !ok {
		t.Fatalf("envelope.Get(%q) not found; init() side effect failed", dockercompose.Name)
	}
	if got.Name() != dockercompose.Name {
		t.Errorf("Name() = %q, want %q", got.Name(), dockercompose.Name)
	}
}

func TestDockerCompose_DetectClearMarker(t *testing.T) {
	cases := []struct {
		name, sample string
		want         event.Confidence
	}{
		{
			name:   "first-line-prefixed",
			sample: "testrunner-1  | === RUN   TestThing\ntestrunner-1  | --- FAIL: TestThing\n",
			want:   1.0,
		},
		{
			name:   "single-service-no-replica",
			sample: "api  | starting up\napi  | listening on :8080\n",
			want:   1.0,
		},
		{
			name:   "multi-service-aligned-padding",
			sample: "api     | up\nworker  | up\ndb      | up\n",
			want:   1.0,
		},
		{
			name:   "leading-blank-lines-ignored",
			sample: "\n\ntestrunner-1  | first real line\n",
			want:   1.0,
		},
		{
			// docker compose with one attached service emits a
			// single space before `|` — no column padding to
			// align against. Common shape for
			// `docker compose run` against one service.
			name:   "single-space-single-service",
			sample: "testrunner-1 | === RUN TestFoo\ntestrunner-1 | PASS\n",
			want:   1.0,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := dockercompose.Stripper{}.Detect([]byte(tc.sample))
			if got != tc.want {
				t.Errorf("Detect = %v, want %v; sample=%q", got, tc.want, tc.sample)
			}
		})
	}
}

func TestDockerCompose_DetectFuzzyWithPreamble(t *testing.T) {
	// First line is image-pull output (no prefix); later lines
	// carry the prefix. Five distinct prefixed lines hit the
	// fuzzy threshold.
	sample := "" +
		"[+] Pulling testrunner ...\n" +
		"[+] Pull complete\n" +
		"Attaching to testrunner-1\n" +
		"testrunner-1  | line one\n" +
		"testrunner-1  | line two\n" +
		"testrunner-1  | line three\n" +
		"testrunner-1  | line four\n"
	got := dockercompose.Stripper{}.Detect([]byte(sample))
	if got != 0.8 {
		t.Errorf("Detect on prefixed-after-preamble = %v, want 0.8", got)
	}
}

func TestDockerCompose_DetectNegative(t *testing.T) {
	cases := []struct {
		name, sample string
	}{
		{"plain-text", "hello\nworld\n"},
		{"single-pipe-coincidence", "echo a | tee b\nls -l\n"},
		{"uppercase-service-name", "TestRunner-1  | this is uppercase, not a service name\n"},
		{"too-few-prefixed-lines", "preamble\nmore preamble\nsvc  | one\nsvc  | two\nsvc  | three\n"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := dockercompose.Stripper{}.Detect([]byte(tc.sample))
			if got != 0 {
				t.Errorf("Detect = %v, want 0; sample=%q", got, tc.sample)
			}
		})
	}
}

func TestDockerCompose_StripBasic(t *testing.T) {
	input := "" +
		"testrunner-1  | === RUN   TestLogin\n" +
		"testrunner-1  | --- FAIL: TestLogin (0.02s)\n" +
		"testrunner-1  |     auth_test.go:42: expected 200, got 502\n" +
		"testrunner-1  | FAIL\n"
	want := "" +
		"=== RUN   TestLogin\n" +
		"--- FAIL: TestLogin (0.02s)\n" +
		"    auth_test.go:42: expected 200, got 502\n" +
		"FAIL\n"
	cleaned, signals := stripAll(t, input)
	if cleaned != want {
		t.Errorf("cleaned = %q, want %q", cleaned, want)
	}
	if len(signals) != 0 {
		t.Errorf("expected 0 signals (docker-compose stripper emits none), got %d", len(signals))
	}
}

func TestDockerCompose_StripMultiServiceAlignment(t *testing.T) {
	// Real docker compose pads every prefix to the same column so
	// the `|` separator aligns. Both shorter and longer service
	// names must work.
	input := "" +
		"api      | up\n" +
		"db       | ready\n" +
		"worker   | starting\n"
	want := "" +
		"up\n" +
		"ready\n" +
		"starting\n"
	cleaned, _ := stripAll(t, input)
	if cleaned != want {
		t.Errorf("cleaned = %q, want %q", cleaned, want)
	}
}

func TestDockerCompose_PassesThroughUnprefixedLines(t *testing.T) {
	// Preamble lines (image pulls, attach banner) and any line
	// that doesn't match the prefix shape must survive intact —
	// docker compose interleaves them with attached output and
	// the inner-format detector handles them as ordinary bytes.
	input := "" +
		"[+] Pulling testrunner ...\n" +
		"Attaching to testrunner-1\n" +
		"testrunner-1  | === RUN TestFoo\n" +
		"testrunner-1  | PASS\n"
	want := "" +
		"[+] Pulling testrunner ...\n" +
		"Attaching to testrunner-1\n" +
		"=== RUN TestFoo\n" +
		"PASS\n"
	cleaned, _ := stripAll(t, input)
	if cleaned != want {
		t.Errorf("cleaned = %q, want %q", cleaned, want)
	}
}

// TestDockerCompose_StripPreservesInnerFormatBytes is the round-trip
// test that closes KNOWN_ISSUES.md #2: a docker-compose-wrapped
// gotest run must detect as gotest after the stripper has had its
// turn.
func TestDockerCompose_StripPreservesInnerFormatBytes(t *testing.T) {
	wrapped := "" +
		"Attaching to testrunner-1\n" +
		"testrunner-1  | === RUN   TestLogin\n" +
		"testrunner-1  | --- FAIL: TestLogin (0.02s)\n" +
		"testrunner-1  |     auth_test.go:42: expected 200, got 502\n" +
		"testrunner-1  | FAIL\n" +
		"testrunner-1  | FAIL\tgithub.com/example/m\t0.123s\n"
	cleaned, _, stripper, err := envelope.Wrap(
		context.Background(),
		strings.NewReader(wrapped),
		envelope.Options{Choice: envelope.ChoiceAuto},
	)
	if err != nil {
		t.Fatalf("envelope.Wrap: %v", err)
	}
	if stripper.Name() != dockercompose.Name {
		t.Fatalf("Wrap picked %q, want %q", stripper.Name(), dockercompose.Name)
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

func TestDockerCompose_StreamingStripsIncrementally(t *testing.T) {
	input := "" +
		"testrunner-1  | first\n" +
		"testrunner-1  | second\n"
	src := &testutil.SlowReader{
		Inner:      strings.NewReader(input),
		ChunkSize:  4,
		ChunkDelay: 20 * time.Millisecond,
	}
	cleaned, signals, err := dockercompose.Stripper{}.Strip(context.Background(), src)
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	go func() {
		for range signals { //nolint:revive // discard
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

func TestDockerCompose_StripContextCancellation(t *testing.T) {
	hang := &blockingReader{ch: make(chan struct{})}
	defer close(hang.ch)
	ctx, cancel := context.WithCancel(context.Background())
	cleaned, _, err := dockercompose.Stripper{}.Strip(ctx, hang)
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.ReadAll(cleaned)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Strip did not exit within 500ms after cancel")
	}
}

func stripAll(t *testing.T, input string) (cleaned string, signals []event.Event) {
	t.Helper()
	r, sigCh, err := dockercompose.Stripper{}.Strip(context.Background(), strings.NewReader(input))
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
