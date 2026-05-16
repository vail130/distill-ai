// Package testutil provides shared helpers for distill-ai's own tests.
// It is intended to be imported only from _test.go files in this
// repository; the package is not part of the public API and may change
// without notice.
package testutil

import (
	"context"
	"errors"
	"io"
	"time"
)

// SlowReader wraps an io.Reader and emits its bytes in fixed-size
// chunks with a configurable delay between chunks. It is used by
// streaming property tests to prove that Format.Parse and the pipeline
// emit Events incrementally rather than buffering the whole input
// until EOF.
//
// SlowReader is safe for use only inside tests. It blocks the caller
// for ChunkDelay between reads, which is fine for test goroutines but
// would be pathological in production.
//
// Zero values:
//
//   - Inner is required; nil means the reader returns io.ErrUnexpectedEOF
//     on the first Read.
//   - ChunkSize ≤ 0 is normalised to 1 (one byte per read).
//   - ChunkDelay ≤ 0 means no delay; the reader behaves like Inner.
//   - Ctx, when non-nil, is honoured during the delay: a cancelled
//     context unblocks the Read immediately with ctx.Err().
type SlowReader struct {
	// Inner is the underlying reader.
	Inner io.Reader

	// ChunkSize is the maximum number of bytes returned per Read.
	// Values ≤ 0 are treated as 1.
	ChunkSize int

	// ChunkDelay is the time to sleep before each Read returns.
	ChunkDelay time.Duration

	// Ctx, if set, causes Read to return ctx.Err() when the context
	// is cancelled mid-delay. Optional; tests that don't need
	// cancellation can leave it nil.
	Ctx context.Context
}

// Read implements io.Reader. It honours the configured ChunkSize and
// ChunkDelay. Errors from the inner reader propagate unchanged.
func (s *SlowReader) Read(p []byte) (int, error) {
	if s.Inner == nil {
		return 0, io.ErrUnexpectedEOF
	}
	if s.ChunkDelay > 0 {
		if s.Ctx != nil {
			t := time.NewTimer(s.ChunkDelay)
			select {
			case <-s.Ctx.Done():
				if !t.Stop() {
					<-t.C
				}
				return 0, s.Ctx.Err()
			case <-t.C:
			}
		} else {
			time.Sleep(s.ChunkDelay)
		}
	}
	size := s.ChunkSize
	if size <= 0 {
		size = 1
	}
	if size > len(p) {
		size = len(p)
	}
	return s.Inner.Read(p[:size])
}

// ErrSlowReaderClosed is returned when a SlowReader's context is
// cancelled. It is sentinel so callers can errors.Is past the wrapper.
var ErrSlowReaderClosed = errors.New("slowreader: context cancelled")
