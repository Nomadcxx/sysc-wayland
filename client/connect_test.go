package client

import (
	"errors"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestConnectUsesAbsoluteWaylandDisplay(t *testing.T) {
	runtimeDir := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "wayland-0")
	listenConnectSocket(t, socketPath)
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("WAYLAND_DISPLAY", socketPath)

	display, err := Connect("")
	if err != nil {
		t.Fatalf("Connect(\"\") with absolute WAYLAND_DISPLAY: %v", err)
	}
	if err := display.Context().Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConnectJoinsRelativeWaylandDisplayToRuntimeDir(t *testing.T) {
	runtimeDir := t.TempDir()
	listenConnectSocket(t, filepath.Join(runtimeDir, "wayland-test"))
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")

	display, err := Connect("")
	if err != nil {
		t.Fatalf("Connect(\"\") with relative WAYLAND_DISPLAY: %v", err)
	}
	if err := display.Context().Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConnectUsesWaylandSocket(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fds[1])
	t.Setenv("WAYLAND_SOCKET", strconv.Itoa(fds[0]))
	t.Setenv("WAYLAND_DISPLAY", "unused")
	t.Setenv("XDG_RUNTIME_DIR", "")

	display, err := Connect("")
	if err != nil {
		t.Fatalf("Connect(\"\") with WAYLAND_SOCKET: %v", err)
	}
	defer display.Context().Close()

	if _, err := unix.FcntlInt(uintptr(fds[0]), unix.F_GETFD, 0); !errors.Is(err, syscall.EBADF) {
		t.Fatalf("original WAYLAND_SOCKET fd remains open: %v", err)
	}
	if _, err := display.Context().conn.Write([]byte{1}); err != nil {
		t.Fatalf("write through connected socket: %v", err)
	}
	var received [1]byte
	n, err := unix.Read(fds[1], received[:])
	if err != nil || n != 1 || received[0] != 1 {
		t.Fatalf("peer read = (%d, %v, %d), want (1, nil, 1)", n, err, received[0])
	}
}

func TestConnectRejectsInvalidWaylandSocket(t *testing.T) {
	t.Setenv("WAYLAND_SOCKET", "not-a-file-descriptor")
	_, err := Connect("")
	if err == nil || !strings.Contains(err.Error(), "WAYLAND_SOCKET") {
		t.Fatalf("Connect(\"\") error = %v, want a WAYLAND_SOCKET parse error", err)
	}
}

func listenConnectSocket(t *testing.T, path string) {
	t.Helper()
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
}
