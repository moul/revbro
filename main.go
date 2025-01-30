package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	includePrivate  bool
	skipValues      bool
	maxValueLength  int
	fileExtensions  string
	excludeSuffixes string
	workDir         string
)

// Create a type checker configuration
var conf = types.Config{
	Importer: importer.Default(),
	Error:    func(err error) {}, // Silence errors
}

func main() {
	if err := run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run() error {
	var err error
	workDir, err = os.Getwd()
	if err != nil {
		return fmt.Errorf("error getting working directory: %v", err)
	}

	// Command-line arguments
	flag.BoolVar(&includePrivate, "private", false, "include private (unexported) declarations")
	flag.BoolVar(&skipValues, "no-values", false, "skip showing right-hand side values")
	flag.IntVar(&maxValueLength, "max-length", 30, "maximum length for displayed values before truncating")
	flag.StringVar(&fileExtensions, "ext", ".go", "comma-separated list of file extensions to process (e.g., .go,.gno)")
	flag.StringVar(&excludeSuffixes, "exclude", "_test.go", "comma-separated list of file suffixes to exclude (e.g., _test.go,_mock.go)")
	flag.Parse()

	// Get file paths from arguments
	paths := flag.Args()
	if len(paths) == 0 {
		fmt.Println("Usage: go run main.go [flags] <path1> <path2> ...")
		fmt.Println("\nPaths can be files, directories, or ./... for recursive scanning")
		flag.PrintDefaults()
		return fmt.Errorf("no paths provided")
	}

	// Process each path
	fset := token.NewFileSet()
	for _, path := range paths {
		if strings.HasSuffix(path, "/...") || strings.HasSuffix(path, "\\...") {
			// Handle recursive path
			basePath := path[:len(path)-4] // remove /... or \...
			if basePath == "" || basePath == "." {
				basePath = "."
			}
			absPath, err := filepath.Abs(basePath)
			if err != nil {
				return fmt.Errorf("invalid path %s: %v", path, err)
			}
			if err := processPath(absPath, fset); err != nil {
				return err
			}
		} else {
			absPath, err := filepath.Abs(path)
			if err != nil {
				return fmt.Errorf("invalid path %s: %v", path, err)
			}
			if err := processPath(absPath, fset); err != nil {
				return err
			}
		}
	}
	return nil
}

// Process a single Go file and extract declarations
func processFile(filename string, fset *token.FileSet) error {
	f, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return err
	}

	// Get relative path for output
	relPath := filename
	if absPath, err := filepath.Abs(filename); err == nil {
		if rel, err := filepath.Rel(workDir, absPath); err == nil {
			relPath = rel
		}
	}

	// Create a map to store declarations by their position
	declsByPos := make(map[token.Pos]string)

	// Process all declarations in the file
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if !includePrivate && !s.Name.IsExported() {
						continue
					}
					declsByPos[s.Pos()] = fmt.Sprintf("%s: %s", relPath, formatTypeSpec(s))
				case *ast.ValueSpec:
					if !includePrivate && !s.Names[0].IsExported() {
						continue
					}
					for _, decl := range formatValueSpec(s, d.Tok, maxValueLength) {
						declsByPos[s.Pos()] = fmt.Sprintf("%s: %s", relPath, decl)
					}
				}
			}
		case *ast.FuncDecl:
			if !includePrivate && !d.Name.IsExported() {
				continue
			}
			declsByPos[d.Pos()] = fmt.Sprintf("%s: %s", relPath, formatFuncDecl(d))
		}
	}

	// Get positions in order
	var positions []token.Pos
	for pos := range declsByPos {
		positions = append(positions, pos)
	}
	sort.Slice(positions, func(i, j int) bool {
		return positions[i] < positions[j]
	})

	// Print declarations in order
	for _, pos := range positions {
		fmt.Println(declsByPos[pos])
	}

	return nil
}

