package client

import (
	"bytes"
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

func TestDispatchKeepsHandlerOwnedFDWhenFatal(t *testing.T) {
	t.Run("keymap", func(t *testing.T) {
		ctx, peer := socketPairContext(t)
		keyboard := NewKeyboard(ctx)
		owned := -1
		keyboard.SetKeymapHandler(func(e KeyboardKeymapEvent) {
			owned = e.Fd
			ctx.setFatal(errors.New("handler write failed"))
		})
		body := make([]byte, 8)
		PutUint32(body[0:4], 1)
		PutUint32(body[4:8], 4)
		pipe := sendFDFrame(t, peer, keyboard.ID(), 0, body)

		err := ctx.Dispatch()
		if err == nil || err != ctx.fatalErr || !strings.Contains(err.Error(), "handler write failed") {
			t.Fatalf("Dispatch() error = %v, fatal = %v, want handler write failed", err, ctx.fatalErr)
		}
		assertHandlerFDStillOpen(t, owned, pipe)
	})

	t.Run("data_source_send", func(t *testing.T) {
		ctx, peer := socketPairContext(t)
		source := NewDataSource(ctx)
		owned := -1
		source.SetSendHandler(func(e DataSourceSendEvent) {
			owned = e.Fd
			ctx.setFatal(errors.New("handler write failed"))
		})
		mime := "text/plain"
		body := make([]byte, 4+PaddedLen(len(mime)+1))
		PutString(body, mime)
		pipe := sendFDFrame(t, peer, source.ID(), 1, body)

		err := ctx.Dispatch()
		if err == nil || err != ctx.fatalErr || !strings.Contains(err.Error(), "handler write failed") {
			t.Fatalf("Dispatch() error = %v, fatal = %v, want handler write failed", err, ctx.fatalErr)
		}
		assertHandlerFDStillOpen(t, owned, pipe)
	})
}

func assertHandlerFDStillOpen(t *testing.T, fd int, pipe *testPipe) {
	t.Helper()
	if fd < 0 {
		t.Fatal("handler descriptor = -1, want a descriptor")
	}
	t.Cleanup(func() { unix.Close(fd) })
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err != nil {
		t.Fatalf("F_GETFD(handler fd) = %v, want open descriptor", err)
	}
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

func TestDispatchAllowNullStringSurfacesNull(t *testing.T) {
	t.Run("null target", func(t *testing.T) {
		ctx, peer := socketPairContext(t)
		source := NewDataSource(ctx)
		var got DataSourceTargetEvent
		called := false
		source.SetTargetHandler(func(e DataSourceTargetEvent) {
			called = true
			got = e
		})
		body := make([]byte, 4)
		PutUint32(body, 0)
		if _, err := peer.Write(testFrame(source.ID(), 0, body)); err != nil {
			t.Fatal(err)
		}

		if err := ctx.Dispatch(); err != nil {
			t.Fatalf("Dispatch() error = %v, want nil for a NULL target mime", err)
		}
		if !called {
			t.Fatal("NULL target did not call its handler")
		}
		mime, ok := any(got.MimeType).(*string)
		if !ok || mime != nil {
			t.Fatalf("MimeType = %#v, want nil *string", got.MimeType)
		}

		cancelled := false
		source.SetCancelledHandler(func(DataSourceCancelledEvent) { cancelled = true })
		if _, err := peer.Write(testFrame(source.ID(), 2, nil)); err != nil {
			t.Fatal(err)
		}
		if err := ctx.Dispatch(); err != nil {
			t.Fatalf("Dispatch() after NULL target error = %v", err)
		}
		if !cancelled {
			t.Fatal("connection stayed fatal after a NULL target mime")
		}
	})

	t.Run("empty target", func(t *testing.T) {
		ctx, peer := socketPairContext(t)
		source := NewDataSource(ctx)
		var got DataSourceTargetEvent
		source.SetTargetHandler(func(e DataSourceTargetEvent) { got = e })
		body := make([]byte, 8)
		PutUint32(body[:4], 1)
		if _, err := peer.Write(testFrame(source.ID(), 0, body)); err != nil {
			t.Fatal(err)
		}

		if err := ctx.Dispatch(); err != nil {
			t.Fatalf("Dispatch() error = %v, want nil for an empty target mime", err)
		}
		mime, ok := any(got.MimeType).(*string)
		if !ok || mime == nil || *mime != "" {
			t.Fatalf("MimeType = %#v, want pointer to empty string", got.MimeType)
		}
	})

	t.Run("present target", func(t *testing.T) {
		ctx, peer := socketPairContext(t)
		source := NewDataSource(ctx)
		var got DataSourceTargetEvent
		source.SetTargetHandler(func(e DataSourceTargetEvent) { got = e })
		const mimeType = "text/plain"
		body := make([]byte, 4+PaddedLen(len(mimeType)+1))
		PutString(body, mimeType)
		if _, err := peer.Write(testFrame(source.ID(), 0, body)); err != nil {
			t.Fatal(err)
		}

		if err := ctx.Dispatch(); err != nil {
			t.Fatalf("Dispatch() error = %v", err)
		}
		mime, ok := any(got.MimeType).(*string)
		if !ok || mime == nil || *mime != mimeType {
			t.Fatalf("MimeType = %#v, want %q", got.MimeType, mimeType)
		}
	})

	t.Run("null send still fatal", func(t *testing.T) {
		ctx, peer := socketPairContext(t)
		source := NewDataSource(ctx)
		called := false
		source.SetSendHandler(func(DataSourceSendEvent) { called = true })
		body := make([]byte, 4)
		PutUint32(body, 0)
		if _, err := peer.Write(testFrame(source.ID(), 1, body)); err != nil {
			t.Fatal(err)
		}

		err := ctx.Dispatch()
		if err == nil || !strings.Contains(err.Error(), "NUL terminator") {
			t.Fatalf("Dispatch() error = %v, want NUL terminator", err)
		}
		if called {
			t.Fatal("non-null string called its handler")
		}
	})
}

func TestDispatchRejectsMalformedRegistryStrings(t *testing.T) {
	tests := []struct {
		name        string
		declaredLen uint32
		data        []byte
		want        string
	}{
		{name: "missing NUL", declaredLen: 3, data: []byte("bad"), want: "NUL terminator"},
		{name: "length exceeds body", declaredLen: 64, want: "truncated event string"},
		{name: "empty length", want: "NUL terminator"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, peer := socketPairContext(t)
			registry := NewRegistry(ctx)
			called := false
			registry.SetGlobalHandler(func(RegistryGlobalEvent) { called = true })

			paddedLen := PaddedLen(len(tt.data))
			body := make([]byte, 4+4+paddedLen+4)
			PutUint32(body[:4], 42)
			PutUint32(body[4:8], tt.declaredLen)
			copy(body[8:], tt.data)
			PutUint32(body[8+paddedLen:], 4)
			if _, err := peer.Write(testFrame(registry.ID(), 0, body)); err != nil {
				t.Fatal(err)
			}

			err := ctx.Dispatch()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Dispatch() error = %v, want %q", err, tt.want)
			}
			if called {
				t.Fatal("malformed registry event called its handler")
			}
		})
	}
}

