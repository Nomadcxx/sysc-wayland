package client

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const firstServerID = uint32(0xff000000)

type Context struct {
	conn *net.UnixConn
	// ponytail: one goroutine owns this map; add locking only if the public ownership contract changes.
	objects   map[uint32]Proxy
	currentID uint32
	fatalErr  error
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
	if _, live := ctx.objects[id]; live {
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
		return ctx.setFatal(fmt.Errorf("%w: %w", ErrDispatchUnableToReadMsg, err))
	}
	proxy, ok := ctx.lookupProxy(senderID)
	if !ok {
		closeReceivedFD(fd)
		return ctx.setFatal(fmt.Errorf("%w (senderID=%d)", ErrDispatchSenderNotFound, senderID))
	}
	if proxy.IsZombie() {
		closeReceivedFD(fd)
		return nil
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
	if ctx.fatalErr != nil {
		closeReceivedFD(fd)
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
	}
	return ctx.fatalErr
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
	if addr == "" {
		runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
		if runtimeDir == "" {
			return nil, errors.New("env XDG_RUNTIME_DIR not set")
		}
		if addr == "" {
			addr = os.Getenv("WAYLAND_DISPLAY")
		}
		if addr == "" {
			addr = "wayland-0"
		}
		addr = runtimeDir + "/" + addr
	}

	ctx := &Context{objects: make(map[uint32]Proxy)}

	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: addr, Net: "unix"})
	if err != nil {
		return nil, err
	}
	ctx.conn = conn

	return NewDisplay(ctx), nil
}
