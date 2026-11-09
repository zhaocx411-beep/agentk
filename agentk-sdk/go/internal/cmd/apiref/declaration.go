package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/tools/go/packages"
)

type identifierMeaning struct {
	name   string
	target *SymbolTarget
}

type formattedIdentifier struct {
	name      string
	startByte int
	endByte   int
}

func formatAnnotatedNode(pkg *packages.Package, node ast.Node) (AnnotatedText, error) {
	text, err := formatNode(pkg.Fset, node)
	if err != nil {
		return AnnotatedText{}, err
	}
	identifiers, err := declarationIdentifiers(text)
	if err != nil {
		return AnnotatedText{}, err
	}
	return annotateIdentifiers(text, identifiers, semanticIdentifiers(node, pkg.TypesInfo))
}

func formatAnnotatedExpr(pkg *packages.Package, expression ast.Expr) (AnnotatedText, error) {
	text, err := formatNode(pkg.Fset, expression)
	if err != nil {
		return AnnotatedText{}, err
	}
	identifiers, err := expressionIdentifiers(text)
	if err != nil {
		return AnnotatedText{}, err
	}
	return annotateIdentifiers(text, identifiers, semanticIdentifiers(expression, pkg.TypesInfo))
}

func semanticIdentifiers(node ast.Node, info *types.Info) []identifierMeaning {
	identifiers := make([]identifierMeaning, 0)
	ast.Inspect(node, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		meaning := identifierMeaning{name: identifier.Name}
		if target, ok := symbolTarget(info.Uses[identifier]); ok {
			meaning.target = &target
		}
		identifiers = append(identifiers, meaning)
		return true
	})
	return identifiers
}

func symbolTarget(object types.Object) (SymbolTarget, bool) {
	if object == nil {
		return SymbolTarget{}, false
	}
	if packageName, ok := object.(*types.PkgName); ok {
		return SymbolTarget{PackagePath: packageName.Imported().Path()}, true
	}
	if object.Pkg() != nil {
		return SymbolTarget{
			PackagePath: object.Pkg().Path(),
			Symbol:      canonicalObjectName(object),
		}, true
	}
	if object.Parent() == types.Universe {
		return SymbolTarget{Symbol: object.Name(), Builtin: true}, true
	}
	return SymbolTarget{}, false
}

func canonicalObjectName(object types.Object) string {
	function, ok := object.(*types.Func)
	if !ok {
		return object.Name()
	}
	signature, ok := function.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return function.Name()
	}
	receiver := signature.Recv().Type()
	if pointer, ok := receiver.(*types.Pointer); ok {
		receiver = pointer.Elem()
	}
	switch receiver := receiver.(type) {
	case *types.Named:
		return receiver.Obj().Name() + "." + function.Name()
	case *types.Alias:
		return receiver.Obj().Name() + "." + function.Name()
	default:
		return function.Name()
	}
}

func declarationIdentifiers(text string) ([]formattedIdentifier, error) {
	const prefix = "package reference\n"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(
		fset,
		"declaration.go",
		prefix+text,
		parser.ParseComments|parser.SkipObjectResolution,
	)
	if err != nil {
		return nil, fmt.Errorf("parse formatted declaration: %w", err)
	}
	if len(file.Decls) != 1 {
		return nil, fmt.Errorf("parse formatted declaration: got %d declarations", len(file.Decls))
	}
	return positionedIdentifiers(fset, file.Decls[0], len(prefix)), nil
}

func expressionIdentifiers(text string) ([]formattedIdentifier, error) {
	fset := token.NewFileSet()
	expression, err := parser.ParseExprFrom(
		fset,
		"expression.go",
		text,
		parser.ParseComments|parser.SkipObjectResolution,
	)
	if err != nil {
		return nil, fmt.Errorf("parse formatted type expression: %w", err)
	}
	return positionedIdentifiers(fset, expression, 0), nil
}

func positionedIdentifiers(
	fset *token.FileSet,
	node ast.Node,
	baseOffset int,
) []formattedIdentifier {
	identifiers := make([]formattedIdentifier, 0)
	ast.Inspect(node, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		start := fset.PositionFor(identifier.Pos(), false).Offset - baseOffset
		identifiers = append(identifiers, formattedIdentifier{
			name:      identifier.Name,
			startByte: start,
			endByte:   start + len(identifier.Name),
		})
		return true
	})
	return identifiers
}

