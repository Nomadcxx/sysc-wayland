package client

import "sync/atomic"

type Dispatcher interface {
	Dispatch(opcode uint32, fd int, data []byte)
}

// FDDispatcher reports whether an opcode consumes a received file descriptor.
// It is optional so existing custom dispatchers keep the original ownership
// behavior; generated dispatchers implement it for protocol-aware routing.
type FDDispatcher interface {
	HasFD(opcode uint32) bool
}

type Proxy interface {
	Context() *Context
	SetContext(ctx *Context)
	ID() uint32
	SetID(id uint32)
	IsZombie() bool
	MarkZombie()
}

type WaylandDisplay interface {
	Context() *Context
	GetRegistry() (*Registry, error)
	Roundtrip() error
	Destroy() error
}

var _ WaylandDisplay = (*Display)(nil)

type BaseProxy struct {
	ctx    *Context
	id     uint32
	zombie atomic.Bool
}

func (p *BaseProxy) ID() uint32 {
	return p.id
}

func (p *BaseProxy) SetID(id uint32) {
	p.id = id
}

func (p *BaseProxy) Context() *Context {
	return p.ctx
}

func (p *BaseProxy) SetContext(ctx *Context) {
	p.ctx = ctx
}

func (p *BaseProxy) IsZombie() bool {
	return p.zombie.Load()
}

func (p *BaseProxy) MarkZombie() {
	p.zombie.Store(true)
}
