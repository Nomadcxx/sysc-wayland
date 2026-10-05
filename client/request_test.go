package client

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
)

func TestWriteFrameSendsRightsOnce(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5, 6}
	rights := []byte{7, 8, 9}
	firstCalls := 0
	first := func(gotData, gotRights []byte, _ *net.UnixAddr) (int, int, error) {
		firstCalls++
		if !bytes.Equal(gotData, data) || !bytes.Equal(gotRights, rights) {
			t.Fatalf("first write = (%v, %v), want (%v, %v)", gotData, gotRights, data, rights)
		}
		return 2, len(gotRights), nil
	}
	var plain []byte
	write := func(remaining []byte) (int, error) {
		plain = append(plain, remaining...)
		return len(remaining), nil
	}

	if err := writeFrame(first, write, data, rights); err != nil {
		t.Fatalf("writeFrame() error = %v", err)
	}
	if firstCalls != 1 {
		t.Fatalf("first write calls = %d, want 1", firstCalls)
	}
	if !bytes.Equal(plain, data[2:]) {
		t.Fatalf("plain writes = %v, want %v", plain, data[2:])
	}
}

func TestWriteFrameCompletesRepeatedShortWrites(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5, 6}
	first := func([]byte, []byte, *net.UnixAddr) (int, int, error) { return 1, 0, nil }
	var written []byte
	write := func(remaining []byte) (int, error) {
		n := 2
		if n > len(remaining) {
			n = len(remaining)
		}
		written = append(written, remaining[:n]...)
		return n, nil
	}

	if err := writeFrame(first, write, data, nil); err != nil {
		t.Fatalf("writeFrame() error = %v", err)
	}
	if !bytes.Equal(written, data[1:]) {
		t.Fatalf("plain writes = %v, want %v", written, data[1:])
	}
}

func TestWriteFrameRejectsZeroProgress(t *testing.T) {
	first := func([]byte, []byte, *net.UnixAddr) (int, int, error) { return 0, 0, nil }

	err := writeFrame(first, func([]byte) (int, error) { return 0, nil }, []byte{1}, nil)
	if !errors.Is(err, io.ErrNoProgress) {
		t.Fatalf("writeFrame() error = %v, want io.ErrNoProgress", err)
	}
}

func TestWriteFrameRejectsShortAncillaryWrite(t *testing.T) {
	plainCalls := 0
	first := func([]byte, []byte, *net.UnixAddr) (int, int, error) { return 2, 1, nil }
	write := func([]byte) (int, error) {
		plainCalls++
		return 0, nil
	}

	err := writeFrame(first, write, []byte{1, 2, 3}, []byte{4, 5})
	if err == nil || !strings.Contains(err.Error(), "ancillary") {
		t.Fatalf("writeFrame() error = %v, want ancillary error", err)
	}
	if plainCalls != 0 {
		t.Fatalf("plain write calls = %d, want 0", plainCalls)
	}
}

func TestWriteFrameReturnsFatalErrorAfterPartialWrite(t *testing.T) {
	want := errors.New("write failed")
	first := func([]byte, []byte, *net.UnixAddr) (int, int, error) { return 2, 0, nil }
	write := func([]byte) (int, error) { return 0, want }

	err := writeFrame(first, write, []byte{1, 2, 3}, nil)
	if !errors.Is(err, want) || !errors.Is(err, ErrPartialFrame) {
		t.Fatalf("writeFrame() error = %v, want joined partial-frame and write errors", err)
	}
}

// A request whose marshalled length does not fit the 16-bit Wayland size field would wrap it
// and desynchronize the connection. WriteMsg must refuse it before writing any byte and leave
// the connection usable.
func TestWriteMsgRefusesOversizeFrame(t *testing.T) {
	ctx, peer := socketPairContext(t)

	oversize := make([]byte, 0x10000) // 65536 > 65535
	if err := ctx.WriteMsg(oversize, nil); !errors.Is(err, ErrFrameSize) {
		t.Fatalf("WriteMsg() oversize error = %v, want ErrFrameSize", err)
	}
	if ctx.fatalErr != nil {
		t.Fatalf("fatalErr = %v after a refused oversize frame, want nil", ctx.fatalErr)
	}

	unaligned := make([]byte, 10) // not a multiple of 4
	if err := ctx.WriteMsg(unaligned, nil); !errors.Is(err, ErrFrameSize) {
		t.Fatalf("WriteMsg() unaligned error = %v, want ErrFrameSize", err)
	}

	// Nothing reached the socket, and a valid frame still writes normally.
	if pending, err := pendingBytes(peer); err != nil || pending != 0 {
		t.Fatalf("peer pending = %d, %v, want 0", pending, err)
	}
	if err := ctx.WriteMsg(testFrame(1, 7, []byte{1, 2, 3, 4}), nil); err != nil {
		t.Fatalf("WriteMsg() valid frame: %v", err)
	}
	if pending, err := pendingBytes(peer); err != nil || pending == 0 {
		t.Fatalf("peer pending = %d, %v, want the valid frame", pending, err)
	}
}

func TestWriteMsgRejectsIncompleteOrMismatchedFrame(t *testing.T) {
	shortHeader := make([]byte, 4)
	smallSize := testFrame(1, 0, []byte{1, 2, 3, 4})
	PutUint32(smallSize[4:8], 8<<16)
	largeSize := testFrame(1, 0, []byte{1, 2, 3, 4})
	PutUint32(largeSize[4:8], 16<<16)
	batched := append(testFrame(1, 0, nil), testFrame(1, 0, nil)...)
	for _, tt := range []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"short header", shortHeader},
		{"header size too small", smallSize},
		{"header size too large", largeSize},
		{"multiple frames", batched},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, peer := socketPairContext(t)
			if err := ctx.WriteMsg(tt.data, nil); !errors.Is(err, ErrFrameSize) {
				t.Fatalf("WriteMsg() error = %v, want ErrFrameSize", err)
			}
			if ctx.fatalErr != nil {
				t.Fatalf("fatalErr = %v after a rejected frame", ctx.fatalErr)
			}
			if pending, err := pendingBytes(peer); err != nil || pending != 0 {
				t.Fatalf("peer pending = %d, %v, want 0", pending, err)
			}
			valid := testFrame(1, 0, nil)
			if err := ctx.WriteMsg(valid, nil); err != nil {
				t.Fatal(err)
			}
			got := make([]byte, len(valid))
			if _, err := io.ReadFull(peer, got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, valid) {
				t.Fatalf("received frame = %v, want %v", got, valid)
			}
		})
	}
}

func TestWriteMsgAcceptsLargestAlignedFrame(t *testing.T) {
	ctx, peer := socketPairContext(t)
	frame := testFrame(1, 0, make([]byte, 65532-8))
	if err := ctx.WriteMsg(frame, nil); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(frame))
	if _, err := io.ReadFull(peer, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, frame) {
		t.Fatal("largest aligned frame was changed during the write")
	}
}
