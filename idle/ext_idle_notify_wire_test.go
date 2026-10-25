package idle

import (
	"testing"

	"github.com/Nomadcxx/sysc-wayland/client"
	"github.com/Nomadcxx/sysc-wayland/internal/waylandtest"
)

func TestGetIdleNotificationEncodesTheRequest(t *testing.T) {
	ctx, peer := waylandtest.Pair(t)
	seat := client.NewSeat(ctx)
	notifier := NewExtIdleNotifierV1(ctx)

	notification, err := notifier.GetIdleNotification(4200, seat)
	if err != nil {
		t.Fatalf("GetIdleNotification() error = %v", err)
	}

	sender, opcode, body := waylandtest.ReadRequest(t, peer)
	if sender != notifier.ID() {
		t.Fatalf("sender = %d, want the notifier %d", sender, notifier.ID())
	}
	if opcode != 1 {
		t.Fatalf("opcode = %d, want 1 (get_idle_notification)", opcode)
	}
	if len(body) != 12 {
		t.Fatalf("body is %d bytes, want new id, timeout, seat", len(body))
	}
	if got := client.Uint32(body[0:4]); got != notification.ID() {
		t.Fatalf("new id = %d, want %d", got, notification.ID())
	}
	if got := client.Uint32(body[4:8]); got != 4200 {
		t.Fatalf("timeout = %d, want 4200", got)
	}
	if got := client.Uint32(body[8:12]); got != seat.ID() {
		t.Fatalf("seat = %d, want %d", got, seat.ID())
	}
}

// The zero timeout is legal and means "notify as soon as the seat is idle";
// a guard against treating it as unset would drop that request.
func TestGetIdleNotificationAcceptsZeroTimeout(t *testing.T) {
	ctx, peer := waylandtest.Pair(t)
	seat := client.NewSeat(ctx)
	notifier := NewExtIdleNotifierV1(ctx)

	if _, err := notifier.GetIdleNotification(0, seat); err != nil {
		t.Fatalf("GetIdleNotification(0) error = %v", err)
	}
	_, opcode, body := waylandtest.ReadRequest(t, peer)
	if opcode != 1 || client.Uint32(body[4:8]) != 0 {
		t.Fatalf("opcode = %d timeout = %d, want 1 and 0", opcode, client.Uint32(body[4:8]))
	}
}

func TestIdledAndResumedReachTheirHandlers(t *testing.T) {
	ctx, peer := waylandtest.Pair(t)
	notification := NewExtIdleNotificationV1(ctx)

	var idled, resumed int
	notification.SetIdledHandler(func(ExtIdleNotificationV1IdledEvent) { idled++ })
	notification.SetResumedHandler(func(ExtIdleNotificationV1ResumedEvent) { resumed++ })

	for _, opcode := range []uint32{0, 1, 0} {
		if _, err := peer.Write(waylandtest.Frame(notification.ID(), opcode, nil)); err != nil {
			t.Fatal(err)
		}
		if err := ctx.Dispatch(); err != nil {
			t.Fatalf("Dispatch() opcode %d error = %v", opcode, err)
		}
	}

	if idled != 2 || resumed != 1 {
		t.Fatalf("idled = %d resumed = %d, want 2 and 1", idled, resumed)
	}
}

func TestIdleEventWithoutAHandlerIsIgnored(t *testing.T) {
	ctx, peer := waylandtest.Pair(t)
	notification := NewExtIdleNotificationV1(ctx)

	if _, err := peer.Write(waylandtest.Frame(notification.ID(), 0, nil)); err != nil {
		t.Fatal(err)
	}
	if err := ctx.Dispatch(); err != nil {
		t.Fatalf("Dispatch() without a handler error = %v", err)
	}
}
