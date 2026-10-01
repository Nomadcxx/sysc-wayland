package client

import (
	"errors"
	"fmt"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func newTestContext() *Context {
	return &Context{objects: make(map[uint32]Proxy)}
}

func TestObjectDisplayClaimsIDOne(t *testing.T) {
	ctx := newTestContext()
	display := NewDisplay(ctx)

	if display.ID() != 1 {
		t.Fatalf("display ID = %d, want 1", display.ID())
	}
	if got, ok := ctx.lookupProxy(1); !ok || got != display {
		t.Fatalf("lookup display = (%T, %v), want (%T, true)", got, ok, display)
	}
}

func TestObjectClientAllocationSkipsLiveID(t *testing.T) {
	ctx := newTestContext()
	NewDisplay(ctx)

	live := &BaseProxy{}
	live.SetID(2)
	live.SetContext(ctx)
	ctx.objects[2] = live

	allocated := &BaseProxy{}
	ctx.Register(allocated)

	if allocated.ID() != 3 {
		t.Fatalf("allocated ID = %d, want 3", allocated.ID())
	}
	if allocated.ID() >= firstServerID {
		t.Fatalf("allocated server-range ID %#x", allocated.ID())
	}
}

func TestObjectClientAllocationFailsAtServerRange(t *testing.T) {
	ctx := newTestContext()
	ctx.currentID = firstServerID - 1

	mustPanic(t, func() { ctx.Register(&BaseProxy{}) })
}

func TestObjectServerRegistrationRejectsInvalidID(t *testing.T) {
	for _, id := range []uint32{0, 1, firstServerID - 1} {
		t.Run(fmt.Sprintf("%#x", id), func(t *testing.T) {
			ctx := newTestContext()
			mustPanic(t, func() { ctx.RegisterWithID(&BaseProxy{}, id) })
		})
	}
}

func TestObjectServerRegistrationRejectsLiveID(t *testing.T) {
	ctx := newTestContext()
	ctx.RegisterWithID(&BaseProxy{}, firstServerID)

	mustPanic(t, func() { ctx.RegisterWithID(&BaseProxy{}, firstServerID) })
}

// A server-allocated object gets no delete_id: the compositor frees its ID as
// soon as it handles the client's destroy, and may hand the same ID to the
// next object it creates. The destroyed proxy stays in the map as a zombie to
// absorb events already in flight, so the next registration at that ID has to
// replace it. Refusing it was sysc-shell's crash: a wl_data_offer destroyed on
// a selection change, then a new offer at 0xff000000 on the next focus change.
func TestObjectServerRegistrationReplacesZombie(t *testing.T) {
	ctx, peer := socketPairContext(t)
	device := &DataDevice{}
	ctx.Register(device)
	var offers []*DataOffer
	device.SetDataOfferHandler(func(e DataDeviceDataOfferEvent) { offers = append(offers, e.Id) })
	offer := func() {
		t.Helper()
		body := make([]byte, 4)
		PutUint32(body, firstServerID)
		if _, err := peer.Write(testFrame(device.ID(), 0, body)); err != nil {
			t.Fatal(err)
		}
		if err := ctx.Dispatch(); err != nil {
			t.Fatalf("Dispatch(data_offer) = %v", err)
		}
	}

	offer()
	if err := offers[0].Destroy(); err != nil {
		t.Fatal(err)
	}
	offer()

	if len(offers) != 2 || offers[1] == offers[0] || offers[1].IsZombie() {
		t.Fatalf("offers after reuse = %v, want a second, live offer", offers)
	}
	if got := ctx.GetProxy(firstServerID); got != offers[1] {
		t.Fatalf("proxy at reused ID = %p, want the new offer %p", got, offers[1])
	}
}

func TestObjectDeleteRemovesProxy(t *testing.T) {
	ctx := newTestContext()
	proxy := &BaseProxy{}
	ctx.RegisterWithID(proxy, firstServerID)

	ctx.DeleteID(firstServerID)

	if got, ok := ctx.lookupProxy(firstServerID); ok || got != nil {
		t.Fatalf("lookup deleted proxy = (%T, %v), want (nil, false)", got, ok)
	}
}

func TestProxyIDNeverReturnsZeroOrLiveID(t *testing.T) {
	ctx := newTestContext()
	first := &BaseProxy{}
	second := &BaseProxy{}
	ctx.Register(first)
	ctx.Register(second)

	if first.ID() == 0 || second.ID() == 0 {
		t.Fatalf("allocated zero ID: first=%d second=%d", first.ID(), second.ID())
	}
	if first.ID() == second.ID() {
		t.Fatalf("allocated duplicate ID %d", first.ID())
	}
}

func mustPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("call did not panic")
		}
	}()
	fn()
}

func TestControlFDProvidesLiveSocket(t *testing.T) {
	ctx, _ := socketPairContext(t)
	callbackErr := errors.New("callback failed")
	called := false

	err := ctx.ControlFD(func(fd int) error {
		called = true
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err != nil {
			t.Fatalf("callback descriptor is invalid: %v", err)
		}
		return callbackErr
	})
	if !called {
		t.Fatal("ControlFD() did not call callback")
	}
	if !errors.Is(err, callbackErr) {
		t.Fatalf("ControlFD() error = %v, want callback error", err)
	}
}

func TestControlFDRejectsNilBeforeSocketAccess(t *testing.T) {
	err := (&Context{}).ControlFD(nil)
	if !errors.Is(err, ErrNilControlCallback) {
		t.Fatalf("ControlFD(nil) error = %v, want ErrNilControlCallback", err)
	}
}

func TestControlFDPreservesControlAndCallbackErrors(t *testing.T) {
	controlErr := errors.New("control failed")
	callbackErr := errors.New("callback failed")
	raw := fakeRawConn{controlErr: controlErr}

	err := controlFD(raw, func(int) error { return callbackErr })
	if !errors.Is(err, controlErr) || !errors.Is(err, callbackErr) {
		t.Fatalf("controlFD() error = %v, want both errors", err)
	}
}

type fakeRawConn struct {
	controlErr error
}

func (f fakeRawConn) Control(fn func(uintptr)) error {
	fn(123)
	return f.controlErr
}

func (fakeRawConn) Read(func(uintptr) bool) error  { return syscall.EINVAL }
func (fakeRawConn) Write(func(uintptr) bool) error { return syscall.EINVAL }
