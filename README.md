# sysc-wayland

`sysc-wayland` is the pure-Go Wayland foundation for the sysc projects. It owns wire framing, file-descriptor transfer, proxy lifecycle, core generated bindings, and protocol generation.

The project targets Linux. It does not carry cross-platform transport abstractions or compositor policy.

The project starts as a focused extraction from [`dankgo`](https://github.com/AvengeMedia/dankgo) at
commit `10434658325c819efaf063f48eec4ae36555727e`. It does not import the rest of `dankgo`. [UPSTREAM.md](UPSTREAM.md)
records copied paths, licences, and local divergences.

The foundation candidate provides the `client` package and the `sysc-wayland-scanner` command. The
approved design and implementation plan are:

- [Architecture design](docs/plans/2026-08-27-sysc-wayland-design.md)
- [Foundation implementation plan](docs/plans/2026-08-27-sysc-wayland-foundation.md)

The first consumer will be [`sysc-shell`](https://github.com/Nomadcxx/sysc-shell).

## Candidate qualification

Run the repository gate from the module root:

```bash
go mod tidy
git diff --exit-code -- go.mod go.sum
go test -race ./...
go vet ./...
go build ./...
```

Test the published candidate from a temporary module containing the protocol XML files:

```bash
go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.0-rc.1 -pkg xdgshell -o xdg_shell.go -i protocols/xdg-shell.xml
go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.0-rc.1 -pkg layershell -xdg-shell-import example.invalid/probe/xdgshell -o layer_shell.go -i protocols/wlr-layer-shell-unstable-v1.xml
go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.0-rc.1 -pkg fractionalscale -o fractional_scale.go -i protocols/fractional-scale-v1.xml
go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.0-rc.1 -pkg viewporter -o viewporter.go -i protocols/viewporter.xml
```

## Licence

`sysc-wayland` uses the [BSD 3-Clause License](LICENSE). Extracted upstream code retains its copyright
notices and subtree licences.
