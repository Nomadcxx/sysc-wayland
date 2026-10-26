module github.com/Nomadcxx/sysc-wayland

// The module calls x509.Certificate.Verify, so it needs a standard library
// without GO-2026-6809 (fixed in go1.26.10). CI reads this directive for the
// toolchain it installs, so the floor here is the floor everywhere.
go 1.26.10

require (
	github.com/iancoleman/strcase v0.3.0
	golang.org/x/crypto v0.55.0
	golang.org/x/sys v0.47.0
	golang.org/x/tools v0.49.0
	mvdan.cc/gofumpt v0.11.0
)

require (
	golang.org/x/mod v0.39.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
)
