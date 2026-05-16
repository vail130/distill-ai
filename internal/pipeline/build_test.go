package pipeline

import (
	"context"
	"testing"

	"github.com/vail130/distill-ai/internal/event"
)

func TestBuild_DefaultsAreSafe(t *testing.T) {
	events := []event.Event{
		{Title: "a"}, {Title: "a"}, {Title: "b"},
	}
	src := &channelSource{events: events, buf: 4}
	sink := &collectSink{}
	p := Build(src, sink, Options{})
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// DedupeWindow=0: every event passes through.
	if len(sink.got) != 3 {
		t.Fatalf("got %d events, want 3 (dedupe should be off)", len(sink.got))
	}
	for _, ev := range sink.got {
		if ev.Count != 1 {
			t.Errorf("Count=%d, want 1 with DedupeWindow=0", ev.Count)
		}
	}
}

func TestBuild_DedupeAndCollapseChainTogether(t *testing.T) {
	frames := []event.StackFrame{
		{File: "app/main.py"},
		{File: "/lib/python3.11/site-packages/x.py"},
		{File: "/lib/python3.11/site-packages/y.py"},
		{File: "app/handler.py"},
	}
	ev := event.Event{
		Title:    "boom",
		Location: &event.Location{File: "app/main.py", Line: 1},
		Frames:   frames,
	}
	events := []event.Event{ev, ev, ev, ev}
	src := &channelSource{events: events, buf: 4}
	sink := &collectSink{}
	p := Build(src, sink, Options{DedupeWindow: 8})
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.got) != 1 {
		t.Fatalf("got %d events, want 1", len(sink.got))
	}
	out := sink.got[0]
	if out.Count != 4 {
		t.Errorf("Count=%d, want 4", out.Count)
	}
	if out.FramesCollapsed != 2 {
		t.Errorf("FramesCollapsed=%d, want 2", out.FramesCollapsed)
	}
	if len(out.Frames) != 2 {
		t.Errorf("len(Frames)=%d, want 2 after collapse", len(out.Frames))
	}
}

func TestBuild_StageOrder_CollapseBeforeDedupe(t *testing.T) {
	p := Build(&channelSource{}, &collectSink{}, Options{})
	if len(p.Stages) != 2 {
		t.Fatalf("got %d stages, want 2", len(p.Stages))
	}
	if _, ok := p.Stages[0].(CollapseStage); !ok {
		t.Errorf("stage 0 = %T, want CollapseStage", p.Stages[0])
	}
	if _, ok := p.Stages[1].(DedupeStage); !ok {
		t.Errorf("stage 1 = %T, want DedupeStage", p.Stages[1])
	}
}

func TestBuild_OptionsForwarded(t *testing.T) {
	p := Build(&channelSource{}, &collectSink{}, Options{
		DedupeWindow: 99,
		KeepVendor:   true,
		BufferSize:   42,
	})
	if p.BufferSize != 42 {
		t.Errorf("BufferSize=%d, want 42", p.BufferSize)
	}
	collapse, ok := p.Stages[0].(CollapseStage)
	if !ok || !collapse.KeepVendor {
		t.Errorf("Stages[0]=%+v, want CollapseStage{KeepVendor:true}", p.Stages[0])
	}
	dedupe, ok := p.Stages[1].(DedupeStage)
	if !ok || dedupe.Window != 99 {
		t.Errorf("Stages[1]=%+v, want DedupeStage{Window:99}", p.Stages[1])
	}
}

func TestBuild_KeepVendorPreservesFramesEndToEnd(t *testing.T) {
	frames := []event.StackFrame{
		{File: "app/main.py"},
		{File: "/lib/python3.11/site-packages/x.py"},
		{File: "app/handler.py"},
	}
	ev := event.Event{Title: "boom", Frames: frames}
	src := &channelSource{events: []event.Event{ev}, buf: 1}
	sink := &collectSink{}
	p := Build(src, sink, Options{KeepVendor: true, DedupeWindow: 0})
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.got) != 1 {
		t.Fatalf("got %d events, want 1", len(sink.got))
	}
	if got := sink.got[0]; len(got.Frames) != 3 || got.FramesCollapsed != 0 {
		t.Errorf("KeepVendor: Frames=%+v FramesCollapsed=%d, want 3/0", got.Frames, got.FramesCollapsed)
	}
	if !sink.got[0].Frames[1].Vendor {
		t.Error("vendor frame should still carry Vendor=true under KeepVendor")
	}
}
