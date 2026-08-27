# sysc-wayland Architecture Design

Date: 2026-08-27
Status: Approved except for the project licence choice

## Purpose

`sysc-wayland` provides the smallest Wayland client foundation required by `sysc-shell` and later sysc programs. It gives the project control over correctness fixes and release timing without adopting the unrelated packages in `dankgo`.

The first release extracts the Wayland client and protocol scanner from `dankgo` commit `10434658325c819efaf063f48eec4ae36555727e`. It preserves upstream notices and records every divergence.

## Repository identity

- Repository: `https://github.com/Nomadcxx/sysc-wayland`
- Module: `github.com/Nomadcxx/sysc-wayland`
- First consumer: `github.com/Nomadcxx/sysc-shell`
- Initial language level: Go 1.26
- Initial release: `v0.1.0`

The owner must approve a project licence before Task 1. BSD-3-Clause is the smallest fit because both
copied subtrees already use it; do not infer that choice from the upstream repository's root licence.

The project uses its own module path. Consumers import it directly rather than using a local replacement for `dankgo`.

## Scope

The foundation owns:

- connection discovery through `WAYLAND_DISPLAY` and `XDG_RUNTIME_DIR`;
- Wayland frame reads and request writes over a Unix stream socket;
- ancillary file-descriptor receipt and transfer;
- object ID allocation, proxy registration, dispatch, and deletion;
- core Wayland generated client bindings;
- display errors and roundtrips;
- safe access to the connection descriptor during polling;
- a protocol XML scanner that generates Go client bindings;
- pinned provenance and licence records.

The foundation does not own:

- layer-shell, fractional-scale, viewporter, or other extension policy;
- output hosts, surfaces, shared-memory pools, rendering, input actions, or frame scheduling;
- Niri IPC or shell state;
- D-Bus, HTTP, terminal, clipboard, application, or supervision helpers from `dankgo`;
- a Wayland server implementation;
- multi-goroutine proxy access.

Extension XML and generated bindings stay with the consumer until a second real consumer needs the same package.

## Planned layout

```text
client/                         wire client and generated core protocol
cmd/sysc-wayland-scanner/      protocol XML to Go generator
protocols/wayland.xml          pinned core protocol source
LICENSE                        project licence approved by the owner
LICENSES/                      upstream and scanner licences
UPSTREAM.md                    source commit, copied paths and divergence ledger
docs/plans/                    design and implementation plans
```

Do not add empty package directories or placeholder Go files.

## Concurrency contract

One goroutine owns a `client.Context` and every proxy created from it. The owner sends requests, dispatches events, creates and destroys proxies, and closes the connection.

The API does not claim that dispatch can move to another goroutine. Applications wake the owner through their own pipe, eventfd, or channel bridge. `sysc-shell` will poll its wake descriptor and the Wayland descriptor from the owner goroutine.

The context uses a plain object map because all access follows this ownership rule. A concurrent map copied from `dankgo` would preserve an unsupported access pattern and add hundreds of lines.

## Wire framing

A Wayland connection is `SOCK_STREAM`. One read may return part of a header, part of a body, or bytes from a later write. One write may accept only part of a request.

The reader:

1. reads exactly eight header bytes across as many socket reads as required;
2. collects ancillary data from every read;
3. rejects truncated ancillary data, validates object ID, message size, four-byte alignment, and the
   maximum 16-bit frame size;
4. reads exactly the declared body without consuming the next frame;
5. returns one frame or a descriptive error;
6. closes file descriptors that cannot be represented or delivered, including every error after receipt.

The ancillary buffer holds two descriptors so the reader can detect one descriptor beyond the supported
limit. Version one accepts at most one file descriptor per message because the extracted generated
dispatcher accepts one descriptor. A `ponytail:` comment records that ceiling and names multi-FD
generator support as the upgrade path. `MSG_CTRUNC` is fatal even when the kernel closes descriptors that
did not fit the buffer.

The writer sends ancillary rights once with the first accepted data byte, then writes any remaining bytes without repeating the rights. A partial request followed by an error makes the connection unusable and returns a fatal error.

## Object and error lifecycle