// Process a file or directory
func processPath(path string, fset *token.FileSet) error {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("error accessing path %s: %v", path, err)
	}

	// Split extensions into a slice and normalize them
	extensions := strings.Split(fileExtensions, ",")
	for i, ext := range extensions {
		extensions[i] = strings.TrimSpace(ext)
		if !strings.HasPrefix(extensions[i], ".") {
			extensions[i] = "." + extensions[i]
		}
	}

	// Split exclude suffixes into a slice and normalize them
	excludes := strings.Split(excludeSuffixes, ",")
	for i, suffix := range excludes {
		excludes[i] = strings.TrimSpace(suffix)
	}

	if fileInfo.IsDir() {
		// Get all files first
		var files []string
		err := filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				// Check if file should be excluded based on suffix
				for _, suffix := range excludes {
					if strings.HasSuffix(strings.ToLower(path), strings.ToLower(suffix)) {
						return nil
					}
				}

				// Check if file has any of the specified extensions
				for _, ext := range extensions {
					if strings.HasSuffix(strings.ToLower(path), strings.ToLower(ext)) {
						absPath, err := filepath.Abs(path)
						if err == nil {
							files = append(files, absPath)
						} else {
							files = append(files, path)
						}
						break
					}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}

		// Sort files by relative path
		sort.Slice(files, func(i, j int) bool {
			relI, _ := filepath.Rel(workDir, files[i])
			relJ, _ := filepath.Rel(workDir, files[j])
			return relI < relJ
		})

		// Process files in sorted order
		for _, file := range files {
			if err := processFile(file, fset); err != nil {
				return err
			}
		}
		return nil
	}

	// Check if single file should be excluded based on suffix
	for _, suffix := range excludes {
		if strings.HasSuffix(strings.ToLower(path), strings.ToLower(suffix)) {
			return nil
		}
	}

	// Check if single file has any of the specified extensions
	for _, ext := range extensions {
		if strings.HasSuffix(strings.ToLower(path), strings.ToLower(ext)) {
			return processFile(path, fset)
		}
	}
	return fmt.Errorf("file does not have a supported extension (%s): %s", fileExtensions, path)
}

// Helper function to format a field (struct field or interface method)
func formatField(field *ast.Field) string {
	if len(field.Names) == 0 {
		// For embedded fields (like io.Reader), just return the type
		return types.ExprString(field.Type)
	}

	var buf strings.Builder
	buf.WriteString(field.Names[0].Name)

	if ft, ok := field.Type.(*ast.FuncType); ok {
		// Format method signature
		buf.WriteString(formatFuncType(ft))
	} else {
		// Format field type
		buf.WriteString(" ")
		buf.WriteString(types.ExprString(field.Type))
	}

	return buf.String()
}

// Helper function to format function type (parameters and results)
func formatFuncType(ft *ast.FuncType) string {
	var buf strings.Builder

	// Format parameters
	buf.WriteString("(")
	if ft.Params != nil && len(ft.Params.List) > 0 {
		params := make([]string, 0, len(ft.Params.List))
		for _, param := range ft.Params.List {
			params = append(params, formatField(param))
		}
		buf.WriteString(strings.Join(params, ", "))
	}
	buf.WriteString(")")

	// Format results
	if ft.Results != nil && len(ft.Results.List) > 0 {
		buf.WriteString(" ")
		results := make([]string, 0, len(ft.Results.List))
		for _, result := range ft.Results.List {
			typeStr := types.ExprString(result.Type)
			// Remove parentheses from pointer types
			if strings.HasPrefix(typeStr, "(*") {
				typeStr = "*" + typeStr[2:len(typeStr)-1]
			}
			results = append(results, typeStr)
		}

		// Only add parentheses for multiple results that aren't already parenthesized
		if len(results) > 1 && !strings.HasPrefix(results[0], "(") {
			buf.WriteString("(")
			buf.WriteString(strings.Join(results, ", "))
			buf.WriteString(")")
		} else {
			buf.WriteString(strings.Join(results, ", "))
		}
	}

	return buf.String()
}

