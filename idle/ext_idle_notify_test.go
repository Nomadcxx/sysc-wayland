package idle

import (
	"reflect"
	"testing"

	"github.com/Nomadcxx/sysc-wayland/client"
)

func TestGeneratedExtIdleNotifySurface(t *testing.T) {
	var notifier *ExtIdleNotifierV1
	_ = notifier.GetIdleNotification
	_ = notifier.GetInputIdleNotification
	_ = notifier.Destroy

	var note *ExtIdleNotificationV1
	_ = note.SetIdledHandler
	_ = note.SetResumedHandler
	_ = note.Destroy

	if ExtIdleNotifierV1InterfaceName != "ext_idle_notifier_v1" {
		t.Fatalf("interface name = %q", ExtIdleNotifierV1InterfaceName)
	}
	if reflect.TypeOf(note).Kind() != reflect.Ptr {
		t.Fatal("notification should be a proxy type")
	}
}

func TestGetIdleNotificationSignature(t *testing.T) {
	sig := reflect.TypeOf((*ExtIdleNotifierV1).GetIdleNotification)
	if in := sig.NumIn(); in != 3 { // receiver, timeout, seat
		t.Fatalf("GetIdleNotification takes %d args, want 3", in)
	}
	if !sig.IsVariadic() {
		if seat := sig.In(2); seat != reflect.TypeOf((*client.Seat)(nil)) {
			t.Fatalf("seat arg = %v", seat)
		}
	}
	out := sig.NumOut()
	if out != 2 { // notification, error
		t.Fatalf("GetIdleNotification returns %d values, want 2", out)
	}
	if got := sig.Out(0); got != reflect.TypeOf((*ExtIdleNotificationV1)(nil)) {
		t.Fatalf("new object type = %v", got)
	}
	if sig.Out(1) != reflect.TypeOf((*error)(nil)).Elem() {
		t.Fatal("second return should be error")
	}
}
