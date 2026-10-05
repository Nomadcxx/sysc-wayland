package client

import (
	"errors"
	"io"
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

func TestReadFrameClosesUnclaimedCoalescedFDs(t *testing.T) {
	ctx, peer := socketPairContext(t)
	first := newPipe(t)
	second := newPipe(t)
	rights := unix.UnixRights(first.read, second.read)
	frame := testFrame(1, 0, nil)
	if _, _, err := peer.WriteMsgUnix(frame, rights, nil); err != nil {
		t.Fatal(err)
	}
	if err := peer.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	first.closeRead(t)
	second.closeRead(t)

	_, _, fd, _, err := ctx.ReadMsg()
	if err != nil {
		t.Fatalf("first ReadMsg() error = %v", err)
	}
	if fd < 0 {
		t.Fatal("first ReadMsg() descriptor = -1, want descriptor")
	}
	t.Cleanup(func() { unix.Close(fd) })

	_, _, fd, _, err = ctx.ReadMsg()
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("second ReadMsg() error = %v, want io.ErrUnexpectedEOF", err)
	}
	if fd != -1 {
		t.Fatalf("second ReadMsg() descriptor = %d, want -1", fd)
	}
	if _, err := unix.Write(second.write, []byte{'x'}); !errors.Is(err, unix.EPIPE) {
		t.Fatalf("write to unclaimed descriptor pipe = %v, want EPIPE", err)
	}
}

func TestContextCloseClosesQueuedFD(t *testing.T) {
	ctx, peer := socketPairContext(t)
	first := newPipe(t)
	second := newPipe(t)
	rights := unix.UnixRights(first.read, second.read)
	if _, _, err := peer.WriteMsgUnix(testFrame(1, 0, nil), rights, nil); err != nil {
		t.Fatal(err)
	}
	first.closeRead(t)
	second.closeRead(t)

	_, _, fd, _, err := ctx.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg() error = %v", err)
	}
	if fd < 0 {
		t.Fatal("ReadMsg() descriptor = -1, want descriptor")
	}
	t.Cleanup(func() { unix.Close(fd) })

	if err := ctx.Close(); err != nil {
		t.Fatalf("Context.Close() error = %v", err)
	}
	if _, err := unix.Write(second.write, []byte{'x'}); !errors.Is(err, unix.EPIPE) {
		t.Fatalf("write to queued descriptor pipe = %v, want EPIPE", err)
	}
}

func TestReadFrameDispatchesCoalescedFileDescriptorsInOrder(t *testing.T) {
	ctx, peer := socketPairContext(t)
	first := newPipe(t)
	second := newPipe(t)
	rights := unix.UnixRights(first.read, second.read)
	frames := append(testFrame(1, 0, nil), testFrame(1, 1, nil)...)
	if _, _, err := peer.WriteMsgUnix(frames, rights, nil); err != nil {
		t.Fatal(err)
	}
	first.closeRead(t)
	second.closeRead(t)

	_, opcode, firstFD, _, err := ctx.ReadMsg()
	if err != nil {
		t.Fatalf("first ReadMsg() error = %v", err)
	}
	if opcode != 0 {
		t.Fatalf("first opcode = %d, want 0", opcode)
	}
	t.Cleanup(func() { unix.Close(firstFD) })

	_, opcode, secondFD, _, err := ctx.ReadMsg()
	if err != nil {
		t.Fatalf("second ReadMsg() error = %v", err)
	}
	if opcode != 1 {
		t.Fatalf("second opcode = %d, want 1", opcode)
	}
	t.Cleanup(func() { unix.Close(secondFD) })

	if _, err := unix.Write(first.write, []byte{'a'}); err != nil {
		t.Fatal(err)
	}
	if _, err := unix.Write(second.write, []byte{'b'}); err != nil {
		t.Fatal(err)
	}
	var got [1]byte
	if _, err := unix.Read(firstFD, got[:]); err != nil {
		t.Fatal(err)
	}
	if got[0] != 'a' {
		t.Fatalf("first descriptor received %q, want a", got[0])
	}
	if _, err := unix.Read(secondFD, got[:]); err != nil {
		t.Fatal(err)
	}
	if got[0] != 'b' {
		t.Fatalf("second descriptor received %q, want b", got[0])
	}
}

func TestReadFrameRejectsControlTruncation(t *testing.T) {
	read := func(data, oob []byte) (int, int, int, *net.UnixAddr, error) {
		return len(data), 0, unix.MSG_CTRUNC, nil, nil
	}
	var fds []int

	_, err := readExactWith(read, make([]byte, 8), &fds)
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("readExactWith() error = %v, want truncation error", err)
	}
}

func TestGetFdsFromOobClosesParsedDescriptorsOnError(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := unix.FcntlInt(uintptr(fds[0]), unix.F_GETFD, 0); err == nil {
			_ = unix.Close(fds[0])
		}
		_ = unix.Close(fds[1])
	})

	oob := append(unix.UnixRights(fds[0]), unix.UnixCredentials(&unix.Ucred{})...)
	_, err = getFdsFromOob(oob, len(oob), "test")
	if err == nil {
		t.Fatal("getFdsFromOob() error = nil, want non-rights control message error")
	}
	if _, err := unix.FcntlInt(uintptr(fds[0]), unix.F_GETFD, 0); !errors.Is(err, unix.EBADF) {
		t.Fatalf("parsed descriptor remains open: %v", err)
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
