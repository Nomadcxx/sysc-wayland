# Upstream provenance

## Foundation source

- Repository: <https://github.com/AvengeMedia/dankgo>
- Base commit: `10434658325c819efaf063f48eec4ae36555727e`
- Last reviewed commit: `10434658325c819efaf063f48eec4ae36555727e`
- Copied client path: `wayland/client/`
- Copied scanner path: `cmd/go-wayland-scanner/`

The client licence is preserved in `LICENSES/dankgo-wayland.txt`. The scanner licence is preserved in
`LICENSES/go-wayland-scanner.txt`. Generated bindings retain the protocol copyright text emitted from
their source XML.

## Core protocol XML

- Vendored path: `protocols/wayland.xml`
- Source: <https://gitlab.freedesktop.org/wayland/wayland/-/raw/1.24.0/protocol/wayland.xml?ref_type=tags>
- Upstream release: `1.24.0`
- SHA-256: `60abb5864546288a660f3d8af0c838fc87c85bb386582da23e52cb8476e9adbf`

The vendored XML and `client/client.go` use Wayland 1.24.0. The scanner reproduces the checked binding
from this local source; upgrading the protocol XML requires an explicit public API review.

## Divergences

| Commit | Invariant | Check | Upstream status |
|---|---|---|---|
| `c06156e` | One connection goroutine owns a plain proxy map and ID allocation. | `go test ./client -run 'TestObject\|TestProxyID'` | Local divergence |
| `ad90f18` | Frame reads tolerate fragmentation, preserve frame boundaries, and reject invalid headers. | `go test ./client -run TestReadFrame` | Local divergence |
| `2d9acb8` | Reads retain one descriptor across fragments; writes send rights once and complete short writes. | `go test ./client -run 'TestReadFrame.*FD\|TestWriteFrame'` | Local divergence |
| `47068e9` | `ControlFD` scopes descriptor access; dispatch stays on the owner goroutine and makes fatal errors sticky. | `go test ./client -run 'TestControlFD\|TestDispatchOwnership'` | Local divergence |
| `23e960c` | The scanner emits the sysc client import, requires explicit external xdg-shell imports, and preserves fatal dispatch behavior. | `go test ./cmd/sysc-wayland-scanner` | Local divergence |
