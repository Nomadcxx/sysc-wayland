package client

import (
	"errors"
	"net"
	"os"
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

func TestConnectDropsAdoptedWaylandSocket(t *testing.T) {
	t.Run("success falls back later", func(t *testing.T) {
		runtimeDir := t.TempDir()
		listenConnectSocket(t, filepath.Join(runtimeDir, "wayland-0"))
		fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(fds[1])
		socketValue := strconv.Itoa(fds[0])
		t.Setenv("WAYLAND_SOCKET", socketValue)
		t.Setenv("WAYLAND_DISPLAY", "wayland-0")
		t.Setenv("XDG_RUNTIME_DIR", runtimeDir)

		display, err := Connect("")
		if err != nil {
			t.Fatalf("Connect(\"\") with WAYLAND_SOCKET: %v", err)
		}
		if err := display.Context().Close(); err != nil {
			t.Fatal(err)
		}
		if got, ok := os.LookupEnv("WAYLAND_SOCKET"); ok {
			t.Fatalf("WAYLAND_SOCKET = %q after a successful adopt", got)
		}

		again, err := Connect("")
		if err != nil {
			t.Fatalf("Connect(\"\") after adopting WAYLAND_SOCKET: %v", err)
		}
		if err := again.Context().Close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("failed adopt falls back later", func(t *testing.T) {
		runtimeDir := t.TempDir()
		listenConnectSocket(t, filepath.Join(runtimeDir, "wayland-0"))
		file, err := os.CreateTemp(t.TempDir(), "not-a-socket")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		fd, err := unix.Dup(int(file.Fd()))
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("WAYLAND_SOCKET", strconv.Itoa(fd))
		t.Setenv("WAYLAND_DISPLAY", "wayland-0")
		t.Setenv("XDG_RUNTIME_DIR", runtimeDir)

		if _, err := Connect(""); err == nil {
			t.Fatal("Connect(\"\") adopted a regular file")
		}
		if got, ok := os.LookupEnv("WAYLAND_SOCKET"); ok {
			t.Fatalf("WAYLAND_SOCKET = %q after the descriptor was closed", got)
		}

		display, err := Connect("")
		if err != nil {
			t.Fatalf("Connect(\"\") after a failed adopt: %v", err)
		}
		if err := display.Context().Close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("invalid number stays set", func(t *testing.T) {
		runtimeDir := t.TempDir()
		listenConnectSocket(t, filepath.Join(runtimeDir, "wayland-0"))
		t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
		t.Setenv("WAYLAND_DISPLAY", "wayland-0")
		for _, value := range []string{"not-a-file-descriptor", "-1"} {
			t.Run(value, func(t *testing.T) {
				t.Setenv("WAYLAND_SOCKET", value)
				_, err := Connect("")
				if err == nil || !strings.Contains(err.Error(), "WAYLAND_SOCKET") {
					t.Fatalf("Connect(\"\") error = %v, want a WAYLAND_SOCKET parse error", err)
				}
				if got := os.Getenv("WAYLAND_SOCKET"); got != value {
					t.Fatalf("WAYLAND_SOCKET = %q, want %q", got, value)
				}
			})
		}
	})

	t.Run("explicit name leaves it set", func(t *testing.T) {
		socketPath := filepath.Join(t.TempDir(), "wayland-0")
		listenConnectSocket(t, socketPath)
		fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(fds[0])
		defer unix.Close(fds[1])
		socketValue := strconv.Itoa(fds[0])
		t.Setenv("WAYLAND_SOCKET", socketValue)
		t.Setenv("XDG_RUNTIME_DIR", "")
		t.Setenv("WAYLAND_DISPLAY", "unused")

		display, err := Connect(socketPath)
		if err != nil {
			t.Fatalf("Connect(%q): %v", socketPath, err)
		}
		if err := display.Context().Close(); err != nil {
			t.Fatal(err)
		}
		if got := os.Getenv("WAYLAND_SOCKET"); got != socketValue {
			t.Fatalf("WAYLAND_SOCKET = %q, want %q", got, socketValue)
		}
		if _, err := unix.FcntlInt(uintptr(fds[0]), unix.F_GETFD, 0); err != nil {
			t.Fatalf("explicit connect closed WAYLAND_SOCKET fd: %v", err)
		}
	})
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
