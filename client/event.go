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

var oobSpace = unix.CmsgSpace(2 * 4)

func (ctx *Context) ReadMsg() (senderID uint32, opcode uint32, fd int, msg []byte, err error) {
	fd = -1
	var fds []int
	fail := func(err error) (uint32, uint32, int, []byte, error) {
		closeFDs(fds)
		return 0, 0, -1, nil, err
	}

	header := make([]byte, 8)
	if err := ctx.readExact(header, &fds); err != nil {
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
		if err := ctx.readExact(msg, &fds); err != nil {
			return fail(fmt.Errorf("ctx.ReadMsg: body: %w", err))
		}
	}

	if len(fds) > 1 {
		// ponytail: generated dispatch supports one descriptor per message; add multi-FD generator
		// output before lifting this ceiling.
		return fail(fmt.Errorf("ctx.ReadMsg: supports at most one file descriptor, received %d", len(fds)))
	}
	if len(fds) == 1 {
		fd = fds[0]
		fds = nil
	}

	return senderID, opcode, fd, msg, nil
}

func (ctx *Context) readExact(dst []byte, fds *[]int) error {
	return readExactWith(ctx.conn.ReadMsgUnix, dst, fds)
}

type readMsgUnixFunc func([]byte, []byte) (int, int, int, *net.UnixAddr, error)

func readExactWith(read readMsgUnixFunc, dst []byte, fds *[]int) error {
	for len(dst) > 0 {
		oob := make([]byte, oobSpace)
		n, oobn, flags, _, readErr := read(dst, oob)
		if oobn > 0 {
			received, err := getFdsFromOob(oob, oobn, "frame")
			if err != nil {
				return err
			}
			*fds = append(*fds, received...)
		}
		if flags&(unix.MSG_CTRUNC|unix.MSG_TRUNC) != 0 {
			return fmt.Errorf("truncated socket message flags %#x", flags)
		}
		if n > len(dst) {
			return fmt.Errorf("socket returned %d bytes for %d-byte buffer", n, len(dst))
		}
		if n > 0 {
			dst = dst[n:]
		}
		if readErr != nil {
			if len(dst) == 0 {
				return nil
			}
			if errors.Is(readErr, io.EOF) {
				return io.ErrUnexpectedEOF
			}
			return readErr
		}
		if n == 0 {
			return io.ErrUnexpectedEOF
		}
	}
	return nil
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
	scms, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return nil, fmt.Errorf("getFdsFromOob: unable to parse control message from %s: %w", source, err)
	}

	var fdsRet []int
	for _, scm := range scms {
		fds, err := unix.ParseUnixRights(&scm)
		if err != nil {
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
	src = src[:idx:idx]
	return *(*string)(unsafe.Pointer(&src))
}

func Fixed(src []byte) float64 {
	_ = src[3]
	fx := *(*int32)(unsafe.Pointer(&src[0]))
	return fixedToFloat64(fx)
}