func TestDispatchRejectsOversizedKeyboardArray(t *testing.T) {
	ctx, peer := socketPairContext(t)
	keyboard := NewKeyboard(ctx)
	called := false
	keyboard.SetEnterHandler(func(KeyboardEnterEvent) { called = true })
	body := make([]byte, 12)
	PutUint32(body[0:4], 1)
	PutUint32(body[4:8], 0)
	PutUint32(body[8:12], 64)
	if _, err := peer.Write(testFrame(keyboard.ID(), 1, body)); err != nil {
		t.Fatal(err)
	}

	err := ctx.Dispatch()
	if err == nil || !strings.Contains(err.Error(), "truncated event array") {
		t.Fatalf("Dispatch() error = %v, want truncated event array", err)
	}
	if called {
		t.Fatal("malformed keyboard event called its handler")
	}
}

func TestDispatchRegistersDataOfferWithoutHandler(t *testing.T) {
	ctx, peer := socketPairContext(t)
	device := NewDataDevice(ctx)
	offerID := firstServerID
	body := make([]byte, 4)
	PutUint32(body, offerID)
	if _, err := peer.Write(testFrame(device.ID(), 0, body)); err != nil {
		t.Fatal(err)
	}

	if err := ctx.Dispatch(); err != nil {
		t.Fatalf("data_offer Dispatch() error = %v", err)
	}
	offer, ok := ctx.GetProxy(offerID).(*DataOffer)
	if !ok || offer == nil || offer.ID() != offerID {
		t.Fatalf("registered offer = %#v, want live *DataOffer %#x", ctx.GetProxy(offerID), offerID)
	}

	mime := "text/plain"
	offerBody := make([]byte, 4+PaddedLen(len(mime)+1))
	PutString(offerBody, mime)
	if _, err := peer.Write(testFrame(offerID, 0, offerBody)); err != nil {
		t.Fatal(err)
	}
	if err := ctx.Dispatch(); err != nil {
		t.Fatalf("following data_offer.offer Dispatch() error = %v", err)
	}
}