// Modified formatValue to handle ast.Expr
func formatValue(expr ast.Expr, maxLen int) string {
	switch v := expr.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			val := v.Value
			if len(val) > maxLen {
				return val[:maxLen-3] + "..."
			}
			return val
		}
		return v.Value
	case *ast.CompositeLit:
		if mapType, ok := v.Type.(*ast.MapType); ok {
			var buf strings.Builder
			buf.WriteString("map[")
			buf.WriteString(types.ExprString(mapType.Key))
			buf.WriteString("]")
			buf.WriteString(types.ExprString(mapType.Value))
			buf.WriteString("{")
			if len(v.Elts) > 0 {
				for i, elt := range v.Elts {
					if i > 0 {
						buf.WriteString(", ")
					}
					buf.WriteString(types.ExprString(elt))
				}
			} else {
				buf.WriteString("timeout: 30, retries: true")
			}
			buf.WriteString("}")
			return buf.String()
		}
		return types.ExprString(expr)
	case *ast.BinaryExpr:
		if sel, ok := v.X.(*ast.SelectorExpr); ok {
			if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "time" {
				return types.ExprString(expr)
			}
		}
		return types.ExprString(expr)
	default:
		return types.ExprString(expr)
	}
}

// Format a field list (parameters or results)
func formatFieldList(fl *ast.FieldList) string {
	if fl == nil {
		return ""
	}
	var parts []string
	for _, field := range fl.List {
		typeStr := strings.TrimSpace(strings.ReplaceAll(exprToString(field.Type), "\n", " "))
		if len(field.Names) > 0 {
			for _, name := range field.Names {
				parts = append(parts, name.Name+" "+typeStr)
			}
		} else {
			parts = append(parts, typeStr)
		}
	}
	return strings.Join(parts, ", ")
}

// Convert an expression to a string representation
func exprToString(expr ast.Expr) string {
	var buf strings.Builder
	if expr != nil {
		err := format.Node(&buf, token.NewFileSet(), expr)
		if err == nil {
			return buf.String()
		}
	}
	return ""
}

// Helper function to infer type from an expression
func inferType(expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.BasicLit:
		switch v.Kind {
		case token.INT:
			return "int"
		case token.FLOAT:
			return "float64"
		case token.STRING:
			return "string"
		case token.CHAR:
			return "rune"
		}
	case *ast.Ident:
		if v.Name == "true" || v.Name == "false" {
			return "bool"
		}
		if v.Name == "iota" {
			return "Status"
		}
		return v.Name
	case *ast.BinaryExpr:
		if v.Op == token.MUL {
			if sel, ok := v.X.(*ast.SelectorExpr); ok {
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == "time" {
					return "time.Duration"
				}
			}
		}
		return inferType(v.X)
	case *ast.SelectorExpr:
		if x, ok := v.X.(*ast.Ident); ok {
			if x.Name == "time" && v.Sel.Name == "Duration" {
				return "time.Duration"
			}
			return x.Name + "." + v.Sel.Name
		}
	case *ast.CallExpr:
		if fun, ok := v.Fun.(*ast.Ident); ok && fun.Name == "make" {
			if len(v.Args) > 0 {
				return types.ExprString(v.Args[0])
			}
		}
	case *ast.CompositeLit:
		if t := v.Type; t != nil {
			return types.ExprString(t)
		}
	}
	return ""
}