func annotateIdentifiers(
	text string,
	formatted []formattedIdentifier,
	semantic []identifierMeaning,
) (AnnotatedText, error) {
	if len(formatted) != len(semantic) {
		return AnnotatedText{}, fmt.Errorf(
			"annotate declaration: formatted identifier count %d does not match semantic count %d",
			len(formatted),
			len(semantic),
		)
	}
	references := make([]TextReference, 0)
	for index, identifier := range formatted {
		meaning := semantic[index]
		if identifier.name != meaning.name {
			return AnnotatedText{}, fmt.Errorf(
				"annotate declaration: formatted identifier %q does not match semantic identifier %q",
				identifier.name,
				meaning.name,
			)
		}
		if meaning.target == nil {
			continue
		}
		start, end, err := runeRange(text, identifier.startByte, identifier.endByte)
		if err != nil {
			return AnnotatedText{}, err
		}
		references = append(references, TextReference{
			Start:  start,
			End:    end,
			Target: *meaning.target,
		})
	}
	return AnnotatedText{Text: text, References: references}, nil
}

func runeRange(text string, startByte, endByte int) (int, int, error) {
	if startByte < 0 || endByte < startByte || endByte > len(text) {
		return 0, 0, fmt.Errorf(
			"annotate declaration: invalid byte range [%d:%d] for %d-byte declaration",
			startByte,
			endByte,
			len(text),
		)
	}
	return utf8.RuneCountInString(text[:startByte]), utf8.RuneCountInString(text[:endByte]), nil
}

func mirrorFuncSignature(pkg *packages.Package, declaration *ast.FuncDecl) (FuncSignature, error) {
	signature := FuncSignature{
		TypeParameters: make([]TypeParameter, 0),
		Parameters:     make([]ParameterDoc, 0),
		Results:        make([]ParameterDoc, 0),
	}
	if declaration.Recv != nil && len(declaration.Recv.List) > 0 {
		receiver, err := mirrorParameter(pkg, declaration.Recv.List[0])
		if err != nil {
			return FuncSignature{}, err
		}
		signature.Receiver = &receiver
	}
	if declaration.Type.TypeParams != nil {
		for _, field := range declaration.Type.TypeParams.List {
			constraint, err := formatAnnotatedExpr(pkg, field.Type)
			if err != nil {
				return FuncSignature{}, err
			}
			for _, name := range field.Names {
				signature.TypeParameters = append(signature.TypeParameters, TypeParameter{
					Name:       name.Name,
					Constraint: constraint,
				})
			}
		}
	}
	parameters, err := mirrorFieldList(pkg, declaration.Type.Params)
	if err != nil {
		return FuncSignature{}, err
	}
	results, err := mirrorFieldList(pkg, declaration.Type.Results)
	if err != nil {
		return FuncSignature{}, err
	}
	signature.Parameters = parameters
	signature.Results = results
	return signature, nil
}

func mirrorFieldList(pkg *packages.Package, list *ast.FieldList) ([]ParameterDoc, error) {
	parameters := make([]ParameterDoc, 0)
	if list == nil {
		return parameters, nil
	}
	for _, field := range list.List {
		parameter, err := mirrorParameter(pkg, field)
		if err != nil {
			return nil, err
		}
		parameters = append(parameters, parameter)
	}
	return parameters, nil
}

func mirrorParameter(pkg *packages.Package, field *ast.Field) (ParameterDoc, error) {
	expression := field.Type
	variadic := false
	if ellipsis, ok := expression.(*ast.Ellipsis); ok {
		variadic = true
		expression = ellipsis.Elt
	}
	typeText, err := formatAnnotatedExpr(pkg, expression)
	if err != nil {
		return ParameterDoc{}, err
	}
	names := make([]string, 0, len(field.Names))
	for _, name := range field.Names {
		names = append(names, name.Name)
	}
	return ParameterDoc{Names: names, Type: typeText, Variadic: variadic}, nil
}

