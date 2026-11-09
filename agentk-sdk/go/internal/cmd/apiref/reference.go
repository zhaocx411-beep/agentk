package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/doc"
	"go/format"
	"go/token"
	"go/types"
	"io"
	"os"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

type Reference struct {
	ModulePath string       `json:"modulePath"`
	Packages   []PackageDoc `json:"packages"`
}

type PackageDoc struct {
	ImportPath string     `json:"importPath"`
	Name       string     `json:"name"`
	Doc        string     `json:"doc"`
	Consts     []ValueDoc `json:"consts"`
	Vars       []ValueDoc `json:"vars"`
	Funcs      []FuncDoc  `json:"funcs"`
	Types      []TypeDoc  `json:"types"`
}

type ValueDoc struct {
	Names       []string      `json:"names"`
	Doc         string        `json:"doc"`
	Declaration AnnotatedText `json:"declaration"`
}

type FuncDoc struct {
	Name        string        `json:"name"`
	Doc         string        `json:"doc"`
	Declaration AnnotatedText `json:"declaration"`
	Signature   FuncSignature `json:"signature"`
}

type TypeDoc struct {
	Name        string        `json:"name"`
	Doc         string        `json:"doc"`
	Declaration AnnotatedText `json:"declaration"`
	Consts      []ValueDoc    `json:"consts"`
	Vars        []ValueDoc    `json:"vars"`
	Funcs       []FuncDoc     `json:"funcs"`
	Methods     []FuncDoc     `json:"methods"`
}

type AnnotatedText struct {
	Text       string          `json:"text"`
	References []TextReference `json:"references"`
}

type TextReference struct {
	Start  int          `json:"start"`
	End    int          `json:"end"`
	Target SymbolTarget `json:"target"`
}

type SymbolTarget struct {
	PackagePath string `json:"packagePath"`
	Symbol      string `json:"symbol"`
	Builtin     bool   `json:"builtin"`
}

type FuncSignature struct {
	Receiver       *ParameterDoc   `json:"receiver"`
	TypeParameters []TypeParameter `json:"typeParameters"`
	Parameters     []ParameterDoc  `json:"parameters"`
	Results        []ParameterDoc  `json:"results"`
}

type ParameterDoc struct {
	Names    []string      `json:"names"`
	Type     AnnotatedText `json:"type"`
	Variadic bool          `json:"variadic"`
}

type TypeParameter struct {
	Name       string        `json:"name"`
	Constraint AnnotatedText `json:"constraint"`
}

type documentedPackage struct {
	loaded *packages.Package
	doc    *doc.Package
}

func run(stdout, stderr io.Writer) (err error) {
	defer func() {
		if err != nil {
			fmt.Fprintln(stderr, err)
		}
	}()

	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	reference, err := loadReference(dir, "./...")
	if err != nil {
		return err
	}

	data, err := json.Marshal(reference)
	if err != nil {
		return fmt.Errorf("marshal API reference: %w", err)
	}
	data = append(data, '\n')
	if _, err := stdout.Write(data); err != nil {
		return fmt.Errorf("write API reference: %w", err)
	}
	return nil
}

func loadReference(dir string, patterns ...string) (Reference, error) {
	loaded, err := packages.Load(
		&packages.Config{
			Dir:  dir,
			Mode: packages.LoadSyntax | packages.NeedModule,
		},
		patterns...,
	)
	if err != nil {
		return Reference{}, fmt.Errorf("load packages: %w", err)
	}
	if err := loadedPackagesError(loaded); err != nil {
		return Reference{}, err
	}

	modulePath, err := mainModulePath(loaded)
	if err != nil {
		return Reference{}, err
	}
	documented, err := documentPackages(loaded)
	if err != nil {
		return Reference{}, err
	}
	objectDocs := indexObjectDocs(documented)

	reference := Reference{
		ModulePath: modulePath,
		Packages:   make([]PackageDoc, 0, len(documented)),
	}
	for _, documentedPackage := range documented {
		pkg := documentedPackage.loaded
		if pkg.Module == nil ||
			pkg.Module.Path != modulePath ||
			containsInternalSegment(pkg.PkgPath) {
			continue
		}
		mirrored, err := mirrorPackage(pkg, documentedPackage.doc, objectDocs)
		if err != nil {
			return Reference{}, fmt.Errorf("mirror package %s: %w", pkg.PkgPath, err)
		}
		reference.Packages = append(reference.Packages, mirrored)
	}

	sortReference(&reference)
	return reference, nil
}

func loadedPackagesError(loaded []*packages.Package) error {
	var messages []string
	for _, pkg := range loaded {
		for _, pkgErr := range pkg.Errors {
			messages = append(messages, pkgErr.Error())
		}
	}
	if len(messages) == 0 {
		return nil
	}
	sort.Strings(messages)
	return fmt.Errorf("load packages:\n%s", strings.Join(messages, "\n"))
}

