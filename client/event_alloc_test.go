package client

import (
	"testing"
)

// TestReadMsgAllocatesNothingForAnEmptyEvent is the allocation guard for the
// event loop. A pointer-motion stream delivers thousands of events a second,
// so the header and the control-message buffer must not be allocated per
// read: two kilobytes of garbage per event is a garbage collector's problem.
func TestReadMsgAllocatesNothingForAnEmptyEvent(t *testing.T) {
	ctx, peer := socketPairContext(t)
	frame := testFrame(1, 7, nil)
	const runs = 32
	// AllocsPerRun calls the function once to warm it up, so the socket needs
	// one frame more than the number of measured reads.
	for i := 0; i < runs+1; i++ {
		if _, err := peer.Write(frame); err != nil {
			t.Fatal(err)
		}
	}
	allocs := testing.AllocsPerRun(runs, func() {
		if _, _, _, _, err := ctx.ReadMsg(); err != nil {
			t.Fatalf("ReadMsg() error = %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("ReadMsg allocated %v objects per event, want none", allocs)
	}
}

// TestReadMsgAllocatesTheBodyAndNothingElse covers the other half: the body
// the caller receives is the one allocation a read may make.
func TestReadMsgAllocatesTheBodyAndNothingElse(t *testing.T) {
	ctx, peer := socketPairContext(t)
	frame := testFrame(1, 7, []byte{1, 2, 3, 4})
	const runs = 32
	for i := 0; i < runs+1; i++ {
		if _, err := peer.Write(frame); err != nil {
			t.Fatal(err)
		}
	}
	allocs := testing.AllocsPerRun(runs, func() {
		if _, _, _, _, err := ctx.ReadMsg(); err != nil {
			t.Fatalf("ReadMsg() error = %v", err)
		}
	})
	if allocs != 1 {
		t.Fatalf("ReadMsg allocated %v objects per event, want only the body", allocs)
	}
}

// BenchmarkReadMsg measures the read path. A feeder keeps the socket full so
// the measured loop only reads.
func BenchmarkReadMsg(b *testing.B) {
	ctx, peer := socketPairContext(b)
	frame := testFrame(1, 7, []byte{1, 2, 3, 4})
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := peer.Write(frame); err != nil {
				return
			}
		}
	}()
	b.Cleanup(func() { close(stop) })
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, _, err := ctx.ReadMsg(); err != nil {
			b.Fatal(err)
		}
	}
}
