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

The extracted `client/client.go` remains based on Wayland 1.24.0. Generating from the pinned 1.26.0 XML
with `-prefix wl_` changes the public API and protocol surface, including compositor and data-device
manager release requests, per-commit surface release, pointer warp, and additional errors and formats.
Replacing the binding would also overwrite the hardened dispatch behavior added after extraction.

**Release blocker:** qualify generation from the original Wayland 1.24.0 XML or explicitly approve the
1.26.0 API upgrade and reapply the client dispatch invariants before publishing v0.1.0.

## Divergences

| Commit | Invariant | Check | Upstream status |
|---|---|---|---|