func mainModulePath(loaded []*packages.Package) (string, error) {
	paths := make(map[string]struct{})
	for _, pkg := range loaded {
		if pkg.Module != nil && pkg.Module.Main {
			paths[pkg.Module.Path] = struct{}{}
		}
	}
	if len(paths) == 0 {
		return "", errors.New("load packages: no main module found")
	}
	if len(paths) > 1 {
		found := make([]string, 0, len(paths))
		for path := range paths {
			found = append(found, path)
		}
		sort.Strings(found)
		return "", fmt.Errorf("load packages: multiple main modules found: %s", strings.Join(found, ", "))
	}
	for path := range paths {
		return path, nil
	}
	panic("unreachable")
}

func documentPackages(loaded []*packages.Package) ([]documentedPackage, error) {
	documented := make([]documentedPackage, 0, len(loaded))
	for _, pkg := range loaded {
		if len(pkg.Syntax) == 0 {
			continue
		}
		docPkg, err := doc.NewFromFiles(pkg.Fset, pkg.Syntax, pkg.PkgPath, doc.PreserveAST)
		if err != nil {
			return nil, fmt.Errorf("document package %s: %w", pkg.PkgPath, err)
		}
		documented = append(documented, documentedPackage{loaded: pkg, doc: docPkg})
	}
	return documented, nil
}

func indexObjectDocs(documented []documentedPackage) map[types.Object]string {
	docs := make(map[types.Object]string)
	for _, documentedPackage := range documented {
		pkg := documentedPackage.loaded
		for _, file := range pkg.Syntax {
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Doc == nil {
					continue
				}
				object := pkg.TypesInfo.Defs[function.Name]
				if object != nil {
					docs[object] = function.Doc.Text()
				}
			}
		}
	}
	return docs
}

func containsInternalSegment(importPath string) bool {
	for _, segment := range strings.Split(importPath, "/") {
		if segment == "internal" {
			return true
		}
	}
	return false
}

func mirrorPackage(
	pkg *packages.Package,
	src *doc.Package,
	objectDocs map[types.Object]string,
) (PackageDoc, error) {
	consts, err := mirrorValues(pkg, src.Consts)
	if err != nil {
		return PackageDoc{}, err
	}
	vars, err := mirrorValues(pkg, src.Vars)
	if err != nil {
		return PackageDoc{}, err
	}
	funcs, err := mirrorFuncs(pkg, src.Funcs)
	if err != nil {
		return PackageDoc{}, err
	}
	types, err := mirrorTypes(pkg, src.Types, objectDocs)
	if err != nil {
		return PackageDoc{}, err
	}
	return PackageDoc{
		ImportPath: pkg.PkgPath,
		Name:       src.Name,
		Doc:        src.Doc,
		Consts:     consts,
		Vars:       vars,
		Funcs:      funcs,
		Types:      types,
	}, nil
}

func mirrorValues(pkg *packages.Package, source []*doc.Value) ([]ValueDoc, error) {
	values := make([]ValueDoc, 0, len(source))
	for _, value := range source {
		declaration, err := formatGenDecl(pkg, value.Decl)
		if err != nil {
			return nil, err
		}
		values = append(values, ValueDoc{
			Names:       append([]string(nil), value.Names...),
			Doc:         value.Doc,
			Declaration: declaration,
		})
	}
	return values, nil
}

func mirrorFuncs(pkg *packages.Package, source []*doc.Func) ([]FuncDoc, error) {
	functions := make([]FuncDoc, 0, len(source))
	for _, function := range source {
		declaration, err := formatFuncDecl(pkg, function.Decl)
		if err != nil {
			return nil, err
		}
		signature, err := mirrorFuncSignature(pkg, function.Decl)
		if err != nil {
			return nil, err
		}
		functions = append(functions, FuncDoc{
			Name:        function.Name,
			Doc:         function.Doc,
			Declaration: declaration,
			Signature:   signature,
		})
	}
	return functions, nil
}

func mirrorTypes(
	pkg *packages.Package,
	source []*doc.Type,
	objectDocs map[types.Object]string,
) ([]TypeDoc, error) {
	types := make([]TypeDoc, 0, len(source))
	for _, typ := range source {
		mirrored, err := mirrorType(pkg, typ, objectDocs)
		if err != nil {
			return nil, err
		}
		types = append(types, mirrored)
	}
	return types, nil
}

