# sysc-wayland

`sysc-wayland` is the pure-Go Wayland foundation for the sysc projects. It owns wire framing, file-descriptor transfer, proxy lifecycle, core generated bindings, and protocol generation.

The project starts as a focused extraction from [`dankgo`](https://github.com/AvengeMedia/dankgo) at commit `10434658325c819efaf063f48eec4ae36555727e`. It does not import the rest of `dankgo`.

No production package exists yet. The approved design and implementation plan are:

- [Architecture design](docs/plans/2026-08-27-sysc-wayland-design.md)
- [Foundation implementation plan](docs/plans/2026-08-27-sysc-wayland-foundation.md)

The first consumer will be [`sysc-shell`](https://github.com/Nomadcxx/sysc-shell).
