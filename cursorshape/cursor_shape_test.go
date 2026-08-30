package cursorshape

import "testing"

func TestGeneratedCursorShapeRequests(t *testing.T) {
	var mgr *WpCursorShapeManagerV1
	_ = mgr.GetPointer
	var dev *WpCursorShapeDeviceV1
	_ = dev.SetShape
	_ = WpCursorShapeDeviceV1ShapeDefault
	_ = WpCursorShapeDeviceV1ShapeText
}
