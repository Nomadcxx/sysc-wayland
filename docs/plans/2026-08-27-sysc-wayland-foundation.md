# sysc-wayland Foundation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Publish a focused pure-Go Wayland client and scanner that fixes fragmented stream I/O and can serve as `sysc-shell`'s pinned platform dependency.

**Architecture:** Extract only the `dankgo` Wayland client and scanner at commit `10434658325c819efaf063f48eec4ae36555727e`. Replace the copied concurrent object map with the single-owner context map, repair stream and descriptor I/O at the wire boundary, and generate consumer bindings under the new module path.

**Tech Stack:** Go 1.26, Unix domain sockets, `syscall.RawConn`, `golang.org/x/sys/unix`, Wayland XML, and the extracted Go protocol scanner.

---

## Working rules

- Execute the plan in a dedicated `foundation/v0.1.0` branch or worktree.
- Keep the upstream extraction commit separate from behavior fixes.
- Use `/tmp/sysc-wayland-upstream` for the pinned source checkout.
- Preserve all copied copyright and licence notices.
- Do not copy `dankgo` packages outside `wayland/client` and `cmd/go-wayland-scanner`.
- Do not add extension protocols, rendering, layer-shell behavior, Niri code, or a server package.
- Commit after every task. Stop after the release gate.

## Task 1: Establish module and provenance

**Files:**

- Create: `go.mod`
- Create: `LICENSE`
- Create: `LICENSES/dankgo-wayland.txt`
- Create: `LICENSES/go-wayland-scanner.txt`
- Create: `UPSTREAM.md`
- Modify: `README.md`

**Step 1: Check out the exact source**

```bash
git clone https://github.com/AvengeMedia/dankgo.git /tmp/sysc-wayland-upstream
git -C /tmp/sysc-wayland-upstream checkout 10434658325c819efaf063f48eec4ae36555727e
git -C /tmp/sysc-wayland-upstream rev-parse HEAD
```

Expected: the final command prints `10434658325c819efaf063f48eec4ae36555727e`.

**Step 2: Confirm the project licence files**

Keep the approved BSD-3-Clause project text in `LICENSE` with
`Copyright (c) 2026, Nomadcxx`. Keep both upstream licence copies under `LICENSES/` even when their terms
match the project licence.

**Step 3: Initialise the module**

```bash
go mod init github.com/Nomadcxx/sysc-wayland
go mod edit -go=1.26
go get golang.org/x/sys@v0.47.0
```

Expected: `go.mod` names only the new module and `x/sys`.

**Step 4: Record provenance**

Copy `wayland/LICENSE` to `LICENSES/dankgo-wayland.txt` and `cmd/go-wayland-scanner/LICENSE` to `LICENSES/go-wayland-scanner.txt`.

Write `UPSTREAM.md` with the repository, full base commit, copied paths, licence locations, and an empty divergence table with columns `Commit`, `Invariant`, `Check`, and `Upstream status`.

**Step 5: Verify and commit**

```bash
test "$(go list -m)" = "github.com/Nomadcxx/sysc-wayland"
git diff --check
git add README.md go.mod go.sum LICENSE LICENSES UPSTREAM.md
git commit -m "build: initialize sysc-wayland module"
```

Expected: the module check and whitespace check exit zero.

## Task 2: Extract the core client under single-owner semantics

**Files:**

- Create: `client/client.go`
- Create: `client/common.go`
- Create: `client/context.go`
- Create: `client/display.go`
- Create: `client/doc.go`
- Create: `client/event.go`
- Create: `client/request.go`
- Create: `client/util.go`
- Create: `client/context_test.go`

**Step 1: Copy the client sources**

Copy the eight Go files from `/tmp/sysc-wayland-upstream/wayland/client/` into `client/`. Do not copy `context_test.go`; its examples recommend cross-goroutine dispatch that this project rejects.

Change generated provenance and package documentation to name `sysc-wayland`. Do not change wire behavior in this step.

**Step 2: Write the failing object-store check**

Add package-local tests that construct a context with an empty object map and assert:

