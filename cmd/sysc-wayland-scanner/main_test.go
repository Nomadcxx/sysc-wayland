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

func TestScannerEmitsFDRequirements(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "fd.xml")
	output := filepath.Join(dir, "fd.go")
	writeFixture(t, input, fdProtocol)

	runScanner(t, "-pkg", "fixture", "-i", input, "-o", output)
	generated := readFile(t, output)
	for _, want := range [][]byte{
		[]byte("func (i *FixtureSource) HasFD(opcode uint32) bool"),
		[]byte("case 0:\n\t\treturn true"),
		[]byte("default:\n\t\treturn false"),
	} {
		if !bytes.Contains(generated, want) {
			t.Fatalf("generated FD requirements do not contain %q:\n%s", want, generated)
		}
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

func TestScannerFramesArrayRequests(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "array.xml")
	output := filepath.Join(dir, "array.go")
	writeFixture(t, input, arrayRequestProtocol)

	runScanner(t, "-pkg", "fixture", "-i", input, "-o", output)
	generated := readFile(t, output)
	for _, want := range [][]byte{
		[]byte("valuesLen := client.PaddedLen(len(values))"),
		[]byte("_reqBufLen := 8 + (4 + valuesLen)"),
		[]byte("client.PutArray(_reqBuf[l:l+(4+valuesLen)], values)"),
		[]byte("l += (4 + valuesLen)"),
	} {
		if !bytes.Contains(generated, want) {
			t.Fatalf("generated array request does not contain %q:\n%s", want, generated)
		}
	}
}

func TestScannerPadsEventArrayAdvancement(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "array-event.xml")
	output := filepath.Join(dir, "array-event.go")
	writeFixture(t, input, arrayEventProtocol)

	runScanner(t, "-pkg", "fixture", "-i", input, "-o", output)
	generated := readFile(t, output)
	for _, want := range [][]byte{
		[]byte("copy(e.Values, data[l:l+valuesLen])"),
		[]byte("valuesPaddedLen := client.PaddedLen(valuesLen)"),
		[]byte("l += valuesPaddedLen"),
		[]byte("e.Count = client.Uint32(data[l : l+4])"),
	} {
		if !bytes.Contains(generated, want) {
			t.Fatalf("generated array event decoder does not contain %q:\n%s", want, generated)
		}
	}
	if _, err := parser.ParseFile(token.NewFileSet(), output, generated, parser.AllErrors); err != nil {
		t.Fatalf("parse generated output: %v", err)
	}
}

func TestScannerBoundsVariableEventFields(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "variable-event.xml")
	output := filepath.Join(dir, "variable-event.go")
	writeFixture(t, input, arrayEventProtocol)

	runScanner(t, "-pkg", "fixture", "-i", input, "-o", output)
	generated := readFile(t, output)
	for _, want := range [][]byte{
		[]byte("if len(data)-l < 4"),
		[]byte("panic(\"client: truncated event string length\")"),
		[]byte("if uint64(messageWireLen) > uint64(len(data)-l)"),
		[]byte("panic(\"client: truncated event string\")"),
		[]byte("e.Message = client.String(data[l : l+messageLen])"),
		[]byte("l += messagePaddedLen"),
		[]byte("if uint64(valuesWireLen) > uint64(len(data)-l)"),
		[]byte("panic(\"client: truncated event array\")"),
		[]byte("copy(e.Values, data[l:l+valuesLen])"),
		[]byte("l += valuesPaddedLen"),
	} {
		if !bytes.Contains(generated, want) {
			t.Fatalf("generated variable event decoder does not contain %q:\n%s", want, generated)
		}
	}
	if _, err := parser.ParseFile(token.NewFileSet(), output, generated, parser.AllErrors); err != nil {
		t.Fatalf("parse generated output: %v", err)
	}
}

