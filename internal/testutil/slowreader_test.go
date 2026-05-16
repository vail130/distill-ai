package testutil_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/testutil"
)

func TestSlowReader_ChunksBySize(t *testing.T) {
	r := &testutil.SlowReader{
		Inner:     strings.NewReader("abcdef"),
		ChunkSize: 2,
	}
	buf := make([]byte, 10)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n != 2 {
		t.Errorf("first Read returned %d bytes, want 2", n)
	}
	if string(buf[:n]) != "ab" {
		t.Errorf("first Read returned %q, want %q", buf[:n], "ab")
	}
}

func TestSlowReader_HonoursDelay(t *testing.T) {
	r := &testutil.SlowReader{
		Inner:      strings.NewReader("xy"),
		ChunkSize:  1,
		ChunkDelay: 10 * time.Millisecond,
	}
	start := time.Now()
	buf := make([]byte, 1)
	_, _ = r.Read(buf)
	elapsed := time.Since(start)
	if elapsed < 8*time.Millisecond {
		t.Errorf("Read returned in %v; expected ≥ 8ms (ChunkDelay=10ms)", elapsed)
	}
}

func TestSlowReader_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &testutil.SlowReader{
		Inner:      strings.NewReader("data"),
		ChunkSize:  1,
		ChunkDelay: 200 * time.Millisecond,
		Ctx:        ctx,
	}
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	buf := make([]byte, 1)
	_, err := r.Read(buf)
	elapsed := time.Since(start)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Read returned %v, want context.Canceled", err)
	}
	if elapsed > 150*time.Millisecond {
		t.Errorf("Read took %v after cancel; should have unblocked promptly", elapsed)
	}
}

func TestSlowReader_NilInner(t *testing.T) {
	r := &testutil.SlowReader{}
	_, err := r.Read(make([]byte, 4))
	if err == nil {
		t.Error("nil Inner should produce an error")
	}
}

func TestSlowReader_ZeroSizeDefaultsToOne(t *testing.T) {
	r := &testutil.SlowReader{
		Inner:     strings.NewReader("xy"),
		ChunkSize: 0,
	}
	buf := make([]byte, 10)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n != 1 {
		t.Errorf("ChunkSize=0 returned %d bytes; want 1 (normalised default)", n)
	}
}

func TestSlowReader_DrainsToEOF(t *testing.T) {
	r := &testutil.SlowReader{
		Inner:     strings.NewReader("hello"),
		ChunkSize: 2,
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(out) != "hello" {
		t.Errorf("ReadAll = %q, want %q", out, "hello")
	}
}
