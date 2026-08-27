package client

import (
	"errors"
	"fmt"
	"io"
	"net"
	"unsafe"
)

var ErrPartialFrame = errors.New("client: partial Wayland frame write")

func (ctx *Context) WriteMsg(b []byte, oob []byte) error {
	if ctx.fatalErr != nil {
		return ctx.fatalErr
	}
	if err := writeFrame(ctx.conn.WriteMsgUnix, ctx.conn.Write, b, oob); err != nil {
		return ctx.setFatal(err)
	}
	return nil
}

type writeMsgUnixFunc func([]byte, []byte, *net.UnixAddr) (int, int, error)
type writeFunc func([]byte) (int, error)

func writeFrame(first writeMsgUnixFunc, write writeFunc, data, oob []byte) error {
	n, oobn, err := first(data, oob, nil)
	if n < 0 || n > len(data) {
		return fmt.Errorf("%w: first write returned invalid byte count %d", ErrPartialFrame, n)
	}
	if oobn < 0 || oobn > len(oob) {
		return fmt.Errorf("%w: first write returned invalid ancillary count %d", ErrPartialFrame, oobn)
	}
	accepted := n > 0 || oobn > 0
	if oobn != len(oob) {
		return fmt.Errorf("%w: ancillary write accepted %d of %d bytes", ErrPartialFrame, oobn, len(oob))
	}
	if err != nil {
		if accepted {
			return errors.Join(ErrPartialFrame, err)
		}
		return err
	}
	if n == 0 {
		if accepted {
			return errors.Join(ErrPartialFrame, io.ErrNoProgress)
		}
		return io.ErrNoProgress
	}

	for offset := n; offset < len(data); {
		written, writeErr := write(data[offset:])
		if written < 0 || written > len(data)-offset {
			return fmt.Errorf("%w: plain write returned invalid byte count %d", ErrPartialFrame, written)
		}
		offset += written
		if writeErr != nil {
			return errors.Join(ErrPartialFrame, writeErr)
		}
		if written == 0 {
			return errors.Join(ErrPartialFrame, io.ErrNoProgress)
		}
	}
	return nil
}

func PutUint32(dst []byte, v uint32) {
	_ = dst[3]
	*(*uint32)(unsafe.Pointer(&dst[0])) = v
}

func PutFixed(dst []byte, f float64) {
	fx := fixedFromfloat64(f)
	_ = dst[3]
	*(*int32)(unsafe.Pointer(&dst[0])) = fx
}

// PutString places a string in Wayland's wire format on the destination buffer.
// It first places the length of the string (plus one for the null terminator) and then the string
// followed by a null byte.
// The length of dst must be equal to, or greater than, len(v) + 5.
func PutString(dst []byte, v string) {
	PutUint32(dst[:4], uint32(len(v)+1))
	copy(dst[4:], v)
	dst[4+len(v)] = '\x00' // To cause panic if dst is not large enough
}

func PutArray(dst []byte, a []byte) {
	PutUint32(dst[:4], uint32(len(a)))
	copy(dst[4:], a)
}