func mirrorType(
	pkg *packages.Package,
	src *doc.Type,
	objectDocs map[types.Object]string,
) (TypeDoc, error) {
	declaration, err := formatGenDecl(pkg, src.Decl)
	if err != nil {
		return TypeDoc{}, err
	}
	consts, err := mirrorValues(pkg, src.Consts)
	if err != nil {
		return TypeDoc{}, err
	}
	vars, err := mirrorValues(pkg, src.Vars)
	if err != nil {
		return TypeDoc{}, err
	}
	funcs, err := mirrorFuncs(pkg, src.Funcs)
	if err != nil {
		return TypeDoc{}, err
	}
	methods, err := mirrorFuncs(pkg, src.Methods)
	if err != nil {
		return TypeDoc{}, err
	}

	mirrored := TypeDoc{
		Name:        src.Name,
		Doc:         src.Doc,
		Declaration: declaration,
		Consts:      consts,
		Vars:        vars,
		Funcs:       funcs,
		Methods:     methods,
	}
	object, _ := pkg.Types.Scope().Lookup(src.Name).(*types.TypeName)
	if object == nil || !object.IsAlias() {
		return mirrored, nil
	}

	seen := make(map[string]struct{}, len(mirrored.Methods))
	for _, method := range mirrored.Methods {
		seen[method.Name] = struct{}{}
	}
	for _, function := range exportedAliasMethods(object.Type()) {
		if _, ok := seen[function.Name()]; ok {
			continue
		}
		method, err := mirrorAliasMethod(src.Name, object, function, objectDocs[function])
		if err != nil {
			return TypeDoc{}, err
		}
		mirrored.Methods = append(mirrored.Methods, method)
		seen[function.Name()] = struct{}{}
	}
	return mirrored, nil
}

func exportedAliasMethods(alias types.Type) []*types.Func {
	unaliased := types.Unalias(alias)
	candidates := []types.Type{alias, unaliased}
	if _, pointer := unaliased.(*types.Pointer); !pointer {
		candidates = append(candidates, types.NewPointer(unaliased))
	}

	seen := make(map[*types.Func]struct{})
	var methods []*types.Func
	for _, candidate := range candidates {
		methodSet := types.NewMethodSet(candidate)
		for index := 0; index < methodSet.Len(); index++ {
			function, ok := methodSet.At(index).Obj().(*types.Func)
			if !ok || !function.Exported() {
				continue
			}
			if _, ok := seen[function]; ok {
				continue
			}
			seen[function] = struct{}{}
			methods = append(methods, function)
		}
	}
	sort.Slice(methods, func(i, j int) bool {
		return methods[i].Name() < methods[j].Name()
	})
	return methods
}

func aliasReceiverType(aliasName string, aliasType, receiverType types.Type) string {
	if alias, ok := aliasType.(*types.Alias); ok && alias.TypeParams() != nil && alias.TypeParams().Len() > 0 {
		parameters := make([]string, 0, alias.TypeParams().Len())
		for index := 0; index < alias.TypeParams().Len(); index++ {
			parameters = append(parameters, alias.TypeParams().At(index).Obj().Name())
		}
		aliasName += "[" + strings.Join(parameters, ", ") + "]"
	}
	if _, pointer := receiverType.(*types.Pointer); pointer {
		return "*" + aliasName
	}
	return aliasName
}

func formatFuncDecl(pkg *packages.Package, src *ast.FuncDecl) (AnnotatedText, error) {
	declaration := *src
	declaration.Doc = nil
	declaration.Body = nil
	return formatAnnotatedNode(pkg, &declaration)
}

func formatGenDecl(pkg *packages.Package, src *ast.GenDecl) (AnnotatedText, error) {
	declaration := *src
	declaration.Doc = nil
	declaration.Specs = append([]ast.Spec(nil), src.Specs...)
	if declaration.Tok == token.TYPE {
		for index, spec := range declaration.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			cloned := *typeSpec
			cloned.Doc = nil
			declaration.Specs[index] = &cloned
		}
	}
	return formatAnnotatedNode(pkg, &declaration)
}

func formatNode(fset *token.FileSet, node ast.Node) (string, error) {
	var buffer bytes.Buffer
	if err := format.Node(&buffer, fset, node); err != nil {
		return "", fmt.Errorf("format declaration: %w", err)
	}
	return buffer.String(), nil
}

func sortReference(reference *Reference) {
	sort.Slice(reference.Packages, func(i, j int) bool {
		return reference.Packages[i].ImportPath < reference.Packages[j].ImportPath
	})
	for index := range reference.Packages {
		sortPackage(&reference.Packages[index])
	}
}

func sortPackage(pkg *PackageDoc) {
	sortValues(pkg.Consts)
	sortValues(pkg.Vars)
	sortFuncs(pkg.Funcs)
	sort.Slice(pkg.Types, func(i, j int) bool {
		return pkg.Types[i].Name < pkg.Types[j].Name
	})
	for index := range pkg.Types {
		sortValues(pkg.Types[index].Consts)
		sortValues(pkg.Types[index].Vars)
		sortFuncs(pkg.Types[index].Funcs)
		sortFuncs(pkg.Types[index].Methods)
	}
}

func sortValues(values []ValueDoc) {
	sort.SliceStable(values, func(i, j int) bool {
		return strings.Join(values[i].Names, "\x00") < strings.Join(values[j].Names, "\x00")
	})
}

func sortFuncs(functions []FuncDoc) {
	sort.Slice(functions, func(i, j int) bool {
		return functions[i].Name < functions[j].Name
	})
}