- the display constructor claims ID 1;
- client allocation stays below `0xff000000`, skips a live ID, and fails on exhaustion;
- server-created registration rejects zero, a client-range ID, and a used ID;
- deleting an ID removes its proxy;
- looking up an absent ID reports `ok == false`;
- allocation never returns zero or a live ID.

Run:

```bash
go test ./client -run 'TestObject|TestProxyID' -v
```

Expected: compilation fails because the copied context still imports `github.com/AvengeMedia/dankgo/syncmap`.

**Step 3: Replace the copied concurrent map**

Use `map[uint32]Proxy` owned by `Context`. Initialise it in the connection constructor and register the display at ID 1. Keep registration, lookup, and deletion methods on `Context`; do not add locks or a map wrapper.

Keep generated constructor calls unchanged. Client allocation returns the next valid ID or panics on the
unreachable exhaustion invariant. `RegisterWithID`, which generated dispatchers call for server-created
objects, panics on invalid or duplicate IDs; `Dispatch` will convert that panic into a fatal error in
Task 5. Add a `ponytail:` comment stating that one goroutine owns the map and that a mutex becomes
necessary only if the public ownership contract changes.

**Step 4: Run checks and commit**

```bash
go test ./client -v
go vet ./client
git add client
git commit -m "feat: extract single-owner Wayland client"
```

Expected: tests and vet pass.

## Task 3: Repair fragmented frame reads

**Files:**

- Modify: `client/event.go`
- Create: `client/event_test.go`

**Step 1: Write socket-pair regression checks**

Use `unix.Socketpair` and convert each descriptor to `*net.UnixConn` through `os.NewFile` and `net.FileConn`. Keep tests in package `client` so they can construct a context around the test connection.

Build one valid frame with an eight-byte header and a four-byte body. Test:

- one-byte header fragments;
- a complete header followed by one-byte body fragments;
- two frames written in one call and read separately;
- EOF after four header bytes;
- EOF before the declared body completes;
- size below eight;
- size not divisible by four.
- sender ID zero.

Run:

```bash
go test ./client -run 'TestReadFrame' -v
```

Expected: the fragmentation cases fail with `incorrect number of bytes read` from the extracted reader.

**Step 2: Implement exact frame reads**

Replace the two single-read assumptions with one unexported helper that repeatedly calls `ReadMsgUnix`
until the requested slice is full. Parse ancillary messages and inspect message flags after every call.
Treat zero bytes with no error as `io.ErrUnexpectedEOF`. Close all accumulated descriptors if a later
read, flag, or frame validation fails.

After the header is complete, validate:

```text
size >= 8
size <= 65535
size % 4 == 0
senderID != 0
```

Allocate only the validated body size. Cap each read to the remaining bytes so the next frame stays in the socket.

**Step 3: Run checks and commit**

```bash
go test ./client -run 'TestReadFrame' -v
go test -race ./client
git add client/event.go client/event_test.go
git commit -m "fix: handle fragmented Wayland frames"
```

Expected: all reader and race checks pass.

## Task 4: Preserve ancillary descriptors and partial writes

**Files:**

- Modify: `client/event.go`
- Modify: `client/request.go`
- Create: `client/fd_test.go`
- Create: `client/request_test.go`

**Step 1: Write failing descriptor checks**

Send a pipe read descriptor with a frame whose header and body arrive in separate writes. Assert that the reader returns the descriptor and that reading it receives a byte written through the paired pipe end.

Size the ancillary buffer with `unix.CmsgSpace(2 * 4)`. Send two descriptors with one frame. Version one
must close both, return an explicit unsupported-count error, and leak neither descriptor. Add a separate
fake-read check for `MSG_CTRUNC`; it must return a fatal truncation error.

Run:

```bash
go test ./client -run 'TestReadFrame.*FD' -v
```

Expected: the fragmented descriptor case fails under the extracted reader.

**Step 2: Implement descriptor accumulation**

Accumulate rights from every header and body read. Return `-1` when none arrived and the one descriptor
when exactly one arrived. Close all received descriptors before returning any error after receipt.

Add a `ponytail:` comment naming one descriptor per message as the version-one ceiling and generated multi-FD dispatch as the upgrade path.

**Step 3: Write failing partial-write checks**

Move request emission into an unexported helper that accepts two function values: one for the first `WriteMsgUnix` call and one for remaining plain writes. Use fakes to assert:

