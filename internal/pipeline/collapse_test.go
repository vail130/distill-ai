package pipeline

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/vail130/distill-ai/internal/event"
)

func eventWithStack(frames []event.StackFrame) event.Event {
	return event.Event{
		Title:  "boom",
		Frames: frames,
		Body:   []string{"boom"},
	}
}

func TestCollapseStage_EventWithoutFrames(t *testing.T) {
	in := []event.Event{{Title: "no frames"}}
	src := &channelSource{events: in, buf: 1}
	sink := &collectSink{}
	p := &Pipeline{
		Source: src,
		Stages: []Stage{CollapseStage{}},
		Sink:   sink,
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.got) != 1 {
		t.Fatalf("got %d events, want 1", len(sink.got))
	}
	if sink.got[0].FramesCollapsed != 0 {
		t.Errorf("FramesCollapsed=%d, want 0", sink.got[0].FramesCollapsed)
	}
}

func TestCollapseStage_CollapsesVendorFrames(t *testing.T) {
	frames := []event.StackFrame{
		{File: "app/main.py"},
		{File: "/lib/python3.11/site-packages/requests/api.py"},
		{File: "/lib/python3.11/site-packages/requests/sessions.py"},
		{File: "app/handler.py"},
	}
	src := &channelSource{events: []event.Event{eventWithStack(frames)}, buf: 1}
	sink := &collectSink{}
	p := &Pipeline{
		Source: src,
		Stages: []Stage{CollapseStage{KeepVendor: false}},
		Sink:   sink,
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.got) != 1 {
		t.Fatalf("got %d events, want 1", len(sink.got))
	}
	if sink.got[0].FramesCollapsed != 2 {
		t.Errorf("FramesCollapsed=%d, want 2", sink.got[0].FramesCollapsed)
	}
	if len(sink.got[0].Frames) != 2 {
		t.Errorf("len(Frames)=%d, want 2", len(sink.got[0].Frames))
	}
}

func TestCollapseStage_KeepVendorRespected(t *testing.T) {
	frames := []event.StackFrame{
		{File: "app/main.py"},
		{File: "/lib/python3.11/site-packages/requests/api.py"},
		{File: "app/handler.py"},
	}
	src := &channelSource{events: []event.Event{eventWithStack(frames)}, buf: 1}
	sink := &collectSink{}
	p := &Pipeline{
		Source: src,
		Stages: []Stage{CollapseStage{KeepVendor: true}},
		Sink:   sink,
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := sink.got[0]
	if out.FramesCollapsed != 0 {
		t.Errorf("FramesCollapsed=%d, want 0", out.FramesCollapsed)
	}
	if len(out.Frames) != 3 {
		t.Fatalf("len(Frames)=%d, want 3", len(out.Frames))
	}
	if !out.Frames[1].Vendor {
		t.Error("vendor frame should still carry Vendor=true under KeepVendor")
	}
}

func TestCollapseStage_ContextCancellation(t *testing.T) {
	runtime.GC()
	before := runtime.NumGoroutine()
	srcCh := make(chan event.Event)
	sink := &collectSink{}
	p := &Pipeline{
		Source: readerSource{ch: srcCh},
		Stages: []Stage{CollapseStage{}},
		Sink:   sink,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("Run did not return within 2s of cancel")
	}
	close(srcCh)
	time.Sleep(50 * time.Millisecond)
	runtime.GC()
	after := runtime.NumGoroutine()
	if after-before > 2 {
		t.Errorf("goroutine leak: before=%d after=%d", before, after)
	}
}
