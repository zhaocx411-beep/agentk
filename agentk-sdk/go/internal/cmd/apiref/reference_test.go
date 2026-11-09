package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

const modulePath = "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go"

func TestMirrorPackage(t *testing.T) {
	root := moduleRoot(t)
	documented := loadDocumented(t, root, "./internal/cmd/apiref/testdata/fixture")
	objectDocs := indexObjectDocs(documented)

	got, err := mirrorPackage(documented[0].loaded, documented[0].doc, objectDocs)
	if err != nil {
		t.Fatalf("mirrorPackage() error = %v", err)
	}
	if got.ImportPath != modulePath+"/internal/cmd/apiref/testdata/fixture" {
		t.Fatalf("ImportPath = %q", got.ImportPath)
	}
	if got.Doc != "Package fixture exercises the public Go documentation model.\n" {
		t.Fatalf("Doc = %q", got.Doc)
	}

	constGroup := findValue(t, got.Consts, "First")
	if !slices.Equal(constGroup.Names, []string{"First", "Second"}) {
		t.Fatalf("constant names = %q", constGroup.Names)
	}
	if strings.Contains(constGroup.Declaration.Text, "hidden") {
		t.Fatalf("constant declaration contains unexported name:\n%s", constGroup.Declaration.Text)
	}

	variableGroup := findValue(t, got.Vars, "Exported")
	if strings.Contains(variableGroup.Declaration.Text, "hiddenV") {
		t.Fatalf("variable declaration contains unexported name:\n%s", variableGroup.Declaration.Text)
	}

	pair := findType(t, got.Types, "Pair")
	for _, required := range []string{"type Pair[T any] struct", "Left T", "Right T"} {
		if !strings.Contains(pair.Declaration.Text, required) {
			t.Fatalf("Pair declaration missing %q:\n%s", required, pair.Declaration.Text)
		}
	}
	if strings.Contains(pair.Declaration.Text, "hidden") {
		t.Fatalf("Pair declaration contains unexported field:\n%s", pair.Declaration.Text)
	}
	constructor := findFunc(t, pair.Funcs, "NewPair")
	if constructor.Declaration.Text != "func NewPair[T any](left, right T) *Pair[T]" {
		t.Fatalf("NewPair declaration = %q", constructor.Declaration.Text)
	}
	if len(constructor.Signature.TypeParameters) != 1 ||
		constructor.Signature.TypeParameters[0].Name != "T" ||
		constructor.Signature.TypeParameters[0].Constraint.Text != "any" {
		t.Fatalf("NewPair type parameters = %#v", constructor.Signature.TypeParameters)
	}
	if len(constructor.Signature.Parameters) != 1 ||
		!slices.Equal(constructor.Signature.Parameters[0].Names, []string{"left", "right"}) ||
		constructor.Signature.Parameters[0].Type.Text != "T" {
		t.Fatalf("NewPair parameters = %#v", constructor.Signature.Parameters)
	}
	valueMethod := findFunc(t, pair.Methods, "Value")
	if strings.Contains(valueMethod.Declaration.Text, "return") || strings.Contains(valueMethod.Declaration.Text, "Value returns") {
		t.Fatalf("Value declaration contains body or doc:\n%s", valueMethod.Declaration.Text)
	}
	if slices.ContainsFunc(pair.Methods, func(fn FuncDoc) bool { return fn.Name == "hiddenValue" }) {
		t.Fatal("Pair methods contain hiddenValue")
	}

	contract := findType(t, got.Types, "Contract")
	if !strings.Contains(contract.Declaration.Text, "Apply(string) error") || strings.Contains(contract.Declaration.Text, "hiddenApply") {
		t.Fatalf("Contract declaration = %q", contract.Declaration.Text)
	}
	grouped := findType(t, got.Types, "Grouped")
	if grouped.Doc != "Grouped is declared inside a grouped type declaration.\n" {
		t.Fatalf("Grouped doc = %q", grouped.Doc)
	}
	if strings.Contains(grouped.Declaration.Text, "Grouped is declared") {
		t.Fatalf("Grouped declaration contains top-level doc:\n%s", grouped.Declaration.Text)
	}
	standalone := findFunc(t, got.Funcs, "Standalone")
	if got := parameterNames(standalone.Signature.Parameters); !slices.Equal(got, []string{"ctx", "input", "pairs"}) {
		t.Fatalf("Standalone parameter names = %q", got)
	}
	if !standalone.Signature.Parameters[2].Variadic {
		t.Fatal("Standalone variadic parameter is not marked variadic")
	}
	assertReference(t, standalone.Declaration, "context", "context", "")
	assertReference(t, standalone.Declaration, "Context", "context", "Context")
	assertReference(t, standalone.Declaration, "Pair", got.ImportPath, "Pair")
	assertBuiltinReference(t, standalone.Declaration, "string")
	assertBuiltinReference(t, standalone.Declaration, "error")
}

