package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
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
func processFile(filePath string, fset *token.FileSet) error {
	// Parse the file
	node, err := parser.ParseFile(fset, filePath, nil, parser.AllErrors)
	if err != nil {
		return fmt.Errorf("error parsing file %s: %v", filePath, err)
	}

	// Get relative path once for all declarations
	relPath, err := filepath.Rel(workDir, filePath)
	if err != nil {
		relPath = filePath // fallback to absolute path if relative path calculation fails
	}

	// Walk through the AST and extract declarations
	ast.Inspect(node, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.GenDecl:
			processGenDecl(decl, fset)
		case *ast.FuncDecl:
			if includePrivate || decl.Name.IsExported() {
				params := formatFieldList(decl.Type.Params)
				results := formatFieldList(decl.Type.Results)

				signature := fmt.Sprintf("func %s(%s)", decl.Name.Name, params)
				if results != "" {
					signature += " " + results
				}
				fmt.Printf("%s: %s\n", relPath, signature)
			}
		}
		return true
	})

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

// Helper to format values with ellipsis if too long
func formatValue(value string) string {
	if len(value) > maxValueLength {
		return value[:maxValueLength] + "..."
	}
	return value
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
			return "int"
		}
		// For enum types, return the identifier name as the type
		if v.Obj != nil && v.Obj.Kind == ast.Typ {
			return v.Name
		}
		return v.Name // Return the type name for custom types
	case *ast.CompositeLit:
		if t, ok := v.Type.(*ast.ArrayType); ok {
			elemType := exprToString(t.Elt)
			return "[]" + elemType
		}
		if m, ok := v.Type.(*ast.MapType); ok {
			keyType := exprToString(m.Key)
			valueType := exprToString(m.Value)
			return fmt.Sprintf("map[%s]%s", keyType, valueType)
		}
		if s, ok := v.Type.(*ast.StructType); ok {
			return "struct" + exprToString(s)
		}
		// For other composite literals, get the type string directly
		return exprToString(v.Type)
	case *ast.ArrayType:
		elemType := exprToString(v.Elt)
		return "[]" + elemType
	case *ast.MapType:
		keyType := exprToString(v.Key)
		valueType := exprToString(v.Value)
		return fmt.Sprintf("map[%s]%s", keyType, valueType)
	case *ast.SelectorExpr:
		// Handle package-qualified types (e.g., time.Time)
		return exprToString(expr)
	case *ast.StarExpr:
		// Handle pointer types
		return "*" + exprToString(v.X)
	case *ast.UnaryExpr:
		return inferType(v.X)
	case *ast.BinaryExpr:
		// For binary expressions (like iota + 1), use the left operand's type
		return inferType(v.X)
	}
	return "" // Return empty string if type cannot be inferred
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