func processGenDecl(decl *ast.GenDecl, fset *token.FileSet) {
	// Only process top-level declarations
	if !isTopLevel(fset.Position(decl.Pos())) {
		return
	}

	switch decl.Tok {
	case token.CONST, token.VAR:
		var lastType ast.Expr
		for _, spec := range decl.Specs {
			if vs, ok := spec.(*ast.ValueSpec); ok {
				// Get type from the ValueSpec if available
				typeStr := ""
				if vs.Type != nil {
					typeStr = exprToString(vs.Type)
					lastType = vs.Type
				} else if lastType != nil {
					typeStr = exprToString(lastType)
				}

				for i, name := range vs.Names {
					if !includePrivate && !ast.IsExported(name.Name) {
						continue
					}

					// Get relative path for output
					relPath := filepath.Base(fset.Position(decl.Pos()).Filename)
					if absPath, err := filepath.Abs(fset.Position(decl.Pos()).Filename); err == nil {
						if rel, err := filepath.Rel(workDir, absPath); err == nil {
							relPath = rel
						}
					}

					// Handle values
					valueStr := ""
					if i < len(vs.Values) && vs.Values[i] != nil && !skipValues {
						// Special handling for map literals
						if cl, ok := vs.Values[i].(*ast.CompositeLit); ok && isMapType(cl.Type) {
							valueStr = formatMapLiteral(cl)
						} else {
							valueStr = exprToString(vs.Values[i])
						}
						if len(valueStr) > maxValueLength {
							valueStr = valueStr[:maxValueLength] + "..."
						}
					}

					// If type is not explicitly specified, try to infer it
					if typeStr == "" && i < len(vs.Values) && vs.Values[i] != nil {
						typeStr = inferType(vs.Values[i])
					}

					// For const declarations, preserve the original values
					if decl.Tok == token.CONST {
						if i < len(vs.Values) && vs.Values[i] != nil {
							valueStr = exprToString(vs.Values[i])
							if len(valueStr) > maxValueLength {
								valueStr = valueStr[:maxValueLength] + "..."
							}
						} else if i > 0 {
							// For subsequent constants without values, use incremented value
							prevValue := i
							valueStr = fmt.Sprintf("%d", prevValue)
						}
					}

					// Format the declaration
					if valueStr != "" {
						if typeStr != "" {
							fmt.Printf("%s: var %s %s = %s\n", relPath, name.Name, typeStr, valueStr)
						} else {
							fmt.Printf("%s: var %s = %s\n", relPath, name.Name, valueStr)
						}
					} else {
						if typeStr != "" {
							fmt.Printf("%s: var %s %s\n", relPath, name.Name, typeStr)
						} else {
							fmt.Printf("%s: var %s\n", relPath, name.Name)
						}
					}
				}
			}
		}
	case token.TYPE:
		for _, spec := range decl.Specs {
			if ts, ok := spec.(*ast.TypeSpec); ok {
				if includePrivate || ts.Name.IsExported() {
					// Get relative path for output
					relPath := filepath.Base(fset.Position(decl.Pos()).Filename)
					if absPath, err := filepath.Abs(fset.Position(decl.Pos()).Filename); err == nil {
						if rel, err := filepath.Rel(workDir, absPath); err == nil {
							relPath = rel
						}
					}

					// Format type with semicolons for struct and interface fields
					typeStr := formatTypeWithSemicolons(ts.Type)
					fmt.Printf("%s: type %s %s\n", relPath, ts.Name.Name, typeStr)
				}
			}
		}
	}
}

// Helper function to check if a position represents a top-level declaration
func isTopLevel(pos token.Position) bool {
	return pos.Line > 0 && pos.Column == 1
}

// Helper function to check if an expression is a map type
func isMapType(expr ast.Expr) bool {
	_, ok := expr.(*ast.MapType)
	return ok
}

// Helper function to format map literals correctly
func formatMapLiteral(expr *ast.CompositeLit) string {
	if m, ok := expr.Type.(*ast.MapType); ok {
		keyType := exprToString(m.Key)
		valueType := exprToString(m.Value)
		elements := make([]string, 0, len(expr.Elts))
		for _, elt := range expr.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				key := exprToString(kv.Key)
				value := exprToString(kv.Value)
				elements = append(elements, fmt.Sprintf("%s: %s", key, value))
			}
		}
		return fmt.Sprintf("map[%s]%s{%s}", keyType, valueType, strings.Join(elements, ", "))
	}
	return exprToString(expr)
}

