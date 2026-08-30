// Package cursorshape holds the generated wp_cursor_shape_v1 binding.
//
// cursor-shape-v1 GetTabletToolV2 takes a zwp_tablet_tool_v2 object, so
// tablet-unstable-v2 is generated into this package rather than left as a
// dangling type. Shell code only needs SetShape on the pointer device.
//
// Upstream: https://gitlab.freedesktop.org/wayland/wayland-protocols
// Revision: tag 1.45
//
//	staging/cursor-shape/cursor-shape-v1.xml
//	SHA-256: bb57d91e53a79dadab7c612dab87c233393cee73673feefa7442cfbfdd9aed2f
//	unstable/tablet/tablet-unstable-v2.xml
//	SHA-256: db291b574adb2d42d27f3d01a77723bb3350a7a32e6d33de16513521b42294a9
package cursorshape

//go:generate go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner -pkg cursorshape -o cursor_shape.go -i ../protocols/cursor-shape-v1.xml
//go:generate go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner -pkg cursorshape -o tablet_v2.go -i ../protocols/tablet-v2.xml