func TestScannerDecodesAllowNullEventString(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "nullable.xml")
	output := filepath.Join(dir, "nullable.go")
	writeFixture(t, input, allowNullStringProtocol)

	runScanner(t, "-pkg", "fixture", "-i", input, "-o", output)
	generated := string(readFile(t, output))

	if !strings.Contains(generated, "MimeType *string") {
		t.Fatalf("allow-null event string is not a pointer:\n%s", generated)
	}
	if !strings.Contains(generated, "Name string") {
		t.Fatalf("non-null event string changed type:\n%s", generated)
	}

	target, ok := eventBody(generated, "case 0:")
	if !ok {
		t.Fatal("generated dispatcher has no opcode 0 branch")
	}
	for _, want := range []string{
		"if mimeTypeLen == 0 {",
		"e.MimeType = nil",
		"mimeType := client.String(data[l : l+mimeTypeLen])",
		"e.MimeType = &mimeType",
	} {
		if !strings.Contains(target, want) {
			t.Fatalf("NULL string decoder missing %q:\n%s", want, target)
		}
	}

	send, ok := eventBody(generated, "case 1:")
	if !ok {
		t.Fatal("generated dispatcher has no opcode 1 branch")
	}
	if strings.Contains(send, "e.Name = nil") || strings.Contains(send, "&name") {
		t.Fatalf("non-null string accepts a length of 0:\n%s", send)
	}
	if !strings.Contains(send, "e.Name = client.String(data[l : l+nameLen])") {
		t.Fatalf("non-null string decoder missing String():\n%s", send)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), output, generated, parser.AllErrors); err != nil {
		t.Fatalf("parse generated output: %v", err)
	}
}

func TestScannerEncodesAllowNullRequestString(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "nullable_request.xml")
	output := filepath.Join(dir, "nullable_request.go")
	writeFixture(t, input, allowNullRequestStringProtocol)

	runScanner(t, "-pkg", "fixture", "-i", input, "-o", output)
	generated := string(readFile(t, output))

	accept, ok := funcBody(generated, "func (i *FixtureOffer) Accept(")
	if !ok {
		t.Fatal("generated output has no Accept method")
	}
	if !strings.Contains(accept, "mimeType *string") {
		t.Fatalf("allow-null request string is not a pointer:\n%s", accept)
	}
	// Nothing in the generated code checks that the body walk l agrees with
	// _reqBufLen, so assert both sides: the size counts the length field, and the
	// null branch advances l by the 4 bytes it wrote so the trailing arg still
	// lands where _reqBufLen says the frame ends.
	for _, want := range []string{
		"mimeTypeLen := 0",
		"if mimeType != nil {",
		"mimeTypeLen = client.PaddedLen(len(*mimeType) + 1)",
		"_reqBufLen := 8 + 4 + (4 + mimeTypeLen) + 4",
		"if mimeType == nil {\n\t\tclient.PutUint32(_reqBuf[l:l+4], 0)\n\t\tl += 4\n\t} else {",
		"client.PutString(_reqBuf[l:l+(4+mimeTypeLen)], *mimeType)",
		"client.PutUint32(_reqBuf[l:l+4], uint32(flags))",
	} {
		if !strings.Contains(accept, want) {
			t.Fatalf("NULL request string encoder missing %q:\n%s", want, accept)
		}
	}

	receive, ok := funcBody(generated, "func (i *FixtureOffer) Receive(")
	if !ok {
		t.Fatal("generated output has no Receive method")
	}
	if !strings.Contains(receive, "mimeType string") || strings.Contains(receive, "mimeType *string") {
		t.Fatalf("non-null request string changed type:\n%s", receive)
	}
	if strings.Contains(receive, "mimeType == nil") {
		t.Fatalf("non-null request string accepts NULL:\n%s", receive)
	}
	if !strings.Contains(receive, "client.PutString(_reqBuf[l:l+(4+mimeTypeLen)], mimeType)") {
		t.Fatalf("non-null request string encoder missing PutString:\n%s", receive)
	}

	if _, err := parser.ParseFile(token.NewFileSet(), output, generated, parser.AllErrors); err != nil {
		t.Fatalf("parse generated output: %v", err)
	}
}

