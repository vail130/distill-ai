package tokens_test

import (
	"strings"
	"testing"

	"github.com/vail130/distill-ai/internal/tokens"
)

// BenchmarkHeuristic_Estimate reports MB/sec for the default
// heuristic. The budget enforcer (M6) calls Estimate once per Event,
// so per-call latency matters more than raw throughput, but a
// throughput number is the easier comparison point across machines.
//
// Target per .opencode/rules/performance.md: ≥ 100 MB/sec on a
// typical laptop. The benchmark sets b.SetBytes so the reported
// MB/s number is automatic.
//
// Run with: make bench
// Or:        go test -bench=BenchmarkHeuristic -benchmem ./internal/tokens/
func BenchmarkHeuristic_Estimate(b *testing.B) {
	// 1 KiB of representative text: a mix of words and symbols
	// approximating distilled log output.
	chunk := "[ERROR] failed to connect: dial tcp 10.0.0.1:8080: i/o timeout (after 3 retries) "
	text := strings.Repeat(chunk, 1+1024/len(chunk))[:1024]
	h := tokens.Default()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Estimate(text)
	}
}

// BenchmarkHeuristic_EstimateLong tests the per-byte cost at scale:
// a 64 KiB input should amortise allocator overhead and give a
// clearer picture of the inner-loop throughput.
func BenchmarkHeuristic_EstimateLong(b *testing.B) {
	const size = 64 * 1024
	chunk := "[ERROR] failed to connect: dial tcp 10.0.0.1:8080: i/o timeout (after 3 retries) "
	text := strings.Repeat(chunk, 1+size/len(chunk))[:size]
	h := tokens.Default()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Estimate(text)
	}
}
