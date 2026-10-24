package client

import "testing"

// Reading an event should cost one allocation: the message body. The frame
// header and the control-message buffer belong to the context, so a busy
// connection does not turn into garbage for the collector to walk.
func TestReadMsgAllocatesOnlyTheBody(t *testing.T) {
	ctx, peer := socketPairContext(t)
	const (
		frames = 200
		runs   = 150
	)
	frame := testFrame(1, 0, make([]byte, 16))
	for range frames {
		if _, err := peer.Write(frame); err != nil {
			t.Fatal(err)
		}
	}

	allocs := testing.AllocsPerRun(runs, func() {
		if _, _, _, _, err := ctx.ReadMsg(); err != nil {
			t.Fatalf("ReadMsg() error = %v", err)
		}
	})
	if allocs > 1 {
		t.Fatalf("ReadMsg() allocated %v times per event, want at most 1 (the message body)", allocs)
	}
}

// BenchmarkReadMsg measures the cost of reading one event.
func BenchmarkReadMsg(b *testing.B) {
	ctx, peer := socketPairContext(b)
	frame := testFrame(1, 0, make([]byte, 16))
	write := make(chan struct{})
	go func() {
		for range write {
			if _, err := peer.Write(frame); err != nil {
				return
			}
		}
	}()
	defer close(write)

	b.ReportAllocs()
	for b.Loop() {
		write <- struct{}{}
		if _, _, _, _, err := ctx.ReadMsg(); err != nil {
			b.Fatalf("ReadMsg() error = %v", err)
		}
	}
}
