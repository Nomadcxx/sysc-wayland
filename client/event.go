package client

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"unsafe"

	"golang.org/x/sys/unix"

	_ "unsafe"
)

// Linux permits at most 253 descriptors in one SCM_RIGHTS message. Keep one
// spare slot for coalesced Wayland frames while retaining truncation checks.
const maxFDsPerControlMessage = 256

var oobSpace = unix.CmsgSpace(maxFDsPerControlMessage * 4)

// ErrReadTimeout marks a read deadline that expired at a frame boundary: no byte of the
// current frame was consumed, so the byte stream is still synchronized and a later read can
// proceed. ReadMsg returns it without setting fatalErr, and Dispatch propagates it without
// poisoning the connection. A deadline that fires after part of a frame was read is a
// different case and stays sticky-fatal.
//
// A caller that sets Context.SetReadDeadline should match on this to tell a benign idle
// timeout from a protocol failure, and keep dispatching after the former:
//
//	for {
//		err := ctx.Dispatch()
//		if errors.Is(err, client.ErrReadTimeout) {
//			continue
//		}
//		if err != nil {
//			return err
//		}
//	}
//
// Without that distinction the caller has to treat every deadline as fatal, which discards a
// connection that is still usable.
var ErrReadTimeout = errors.New("client: read deadline exceeded at frame boundary")

// isDeadline reports whether err is a deadline expiry (a net.Error whose Timeout is true).
func isDeadline(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func (ctx *Context) ReadMsg() (senderID uint32, opcode uint32, fd int, msg []byte, err error) {
	if ctx.fatalErr != nil {
		return 0, 0, -1, nil, ctx.fatalErr
	}
	fd = -1
	fail := func(err error) (uint32, uint32, int, []byte, error) {
		return 0, 0, -1, nil, ctx.setFatal(err)
	}

	header := make([]byte, 8)
	if n, err := ctx.readExact(header); err != nil {
		if n == 0 && isDeadline(err) {
			return 0, 0, -1, nil, fmt.Errorf("%w: %w", ErrReadTimeout, err)
		}
		return fail(fmt.Errorf("ctx.ReadMsg: header: %w", err))
	}

	senderID = Uint32(header[:4])
	opcodeAndSize := Uint32(header[4:8])
	opcode = opcodeAndSize & 0xffff
	size := opcodeAndSize >> 16
	if senderID == 0 {
		return fail(fmt.Errorf("ctx.ReadMsg: sender ID is zero"))
	}
	if size < 8 || size > 65535 {
		return fail(fmt.Errorf("ctx.ReadMsg: invalid frame size %d", size))
	}
	if size%4 != 0 {
		return fail(fmt.Errorf("ctx.ReadMsg: unaligned frame size %d", size))
	}

	msgSize := int(size - 8)
	msg = make([]byte, msgSize)
	if msgSize > 0 {
		if _, err := ctx.readExact(msg); err != nil {
			return fail(fmt.Errorf("ctx.ReadMsg: body: %w", err))
		}
	}

	fd = ctx.takeFD()

	return senderID, opcode, fd, msg, nil
}

func (ctx *Context) readExact(dst []byte) (int, error) {
	return readExactWith(ctx.conn.ReadMsgUnix, dst, &ctx.pendingFDs)
}

func (ctx *Context) takeFD() int {
	if len(ctx.pendingFDs) == 0 {
		return -1
	}
	fd := ctx.pendingFDs[0]
	ctx.pendingFDs[0] = -1
	ctx.pendingFDs = ctx.pendingFDs[1:]
	if len(ctx.pendingFDs) == 0 {
		ctx.pendingFDs = nil
	}
	return fd
}

type readMsgUnixFunc func([]byte, []byte) (int, int, int, *net.UnixAddr, error)

// readExactWith reads len(dst) bytes. On error it also returns how many bytes of dst were
// consumed before the error, so callers can tell a frame-boundary failure (0 bytes) from a
// partial read that desynchronized the stream.
func readExactWith(read readMsgUnixFunc, dst []byte, fds *[]int) (int, error) {
	consumed := 0
	oob := make([]byte, oobSpace)
	for len(dst) > 0 {
		n, oobn, flags, _, readErr := read(dst, oob)
		if oobn > 0 {
			received, err := getFdsFromOob(oob, oobn, "frame")
			if err != nil {
				return consumed, err
			}
			*fds = append(*fds, received...)
		}
		if flags&(unix.MSG_CTRUNC|unix.MSG_TRUNC) != 0 {
			return consumed, fmt.Errorf("truncated socket message flags %#x", flags)
		}
		if n > len(dst) {
			return consumed, fmt.Errorf("socket returned %d bytes for %d-byte buffer", n, len(dst))
		}
		if n > 0 {
			dst = dst[n:]
			consumed += n
		}
		if readErr != nil {
			if len(dst) == 0 {
				return consumed, nil
			}
			if errors.Is(readErr, io.EOF) {
				return consumed, io.ErrUnexpectedEOF
			}
			return consumed, readErr
		}
		if n == 0 {
			return consumed, io.ErrUnexpectedEOF
		}
	}
	return consumed, nil
}

func closeFDs(fds []int) {
	for _, fd := range fds {
		_ = unix.Close(fd)
	}
}

func getFdsFromOob(oob []byte, oobn int, source string) ([]int, error) {
	if oobn > len(oob) {
		return nil, fmt.Errorf("getFdsFromOob: incorrect number of bytes read from %s for oob (oobn=%d)", source, oobn)
	}
	scms, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return nil, fmt.Errorf("getFdsFromOob: unable to parse control message from %s: %w", source, err)
	}

	var fdsRet []int
	for _, scm := range scms {
		fds, err := unix.ParseUnixRights(&scm)
		if err != nil {
			closeFDs(fdsRet)
			closeFDs(fds)
			return nil, fmt.Errorf("getFdsFromOob: unable to parse unix rights from %s: %w", source, err)
		}

		fdsRet = append(fdsRet, fds...)
	}

	return fdsRet, nil
}

func Uint32(src []byte) uint32 {
	_ = src[3]
	return *(*uint32)(unsafe.Pointer(&src[0]))
}

func String(src []byte) string {
	idx := bytes.IndexByte(src, 0)
	if idx < 0 {
		panic("client: string is missing a NUL terminator")
	}
	src = src[:idx:idx]
	return *(*string)(unsafe.Pointer(&src))
}

func Fixed(src []byte) float64 {
	_ = src[3]
	fx := *(*int32)(unsafe.Pointer(&src[0]))
	return fixedToFloat64(fx)
}
