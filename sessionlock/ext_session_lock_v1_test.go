package sessionlock

import (
	"reflect"
	"testing"

	"github.com/Nomadcxx/sysc-wayland/client"
)

func TestGeneratedExtSessionLockSurface(t *testing.T) {
	var manager *ExtSessionLockManagerV1
	_ = manager.Lock
	_ = manager.Destroy

	var lock *ExtSessionLockV1
	_ = lock.UnlockAndDestroy
	_ = lock.GetLockSurface
	_ = lock.SetLockedHandler
	_ = lock.SetFinishedHandler
	_ = lock.Destroy

	var surface *ExtSessionLockSurfaceV1
	_ = surface.AckConfigure
	_ = surface.SetConfigureHandler
	_ = surface.Destroy

	if ExtSessionLockManagerV1InterfaceName != "ext_session_lock_manager_v1" {
		t.Fatalf("manager interface name = %q", ExtSessionLockManagerV1InterfaceName)
	}
	if ExtSessionLockV1InterfaceName != "ext_session_lock_v1" {
		t.Fatalf("lock interface name = %q", ExtSessionLockV1InterfaceName)
	}
	if ExtSessionLockSurfaceV1InterfaceName != "ext_session_lock_surface_v1" {
		t.Fatalf("surface interface name = %q", ExtSessionLockSurfaceV1InterfaceName)
	}
	if reflect.TypeOf(lock).Kind() != reflect.Ptr {
		t.Fatal("lock should be a proxy type")
	}
}

func TestLockSignature(t *testing.T) {
	sig := reflect.TypeOf((*ExtSessionLockManagerV1).Lock)
	if in := sig.NumIn(); in != 1 { // receiver + ctx
		t.Fatalf("Lock takes %d args, want 1", in)
	}
	out := sig.NumOut()
	if out != 2 { // lock, error
		t.Fatalf("Lock returns %d values, want 2", out)
	}
	if got := sig.Out(0); got != reflect.TypeOf((*ExtSessionLockV1)(nil)) {
		t.Fatalf("new object type = %v", got)
	}
	if sig.Out(1) != reflect.TypeOf((*error)(nil)).Elem() {
		t.Fatal("second return should be error")
	}
}

func TestGetLockSurfaceSignature(t *testing.T) {
	sig := reflect.TypeOf((*ExtSessionLockV1).GetLockSurface)
	if in := sig.NumIn(); in != 3 { // receiver, surface, output
		t.Fatalf("GetLockSurface takes %d args, want 3", in)
	}
	if got := sig.In(1); got != reflect.TypeOf((*client.Surface)(nil)) {
		t.Fatalf("surface arg = %v", got)
	}
	if got := sig.In(2); got != reflect.TypeOf((*client.Output)(nil)) {
		t.Fatalf("output arg = %v", got)
	}
	if out := sig.NumOut(); out != 2 {
		t.Fatalf("GetLockSurface returns %d values, want 2", out)
	}
	if got := sig.Out(0); got != reflect.TypeOf((*ExtSessionLockSurfaceV1)(nil)) {
		t.Fatalf("new object type = %v", got)
	}
}

func TestAckConfigureSignature(t *testing.T) {
	sig := reflect.TypeOf((*ExtSessionLockSurfaceV1).AckConfigure)
	if in := sig.NumIn(); in != 2 { // receiver, serial
		t.Fatalf("AckConfigure takes %d args, want 2", in)
	}
	if got := sig.In(1); got != reflect.TypeOf(uint32(0)) {
		t.Fatalf("serial arg = %v", got)
	}
}
