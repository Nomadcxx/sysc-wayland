package cursorshape

import (
	"net"
	"path/filepath"
	"testing"

	"github.com/Nomadcxx/sysc-wayland/client"
)

// A destroyed pad still absorbs in-flight introductions: group, then ring,
// each on a zombie placeholder, must not fatal the connection.
func TestZombieTabletPadAbsorbsNestedNewIDs(t *testing.T) {
	ctx, peer := connectPair(t)
	pad := NewZwpTabletPadV2(ctx)
	pad.MarkZombie()

	const groupID = 0xff000000
	groupBody := make([]byte, 4)
	putUint32(groupBody, groupID)
	writeFrame(t, peer, pad.ID(), 0, groupBody) // zwp_tablet_pad_v2.group
	if err := ctx.Dispatch(); err != nil {
		t.Fatalf("group on zombie pad Dispatch() error = %v", err)
	}
	group, ok := ctx.GetProxy(groupID).(*ZwpTabletPadGroupV2)
	if !ok || group == nil || !group.IsZombie() {
		t.Fatalf("registered group = %#v, want zombie *ZwpTabletPadGroupV2", ctx.GetProxy(groupID))
	}

	const ringID = 0xff000001
	ringBody := make([]byte, 4)
	putUint32(ringBody, ringID)
	writeFrame(t, peer, groupID, 1, ringBody) // zwp_tablet_pad_group_v2.ring
	if err := ctx.Dispatch(); err != nil {
		t.Fatalf("ring on zombie group Dispatch() error = %v", err)
	}
	ring, ok := ctx.GetProxy(ringID).(*ZwpTabletPadRingV2)
	if !ok || ring == nil || !ring.IsZombie() {
		t.Fatalf("registered ring = %#v, want zombie *ZwpTabletPadRingV2", ctx.GetProxy(ringID))
	}

	angleCalled := false
	ring.SetAngleHandler(func(ZwpTabletPadRingV2AngleEvent) { angleCalled = true })
	angleBody := make([]byte, 4)
	putUint32(angleBody, 0)
	writeFrame(t, peer, ringID, 1, angleBody) // zwp_tablet_pad_ring_v2.angle
	if err := ctx.Dispatch(); err != nil {
		t.Fatalf("angle on zombie ring Dispatch() error = %v", err)
	}
	if angleCalled {
		t.Fatal("zombie ring placeholder ran its handler")
	}
}

// connectPair runs a client against a listening Unix socket, giving the test
// an unexported Context without duplicating the client test helpers.
func connectPair(t *testing.T) (*client.Context, *net.UnixConn) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wayland-test")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan *net.UnixConn, 1)
	go func() {
		conn, err := listener.AcceptUnix()
		if err != nil {
			accepted <- nil
			return
		}
		accepted <- conn
	}()
	display, err := client.Connect(path)
	if err != nil {
		t.Fatal(err)
	}
	peer := <-accepted
	if peer == nil {
		t.Fatal("accept failed")
	}
	t.Cleanup(func() {
		peer.Close()
		listener.Close()
	})
	return display.Context(), peer
}

func writeFrame(t *testing.T, peer *net.UnixConn, sender, opcode uint32, body []byte) {
	t.Helper()
	frame := make([]byte, 8, 8+len(body))
	putUint32(frame[0:4], sender)
	putUint32(frame[4:8], uint32(8+len(body))<<16|opcode)
	frame = append(frame, body...)
	if _, err := peer.Write(frame); err != nil {
		t.Fatal(err)
	}
}

func putUint32(dst []byte, v uint32) {
	dst[0] = byte(v)
	dst[1] = byte(v >> 8)
	dst[2] = byte(v >> 16)
	dst[3] = byte(v >> 24)
}
