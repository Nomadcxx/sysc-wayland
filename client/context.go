package client

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const firstServerID = uint32(0xff000000)

type Context struct {
	conn *net.UnixConn
	// ponytail: one goroutine owns this map; add locking only if the public ownership contract changes.
	objects    map[uint32]Proxy
	currentID  uint32
	fatalErr   error
	pendingFDs []int
}

func (ctx *Context) Register(p Proxy) {
	var id uint32
	for {
		if ctx.currentID >= firstServerID-1 {
			panic("client: Wayland object ID space exhausted")
		}
		ctx.currentID++
		id = ctx.currentID
		if _, live := ctx.objects[id]; !live {
			break
		}
	}

	p.SetID(id)
	p.SetContext(ctx)
	ctx.objects[id] = p
}

func (ctx *Context) RegisterWithID(p Proxy, id uint32) {
	if id < firstServerID {
		panic(fmt.Sprintf("client: invalid server object ID %#x", id))
	}
	// The server sends no delete_id for its own objects: it frees the ID once
	// it has handled the client's destroy, and may then reuse it. A zombie
	// there is that destroyed object, kept only to absorb events sent before
	// the destroy arrived, so the new object replaces it. Only a live object
	// at the ID is a duplicate.
	if old, ok := ctx.objects[id]; ok && !old.IsZombie() {
		panic(fmt.Sprintf("client: duplicate Wayland object ID %#x", id))
	}
	p.SetID(id)
	p.SetContext(ctx)
	ctx.objects[id] = p
}

func (ctx *Context) Unregister(p Proxy) {
	delete(ctx.objects, p.ID())
}

func (ctx *Context) DeleteID(id uint32) {
	delete(ctx.objects, id)
}

func (ctx *Context) GetProxy(id uint32) Proxy {
	proxy, _ := ctx.lookupProxy(id)
	return proxy

}

func (ctx *Context) lookupProxy(id uint32) (Proxy, bool) {
	proxy, ok := ctx.objects[id]
	return proxy, ok
}

func (ctx *Context) Close() error {
	closeFDs(ctx.pendingFDs)
	ctx.pendingFDs = nil
	return ctx.conn.Close()
}

func (ctx *Context) SetReadDeadline(t time.Time) error {
	return ctx.conn.SetReadDeadline(t)
}

var ErrNilControlCallback = errors.New("client: nil descriptor callback")

func (ctx *Context) ControlFD(fn func(fd int) error) error {
	if fn == nil {
		return ErrNilControlCallback
	}
	rawConn, err := ctx.conn.SyscallConn()
	if err != nil {
		return err
	}
	return controlFD(rawConn, fn)
}

func controlFD(raw syscall.RawConn, fn func(int) error) error {
	var callbackErr error
	controlErr := raw.Control(func(fd uintptr) {
		callbackErr = fn(int(fd))
	})
	return errors.Join(controlErr, callbackErr)
}

// Dispatch reads and processes incoming messages and calls [client.Dispatcher.Dispatch] on the
// respective wayland protocol.
// Dispatch must be called on the same goroutine as other interactions with the Context.
// Dispatch blocks if there are no incoming messages.
// A Dispatch loop is usually used to handle incoming messages.
func (ctx *Context) Dispatch() (dispatchErr error) {
	if ctx.fatalErr != nil {
		return ctx.fatalErr
	}

	senderID, opcode, fd, data, err := ctx.ReadMsg()
	if err != nil {
		// An idle deadline at a frame boundary consumed no bytes and is not a protocol
		// failure, so propagate it without poisoning the connection.
		if errors.Is(err, errReadTimeout) {
			return err
		}
		return ctx.setFatal(fmt.Errorf("%w: %w", ErrDispatchUnableToReadMsg, err))
	}
	proxy, ok := ctx.lookupProxy(senderID)
	if !ok {
		closeReceivedFD(fd)
		return ctx.setFatal(fmt.Errorf("%w (senderID=%d)", ErrDispatchSenderNotFound, senderID))
	}
	if fdDispatcher, ok := proxy.(FDDispatcher); ok && !fdDispatcher.HasFD(opcode) {
		ctx.putBackFD(fd)
		fd = -1
	}
	if proxy.IsZombie() {
		// wl_display has no destructor request, so Display.Destroy only zombies it locally: the
		// object is still the one that receives delete_id and error. Dropping those would leak
		// object IDs and hide a protocol error, so the display keeps handling its own events.
		// Every other zombie still absorbs in-flight events.
		if _, isDisplay := proxy.(*Display); !isDisplay {
			closeReceivedFD(fd)
			return nil
		}
	}
	sender, ok := proxy.(Dispatcher)
	if !ok {
		closeReceivedFD(fd)
		return ctx.setFatal(fmt.Errorf("%w (senderID=%d)", ErrDispatchSenderUnsupported, senderID))
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			closeReceivedFD(fd)
			dispatchErr = ctx.setFatal(fmt.Errorf("dispatch: panic handling opcode=%d senderID=%d: %v", opcode, senderID, recovered))
		}
	}()
	sender.Dispatch(opcode, fd, data)
	// The dispatcher owns fd after a normal return, including when its
	// handler sticks fatalErr. Panic recovery above still closes fd.
	if ctx.fatalErr != nil {
		return ctx.fatalErr
	}
	return nil
}

