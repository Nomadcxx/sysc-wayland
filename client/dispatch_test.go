package client

import (
	"errors"
	"net"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDispatchOwnershipRejectsUnknownAndSticks(t *testing.T) {
	ctx, peer := socketPairContext(t)
	pipe := sendFDFrame(t, peer, 77, 0, nil)

	firstErr := ctx.Dispatch()
	if !errors.Is(firstErr, ErrDispatchSenderNotFound) {
		t.Fatalf("Dispatch() error = %v, want ErrDispatchSenderNotFound", firstErr)
	}
	assertPipeReadEndClosed(t, pipe)

	if _, err := peer.Write(testFrame(1, 0, nil)); err != nil {
		t.Fatal(err)
	}
	before, err := pendingBytes(ctx.conn)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, _, readErr := ctx.ReadMsg()
	if readErr != firstErr {
		t.Fatalf("ReadMsg() error = %v, want same sticky error %v", readErr, firstErr)
	}
	if writeErr := ctx.WriteMsg(testFrame(1, 0, nil), nil); writeErr != firstErr {
		t.Fatalf("WriteMsg() error = %v, want same sticky error %v", writeErr, firstErr)
	}
	secondErr := ctx.Dispatch()
	after, err := pendingBytes(ctx.conn)
	if err != nil {
		t.Fatal(err)
	}
	if secondErr != firstErr {
		t.Fatalf("second Dispatch() error = %v, want same sticky error %v", secondErr, firstErr)
	}
	if before == 0 || after != before {
		t.Fatalf("pending bytes before/after sticky dispatch = %d/%d, want same non-zero count", before, after)
	}
}

func TestDispatchOwnershipDiscardsZombieAndClosesFD(t *testing.T) {
	ctx, peer := socketPairContext(t)
	proxy := &testDispatcher{}
	ctx.Register(proxy)
	proxy.MarkZombie()
	pipe := sendFDFrame(t, peer, proxy.ID(), 0, nil)

	if err := ctx.Dispatch(); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	assertPipeReadEndClosed(t, pipe)
}

func TestDispatchPreservesQueuedFDForNonFDEvent(t *testing.T) {
	ctx, peer := socketPairContext(t)
	var received []int
	proxy := &testDispatcher{
		hasFD: func(opcode uint32) bool { return opcode == 1 },
		dispatch: func(_ uint32, fd int, _ []byte) {
			received = append(received, fd)
		},
	}
	ctx.Register(proxy)
	pipe := newPipe(t)
	frames := append(testFrame(proxy.ID(), 0, nil), testFrame(proxy.ID(), 1, nil)...)
	if _, _, err := peer.WriteMsgUnix(frames, unix.UnixRights(pipe.read), nil); err != nil {
		t.Fatal(err)
	}
	pipe.closeRead(t)

	if err := ctx.Dispatch(); err != nil {
		t.Fatalf("first Dispatch() error = %v", err)
	}
	if len(received) != 1 || received[0] != -1 {
		t.Fatalf("first dispatched descriptors = %v, want [-1]", received)
	}

	if err := ctx.Dispatch(); err != nil {
		t.Fatalf("second Dispatch() error = %v", err)
	}
	if len(received) != 2 || received[1] < 0 {
		t.Fatalf("second dispatched descriptors = %v, want a descriptor", received)
	}
	fd := received[1]
	t.Cleanup(func() { unix.Close(fd) })
	if _, err := unix.Write(pipe.write, []byte{'x'}); err != nil {
		t.Fatal(err)
	}
	var got [1]byte
	if _, err := unix.Read(fd, got[:]); err != nil {
		t.Fatal(err)
	}
	if got[0] != 'x' {
		t.Fatalf("received byte = %q, want x", got[0])
	}
}

func TestDispatchClosesQueuedFDsWhenFirstFrameIsFatal(t *testing.T) {
	ctx, peer := socketPairContext(t)
	first := newPipe(t)
	second := newPipe(t)
	frames := append(testFrame(77, 0, nil), testFrame(77, 0, nil)...)
	if _, _, err := peer.WriteMsgUnix(frames, unix.UnixRights(first.read, second.read), nil); err != nil {
		t.Fatal(err)
	}
	first.closeRead(t)
	second.closeRead(t)

	err := ctx.Dispatch()
	if !errors.Is(err, ErrDispatchSenderNotFound) {
		t.Fatalf("Dispatch() error = %v, want ErrDispatchSenderNotFound", err)
	}
	for name, writeFD := range map[string]int{"first": first.write, "second": second.write} {
		if _, err := unix.Write(writeFD, []byte{'x'}); !errors.Is(err, unix.EPIPE) {
			t.Errorf("write to %s pipe error = %v, want EPIPE", name, err)
		}
	}
}

func TestDispatchOwnershipMakesDecoderPanicSticky(t *testing.T) {
	ctx, peer := socketPairContext(t)
	proxy := &testDispatcher{dispatch: func(uint32, int, []byte) { panic("bad decoder") }}
	ctx.Register(proxy)
	pipe := sendFDFrame(t, peer, proxy.ID(), 0, nil)

	err := ctx.Dispatch()
	if err == nil || !strings.Contains(err.Error(), "bad decoder") {
		t.Fatalf("Dispatch() error = %v, want decoder panic", err)
	}
	if err != ctx.fatalErr {
		t.Fatalf("Dispatch() error = %v, fatal error = %v", err, ctx.fatalErr)
	}
	assertPipeReadEndClosed(t, pipe)
}

func TestDispatchOwnershipRejectsUnsupportedOpcode(t *testing.T) {
	ctx, peer := socketPairContext(t)
	callback := NewCallback(ctx)
	pipe := sendFDFrame(t, peer, callback.ID(), 99, nil)

	err := ctx.Dispatch()
	if err == nil || !strings.Contains(err.Error(), "unsupported opcode") {
		t.Fatalf("Dispatch() error = %v, want unsupported opcode", err)
	}
	if err != ctx.fatalErr {
		t.Fatalf("Dispatch() error = %v, fatal error = %v", err, ctx.fatalErr)
	}
	assertPipeReadEndClosed(t, pipe)
}

func TestDispatchOwnershipRecordsDisplayErrorBeforeHandler(t *testing.T) {
	ctx, peer := socketPairContext(t)
	display := NewDisplay(ctx)
	called := false
	recorded := false
	display.SetErrorHandler(func(DisplayErrorEvent) {
		called = true
		recorded = ctx.fatalErr != nil
	})
	body := make([]byte, 16)
	PutUint32(body[0:4], display.ID())
	PutUint32(body[4:8], uint32(DisplayErrorNoMemory))
	PutString(body[8:], "bad")
	if _, err := peer.Write(testFrame(display.ID(), 0, body)); err != nil {
		t.Fatal(err)
	}

	err := ctx.Dispatch()
	if err == nil || !strings.Contains(err.Error(), "wl_display.error") {
		t.Fatalf("Dispatch() error = %v, want wl_display.error", err)
	}
	if !called || !recorded {
		t.Fatalf("handler called/observed fatal = %v/%v, want true/true", called, recorded)
	}
}

func TestFixesDestroyRegistryDiscardsInFlightEvents(t *testing.T) {
	ctx, peer := socketPairContext(t)
	fixes := NewFixes(ctx)
	registry := NewRegistry(ctx)
	called := false
	registry.SetGlobalHandler(func(RegistryGlobalEvent) { called = true })

	if err := fixes.DestroyRegistry(registry); err != nil {
		t.Fatalf("DestroyRegistry() error = %v", err)
	}
	body := make([]byte, 4+4+PaddedLen(len("wl_output")+1)+4)
	PutUint32(body[:4], 42)
	PutString(body[4:], "wl_output")
	PutUint32(body[4+4+PaddedLen(len("wl_output")+1):], 4)
	if _, err := peer.Write(testFrame(registry.ID(), 0, body)); err != nil {
		t.Fatal(err)
	}
	if err := ctx.Dispatch(); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !registry.IsZombie() || called {
		t.Fatalf("registry zombie/handler called = %v/%v, want true/false", registry.IsZombie(), called)
	}
}

func TestGeneratedDispatchRejectsUnknownOpcode(t *testing.T) {
	dispatchers := map[string]Dispatcher{
		"display":       &Display{},
		"registry":      &Registry{},
		"callback":      &Callback{},
		"shm":           &Shm{},
		"buffer":        &Buffer{},
		"data_offer":    &DataOffer{},
		"data_source":   &DataSource{},
		"data_device":   &DataDevice{},
		"shell_surface": &ShellSurface{},
		"surface":       &Surface{},
		"seat":          &Seat{},
		"pointer":       &Pointer{},
		"keyboard":      &Keyboard{},
		"touch":         &Touch{},
		"output":        &Output{},
	}
	for name, dispatcher := range dispatchers {
		t.Run(name, func(t *testing.T) {
			mustPanic(t, func() { dispatcher.Dispatch(^uint32(0), -1, nil) })
		})
	}
}

type testDispatcher struct {
	BaseProxy
	dispatch func(uint32, int, []byte)
	hasFD    func(uint32) bool
}

func (p *testDispatcher) Dispatch(opcode uint32, fd int, data []byte) {
	if p.dispatch != nil {
		p.dispatch(opcode, fd, data)
	}
}

func (p *testDispatcher) HasFD(opcode uint32) bool {
	if p.hasFD == nil {
		return true
	}
	return p.hasFD(opcode)
}

func sendFDFrame(t *testing.T, peer *net.UnixConn, sender, opcode uint32, body []byte) *testPipe {
	t.Helper()
	pipe := newPipe(t)
	frame := testFrame(sender, opcode, body)
	rights := unix.UnixRights(pipe.read)
	if _, _, err := peer.WriteMsgUnix(frame, rights, nil); err != nil {
		t.Fatal(err)
	}
	pipe.closeRead(t)
	return pipe
}

func assertPipeReadEndClosed(t *testing.T, pipe *testPipe) {
	t.Helper()
	if _, err := unix.Write(pipe.write, []byte{'x'}); !errors.Is(err, unix.EPIPE) {
		t.Fatalf("write error = %v, want EPIPE", err)
	}
}