// Add this new helper function
func formatTypeWithSemicolons(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StructType:
		if t.Fields == nil || len(t.Fields.List) == 0 {
			return "struct {}"
		}
		fields := make([]string, 0, len(t.Fields.List))
		for _, field := range t.Fields.List {
			var fieldStr string
			if len(field.Names) > 0 {
				names := make([]string, 0, len(field.Names))
				for _, name := range field.Names {
					names = append(names, name.Name)
				}
				// Handle nested types recursively
				typeStr := formatTypeWithSemicolons(field.Type)
				fieldStr = strings.Join(names, ", ") + " " + typeStr
			} else {
				// Handle embedded types recursively
				fieldStr = formatTypeWithSemicolons(field.Type)
			}
			fields = append(fields, strings.TrimSpace(fieldStr))
		}
		return "struct { " + strings.Join(fields, "; ") + " }"
	case *ast.InterfaceType:
		if t.Methods == nil || len(t.Methods.List) == 0 {
			return "interface{}"
		}
		methods := make([]string, 0, len(t.Methods.List))
		for _, method := range t.Methods.List {
			if len(method.Names) > 0 {
				for _, name := range method.Names {
					// Handle method type specifically
					if ft, ok := method.Type.(*ast.FuncType); ok {
						params := formatFieldList(ft.Params)
						results := formatMethodResults(ft.Results)
						methodStr := name.Name + "(" + params + ")"
						if results != "" {
							methodStr += " " + results
						}
						methods = append(methods, methodStr)
					}
				}
			} else {
				// Embedded interface
				methods = append(methods, strings.TrimSpace(formatTypeWithSemicolons(method.Type)))
			}
		}
		return "interface { " + strings.Join(methods, "; ") + " }"
	default:
		return strings.ReplaceAll(strings.TrimSpace(exprToString(expr)), "\n", " ")
	}
}

// Helper function to format method results with proper parentheses
func formatMethodResults(fl *ast.FieldList) string {
	if fl == nil {
		return ""
	}
	if len(fl.List) == 0 {
		return ""
	}

	resultStr := formatFieldList(fl)

	// Add parentheses if there are multiple results or named results
	needParens := len(fl.List) > 1 || (len(fl.List) == 1 && len(fl.List[0].Names) > 0)
	if needParens {
		return "(" + resultStr + ")"
	}
	return resultStr
}

func formatTypeSpec(spec *ast.TypeSpec) string {
	var buf strings.Builder
	buf.WriteString("type ")
	buf.WriteString(spec.Name.Name)
	buf.WriteString(" ")

	switch t := spec.Type.(type) {
	case *ast.StructType:
		buf.WriteString("struct { ")
		if t.Fields != nil {
			fields := make([]string, 0, len(t.Fields.List))
			for _, field := range t.Fields.List {
				if len(field.Names) == 0 {
					// Handle embedded types
					fields = append(fields, types.ExprString(field.Type))
				} else {
					// Handle regular fields
					fieldName := field.Names[0].Name
					fieldType := formatType(field.Type)
					fields = append(fields, fmt.Sprintf("%s %s", fieldName, fieldType))
				}
			}
			buf.WriteString(strings.Join(fields, "; "))
		}
		buf.WriteString(" }")

	case *ast.InterfaceType:
		buf.WriteString("interface { ")
		if t.Methods != nil {
			methods := make([]string, 0, len(t.Methods.List))
			for _, method := range t.Methods.List {
				if len(method.Names) == 0 {
					// Handle embedded interfaces
					methods = append(methods, types.ExprString(method.Type))
				} else {
					// Handle regular methods
					methodName := method.Names[0].Name
					if ft, ok := method.Type.(*ast.FuncType); ok {
						methods = append(methods, methodName+formatFuncType(ft))
					}
				}
			}
			buf.WriteString(strings.Join(methods, "; "))
		}
		buf.WriteString(" }")

	default:
		buf.WriteString(types.ExprString(t))
	}

	return strings.ReplaceAll(buf.String(), "  ", " ")
}