- a two-byte first write sends the descriptor once and writes the remaining bytes without ancillary data;
- repeated short plain writes finish the frame;
- zero bytes with no error fails instead of looping;
- an ancillary short write is fatal and does not retry the descriptor;
- an error after a partial write returns a fatal partial-frame error.

Run:

```bash
go test ./client -run 'TestWriteFrame' -v
```

Expected: compilation fails because the helper does not exist.

**Step 4: Implement complete writes**

Send the rights only with the first call. After at least one byte is accepted, write the remainder without
rights until complete. Validate the ancillary-byte count and reject a zero-progress write. Once the first
call accepts data or ancillary bytes, never resend the rights. Return a fatal connection error when the
frame cannot be completed.

**Step 5: Run checks and commit**

```bash
go test -race ./client
go vet ./client
git add client/event.go client/request.go client/fd_test.go client/request_test.go
git commit -m "fix: preserve Wayland descriptors and writes"
```

Expected: tests and vet pass.

## Task 5: Make descriptor polling and dispatch ownership explicit

**Files:**

- Modify: `client/context.go`
- Modify: `client/doc.go`
- Modify: `client/display.go`
- Modify: `client/client.go`
- Modify: `client/request.go`
- Modify: `client/context_test.go`

**Step 1: Write failing ownership and fatal-error checks**

Test this API:

```go
func (ctx *Context) ControlFD(fn func(fd int) error) error
```

The callback must receive a valid socket descriptor, and its error must be returned. A nil callback must
fail before accessing the socket. When both `RawConn.Control` and the callback fail, the result must
preserve both errors with `errors.Join`.

Also test that dispatch:

- rejects an unknown sender while discarding an event for a registered zombie;
- converts a decoder panic and an unsupported opcode into sticky fatal errors;
- records `wl_display.error` before calling an optional consumer handler;
- returns the same sticky error on later calls without reading another frame.
- closes a received descriptor on unknown-sender, zombie, unsupported-opcode, and decoder-panic paths.

Run:

```bash
go test ./client -run 'TestControlFD|TestDispatchOwnership' -v
```

Expected: compilation fails because `ControlFD` does not exist.

**Step 2: Implement bounded descriptor access**

Call `SyscallConn`, invoke the callback inside `RawConn.Control`, and return both control and callback
errors without exposing the descriptor after the callback returns. Remove the copied `Fd() int` method.

Refactor `Dispatch` to read and dispatch one frame directly. Remove `GetDispatch` and the copied claim
that reads can move to another goroutine. Update `Display.Roundtrip` to call `Dispatch`. Keep the first
fatal read, partial-write, protocol, or decode error on `Context`; return it before every later read or
write and after each generated dispatch. Close an undelivered received descriptor on every dispatch
error or discard path. Update package documentation with the one-owner contract.

**Step 3: Run checks and commit**

```bash
go test -race ./client
go vet ./client
git add client/client.go client/context.go client/context_test.go client/display.go client/doc.go client/request.go
git commit -m "fix: enforce Wayland connection ownership"
```

Expected: tests and vet pass.

## Task 6: Extract and qualify the protocol scanner

**Files:**

- Create: `cmd/sysc-wayland-scanner/LICENSE`
- Create: `cmd/sysc-wayland-scanner/main.go`
- Create: `cmd/sysc-wayland-scanner/main_test.go`
- Create: `protocols/wayland.xml`
- Modify: `go.mod`
- Modify: `go.sum`

**Step 1: Copy the scanner and pin core XML**

Copy `cmd/go-wayland-scanner/scanner.go` to `cmd/sysc-wayland-scanner/main.go`. Copy its licence beside it. Copy `/usr/share/wayland/wayland.xml` to `protocols/wayland.xml` and record the installed Wayland release and file checksum in `UPSTREAM.md`.

Change generated comments and imports from `github.com/AvengeMedia/dankgo/wayland/client` to
`github.com/Nomadcxx/sysc-wayland/client`. Replace the hard-coded upstream xdg-shell import with an
optional `-xdg-shell-import` flag. Require the flag only when generated types reference an external
`xdg_*` interface. Do not add a general import-mapping system or support for other protocol families.