func mirrorAliasMethod(
	aliasName string,
	alias *types.TypeName,
	function *types.Func,
	doc string,
) (FuncDoc, error) {
	signature := function.Type().(*types.Signature)
	qualifier := newTypeQualifier()
	typedSignature := types.TypeString(signature, qualifier.qualify)
	typedSignature = strings.TrimPrefix(typedSignature, "func")

	receiverName := signature.Recv().Name()
	receiverType := aliasReceiverType(aliasName, alias.Type(), signature.Recv().Type())
	receiver := receiverType
	if receiverName != "" {
		receiver = receiverName + " " + receiverType
	}
	text := "func (" + receiver + ") " + function.Name() + typedSignature
	localTargets := map[string]SymbolTarget{
		aliasName: {PackagePath: alias.Pkg().Path(), Symbol: aliasName},
	}
	declaration, err := annotateSyntheticDeclaration(text, qualifier.paths, localTargets)
	if err != nil {
		return FuncDoc{}, err
	}
	structured, err := mirrorTypesSignature(aliasName, alias, signature, qualifier)
	if err != nil {
		return FuncDoc{}, err
	}
	return FuncDoc{
		Name:        function.Name(),
		Doc:         doc,
		Declaration: declaration,
		Signature:   structured,
	}, nil
}

func mirrorTypesSignature(
	aliasName string,
	alias *types.TypeName,
	signature *types.Signature,
	qualifier *typeQualifier,
) (FuncSignature, error) {
	parameters, err := mirrorTypesTuple(signature.Params(), signature.Variadic(), qualifier)
	if err != nil {
		return FuncSignature{}, err
	}
	results, err := mirrorTypesTuple(signature.Results(), false, qualifier)
	if err != nil {
		return FuncSignature{}, err
	}
	structured := FuncSignature{
		TypeParameters: make([]TypeParameter, 0),
		Parameters:     parameters,
		Results:        results,
	}
	localTargets := map[string]SymbolTarget{
		aliasName: {PackagePath: alias.Pkg().Path(), Symbol: aliasName},
	}
	receiverText := aliasReceiverType(aliasName, alias.Type(), signature.Recv().Type())
	receiverType, err := annotateSyntheticExpression(receiverText, qualifier.paths, localTargets)
	if err != nil {
		return FuncSignature{}, err
	}
	receiverNames := make([]string, 0, 1)
	if signature.Recv().Name() != "" {
		receiverNames = append(receiverNames, signature.Recv().Name())
	}
	structured.Receiver = &ParameterDoc{Names: receiverNames, Type: receiverType}
	if signature.TypeParams() != nil {
		for index := 0; index < signature.TypeParams().Len(); index++ {
			parameter := signature.TypeParams().At(index)
			constraint, err := annotatedTypesType(parameter.Constraint(), qualifier)
			if err != nil {
				return FuncSignature{}, err
			}
			structured.TypeParameters = append(structured.TypeParameters, TypeParameter{
				Name:       parameter.Obj().Name(),
				Constraint: constraint,
			})
		}
	}
	return structured, nil
}

func mirrorTypesTuple(
	tuple *types.Tuple,
	variadic bool,
	qualifier *typeQualifier,
) ([]ParameterDoc, error) {
	parameters := make([]ParameterDoc, 0, tuple.Len())
	for index := 0; index < tuple.Len(); index++ {
		variable := tuple.At(index)
		typeValue := variable.Type()
		isVariadic := variadic && index == tuple.Len()-1
		if isVariadic {
			typeValue = typeValue.(*types.Slice).Elem()
		}
		names := make([]string, 0, 1)
		if variable.Name() != "" {
			names = append(names, variable.Name())
		}
		annotated, err := annotatedTypesType(typeValue, qualifier)
		if err != nil {
			return nil, err
		}
		parameters = append(parameters, ParameterDoc{
			Names:    names,
			Type:     annotated,
			Variadic: isVariadic,
		})
	}
	return parameters, nil
}

type typeQualifier struct {
	byPackage map[*types.Package]string
	paths     map[string]string
}

func newTypeQualifier() *typeQualifier {
	return &typeQualifier{
		byPackage: make(map[*types.Package]string),
		paths:     make(map[string]string),
	}
}

