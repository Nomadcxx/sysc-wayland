package client

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// TestDestroyedProxyCannotReachARecycledID is the guard for requests on a
// destroyed proxy. After delete_id the ID belongs to the next object, so a
// second destroy or a stale request would act on that object instead.
func TestDestroyedProxyCannotReachARecycledID(t *testing.T) {
	ctx, peer := socketPairContext(t)
	NewSurface(ctx) // holds ID 1, which stands for the display and is never recycled
	first := NewSurface(ctx)
	if err := first.Destroy(); err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, 8)
	if err := peer.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(peer, frame); err != nil {
		t.Fatal(err)
	}
	if err := first.Destroy(); !errors.Is(err, ErrProxyDestroyed) {
		t.Fatalf("second Destroy() = %v, want ErrProxyDestroyed", err)
	}

	ctx.DeleteID(first.ID())
	second := NewSurface(ctx)
	if second.ID() != first.ID() {
		t.Fatalf("new surface ID = %d, want the recycled %d", second.ID(), first.ID())
	}
	if err := first.Attach(nil, 0, 0); !errors.Is(err, ErrProxyDestroyed) {
		t.Fatalf("Attach() on the destroyed proxy = %v, want ErrProxyDestroyed", err)
	}
	assertNothingWritten(t, peer)
}

// TestDeleteIDRetiresTheProxy covers a delete_id for an object this client
// did not destroy: a server-destroyed object, or a compositor that sends one
// unprompted. The ID is recycled, so the old proxy must stop sending.
func TestDeleteIDRetiresTheProxy(t *testing.T) {
	ctx, peer := socketPairContext(t)
	live := NewSurface(ctx)
	ctx.DeleteID(live.ID())
	if !live.IsZombie() {
		t.Fatal("proxy is live after delete_id")
	}
	if err := live.Commit(); !errors.Is(err, ErrProxyDestroyed) {
		t.Fatalf("Commit() after delete_id = %v, want ErrProxyDestroyed", err)
	}
	if callback, err := live.Frame(); callback != nil || !errors.Is(err, ErrProxyDestroyed) {
		t.Fatalf("Frame() after delete_id = %v, %v; want nil, ErrProxyDestroyed", callback, err)
	}
	assertNothingWritten(t, peer)
}

func assertNothingWritten(t *testing.T, peer *net.UnixConn) {
	t.Helper()
	pending, err := pendingBytes(peer)
	if err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("%d bytes reached the wire from a destroyed proxy", pending)
	}
}

// TestRequestRefusesADestroyedArgument covers the object arguments: a
// destroyed buffer's ID may name another buffer by now.
func TestRequestRefusesADestroyedArgument(t *testing.T) {
	ctx, peer := socketPairContext(t)
	surface := NewSurface(ctx)
	buffer := NewBuffer(ctx)
	ctx.DeleteID(buffer.ID())
	if err := surface.Attach(buffer, 0, 0); !errors.Is(err, ErrProxyDestroyed) {
		t.Fatalf("Attach(destroyed buffer) = %v, want ErrProxyDestroyed", err)
	}
	assertNothingWritten(t, peer)
	if err := surface.Attach(nil, 0, 0); err != nil {
		t.Fatalf("Attach(nil) = %v, want it sent", err)
	}
}
