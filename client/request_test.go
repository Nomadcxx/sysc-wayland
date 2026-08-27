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
