package client

import (
	"errors"
	"net"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestReadFrameFragmentedFD(t *testing.T) {
	ctx, peer := socketPairContext(t)
	pipe := newPipe(t)
	frame := testFrame(1, 3, []byte{1, 2, 3, 4})
	rights := unix.UnixRights(pipe.read)
	n, oobn, err := peer.WriteMsgUnix(frame[:8], rights, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 8 || oobn != len(rights) {
		t.Fatalf("WriteMsgUnix() = (%d, %d), want (8, %d)", n, oobn, len(rights))
	}
	pipe.closeRead(t)
	done := writeAfterDrain(ctx.conn, peer, frame[8:])

	_, _, received, _, err := ctx.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg() error = %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("write body: %v", err)
	}
	if received < 0 {
		t.Fatal("ReadMsg() descriptor = -1, want descriptor")
	}
	t.Cleanup(func() { unix.Close(received) })

	if _, err := unix.Write(pipe.write, []byte{'x'}); err != nil {
		t.Fatal(err)
	}
	var got [1]byte
	if _, err := unix.Read(received, got[:]); err != nil {
		t.Fatal(err)
	}
	if got[0] != 'x' {
		t.Fatalf("received byte = %q, want x", got[0])
	}
}

func TestReadFrameRejectsAndClosesMultipleFDs(t *testing.T) {
	ctx, peer := socketPairContext(t)
	first := newPipe(t)
	second := newPipe(t)
	rights := unix.UnixRights(first.read, second.read)
	frame := testFrame(1, 0, nil)
	if _, _, err := peer.WriteMsgUnix(frame, rights, nil); err != nil {
		t.Fatal(err)
	}
	first.closeRead(t)
	second.closeRead(t)

	_, _, fd, _, err := ctx.ReadMsg()
	if err == nil || !strings.Contains(err.Error(), "at most one file descriptor") {
		t.Fatalf("ReadMsg() = (fd %d, error %v), want unsupported descriptor count", fd, err)
	}
	for name, writeFD := range map[string]int{"first": first.write, "second": second.write} {
		if _, err := unix.Write(writeFD, []byte{'x'}); !errors.Is(err, unix.EPIPE) {
			t.Errorf("write to %s pipe error = %v, want EPIPE", name, err)
		}
	}
}

func TestReadFrameRejectsControlTruncation(t *testing.T) {
	read := func(data, oob []byte) (int, int, int, *net.UnixAddr, error) {
		return len(data), 0, unix.MSG_CTRUNC, nil, nil
	}
	var fds []int

	err := readExactWith(read, make([]byte, 8), &fds)
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("readExactWith() error = %v, want truncation error", err)
	}
}

type testPipe struct {
	read  int
	write int
}

func newPipe(t *testing.T) *testPipe {
	t.Helper()
	fds := []int{-1, -1}
	if err := unix.Pipe2(fds, unix.O_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	pipe := &testPipe{read: fds[0], write: fds[1]}
	t.Cleanup(func() {
		if pipe.read >= 0 {
			unix.Close(pipe.read)
		}
		if pipe.write >= 0 {
			unix.Close(pipe.write)
		}
	})
	return pipe
}

func (p *testPipe) closeRead(t *testing.T) {
	t.Helper()
	if err := unix.Close(p.read); err != nil {
		t.Fatal(err)
	}
	p.read = -1
}
