package tokens_test

import (
	"sync"
	"testing"

	"github.com/vail130/distill-ai/internal/tokens"
)

func TestTiktoken_FactorySucceeds(t *testing.T) {
	est, err := tokens.Tiktoken()
	if err != nil {
		t.Fatalf("Tiktoken: %v", err)
	}
	if est == nil {
		t.Fatal("Tiktoken returned nil estimator")
	}
}

func TestTiktoken_EmptyString(t *testing.T) {
	est, err := tokens.Tiktoken()
	if err != nil {
		t.Fatalf("Tiktoken: %v", err)
	}
	if got := est.Estimate(""); got != 0 {
		t.Errorf("Estimate(\"\") = %d, want 0", got)
	}
}

// TestTiktoken_KnownCounts verifies the encoder produces the exact
// cl100k_base token counts for a small reference corpus. These
// counts were obtained from OpenAI's reference Python tiktoken and
// are the ground truth; any deviation means the encoding broke.
//
// If these ever fail after a tiktoken-go version bump, do not adjust
// the expectations without checking the upstream change first.
func TestTiktoken_KnownCounts(t *testing.T) {
	est, err := tokens.Tiktoken()
	if err != nil {
		t.Fatalf("Tiktoken: %v", err)
	}
	cases := []struct {
		name string
		text string
		want int
	}{
		{"hello-world", "hello world", 2},
		{"simple-sentence", "The quick brown fox jumps over the lazy dog.", 10},
		{"single-token", "hello", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := est.Estimate(c.text)
			if got != c.want {
				t.Errorf("Estimate(%q) = %d, want %d (cl100k_base exact count)", c.text, got, c.want)
			}
		})
	}
}

func TestTiktoken_LazyInitOnce(t *testing.T) {
	est, err := tokens.Tiktoken()
	if err != nil {
		t.Fatalf("Tiktoken: %v", err)
	}
	// 100 concurrent calls must not race or produce different
	// results. The internal sync.Once gates the BPE load.
	const goroutines = 100
	results := make([]int, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			results[i] = est.Estimate("concurrent test input")
		}(i)
	}
	wg.Wait()
	first := results[0]
	if first == 0 {
		t.Fatal("Estimate returned 0 on a non-empty input; init likely failed")
	}
	for i, r := range results {
		if r != first {
			t.Errorf("goroutine %d: result %d, want %d (all should match)", i, r, first)
		}
	}
}

// TestTiktoken_NoNetwork is the build-time guard against a future
// regression that re-enables tiktoken-go's default network loader.
// The offline loader is wired in package init(); this test calls a
// probe that will fail if the loader is misconfigured. With the
// offline loader, loading cl100k_base never touches the network.
func TestTiktoken_NoNetwork(t *testing.T) {
	if !tokens.TestLoaderConfigured() {
		t.Fatal("tiktoken offline loader is not configured; the default tiktoken-go loader would download the BPE vocab from the network on first use, violating distill-ai's no-network rule")
	}
}

func TestTiktoken_HandlesLongInput(t *testing.T) {
	est, err := tokens.Tiktoken()
	if err != nil {
		t.Fatalf("Tiktoken: %v", err)
	}
	// 16 KB of arbitrary text; the encoder must not crash and
	// must produce a non-trivial count.
	var s []byte
	for len(s) < 16*1024 {
		s = append(s, []byte("the quick brown fox jumps over the lazy dog. ")...)
	}
	got := est.Estimate(string(s))
	if got < 1000 {
		t.Errorf("16 KB input encoded to %d tokens; expected > 1000", got)
	}
}

func TestByName_Heuristic(t *testing.T) {
	est, err := tokens.ByName(tokens.NameHeuristic)
	if err != nil {
		t.Fatalf("ByName(heuristic): %v", err)
	}
	if est == nil {
		t.Fatal("ByName(heuristic) returned nil")
	}
	// Sanity: the heuristic produces a non-zero count for non-empty
	// input. We can't assert exact equality with Default() because
	// the interface hides the concrete type.
	if est.Estimate("hello world") == 0 {
		t.Error("heuristic Estimate returned 0 on non-empty input")
	}
}

func TestByName_EmptyStringIsHeuristic(t *testing.T) {
	// "" should select the heuristic per the documented CLI default.
	est, err := tokens.ByName("")
	if err != nil {
		t.Fatalf("ByName(\"\"): %v", err)
	}
	if est == nil {
		t.Fatal("ByName(\"\") returned nil")
	}
}

func TestByName_Tiktoken(t *testing.T) {
	est, err := tokens.ByName(tokens.NameTiktoken)
	if err != nil {
		t.Fatalf("ByName(tiktoken): %v", err)
	}
	if est == nil {
		t.Fatal("ByName(tiktoken) returned nil")
	}
}

func TestByName_Unknown(t *testing.T) {
	_, err := tokens.ByName("bogus")
	if err == nil {
		t.Error("ByName(bogus) should return an error")
	}
}
