package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScannerUsesSyscClientAndIsReproducible(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "fixture.xml")
	first := filepath.Join(dir, "first.go")
	second := filepath.Join(dir, "second.go")
	writeFixture(t, input, simpleProtocol)

	runScanner(t, "-pkg", "fixture", "-i", input, "-o", first)
	runScanner(t, "-pkg", "fixture", "-i", input, "-o", second)
	firstData := readFile(t, first)
	secondData := readFile(t, second)

	if !bytes.Equal(firstData, secondData) {
		t.Fatal("scanner output differs between identical runs")
	}
	if !bytes.Contains(firstData, []byte(`"github.com/Nomadcxx/sysc-wayland/client"`)) {
		t.Fatal("generated output does not import sysc-wayland/client")
	}
	if bytes.Contains(firstData, []byte("AvengeMedia")) {
		t.Fatal("generated output retains upstream module path")
	}
	if !bytes.Contains(firstData, []byte(`panic("client: unsupported opcode")`)) {
		t.Fatal("generated dispatcher does not reject unknown opcodes")
	}
	if _, err := parser.ParseFile(token.NewFileSet(), first, firstData, parser.AllErrors); err != nil {
		t.Fatalf("parse generated output: %v", err)
	}
}

func TestScannerRequiresExplicitXDGImport(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "xdg.xml")
	output := filepath.Join(dir, "xdg.go")
	writeFixture(t, input, externalXDGProtocol)

	cmd := scannerCommand("-pkg", "fixture", "-i", input, "-o", output)
	combined, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(combined), "xdg-shell-import") {
		t.Fatalf("scanner without xdg import = (%v, %q), want named failure", err, combined)
	}

	runScanner(t,
		"-pkg", "fixture",
		"-xdg-shell-import", "example.invalid/probe/xdgshell",
		"-i", input,
		"-o", output,
	)
	generated := readFile(t, output)
	if !bytes.Contains(generated, []byte(`xdg_shell "example.invalid/probe/xdgshell"`)) {
		t.Fatalf("generated output does not contain supplied xdg-shell import:\n%s", generated)
	}
	if bytes.Contains(generated, []byte("AvengeMedia")) {
		t.Fatal("generated xdg output retains upstream module path")
	}
}

func TestScannerRecordsDisplayErrorBeforeHandler(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "wayland.xml")
	output := filepath.Join(dir, "wayland.go")
	writeFixture(t, input, displayErrorProtocol)

	runScanner(t, "-pkg", "client", "-prefix", "wl_", "-i", input, "-o", output)
	generated := readFile(t, output)
	record := bytes.Index(generated, []byte("i.Context().recordDisplayError(e)"))
	handle := bytes.Index(generated, []byte("i.errorHandler(e)"))
	if record == -1 || handle == -1 || record > handle {
		t.Fatalf("generated display error ordering is unsafe:\n%s", generated)
	}
}

func runScanner(t *testing.T, args ...string) {
	t.Helper()
	cmd := scannerCommand(args...)
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("scanner failed: %v\n%s", err, combined)
	}
}

func scannerCommand(args ...string) *exec.Cmd {
	cmd := exec.Command("go", append([]string{"run", "."}, args...)...)
	cmd.Dir = "."
	return cmd
}

func writeFixture(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

const simpleProtocol = `<?xml version="1.0" encoding="UTF-8"?>
<protocol name="fixture">
  <copyright>Fixture copyright.</copyright>
  <interface name="fixture_widget" version="1">
    <request name="destroy" type="destructor"/>
    <event name="done">
      <arg name="serial" type="uint"/>
    </event>
  </interface>
</protocol>
`

const externalXDGProtocol = `<?xml version="1.0" encoding="UTF-8"?>
<protocol name="fixture">
  <copyright>Fixture copyright.</copyright>
  <interface name="fixture_widget" version="1">
    <request name="attach_popup">
      <arg name="popup" type="object" interface="xdg_popup"/>
    </request>
  </interface>
</protocol>
`

const displayErrorProtocol = `<?xml version="1.0" encoding="UTF-8"?>
<protocol name="wayland">
  <copyright>Fixture copyright.</copyright>
  <interface name="wl_display" version="1">
    <event name="error">
      <arg name="object_id" type="object"/>
      <arg name="code" type="uint"/>
      <arg name="message" type="string"/>
    </event>
  </interface>
</protocol>
`