func TestAliasMethods(t *testing.T) {
	root := moduleRoot(t)
	documented := loadDocumented(
		t,
		root,
		"./internal/cmd/apiref/testdata/alias",
		"./internal/cmd/apiref/testdata/internal/target",
	)
	objectDocs := indexObjectDocs(documented)

	aliasPackage := findDocumentedPackage(t, documented, modulePath+"/internal/cmd/apiref/testdata/alias")
	got, err := mirrorPackage(aliasPackage.loaded, aliasPackage.doc, objectDocs)
	if err != nil {
		t.Fatalf("mirrorPackage() error = %v", err)
	}
	alias := findType(t, got.Types, "Subject")

	value := findFunc(t, alias.Methods, "Value")
	if value.Declaration.Text != "func (Subject) Value() string" {
		t.Fatalf("Value declaration = %q", value.Declaration.Text)
	}
	if value.Doc != "Value is a value-receiver method.\n" {
		t.Fatalf("Value doc = %q", value.Doc)
	}
	pointer := findFunc(t, alias.Methods, "Pointer")
	if pointer.Declaration.Text != "func (*Subject) Pointer() string" {
		t.Fatalf("Pointer declaration = %q", pointer.Declaration.Text)
	}
	if pointer.Doc != "Pointer is a pointer-receiver method.\n" {
		t.Fatalf("Pointer doc = %q", pointer.Doc)
	}
	if pointer.Signature.Receiver == nil || pointer.Signature.Receiver.Type.Text != "*Subject" {
		t.Fatalf("Pointer receiver = %#v", pointer.Signature.Receiver)
	}
	assertReference(t, pointer.Declaration, "Subject", aliasPackage.loaded.PkgPath, "Subject")
	assertBuiltinReference(t, pointer.Declaration, "string")
	if slices.ContainsFunc(alias.Methods, func(fn FuncDoc) bool { return fn.Name == "hidden" }) {
		t.Fatal("alias methods contain hidden")
	}
}

func TestCurrentModuleReference(t *testing.T) {
	root := moduleRoot(t)
	first, err := loadReference(root, "./...")
	if err != nil {
		t.Fatalf("loadReference() error = %v", err)
	}
	second, err := loadReference(root, "./...")
	if err != nil {
		t.Fatalf("second loadReference() error = %v", err)
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("json.Marshal(first) error = %v", err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("json.Marshal(second) error = %v", err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatal("loadReference() output is not deterministic")
	}

	if first.ModulePath != modulePath {
		t.Fatalf("ModulePath = %q", first.ModulePath)
	}
	for _, pkg := range first.Packages {
		if containsInternalSegment(pkg.ImportPath) {
			t.Fatalf("output contains internal package %q", pkg.ImportPath)
		}
		if strings.HasSuffix(pkg.ImportPath, "/protocoltest") {
			t.Fatalf("output contains test-only package %q", pkg.ImportPath)
		}
		if strings.HasPrefix(pkg.ImportPath, modulePath+"/gen/") {
			t.Fatalf("output contains generated module package %q", pkg.ImportPath)
		}
		assertNonNilSlices(t, pkg)
	}

	client := findPackage(t, first.Packages, modulePath+"/client")
	clientType := findType(t, client.Types, "Client")
	findFunc(t, clientType.Methods, "CreateSession")
	findFunc(t, clientType.Methods, "WatchEvents")
	streamType := findType(t, client.Types, "EventStream")
	for _, method := range []string{"Receive", "Msg", "Err", "Close"} {
		findFunc(t, streamType.Methods, method)
	}

	findPackage(t, first.Packages, modulePath+"/temporal")
	findPackage(t, first.Packages, modulePath+"/worker/discovery")
}

func TestGeneratedModuleReference(t *testing.T) {
	reference, err := loadReference(filepath.Join(moduleRoot(t), "gen"), "./...")
	if err != nil {
		t.Fatalf("loadReference() error = %v", err)
	}
	if reference.ModulePath != modulePath+"/gen" {
		t.Fatalf("ModulePath = %q", reference.ModulePath)
	}
	api := findPackage(t, reference.Packages, modulePath+"/gen/api/v1")
	request := findType(t, api.Types, "CreateSessionRequest")
	for _, private := range []string{"state", "sizeCache", "unknownFields"} {
		if strings.Contains(request.Declaration.Text, private) {
			t.Fatalf("protobuf declaration contains private field %q:\n%s", private, request.Declaration.Text)
		}
	}
	for _, public := range []string{"Title", "Profile"} {
		if !strings.Contains(request.Declaration.Text, public) {
			t.Fatalf("protobuf declaration missing public field %q:\n%s", public, request.Declaration.Text)
		}
	}
}

func TestRunWritesCompactJSON(t *testing.T) {
	t.Chdir(moduleRoot(t))
	var stdout, stderr bytes.Buffer
	if err := run(&stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v; stderr = %q", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if bytes.Count(stdout.Bytes(), []byte{'\n'}) != 1 || stdout.Bytes()[stdout.Len()-1] != '\n' {
		t.Fatalf("stdout is not one compact JSON line")
	}
	var decoded Reference
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(stdout) error = %v", err)
	}
	if decoded.ModulePath != modulePath {
		t.Fatalf("ModulePath = %q", decoded.ModulePath)
	}
}

func TestRunFailureWritesNoJSON(t *testing.T) {
	t.Chdir(t.TempDir())
	var stdout, stderr bytes.Buffer
	if err := run(&stdout, &stderr); err == nil {
		t.Fatal("run() error = nil")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Fatal("stderr is empty")
	}
}

func loadDocumented(t *testing.T, dir string, patterns ...string) []documentedPackage {
	t.Helper()
	loaded, err := packages.Load(
		&packages.Config{Dir: dir, Mode: packages.LoadSyntax | packages.NeedModule},
		patterns...,
	)
	if err != nil {
		t.Fatalf("packages.Load() error = %v", err)
	}
	if err := loadedPackagesError(loaded); err != nil {
		t.Fatal(err)
	}
	documented, err := documentPackages(loaded)
	if err != nil {
		t.Fatalf("documentPackages() error = %v", err)
	}
	return documented
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("module root: %v", err)
	}
	return root
}

