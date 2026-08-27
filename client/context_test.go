package client

import (
	"fmt"
	"testing"
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
