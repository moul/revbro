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
	"strings"
)

var (
	includePrivate bool
	includeValues  bool
	maxValueLength int
	fileExtensions string
	workDir        string
)

func main() {
	// Get working directory for relative path calculations
	var err error             // declare err separately
	workDir, err = os.Getwd() // use existing global workDir
	if err != nil {
		fmt.Printf("Error getting working directory: %v\n", err)
		os.Exit(1)
	}

	// Command-line arguments
	flag.BoolVar(&includePrivate, "private", false, "include private (unexported) declarations")
	flag.BoolVar(&includeValues, "values", false, "include right-hand side values")
	flag.IntVar(&maxValueLength, "max-length", 30, "maximum length for displayed values before truncating")
	flag.StringVar(&fileExtensions, "ext", ".go", "comma-separated list of file extensions to process (e.g., .go,.gno)")
	flag.Parse()

	// Get file paths from arguments
	paths := flag.Args()
	if len(paths) == 0 {
		fmt.Println("Usage: go run main.go [flags] <path1> <path2> ...")
		fmt.Println("\nPaths can be files, directories, or ./... for recursive scanning")
		flag.PrintDefaults()
		os.Exit(1)
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
				fmt.Printf("Invalid path %s: %v\n", path, err)
				continue
			}
			err = processPath(absPath, fset)
			if err != nil {
				fmt.Println(err)
			}
		} else {
			absPath, err := filepath.Abs(path)
			if err != nil {
				fmt.Printf("Invalid path %s: %v\n", path, err)
				continue
			}
			err = processPath(absPath, fset)
			if err != nil {
				fmt.Println(err)
			}
		}
	}
}

// Process a single Go file and extract declarations
func processFile(filePath string, fset *token.FileSet) error {
	// Parse the file
	node, err := parser.ParseFile(fset, filePath, nil, parser.AllErrors)
	if err != nil {
		return fmt.Errorf("error parsing file %s: %v", filePath, err)
	}

	// Walk through the AST and extract declarations
	ast.Inspect(node, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.GenDecl:
			processGenDecl(filePath, decl)
		case *ast.FuncDecl:
			processFuncDecl(filePath, decl)
		}
		return true
	})

	return nil
}

// Process generic declarations (const, var, type)
func processGenDecl(filePath string, decl *ast.GenDecl) {
	// Get relative path
	relPath, err := filepath.Rel(workDir, filePath)
	if err != nil {
		relPath = filePath // fallback to absolute path if relative path calculation fails
	}

	for _, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.ValueSpec:
			for i, name := range s.Names {
				if includePrivate || name.IsExported() {
					val := ""
					if includeValues && len(s.Values) > i {
						val = fmt.Sprintf(" = %s", formatValue(exprToString(s.Values[i])))
					}
					fmt.Printf("%s: var %s%s\n", relPath, name.Name, val)
				}
			}
		case *ast.TypeSpec:
			if includePrivate || s.Name.IsExported() {
				fmt.Printf("%s: type %s\n", relPath, s.Name.Name)
			}
		}
	}
}

// Process function declarations
func processFuncDecl(filePath string, decl *ast.FuncDecl) {
	// Get relative path
	relPath, err := filepath.Rel(workDir, filePath)
	if err != nil {
		relPath = filePath // fallback to absolute path if relative path calculation fails
	}

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

	if fileInfo.IsDir() {
		return filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				// Check if file has any of the specified extensions
				for _, ext := range extensions {
					if strings.HasSuffix(strings.ToLower(path), strings.ToLower(ext)) {
						return processFile(path, fset)
					}
				}
			}
			return nil
		})
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
		typeStr := exprToString(field.Type)
		for _, name := range field.Names {
			parts = append(parts, fmt.Sprintf("%s %s", name.Name, typeStr))
		}
		if len(field.Names) == 0 {
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