func TestScannerMarksDestroyedRegistryZombie(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "fixes.xml")
	output := filepath.Join(dir, "fixes.go")
	writeFixture(t, input, destroyRegistryProtocol)

	runScanner(t, "-pkg", "client", "-prefix", "wl_", "-i", input, "-o", output)
	generated := readFile(t, output)
	want := []byte("func (i *Fixes) DestroyRegistry(registry *Registry) error {\n\tdefer registry.MarkZombie()")
	if !bytes.Contains(generated, want) {
		t.Fatalf("generated DestroyRegistry does not mark its target zombie:\n%s", generated)
	}
}

func TestScannerReproducesVendoredCoreBinding(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(repoRoot, "protocols", "wayland.xml")
	output := filepath.Join(t.TempDir(), "client.go")
	runScanner(t, "-pkg", "client", "-prefix", "wl", "-i", input, "-o", output)

	got := normalizeXMLSource(readFile(t, output))
	want := normalizeXMLSource(readFile(t, filepath.Join(repoRoot, "client", "client.go")))
	if !bytes.Equal(got, want) {
		t.Fatal("vendored core XML does not reproduce client/client.go")
	}
}

func normalizeXMLSource(data []byte) []byte {
	lines := bytes.Split(data, []byte("\n"))
	for i, line := range lines {
		if bytes.HasPrefix(line, []byte("// XML file : ")) {
			lines[i] = []byte("// XML file : <source>")
			break
		}
	}
	return bytes.Join(lines, []byte("\n"))
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

const fdProtocol = `<?xml version="1.0" encoding="UTF-8"?>
<protocol name="fixture">
  <copyright>Fixture copyright.</copyright>
  <interface name="fixture_source" version="1">
    <event name="send">
      <arg name="fd" type="fd"/>
    </event>
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

const arrayRequestProtocol = `<?xml version="1.0" encoding="UTF-8"?>
<protocol name="fixture">
  <copyright>Fixture copyright.</copyright>
  <interface name="array_widget" version="1">
    <request name="set_values">
      <arg name="values" type="array"/>
    </request>
  </interface>
</protocol>
`

const arrayEventProtocol = `<?xml version="1.0" encoding="UTF-8"?>
<protocol name="fixture">
  <copyright>Fixture copyright.</copyright>
  <interface name="array_widget" version="1">
    <event name="values">
      <arg name="message" type="string"/>
      <arg name="values" type="array"/>
      <arg name="count" type="uint"/>
    </event>
  </interface>
</protocol>
`

const allowNullStringProtocol = `<?xml version="1.0" encoding="UTF-8"?>
<protocol name="fixture">
  <copyright>Fixture copyright.</copyright>
  <interface name="fixture_source" version="1">
    <event name="target">
      <arg name="mime_type" type="string" allow-null="true"/>
    </event>
    <event name="named">
      <arg name="name" type="string"/>
    </event>
  </interface>
</protocol>
`

const allowNullRequestStringProtocol = `<?xml version="1.0" encoding="UTF-8"?>
<protocol name="fixture">
  <copyright>Fixture copyright.</copyright>
  <interface name="fixture_offer" version="3">
    <request name="accept">
      <arg name="serial" type="uint"/>
      <arg name="mime_type" type="string" allow-null="true"/>
      <arg name="flags" type="uint"/>
    </request>
    <request name="receive">
      <arg name="mime_type" type="string"/>
    </request>
  </interface>
</protocol>
`

const destroyRegistryProtocol = `<?xml version="1.0" encoding="UTF-8"?>
<protocol name="wayland">
  <copyright>Fixture copyright.</copyright>
  <interface name="wl_registry" version="1">
  </interface>
  <interface name="wl_fixes" version="1">
    <request name="destroy" type="destructor"/>
    <request name="destroy_registry">
      <arg name="registry" type="object" interface="wl_registry"/>
    </request>
  </interface>
</protocol>
`

// An object argument in an event references an object that already exists.
// Binding a fresh proxy to its id is wrong twice over: the id belongs to
// whoever created the object, and when the client created it the id is in the
// client range, which RegisterWithID rejects with a panic. A wl_pointer.leave
// naming a surface this client has just destroyed then takes the whole
// connection down rather than being ignored.
//
// A new_id argument is the server creating an object and must still register.
func TestScannerDoesNotRegisterObjectArguments(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "objects.xml")
	out := filepath.Join(dir, "objects.go")
	writeFixture(t, input, objectArgProtocol)

	runScanner(t, "-pkg", "fixture", "-i", input, "-o", out)
	data := readFile(t, out)

	leave, ok := eventBody(string(data), "case 1:")
	if !ok {
		t.Fatal("generated dispatcher has no opcode 1 branch")
	}
	if strings.Contains(leave, "RegisterWithID") {
		t.Fatalf("an object argument still registers a proxy:\n%s", leave)
	}
	if !strings.Contains(leave, "e.Surface = nil") {
		t.Fatalf("an unresolved object argument does not clear its field:\n%s", leave)
	}

	created, ok := eventBody(string(data), "case 0:")
	if !ok {
		t.Fatal("generated dispatcher has no opcode 0 branch")
	}
	if !strings.Contains(created, "RegisterWithID") {
		t.Fatalf("a new_id argument no longer registers its proxy:\n%s", created)
	}

	if _, err := parser.ParseFile(token.NewFileSet(), out, data, parser.AllErrors); err != nil {
		t.Fatalf("parse generated output: %v", err)
	}
}

// A nil handler must not skip registration. The server follows a new_id
// event with events on that id, and an unknown sender fatals the connection.
func TestScannerRegistersNewIDBeforeNilHandler(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "objects.xml")
	out := filepath.Join(dir, "objects.go")
	writeFixture(t, input, objectArgProtocol)

	runScanner(t, "-pkg", "fixture", "-i", input, "-o", out)
	created, ok := eventBody(string(readFile(t, out)), "case 0:")
	if !ok {
		t.Fatal("generated dispatcher has no opcode 0 branch")
	}
	register := strings.Index(created, "RegisterWithID")
	nilCheck := strings.Index(created, "Handler == nil")
	call := strings.Index(created, "Handler(e)")
	if register < 0 || nilCheck < 0 || call < 0 || register > nilCheck || nilCheck > call {
		t.Fatalf("new_id registration is not before the nil-handler return:\n%s", created)
	}
}

// eventBody returns the generated source between one opcode branch and the
// next, so a test can assert on one event without matching the whole file.
func eventBody(source, branch string) (string, bool) {
	start := strings.Index(source, branch)
	if start < 0 {
		return "", false
	}
	rest := source[start+len(branch):]
	if end := strings.Index(rest, "\tcase "); end >= 0 {
		return rest[:end], true
	}
	return rest, true
}

// funcBody returns the generated source of one function, so a test can assert
// on one request without matching the whole file.
func funcBody(source, signature string) (string, bool) {
	start := strings.Index(source, signature)
	if start < 0 {
		return "", false
	}
	rest := source[start:]
	if end := strings.Index(rest[1:], "\nfunc "); end >= 0 {
		return rest[:end+1], true
	}
	return rest, true
}

const objectArgProtocol = `<?xml version="1.0" encoding="UTF-8"?>
<protocol name="fixture">
  <copyright>Fixture copyright.</copyright>
  <interface name="fixture_offer" version="1">
    <request name="destroy" type="destructor"/>
  </interface>
  <interface name="fixture_surface" version="1">
    <request name="destroy" type="destructor"/>
  </interface>
  <interface name="fixture_seat" version="1">
    <event name="offered">
      <arg name="id" type="new_id" interface="fixture_offer"/>
    </event>
    <event name="left">
      <arg name="surface" type="object" interface="fixture_surface"/>
    </event>
  </interface>
</protocol>
`