The context owns a map from object ID to proxy. The display constructor claims ID 1. Client allocation
stays below Wayland's server-ID range, skips live IDs, and fails on exhaustion. Registration of a
server-created object rejects zero, client-range IDs, and IDs already in use. Invalid generated
registration panics inside dispatch; the dispatch recovery converts that panic into a fatal connection
error.

Dispatch rejects unknown senders and unsupported opcodes. It may discard an event for a registered proxy
that the client marked as a zombie while it waits for `wl_display.delete_id`. That event remains
distinguishable from an event for an unknown object because the zombie stays in the map until
`delete_id` arrives. Dispatch closes a received descriptor on unknown-sender, zombie, unsupported-opcode,
and decoder-panic paths. A generated handler owns the descriptor only after dispatch reaches that handler
without an error.

`wl_display.error` becomes a sticky fatal error before an optional consumer handler runs. A consumer
cannot replace that internal recording path. Socket EOF, malformed frames, partial failed writes,
unknown opcodes, and decoder failures also terminate the connection.

The context exposes descriptor access through a callback executed inside `syscall.RawConn.Control`. It does not return a descriptor whose lifetime escapes that callback.

## Generated code

The repository pins `protocols/wayland.xml` and generates `client/client.go`. The scanner emits core
imports under `github.com/Nomadcxx/sysc-wayland/client` and does not mention `dankgo` in generated
output. A caller generating a protocol that references an external `xdg_*` interface supplies its package
with `-xdg-shell-import`. The scanner fails with a named error when such a reference has no import path.
This keeps xdg-shell and layer-shell bindings in `sysc-shell` without hard-coding the consumer module into
the generator.

The first scanner extraction may retain its focused build-time dependencies. Runtime packages may depend only on the Go standard library and `golang.org/x/sys`. A later scanner simplification needs measured maintenance value; it is not part of the first release.

Generation runs twice and compares output byte for byte. The Go language directive and scanner version form part of the reproducibility input.

## Upstream maintenance

`UPSTREAM.md` records:

- upstream repository and base commit;
- copied source paths;
- licences and notices;
- each local divergence and its regression check;
- the last reviewed upstream commit;
- whether a local patch was proposed or accepted upstream.

Upstream review happens before a sysc-wayland release, after a security or correctness fix, or when a consumer needs an upstream feature. There is no continuous merge job.

Keep deviations as focused commits. Do not mix transport fixes with formatting, renaming, or API expansion.

## Dependency ownership ladder

The sysc projects use four levels:

1. pin an upstream dependency;
2. maintain a patch fork after a verified gate requires it;
3. own a focused component after sustained divergence or a second consumer;
4. rewrite only when the required subset is cheaper to own than the fork.

`sysc-wayland` starts at level three by owner decision. Other dependencies do not inherit that status.

## Testing

Socket-pair checks cover:

- fragmented header and body reads;
- two coalesced frames;
- EOF during a header or body;
- invalid size and alignment;
- one received file descriptor;
- excess descriptor rejection without leaks;
- ancillary truncation rejection;
- short writes with and without a descriptor;
- object registration and deletion;
- unknown sender and opcode rejection;
- sticky protocol-error propagation.

Generator checks use a small pinned XML fixture and compare two generated files. Release checks run the race detector, vet, a clean build, and a live Niri roundtrip through a temporary probe outside the repository.

## Release gate

`v0.1.0` may be published when:

- the extracted core client and scanner retain required notices;
- fragmented reads and partial writes pass focused checks;
- no runtime dependency from unrelated `dankgo` packages remains;
- generated core and fixture output reproduce byte for byte;
- `go test -race ./...`, `go vet ./...`, and `go build ./...` pass;
- a temporary client connects to Niri, performs two roundtrips, receives output names, and exits cleanly;
- `sysc-shell` can generate its local xdg-shell binding plus its layer-shell, fractional-scale, and
  viewporter packages with the release candidate.

## Stop conditions

Stop and revise the design if the extracted scanner cannot generate the required protocols, ancillary descriptor handling needs a public multi-FD API for the shell proof, or the independent module requires copying unrelated `dankgo` packages.