**Step 2: Add only scanner dependencies**

Run `go mod tidy` after the copy. Inspect `go.mod` and confirm every new direct dependency is imported by the scanner. Runtime package `client` must still import only the standard library and `golang.org/x/sys/unix`.

**Step 3: Write the failing import-path check**

Generate a small test protocol into `t.TempDir()` by running the scanner command. Assert that output
imports `github.com/Nomadcxx/sysc-wayland/client`, contains no `AvengeMedia` string, and parses with
`go/parser`. Add a fixture that references external `xdg_popup`: generation must fail without
`-xdg-shell-import` and emit the supplied package path with it. Add an unknown-opcode case to the output
check so generated dispatchers preserve Task 5's fatal-error contract.

Run:

```bash
go test ./cmd/sysc-wayland-scanner -v
```

Expected: the copied scanner still emits the upstream import and the test fails.

**Step 4: Fix output and prove reproducibility**

Generate the same fixture twice and compare the byte slices. Generate `client/client.go` from `protocols/wayland.xml` only if the scanner's core mode reproduces the extracted API. If it cannot, keep the extracted generated core file and record that core generation gap as a release blocker rather than changing its public API silently.

Run:

```bash
go test ./cmd/sysc-wayland-scanner -v
go test ./...
go vet ./...
```

Expected: scanner output is byte-stable and all checks pass.

**Step 5: Commit**

```bash
git add cmd protocols client/client.go go.mod go.sum UPSTREAM.md
git commit -m "feat: add sysc Wayland protocol scanner"
```

## Task 7: Qualify the consumer path and release candidate

**Files:**

- Modify: `UPSTREAM.md`
- Modify: `README.md`

**Step 1: Run the full automated gate**

```bash
go mod tidy
git diff --exit-code -- go.mod go.sum
go test -race ./...
go vet ./...
go build ./...
```

Expected: every command exits zero.

**Step 2: Prove generator reproducibility**

Generate the `sysc-shell` xdg-shell package first. Generate layer-shell with
`-xdg-shell-import` pointing at that package, then generate fractional-scale and viewporter. Run the four
commands twice in a temporary module under `/tmp` and compare each pair with `cmp`.

Use the release command shape below; do not use a local scanner binary or `replace` directive:

```bash
go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.0 -pkg xdgshell -o xdg_shell.go -i protocols/xdg-shell.xml
go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.0 -pkg layershell -xdg-shell-import example.invalid/probe/xdgshell -o layer_shell.go -i protocols/wlr-layer-shell-unstable-v1.xml
go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.0 -pkg fractionalscale -o fractional_scale.go -i protocols/fractional-scale-v1.xml
go run github.com/Nomadcxx/sysc-wayland/cmd/sysc-wayland-scanner@v0.1.0 -pkg viewporter -o viewporter.go -i protocols/viewporter.xml
```

Expected: all four comparisons exit zero and generated imports use `github.com/Nomadcxx/sysc-wayland/client`.

**Step 3: Run a temporary live Niri probe**

Create the probe under `/tmp`, not this repository. It must connect, install a display-error handler, obtain the registry, perform two roundtrips, print `wl_output.name` values, and close without a protocol error.

Expected: the output includes the active connector names and the process exits zero.

**Step 4: Update the divergence ledger**

Record the fragmented-read fix, descriptor handling, complete writes, object-map change, descriptor callback, removed cross-goroutine dispatch, and scanner import change. Link each entry to its focused check.

**Step 5: Commit the release candidate**

```bash
git add README.md UPSTREAM.md
git commit -m "docs: record sysc-wayland qualification"
git status --short
```

Expected: the worktree is clean after the commit.

**Step 6: Publish only after owner review**

After the owner approves the diff and live evidence:

```bash
git tag -a v0.1.0 -m "sysc-wayland v0.1.0"
git push origin main
git push origin v0.1.0
```

Do not publish from an unattended implementation run.

## Stop condition

Stop after the approved `v0.1.0` tag exists remotely and `sysc-shell` resolves `github.com/Nomadcxx/sysc-wayland@v0.1.0`. Do not add extension bindings or begin `sysc-shell` product implementation in this worktree.
