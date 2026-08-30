package textinput

import (
	"reflect"
	"testing"
)

func TestGeneratedTextInputRequests(t *testing.T) {
	var ti *ZwpTextInputV3
	_ = ti.Enable
	_ = ti.Disable
	_ = ti.SetSurroundingText
	_ = ti.SetContentType
	_ = ti.SetCursorRectangle
	_ = ti.Commit
	var mgr *ZwpTextInputManagerV3
	_ = mgr.GetTextInput
}

func TestTextInputHasNoSetSurfaces(t *testing.T) {
	var ti *ZwpTextInputV3
	if _, ok := reflect.TypeOf(ti).MethodByName("SetSurfaces"); ok {
		t.Fatal("text-input-v3 has no SetSurfaces request")
	}
}