// wl_data_device.data_offer on a released device still introduces the offer.
// Dropping the new_id leaves the compositor's follow-up events on that id
// without a sender, which fatals the connection.
func TestDispatchZombieParentRegistersNewIDChild(t *testing.T) {
	ctx, peer := socketPairContext(t)
	device := NewDataDevice(ctx)
	called := false
	device.SetDataOfferHandler(func(DataDeviceDataOfferEvent) { called = true })
	if err := device.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if !device.IsZombie() {
		t.Fatal("released data device is not a zombie")
	}

	offerID := firstServerID
	body := make([]byte, 4)
	PutUint32(body, offerID)
	if _, err := peer.Write(testFrame(device.ID(), 0, body)); err != nil {
		t.Fatal(err)
	}
	if err := ctx.Dispatch(); err != nil {
		t.Fatalf("data_offer on released device Dispatch() error = %v", err)
	}
	if called {
		t.Fatal("zombie device ran its data_offer handler")
	}
	offer, ok := ctx.GetProxy(offerID).(*DataOffer)
	if !ok || offer == nil || offer.ID() != offerID {
		t.Fatalf("registered offer = %#v, want zombie *DataOffer %#x", ctx.GetProxy(offerID), offerID)
	}
	if !offer.IsZombie() {
		t.Fatal("absorbed offer placeholder is not a zombie")
	}

	mime := "text/plain"
	offerBody := make([]byte, 4+PaddedLen(len(mime)+1))
	PutString(offerBody, mime)
	if _, err := peer.Write(testFrame(offerID, 0, offerBody)); err != nil {
		t.Fatal(err)
	}
	if err := ctx.Dispatch(); err != nil {
		t.Fatalf("offer on zombie placeholder Dispatch() error = %v", err)
	}
}

// A new_id naming an object that is still live duplicates the id. The panic
// must become a sticky fatal error, never escape Dispatch.
func TestDispatchZombieNewIDDuplicateLiveObjectIsStickyFatal(t *testing.T) {
	ctx, peer := socketPairContext(t)
	ctx.RegisterWithID(&DataOffer{}, firstServerID)
	device := NewDataDevice(ctx)
	if err := device.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	body := make([]byte, 4)
	PutUint32(body, firstServerID)
	if _, err := peer.Write(testFrame(device.ID(), 0, body)); err != nil {
		t.Fatal(err)
	}
	err := ctx.Dispatch()
	if err == nil || err != ctx.fatalErr || !strings.Contains(err.Error(), "duplicate Wayland object ID") {
		t.Fatalf("Dispatch() error = %v, fatal = %v, want duplicate ID sticky fatal", err, ctx.fatalErr)
	}
}

// The zombie branch closes a descriptor before absorbing, so a panic inside
// the absorber cannot close it twice, and the absorber still sees the event.
func TestDispatchZombieAbsorbsNewIDAndClosesFD(t *testing.T) {
	ctx, peer := socketPairContext(t)
	absorbedOpcode := ^uint32(0)
	var absorbedData []byte
	proxy := &testDispatcher{
		hasFD: func(uint32) bool { return true },
		absorb: func(opcode uint32, data []byte) {
			absorbedOpcode = opcode
			absorbedData = append([]byte(nil), data...)
		},
	}
	ctx.Register(proxy)
	proxy.MarkZombie()
	body := make([]byte, 4)
	PutUint32(body, firstServerID)
	pipe := sendFDFrame(t, peer, proxy.ID(), 0, body)

	if err := ctx.Dispatch(); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	assertPipeReadEndClosed(t, pipe)
	if absorbedOpcode != 0 || !bytes.Equal(absorbedData, body) {
		t.Fatalf("absorbed = (%d, %v), want (0, %v)", absorbedOpcode, absorbedData, body)
	}
}

