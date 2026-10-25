package cursorshape

import (
	"testing"

	"github.com/Nomadcxx/sysc-wayland/client"
	"github.com/Nomadcxx/sysc-wayland/internal/waylandtest"
)

func TestGetPointerEncodesTheRequest(t *testing.T) {
	ctx, peer := waylandtest.Pair(t)
	pointer := client.NewPointer(ctx)
	manager := NewWpCursorShapeManagerV1(ctx)

	device, err := manager.GetPointer(pointer)
	if err != nil {
		t.Fatalf("GetPointer() error = %v", err)
	}

	sender, opcode, body := waylandtest.ReadRequest(t, peer)
	if sender != manager.ID() {
		t.Fatalf("sender = %d, want the manager %d", sender, manager.ID())
	}
	if opcode != 1 {
		t.Fatalf("opcode = %d, want 1 (get_pointer)", opcode)
	}
	if len(body) != 8 {
		t.Fatalf("body is %d bytes, want new id and pointer", len(body))
	}
	if got := client.Uint32(body[0:4]); got != device.ID() {
		t.Fatalf("new id = %d, want %d", got, device.ID())
	}
	if got := client.Uint32(body[4:8]); got != pointer.ID() {
		t.Fatalf("pointer = %d, want %d", got, pointer.ID())
	}
}

func TestGetTabletToolEncodesOpcodeTwo(t *testing.T) {
	ctx, peer := waylandtest.Pair(t)
	tool := NewZwpTabletToolV2(ctx)
	manager := NewWpCursorShapeManagerV1(ctx)

	if _, err := manager.GetTabletToolV2(tool); err != nil {
		t.Fatalf("GetTabletToolV2() error = %v", err)
	}
	_, opcode, _ := waylandtest.ReadRequest(t, peer)
	if opcode != 2 {
		t.Fatalf("opcode = %d, want 2 (get_tablet_tool_v2)", opcode)
	}
}

// SetShape is the whole reason the extension exists; assert the serial and
// shape land in the right words, in the right order.
func TestSetShapeEncodesSerialThenShape(t *testing.T) {
	ctx, peer := waylandtest.Pair(t)
	device := NewWpCursorShapeDeviceV1(ctx)

	if err := device.SetShape(91, uint32(WpCursorShapeDeviceV1ShapeWait)); err != nil {
		t.Fatalf("SetShape() error = %v", err)
	}
	sender, opcode, body := waylandtest.ReadRequest(t, peer)
	if sender != device.ID() {
		t.Fatalf("sender = %d, want the device %d", sender, device.ID())
	}
	if opcode != 1 {
		t.Fatalf("opcode = %d, want 1 (set_shape)", opcode)
	}
	if len(body) != 8 {
		t.Fatalf("body is %d bytes, want serial and shape", len(body))
	}
	if got := client.Uint32(body[0:4]); got != 91 {
		t.Fatalf("serial = %d, want 91", got)
	}
	if got := client.Uint32(body[4:8]); got != uint32(WpCursorShapeDeviceV1ShapeWait) {
		t.Fatalf("shape = %d, want %d", got, uint32(WpCursorShapeDeviceV1ShapeWait))
	}
}

func TestDestroyMarksTheDeviceZombie(t *testing.T) {
	ctx, peer := waylandtest.Pair(t)
	device := NewWpCursorShapeDeviceV1(ctx)

	if err := device.Destroy(); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	_, opcode, _ := waylandtest.ReadRequest(t, peer)
	if opcode != 0 {
		t.Fatalf("opcode = %d, want 0 (destroy)", opcode)
	}
	if !device.IsZombie() {
		t.Fatal("Destroy() left the device live, so later events would run handlers")
	}
}
