# sysc-wayland agent guide

## Scope

- Use Go. Do not add C, C++, Rust, CGO, Qt, QML, Lua, or Luau.
- Own Wayland transport, core bindings, proxy lifecycle, and protocol generation.
- Do not add layer-shell policy, surfaces, rendering, widgets, Niri integration, or shell state.
- Preserve upstream copyright and licence notices.

## Engineering rules

- One goroutine owns a connection and every proxy created from it.
- Treat the Unix socket as a byte stream. Reads and writes may be short.
- Preserve ancillary file descriptors across fragmented reads and partial writes. Close rejected descriptors.
- Validate frame size, alignment, object identity, opcode data, and integer conversions before use.
- Prefer the standard library. Add a dependency only when it replaces more code than it introduces.
- Keep generated code reproducible from pinned protocol XML.
- Add one focused runnable check for each non-trivial wire or lifecycle invariant.
- Mark a deliberate ceiling with a `ponytail:` comment naming the limit and upgrade path.

## Workflow

1. Read the approved design and current task in the foundation plan.
2. Work in a dedicated branch or worktree.
3. Write the failing check before changing behavior.
4. Keep upstream extraction, behavior changes, and generated output in separate commits.
5. Run `go test -race ./...`, `go vet ./...`, and generator reproducibility checks before a release.
6. Stop at the foundation plan's release gate.