// Zombie events without a new_id stay discarded: making them fatal would kill
// connections for ordinary in-flight events like wl_data_device.leave.
func TestDispatchZombieDiscardsEventWithoutNewID(t *testing.T) {
	ctx, peer := socketPairContext(t)
	device := NewDataDevice(ctx)
	if err := device.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	body := make([]byte, 4)                                                // object arg, null surface
	if _, err := peer.Write(testFrame(device.ID(), 2, body)); err != nil { // wl_data_device.leave
		t.Fatal(err)
	}
	if err := ctx.Dispatch(); err != nil {
		t.Fatalf("leave on released device Dispatch() error = %v", err)
	}
	if ctx.fatalErr != nil {
		t.Fatalf("fatal = %v, want nil", ctx.fatalErr)
	}
}

// A truncated new_id on a zombie is a protocol violation of the sender:
// sticky fatal, never an escaping panic.
func TestDispatchZombieTruncatedNewIDIsStickyFatal(t *testing.T) {
	ctx, peer := socketPairContext(t)
	device := NewDataDevice(ctx)
	if err := device.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if _, err := peer.Write(testFrame(device.ID(), 0, []byte{1, 2})); err != nil {
		t.Fatal(err)
	}
	err := ctx.Dispatch()
	if err == nil || err != ctx.fatalErr {
		t.Fatalf("Dispatch() error = %v, fatal = %v, want sticky fatal", err, ctx.fatalErr)
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
	absorb   func(uint32, []byte)
}

func (p *testDispatcher) Dispatch(opcode uint32, fd int, data []byte) {
	if p.dispatch != nil {
		p.dispatch(opcode, fd, data)
	}
}

func (p *testDispatcher) AbsorbNewIDs(opcode uint32, data []byte) {
	if p.absorb != nil {
		p.absorb(opcode, data)
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

// wl_display has no destructor request, so Display.Destroy only zombies it locally. It still
// receives delete_id and error; those must not be dropped with the zombie short-circuit.
func TestDispatchKeepsDisplayDeleteIdAfterDestroy(t *testing.T) {
	ctx, peer := socketPairContext(t)
	display := NewDisplay(ctx)
	buffer := NewBuffer(ctx)
	registry := NewRegistry(ctx)

	if err := buffer.Destroy(); err != nil {
		t.Fatal(err)
	}
	if !buffer.IsZombie() || ctx.GetProxy(buffer.ID()) != buffer {
		t.Fatal("destroyed buffer must stay registered until delete_id")
	}
	if err := display.Destroy(); err != nil {
		t.Fatal(err)
	}
	body := make([]byte, 4)
	PutUint32(body, buffer.ID())
	if _, err := peer.Write(testFrame(display.ID(), 1, body)); err != nil {
		t.Fatal(err)
	}
	if err := ctx.Dispatch(); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if ctx.GetProxy(buffer.ID()) != nil {
		t.Fatalf("delete_id after Display.Destroy() was discarded: object %d is still registered", buffer.ID())
	}
	if ctx.GetProxy(registry.ID()) != registry {
		t.Fatal("delete_id removed an unrelated live object")
	}
}

func TestDispatchKeepsDisplayErrorAfterDestroy(t *testing.T) {
	ctx, peer := socketPairContext(t)
	display := NewDisplay(ctx)

	if err := display.Destroy(); err != nil {
		t.Fatal(err)
	}
	body := make([]byte, 16)
	PutUint32(body[0:4], display.ID())
	PutUint32(body[4:8], uint32(DisplayErrorNoMemory))
	PutString(body[8:], "bad")
	if _, err := peer.Write(testFrame(display.ID(), 0, body)); err != nil {
		t.Fatal(err)
	}
	err := ctx.Dispatch()
	if err == nil || !strings.Contains(err.Error(), "wl_display.error") {
		t.Fatalf("Dispatch() error = %v, want wl_display.error after Display.Destroy()", err)
	}
	if ctx.fatalErr != err {
		t.Fatalf("fatalErr = %v, want the recorded display error %v", ctx.fatalErr, err)
	}
	if writeErr := ctx.WriteMsg(testFrame(display.ID(), 0, nil), nil); writeErr != err {
		t.Fatalf("WriteMsg() error = %v, want the recorded display error %v", writeErr, err)
	}
}
