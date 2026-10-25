package sessionlock

import (
	"testing"

	"github.com/Nomadcxx/sysc-wayland/client"
	"github.com/Nomadcxx/sysc-wayland/internal/waylandtest"
)

func TestLockEncodesTheRequest(t *testing.T) {
	ctx, peer := waylandtest.Pair(t)
	manager := NewExtSessionLockManagerV1(ctx)

	lock, err := manager.Lock()
	if err != nil {
		t.Fatalf("Lock() error = %v", err)
	}
	sender, opcode, body := waylandtest.ReadRequest(t, peer)
	if sender != manager.ID() {
		t.Fatalf("sender = %d, want the manager %d", sender, manager.ID())
	}
	if opcode != 1 {
		t.Fatalf("opcode = %d, want 1 (lock)", opcode)
	}
	if len(body) != 4 || client.Uint32(body[0:4]) != lock.ID() {
		t.Fatalf("body = %v, want the new lock id %d", body, lock.ID())
	}
}

func TestGetLockSurfaceEncodesSurfaceThenOutput(t *testing.T) {
	ctx, peer := waylandtest.Pair(t)
	lock := NewExtSessionLockV1(ctx)
	surface := client.NewSurface(ctx)
	output := client.NewOutput(ctx)

	lockSurface, err := lock.GetLockSurface(surface, output)
	if err != nil {
		t.Fatalf("GetLockSurface() error = %v", err)
	}
	_, opcode, body := waylandtest.ReadRequest(t, peer)
	if opcode != 1 {
		t.Fatalf("opcode = %d, want 1 (get_lock_surface)", opcode)
	}
	if len(body) != 12 {
		t.Fatalf("body is %d bytes, want new id, surface, output", len(body))
	}
	if got := client.Uint32(body[0:4]); got != lockSurface.ID() {
		t.Fatalf("new id = %d, want %d", got, lockSurface.ID())
	}
	if got := client.Uint32(body[4:8]); got != surface.ID() {
		t.Fatalf("surface = %d, want %d", got, surface.ID())
	}
	if got := client.Uint32(body[8:12]); got != output.ID() {
		t.Fatalf("output = %d, want %d", got, output.ID())
	}
}

// unlock_and_destroy has no new-id argument, so a copy-paste of the lock
// encoder would send four bytes too many and desync the stream.
func TestUnlockAndDestroySendsNoArguments(t *testing.T) {
	ctx, peer := waylandtest.Pair(t)
	lock := NewExtSessionLockV1(ctx)

	if err := lock.UnlockAndDestroy(); err != nil {
		t.Fatalf("UnlockAndDestroy() error = %v", err)
	}
	_, opcode, body := waylandtest.ReadRequest(t, peer)
	if opcode != 2 {
		t.Fatalf("opcode = %d, want 2 (unlock_and_destroy)", opcode)
	}
	if len(body) != 0 {
		t.Fatalf("body is %d bytes, want none", len(body))
	}
	if !lock.IsZombie() {
		t.Fatal("UnlockAndDestroy() left the lock live")
	}
}

func TestLockedAndFinishedReachTheirHandlers(t *testing.T) {
	ctx, peer := waylandtest.Pair(t)
	lock := NewExtSessionLockV1(ctx)

	var locked, finished int
	lock.SetLockedHandler(func(ExtSessionLockV1LockedEvent) { locked++ })
	lock.SetFinishedHandler(func(ExtSessionLockV1FinishedEvent) { finished++ })

	for _, opcode := range []uint32{0, 1} {
		if _, err := peer.Write(waylandtest.Frame(lock.ID(), opcode, nil)); err != nil {
			t.Fatal(err)
		}
		if err := ctx.Dispatch(); err != nil {
			t.Fatalf("Dispatch() opcode %d error = %v", opcode, err)
		}
	}
	if locked != 1 || finished != 1 {
		t.Fatalf("locked = %d finished = %d, want 1 and 1", locked, finished)
	}
}

func TestConfigureCarriesSerialWidthAndHeight(t *testing.T) {
	ctx, peer := waylandtest.Pair(t)
	lockSurface := NewExtSessionLockSurfaceV1(ctx)

	var got ExtSessionLockSurfaceV1ConfigureEvent
	lockSurface.SetConfigureHandler(func(e ExtSessionLockSurfaceV1ConfigureEvent) { got = e })

	body := make([]byte, 12)
	client.PutUint32(body[0:4], 77)
	client.PutUint32(body[4:8], 3440)
	client.PutUint32(body[8:12], 1440)
	if _, err := peer.Write(waylandtest.Frame(lockSurface.ID(), 0, body)); err != nil {
		t.Fatal(err)
	}
	if err := ctx.Dispatch(); err != nil {
		t.Fatalf("Dispatch() configure error = %v", err)
	}
	if got.Serial != 77 || got.Width != 3440 || got.Height != 1440 {
		t.Fatalf("configure = %+v, want serial 77, 3440x1440", got)
	}
}

func TestAckConfigureEncodesTheSerial(t *testing.T) {
	ctx, peer := waylandtest.Pair(t)
	lockSurface := NewExtSessionLockSurfaceV1(ctx)

	if err := lockSurface.AckConfigure(77); err != nil {
		t.Fatalf("AckConfigure() error = %v", err)
	}
	_, opcode, body := waylandtest.ReadRequest(t, peer)
	if opcode != 1 {
		t.Fatalf("opcode = %d, want 1 (ack_configure)", opcode)
	}
	if len(body) != 4 || client.Uint32(body[0:4]) != 77 {
		t.Fatalf("body = %v, want serial 77", body)
	}
}