func (qualifier *typeQualifier) qualify(pkg *types.Package) string {
	if name, ok := qualifier.byPackage[pkg]; ok {
		return name
	}
	base := pkg.Name()
	name := base
	for suffix := 2; ; suffix++ {
		path, exists := qualifier.paths[name]
		if !exists || path == pkg.Path() {
			break
		}
		name = fmt.Sprintf("%s%d", base, suffix)
	}
	qualifier.byPackage[pkg] = name
	qualifier.paths[name] = pkg.Path()
	return name
}

func annotatedTypesType(value types.Type, qualifier *typeQualifier) (AnnotatedText, error) {
	text := types.TypeString(value, qualifier.qualify)
	annotated, err := annotateSyntheticExpression(text, qualifier.paths, nil)
	if err != nil {
		return AnnotatedText{}, err
	}
	return annotated, nil
}

func annotateSyntheticDeclaration(
	text string,
	packagePaths map[string]string,
	localTargets map[string]SymbolTarget,
) (AnnotatedText, error) {
	const prefix = "package reference\n"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(
		fset,
		"declaration.go",
		prefix+text,
		parser.SkipObjectResolution,
	)
	if err != nil {
		return AnnotatedText{}, fmt.Errorf("parse synthetic declaration: %w", err)
	}
	if len(file.Decls) != 1 {
		return AnnotatedText{}, fmt.Errorf("parse synthetic declaration: got %d declarations", len(file.Decls))
	}
	references, err := syntheticReferences(
		text,
		fset,
		file.Decls[0],
		len(prefix),
		packagePaths,
		localTargets,
	)
	return AnnotatedText{Text: text, References: references}, err
}

func annotateSyntheticExpression(
	text string,
	packagePaths map[string]string,
	localTargets map[string]SymbolTarget,
) (AnnotatedText, error) {
	fset := token.NewFileSet()
	expression, err := parser.ParseExprFrom(
		fset,
		"expression.go",
		text,
		parser.SkipObjectResolution,
	)
	if err != nil {
		return AnnotatedText{}, fmt.Errorf("parse synthetic type expression: %w", err)
	}
	references, err := syntheticReferences(
		text,
		fset,
		expression,
		0,
		packagePaths,
		localTargets,
	)
	return AnnotatedText{Text: text, References: references}, err
}

func syntheticReferences(
	text string,
	fset *token.FileSet,
	node ast.Node,
	baseOffset int,
	packagePaths map[string]string,
	localTargets map[string]SymbolTarget,
) ([]TextReference, error) {
	references := make([]TextReference, 0)
	var visitErr error
	var visit func(ast.Node) bool
	visit = func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.SelectorExpr:
			packageName, ok := node.X.(*ast.Ident)
			if !ok {
				return true
			}
			packagePath, ok := packagePaths[packageName.Name]
			if !ok {
				return true
			}
			for _, item := range []struct {
				identifier *ast.Ident
				target     SymbolTarget
			}{
				{packageName, SymbolTarget{PackagePath: packagePath}},
				{node.Sel, SymbolTarget{PackagePath: packagePath, Symbol: node.Sel.Name}},
			} {
				reference, err := positionedReference(text, fset, item.identifier, baseOffset, item.target)
				if err != nil {
					visitErr = err
					return false
				}
				references = append(references, reference)
			}
			return false
		case *ast.Ident:
			target, ok := localTargets[node.Name]
			if !ok && types.Universe.Lookup(node.Name) != nil {
				target = SymbolTarget{Symbol: node.Name, Builtin: true}
				ok = true
			}
			if !ok {
				return true
			}
			reference, err := positionedReference(text, fset, node, baseOffset, target)
			if err != nil {
				visitErr = err
				return false
			}
			references = append(references, reference)
		}
		return true
	}
	ast.Inspect(node, visit)
	if visitErr != nil {
		return nil, visitErr
	}
	sort.Slice(references, func(i, j int) bool {
		return references[i].Start < references[j].Start
	})
	return references, nil
}

func positionedReference(
	text string,
	fset *token.FileSet,
	identifier *ast.Ident,
	baseOffset int,
	target SymbolTarget,
) (TextReference, error) {
	startByte := fset.PositionFor(identifier.Pos(), false).Offset - baseOffset
	start, end, err := runeRange(text, startByte, startByte+len(identifier.Name))
	if err != nil {
		return TextReference{}, err
	}
	return TextReference{Start: start, End: end, Target: target}, nil
}
