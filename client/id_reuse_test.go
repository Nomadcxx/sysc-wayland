package client

import (
	"io"
	"testing"
)

// A client ID acknowledged by wl_display.delete_id is free again. libwayland
// servers (wlroots, Mutter, KWin, Weston) refuse any new_id above
// WL_MAP_MAX_OBJECTS (~15.7M, libwayland-server src/wayland-private.h), so a
// long-running client that never reuses an ID is disconnected once it has
// created that many objects in total, however few are live.
func TestObjectClientIDReusedAfterDeleteID(t *testing.T) {
	ctx := newTestContext()
	NewDisplay(ctx) // ID 1 is never recycled
	first := &BaseProxy{}
	ctx.Register(first)

	ctx.DeleteID(first.ID())

	second := &BaseProxy{}
	ctx.Register(second)

	if second.ID() != first.ID() {
		t.Fatalf("ID after delete_id = %d, want reused %d", second.ID(), first.ID())
	}
}

// Only an ID whose proxy actually existed is recycled, and only once: a bogus
// or repeated delete_id must not put an ID on the free list twice, or two
// later registrations would collide on one ID.
func TestObjectDeleteIDIgnoresUnknownAndDuplicate(t *testing.T) {
	ctx := newTestContext()
	NewDisplay(ctx)
	live := &BaseProxy{}
	ctx.Register(live)

	ctx.DeleteID(4242)      // unknown: no proxy exists at this ID
	ctx.DeleteID(live.ID()) // first: recycled
	ctx.DeleteID(live.ID()) // duplicate: must be ignored

	first := &BaseProxy{}
	second := &BaseProxy{}
	ctx.Register(first)
	ctx.Register(second)

	if first.ID() == second.ID() {
		t.Fatalf("duplicate ID %d after a repeated delete_id", first.ID())
	}
	if first.ID() == 1 || second.ID() == 1 || first.ID() >= firstServerID || second.ID() >= firstServerID {
		t.Fatalf("bad recycled IDs: first=%#x second=%#x", first.ID(), second.ID())
	}
}

// The display (ID 1) and server-allocated IDs (>= firstServerID) are not
// client-allocated, so delete_id must never put them on the free list.
func TestObjectDeleteIDNeverRecyclesDisplayOrServerIDs(t *testing.T) {
	ctx := newTestContext()
	NewDisplay(ctx)

	ctx.DeleteID(1)
	ctx.DeleteID(firstServerID)

	next := &BaseProxy{}
	ctx.Register(next)

	if next.ID() == 1 || next.ID() >= firstServerID {
		t.Fatalf("allocated %#x after delete_id of the display and a server ID", next.ID())
	}
}

// One wl_callback per frame is ordinary (Surface.Frame); a 60 Hz client
// reaches 15.7M IDs in about three days if delete_id never recycles.
func TestObjectClientIDsBoundedAcrossDeleteIDCycles(t *testing.T) {
	ctx := newTestContext()
	display := NewDisplay(ctx) // ID 1

	const cycles = 1_000_000
	var maxID uint32
	for n := 0; n < cycles; n++ {
		cb := NewCallback(ctx)
		if cb.ID() > maxID {
			maxID = cb.ID()
		}
		// Server: wl_callback.done, then wl_display.delete_id(cb).
		body := make([]byte, 4)
		PutUint32(body, cb.ID())
		display.Dispatch(1, -1, body)
	}
	if maxID > 3 {
		t.Fatalf("allocated ID %#x across %d delete_id cycles, want the ID reused", maxID, cycles)
	}
}

// End to end: the ID recycled in the map is also the ID that goes on the wire
// in the next request.
func TestObjectClientIDReusedAcrossSyncRoundtrip(t *testing.T) {
	ctx, peer := socketPairContext(t)
	display := NewDisplay(ctx)

	readSyncRequest := func(wantID uint32) {
		t.Helper()
		req := make([]byte, 12)
		if _, err := io.ReadFull(peer, req); err != nil {
			t.Fatalf("read sync request: %v", err)
		}
		if got := Uint32(req[8:12]); got != wantID {
			t.Fatalf("sync request new_id = %d, want %d", got, wantID)
		}
	}

	first, err := display.Sync()
	if err != nil {
		t.Fatal(err)
	}
	readSyncRequest(first.ID())

	done := make([]byte, 4) // callback_data
	if _, err := peer.Write(testFrame(first.ID(), 0, done)); err != nil {
		t.Fatal(err)
	}
	deleteBody := make([]byte, 4)
	PutUint32(deleteBody, first.ID())
	if _, err := peer.Write(testFrame(1, 1, deleteBody)); err != nil {
		t.Fatal(err)
	}
	if err := ctx.Dispatch(); err != nil {
		t.Fatalf("Dispatch(callback.done) = %v", err)
	}
	if err := ctx.Dispatch(); err != nil {
		t.Fatalf("Dispatch(delete_id) = %v", err)
	}

	second, err := display.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if second.ID() != first.ID() {
		t.Fatalf("second sync ID = %d, want reused %d", second.ID(), first.ID())
	}
	readSyncRequest(second.ID())
}
