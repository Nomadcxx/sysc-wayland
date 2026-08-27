package client

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"runtime"
	"testing"

	"golang.org/x/sys/unix"
)

func TestReadFrameOneByteHeaderFragments(t *testing.T) {
	ctx, peer := socketPairContext(t)
	frame := testFrame(1, 7, []byte{1, 2, 3, 4})
	done := writeFragments(t, ctx.conn, peer, frame, 1)

	sender, opcode, fd, body, err := ctx.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg() error = %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("write fragments: %v", err)
	}
	assertFrame(t, sender, opcode, fd, body, 1, 7, []byte{1, 2, 3, 4})
}

func TestReadFrameOneByteBodyFragments(t *testing.T) {
	ctx, peer := socketPairContext(t)
	frame := testFrame(2, 9, []byte{5, 6, 7, 8})
	if _, err := peer.Write(frame[:8]); err != nil {
		t.Fatal(err)
	}
	done := writeFragments(t, ctx.conn, peer, frame[8:], 1)

	sender, opcode, fd, body, err := ctx.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg() error = %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("write fragments: %v", err)
	}
	assertFrame(t, sender, opcode, fd, body, 2, 9, []byte{5, 6, 7, 8})
}

func TestReadFrameLeavesCoalescedFrame(t *testing.T) {
	ctx, peer := socketPairContext(t)
	first := testFrame(3, 1, []byte{1, 1, 1, 1})
	second := testFrame(4, 2, []byte{2, 2, 2, 2})
	if _, err := peer.Write(append(first, second...)); err != nil {
		t.Fatal(err)
	}

	sender, opcode, fd, body, err := ctx.ReadMsg()
	if err != nil {
		t.Fatalf("first ReadMsg() error = %v", err)
	}
	assertFrame(t, sender, opcode, fd, body, 3, 1, []byte{1, 1, 1, 1})

	sender, opcode, fd, body, err = ctx.ReadMsg()
	if err != nil {
		t.Fatalf("second ReadMsg() error = %v", err)
	}
	assertFrame(t, sender, opcode, fd, body, 4, 2, []byte{2, 2, 2, 2})
}

func TestReadFrameEOFInHeader(t *testing.T) {
	ctx, peer := socketPairContext(t)
	if _, err := peer.Write(testFrame(1, 0, nil)[:4]); err != nil {
		t.Fatal(err)
	}
	if err := peer.CloseWrite(); err != nil {
		t.Fatal(err)
	}

	_, _, _, _, err := ctx.ReadMsg()
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("ReadMsg() error = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestReadFrameEOFInBody(t *testing.T) {
	ctx, peer := socketPairContext(t)
	frame := testFrame(1, 0, []byte{1, 2, 3, 4})
	if _, err := peer.Write(frame[:10]); err != nil {
		t.Fatal(err)
	}
	if err := peer.CloseWrite(); err != nil {
		t.Fatal(err)
	}

	_, _, _, _, err := ctx.ReadMsg()
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("ReadMsg() error = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestReadFrameRejectsSizeBelowHeader(t *testing.T) {
	ctx, peer := socketPairContext(t)
	if _, err := peer.Write(testHeader(1, 0, 4)); err != nil {
		t.Fatal(err)
	}

	if _, _, _, _, err := ctx.ReadMsg(); err == nil {
		t.Fatal("ReadMsg() error = nil, want invalid size")
	}
}

func TestReadFrameRejectsUnalignedSize(t *testing.T) {
	ctx, peer := socketPairContext(t)
	if _, err := peer.Write(append(testHeader(1, 0, 10), 0, 0)); err != nil {
		t.Fatal(err)
	}

	if _, _, _, _, err := ctx.ReadMsg(); err == nil {
		t.Fatal("ReadMsg() error = nil, want unaligned size")
	}
}

func TestReadFrameRejectsSenderZero(t *testing.T) {
	ctx, peer := socketPairContext(t)
	if _, err := peer.Write(testFrame(0, 0, nil)); err != nil {
		t.Fatal(err)
	}

	if _, _, _, _, err := ctx.ReadMsg(); err == nil {
		t.Fatal("ReadMsg() error = nil, want zero sender rejection")
	}
}

func socketPairContext(t *testing.T) (*Context, *net.UnixConn) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}

	toConn := func(fd int, name string) *net.UnixConn {
		file := os.NewFile(uintptr(fd), name)
		conn, err := net.FileConn(file)
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			t.Fatal(err)
		}
		unixConn, ok := conn.(*net.UnixConn)
		if !ok {
			conn.Close()
			t.Fatalf("net.FileConn() = %T, want *net.UnixConn", conn)
		}
		return unixConn
	}

	reader := toConn(fds[0], "sysc-wayland-test-reader")
	peer := toConn(fds[1], "sysc-wayland-test-writer")
	t.Cleanup(func() {
		reader.Close()
		peer.Close()
	})
	return &Context{conn: reader, objects: make(map[uint32]Proxy)}, peer
}

func writeFragments(t *testing.T, reader, peer *net.UnixConn, data []byte, width int) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	first := width
	if first > len(data) {
		first = len(data)
	}
	if _, err := peer.Write(data[:first]); err != nil {
		done <- err
		return done
	}
	go func() {
		for offset := first; offset < len(data); offset += width {
			for {
				pending, err := pendingBytes(reader)
				if err != nil {
					done <- err
					return
				}
				if pending == 0 {
					break
				}
				runtime.Gosched()
			}
			end := offset + width
			if end > len(data) {
				end = len(data)
			}
			if _, err := peer.Write(data[offset:end]); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	return done
}

func pendingBytes(conn *net.UnixConn) (int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var pending int
	var queryErr error
	controlErr := raw.Control(func(fd uintptr) {
		pending, queryErr = unix.IoctlGetInt(int(fd), unix.TIOCINQ)
	})
	return pending, errors.Join(controlErr, queryErr)
}

func writeAfterDrain(reader, peer *net.UnixConn, data []byte) <-chan error {
	done := make(chan error, 1)
	go func() {
		for {
			pending, err := pendingBytes(reader)
			if err != nil {
				done <- err
				return
			}
			if pending == 0 {
				_, err = peer.Write(data)
				done <- err
				return
			}
			runtime.Gosched()
		}
	}()
	return done
}

func testFrame(sender, opcode uint32, body []byte) []byte {
	frame := testHeader(sender, opcode, uint32(8+len(body)))
	return append(frame, body...)
}

func testHeader(sender, opcode, size uint32) []byte {
	header := make([]byte, 8)
	PutUint32(header[:4], sender)
	PutUint32(header[4:], size<<16|opcode)
	return header
}

func assertFrame(t *testing.T, sender, opcode uint32, fd int, body []byte, wantSender, wantOpcode uint32, wantBody []byte) {
	t.Helper()
	if sender != wantSender || opcode != wantOpcode || fd != -1 || !bytes.Equal(body, wantBody) {
		t.Fatalf("frame = (%d, %d, %d, %v), want (%d, %d, -1, %v)", sender, opcode, fd, body, wantSender, wantOpcode, wantBody)
	}
}
