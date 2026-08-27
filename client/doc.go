// Package client implements the sysc pure-Go Wayland client foundation.
// One goroutine owns a Context, its socket, and every proxy registered on it.
package client

//go:generate go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner -pkg client -prefix wl -o client.go -i ../protocols/wayland.xml
