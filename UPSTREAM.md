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

- Source path: `/usr/share/wayland/wayland.xml`
- Installed package: `wayland 1.26.0-1`
- Upstream release: `1.26.0`
- SHA-256: `cc860987e54f8d85c940e97fa1270c69b6e4ad31fbcf5a7f00107ce1157f5e07`

The v0.1.0 client binding remains on Wayland 1.24.0 to preserve the extracted API. The current scanner
reproduces `client/client.go` byte-for-byte from the exact 1.24.0 source URL recorded in its generated
header. The pinned 1.26.0 XML adds requests, events, errors, and formats; adopting it requires an explicit
public API upgrade after v0.1.0.

## Divergences

| Commit | Invariant | Check | Upstream status |
|---|---|---|---|
| `c06156e` | One connection goroutine owns a plain proxy map and ID allocation. | `go test ./client -run 'TestObject\|TestProxyID'` | Local divergence |
| `ad90f18` | Frame reads tolerate fragmentation, preserve frame boundaries, and reject invalid headers. | `go test ./client -run TestReadFrame` | Local divergence |
| `2d9acb8` | Reads retain one descriptor across fragments; writes send rights once and complete short writes. | `go test ./client -run 'TestReadFrame.*FD\|TestWriteFrame'` | Local divergence |
| `47068e9` | `ControlFD` scopes descriptor access; dispatch stays on the owner goroutine and makes fatal errors sticky. | `go test ./client -run 'TestControlFD\|TestDispatchOwnership'` | Local divergence |
| `23e960c` | The scanner emits the sysc client import, requires explicit external xdg-shell imports, and preserves fatal dispatch behavior. | `go test ./cmd/sysc-wayland-scanner` | Local divergence |
