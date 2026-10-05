// Package sessionlock holds the generated ext_session_lock_v1 binding.
//
// Upstream: https://gitlab.freedesktop.org/wayland/wayland-protocols
// Revision: tag 1.49, staging/ext-session-lock/ext-session-lock-v1.xml
// SHA-256:  a05df7d95c5e523e457037b3a149484e8064828cda66971f3c7caeb87d597a81
package sessionlock

//go:generate go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner -pkg sessionlock -o ext_session_lock_v1.go -i ../protocols/ext-session-lock-v1.xml