var ErrDispatchSenderNotFound = errors.New("dispatch: unable to find sender")
var ErrDispatchSenderUnsupported = errors.New("dispatch: sender does not implement Dispatch method")
var ErrDispatchUnableToReadMsg = errors.New("dispatch: unable to read msg")

func (ctx *Context) setFatal(err error) error {
	if ctx.fatalErr == nil {
		ctx.fatalErr = err
		closeFDs(ctx.pendingFDs)
		ctx.pendingFDs = nil
	}
	return ctx.fatalErr
}

// putBackFD restores a descriptor that belongs to a later protocol event.
// ponytail: prepending is O(n); descriptor batches are bounded by the socket ancillary buffer, so a ring is unnecessary until that bound grows.
func (ctx *Context) putBackFD(fd int) {
	if fd < 0 {
		return
	}
	ctx.pendingFDs = append([]int{fd}, ctx.pendingFDs...)
}

func (ctx *Context) recordDisplayError(event DisplayErrorEvent) {
	var objectID uint32
	if event.ObjectId != nil {
		objectID = event.ObjectId.ID()
	}
	ctx.setFatal(fmt.Errorf("wl_display.error: object=%d code=%d: %s", objectID, event.Code, event.Message))
}

func closeReceivedFD(fd int) {
	if fd >= 0 {
		_ = unix.Close(fd)
	}
}

func Connect(addr string) (*Display, error) {
	ctx := &Context{objects: make(map[uint32]Proxy)}
	if addr == "" {
		if value, ok := os.LookupEnv("WAYLAND_SOCKET"); ok {
			fd, err := strconv.Atoi(value)
			if err != nil || fd < 0 {
				return nil, fmt.Errorf("env WAYLAND_SOCKET is not a valid file descriptor: %q", value)
			}
			conn, err := unixConnFromFD(fd)
			// unixConnFromFD closes fd both when the socket is adopted and when
			// that adopt fails. Drop the name so a later Connect or a child
			// falls through to WAYLAND_DISPLAY instead of the closed number.
			unsetErr := os.Unsetenv("WAYLAND_SOCKET")
			if err != nil {
				return nil, errors.Join(fmt.Errorf("env WAYLAND_SOCKET: %w", err), unsetErr)
			}
			if unsetErr != nil {
				_ = conn.Close()
				return nil, unsetErr
			}
			ctx.conn = conn
			return NewDisplay(ctx), nil
		}

		addr = os.Getenv("WAYLAND_DISPLAY")
		if addr == "" {
			addr = "wayland-0"
		}
	}
	if !filepath.IsAbs(addr) {
		runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
		if runtimeDir == "" {
			return nil, errors.New("env XDG_RUNTIME_DIR not set")
		}
		addr = filepath.Join(runtimeDir, addr)
	}

	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: addr, Net: "unix"})
	if err != nil {
		return nil, err
	}
	ctx.conn = conn

	return NewDisplay(ctx), nil
}

func unixConnFromFD(fd int) (*net.UnixConn, error) {
	file := os.NewFile(uintptr(fd), "wayland")
	if file == nil {
		return nil, errors.New("invalid file descriptor")
	}
	conn, err := net.FileConn(file)
	closeErr := file.Close()
	if err != nil {
		return nil, errors.Join(err, closeErr)
	}
	if closeErr != nil {
		_ = conn.Close()
		return nil, closeErr
	}

	unixConn, ok := conn.(*net.UnixConn)
	if !ok || unixConn.LocalAddr() == nil || unixConn.LocalAddr().Network() != "unix" {
		_ = conn.Close()
		return nil, errors.New("file descriptor is not a Unix stream socket")
	}
	return unixConn, nil
}
