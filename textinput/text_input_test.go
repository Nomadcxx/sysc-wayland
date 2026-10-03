package textinput

import (
	"net"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Nomadcxx/sysc-wayland/client"
)

func TestGeneratedTextInputRequests(t *testing.T) {
	var ti *ZwpTextInputV3
	_ = ti.Enable
	_ = ti.Disable
	_ = ti.SetSurroundingText
	_ = ti.SetContentType
	_ = ti.SetCursorRectangle
	_ = ti.Commit
	var mgr *ZwpTextInputManagerV3
	_ = mgr.GetTextInput
}

func TestNullTextInputStringsDoNotFatal(t *testing.T) {
	ctx, peer := textInputSocket(t)
	input := NewZwpTextInputV3(ctx)

	var preedit ZwpTextInputV3PreeditStringEvent
	input.SetPreeditStringHandler(func(e ZwpTextInputV3PreeditStringEvent) { preedit = e })
	preeditBody := make([]byte, 4+4+4)
	client.PutUint32(preeditBody[0:4], 0)
	client.PutUint32(preeditBody[4:8], ^uint32(0))
	client.PutUint32(preeditBody[8:12], ^uint32(0))
	if _, err := peer.Write(textInputFrame(input.ID(), 2, preeditBody)); err != nil {
		t.Fatal(err)
	}
	if err := ctx.Dispatch(); err != nil {
		t.Fatalf("Dispatch() preedit error = %v, want nil for NULL text", err)
	}
	text, ok := any(preedit.Text).(*string)
	if !ok || text != nil || preedit.CursorBegin != -1 || preedit.CursorEnd != -1 {
		t.Fatalf("preedit = (%#v, %d, %d), want (nil, -1, -1)", preedit.Text, preedit.CursorBegin, preedit.CursorEnd)
	}

	var commit ZwpTextInputV3CommitStringEvent
	input.SetCommitStringHandler(func(e ZwpTextInputV3CommitStringEvent) { commit = e })
	commitBody := make([]byte, 4)
	client.PutUint32(commitBody, 0)
	if _, err := peer.Write(textInputFrame(input.ID(), 3, commitBody)); err != nil {
		t.Fatal(err)
	}
	if err := ctx.Dispatch(); err != nil {
		t.Fatalf("Dispatch() commit error = %v, want nil for NULL text", err)
	}
	text, ok = any(commit.Text).(*string)
	if !ok || text != nil {
		t.Fatalf("commit text = %#v, want nil *string", commit.Text)
	}
}

func textInputSocket(t *testing.T) (*client.Context, *net.UnixConn) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wayland-0")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	display, err := client.Connect(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = display.Context().Close() })
	peer, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	return display.Context(), peer
}

func textInputFrame(sender, opcode uint32, body []byte) []byte {
	frame := make([]byte, 8+len(body))
	client.PutUint32(frame[:4], sender)
	client.PutUint32(frame[4:8], uint32(8+len(body))<<16|opcode)
	copy(frame[8:], body)
	return frame
}

func TestTextInputHasNoSetSurfaces(t *testing.T) {
	var ti *ZwpTextInputV3
	if _, ok := reflect.TypeOf(ti).MethodByName("SetSurfaces"); ok {
		t.Fatal("text-input-v3 has no SetSurfaces request")
	}
}
