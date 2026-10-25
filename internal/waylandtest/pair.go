// Package waylandtest provides the socket pair and frame helpers that the
// generated protocol packages use to assert their wire format.
//
// Each generated package owns its requests and its dispatch; what they share
// is only the plumbing to put a Context on one end of a unix socket and read
// or write whole Wayland messages on the other. Keeping that here stops every
// package from growing its own copy, and keeps the copies from drifting.
package waylandtest

import (
	"io"
	"net"
	"path/filepath"
	"testing"

	"github.com/Nomadcxx/sysc-wayland/client"
)

// Pair returns a client Context connected to a listening socket, plus the
// server end. The test can write frames to peer and read the requests the
// client emits from it. Both ends and the listener are closed on cleanup.
func Pair(t *testing.T) (*client.Context, *net.UnixConn) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wayland-0")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("listen on %s: %v", path, err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	display, err := client.Connect(path)
	if err != nil {
		t.Fatalf("connect to %s: %v", path, err)
	}
	t.Cleanup(func() { _ = display.Context().Close() })

	peer, err := listener.AcceptUnix()
	if err != nil {
		t.Fatalf("accept on %s: %v", path, err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	return display.Context(), peer
}

// ReadRequest reads one complete request from the server end of a Pair and
// returns its sender, opcode and body. It fails the test on a short or
// malformed frame, so a test can assert the fields without checking lengths
// it does not care about.
func ReadRequest(t *testing.T, peer *net.UnixConn) (sender uint32, opcode uint32, body []byte) {
	t.Helper()
	header := make([]byte, 8)
	if _, err := io.ReadFull(peer, header); err != nil {
		t.Fatalf("read request header: %v", err)
	}
	sender = client.Uint32(header[0:4])
	word := client.Uint32(header[4:8])
	length := word >> 16
	opcode = word & 0xffff
	if length < 8 {
		t.Fatalf("request length = %d, want at least the 8-byte header", length)
	}
	body = make([]byte, length-8)
	if _, err := io.ReadFull(peer, body); err != nil {
		t.Fatalf("read request body: %v", err)
	}
	return sender, opcode, body
}

// Frame builds an event frame the test can write to the server end of a Pair.
func Frame(sender, opcode uint32, body []byte) []byte {
	frame := make([]byte, 8+len(body))
	client.PutUint32(frame[:4], sender)
	client.PutUint32(frame[4:8], uint32(8+len(body))<<16|opcode)
	copy(frame[8:], body)
	return frame
}