func findDocumentedPackage(t *testing.T, packages []documentedPackage, importPath string) documentedPackage {
	t.Helper()
	for _, pkg := range packages {
		if pkg.loaded.PkgPath == importPath {
			return pkg
		}
	}
	t.Fatalf("documented package %q not found", importPath)
	return documentedPackage{}
}

func findPackage(t *testing.T, packages []PackageDoc, importPath string) PackageDoc {
	t.Helper()
	for _, pkg := range packages {
		if pkg.ImportPath == importPath {
			return pkg
		}
	}
	t.Fatalf("package %q not found", importPath)
	return PackageDoc{}
}

func findValue(t *testing.T, values []ValueDoc, name string) ValueDoc {
	t.Helper()
	for _, value := range values {
		if slices.Contains(value.Names, name) {
			return value
		}
	}
	t.Fatalf("value %q not found", name)
	return ValueDoc{}
}

func findType(t *testing.T, types []TypeDoc, name string) TypeDoc {
	t.Helper()
	for _, typ := range types {
		if typ.Name == name {
			return typ
		}
	}
	t.Fatalf("type %q not found", name)
	return TypeDoc{}
}

func findFunc(t *testing.T, functions []FuncDoc, name string) FuncDoc {
	t.Helper()
	for _, function := range functions {
		if function.Name == name {
			return function
		}
	}
	t.Fatalf("function %q not found", name)
	return FuncDoc{}
}

func assertNonNilSlices(t *testing.T, pkg PackageDoc) {
	t.Helper()
	if pkg.Consts == nil || pkg.Vars == nil || pkg.Funcs == nil || pkg.Types == nil {
		t.Fatalf("package %q contains nil slices", pkg.ImportPath)
	}
	for _, typ := range pkg.Types {
		if typ.Consts == nil || typ.Vars == nil || typ.Funcs == nil || typ.Methods == nil {
			t.Fatalf("type %q in package %q contains nil slices", typ.Name, pkg.ImportPath)
		}
		assertAnnotatedSlices(t, typ.Declaration)
		for _, function := range append(append([]FuncDoc(nil), typ.Funcs...), typ.Methods...) {
			assertFuncSlices(t, function)
		}
	}
	for _, value := range append(append([]ValueDoc(nil), pkg.Consts...), pkg.Vars...) {
		assertAnnotatedSlices(t, value.Declaration)
	}
	for _, function := range pkg.Funcs {
		assertFuncSlices(t, function)
	}
}

func assertFuncSlices(t *testing.T, function FuncDoc) {
	t.Helper()
	assertAnnotatedSlices(t, function.Declaration)
	if function.Signature.TypeParameters == nil ||
		function.Signature.Parameters == nil ||
		function.Signature.Results == nil {
		t.Fatalf("function %q contains nil signature slices", function.Name)
	}
}

func assertAnnotatedSlices(t *testing.T, text AnnotatedText) {
	t.Helper()
	if text.References == nil {
		t.Fatalf("annotated text %q has nil references", text.Text)
	}
}

func parameterNames(parameters []ParameterDoc) []string {
	var names []string
	for _, parameter := range parameters {
		names = append(names, parameter.Names...)
	}
	return names
}

func assertBuiltinReference(t *testing.T, text AnnotatedText, symbol string) {
	t.Helper()
	for _, reference := range text.References {
		if reference.Target.Builtin && reference.Target.Symbol == symbol && referencedText(text, reference) == symbol {
			return
		}
	}
	t.Fatalf("annotated text %q has no builtin reference to %q: %#v", text.Text, symbol, text.References)
}

func assertReference(t *testing.T, text AnnotatedText, rendered, packagePath, symbol string) {
	t.Helper()
	for _, reference := range text.References {
		if reference.Target.PackagePath == packagePath &&
			reference.Target.Symbol == symbol &&
			referencedText(text, reference) == rendered {
			return
		}
	}
	t.Fatalf(
		"annotated text %q has no reference %q -> %s.%s: %#v",
		text.Text,
		rendered,
		packagePath,
		symbol,
		text.References,
	)
}

func referencedText(text AnnotatedText, reference TextReference) string {
	runes := []rune(text.Text)
	return string(runes[reference.Start:reference.End])
}