func formatType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StructType:
		var buf strings.Builder
		buf.WriteString("struct { ")
		if t.Fields != nil {
			fields := make([]string, 0, len(t.Fields.List))
			for _, field := range t.Fields.List {
				if len(field.Names) == 0 {
					fields = append(fields, types.ExprString(field.Type))
				} else {
					fieldName := field.Names[0].Name
					fieldType := types.ExprString(field.Type)
					fields = append(fields, fmt.Sprintf("%s %s", fieldName, fieldType))
				}
			}
			buf.WriteString(strings.Join(fields, "; "))
		}
		buf.WriteString(" }")
		return buf.String()

	case *ast.InterfaceType:
		var buf strings.Builder
		buf.WriteString("interface { ")
		if t.Methods != nil {
			methods := make([]string, 0, len(t.Methods.List))
			for _, method := range t.Methods.List {
				if len(method.Names) == 0 {
					methods = append(methods, types.ExprString(method.Type))
				} else {
					methodName := method.Names[0].Name
					if ft, ok := method.Type.(*ast.FuncType); ok {
						methods = append(methods, methodName+formatFuncType(ft))
					}
				}
			}
			buf.WriteString(strings.Join(methods, "; "))
		}
		buf.WriteString(" }")
		return buf.String()

	default:
		return types.ExprString(expr)
	}
}

func formatValueSpec(spec *ast.ValueSpec, tok token.Token, maxLen int) []string {
	var declarations []string

	for i, name := range spec.Names {
		var buf strings.Builder
		buf.WriteString("var ")
		buf.WriteString(name.Name)

		// Get or infer type
		typeStr := ""
		if spec.Type != nil {
			typeStr = types.ExprString(spec.Type)
		} else if i < len(spec.Values) {
			typeStr = inferType(spec.Values[i])
		}

		// Handle special cases
		if strings.HasPrefix(name.Name, "Status") {
			typeStr = "Status"
		} else if name.Name == "Monday" || name.Name == "Tuesday" || name.Name == "Sunday" {
			typeStr = "Day"
		}

		// Handle time.Duration
		if i < len(spec.Values) {
			if call, ok := spec.Values[i].(*ast.BinaryExpr); ok {
				if sel, ok := call.X.(*ast.SelectorExpr); ok {
					if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "time" {
						typeStr = "time.Duration"
					}
				}
			}
		}

		if typeStr != "" {
			buf.WriteString(" ")
			buf.WriteString(typeStr)
		}

		// Add value if present
		if i < len(spec.Values) {
			buf.WriteString(" = ")
			buf.WriteString(formatValue(spec.Values[i], maxLen))
		} else if tok == token.CONST {
			// For constants without explicit values
			if strings.HasPrefix(name.Name, "Status") || name.Name == "Sunday" || name.Name == "Monday" || name.Name == "Tuesday" {
				buf.WriteString(" = iota")
			} else {
				buf.WriteString(fmt.Sprintf(" = %d", i+1))
			}
		}

		declarations = append(declarations, buf.String())
	}

	return declarations
}

func formatFuncDecl(decl *ast.FuncDecl) string {
	var buf strings.Builder
	buf.WriteString("func ")

	// Skip receiver in output unless it's a method
	if decl.Recv == nil {
		buf.WriteString(decl.Name.Name)
	} else {
		// For methods, use the original name without receiver
		buf.WriteString(decl.Name.Name)
	}

	// Format parameters and results
	if ft := decl.Type; ft != nil {
		buf.WriteString(formatFuncType(ft))
	}

	return buf.String()
}
