package main

import (
	"bytes"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Helper function to capture stdout during tests
func captureOutput(fn func()) string {
	// Create a pipe
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}

	// Save the original stdout
	stdout := os.Stdout

	// Set stdout to our pipe
	os.Stdout = w

	// Create a channel to ensure we get all output
	outputChan := make(chan string)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		outputChan <- buf.String()
	}()

	// Run the function
	fn()

	// Restore stdout and close the write end of the pipe
	os.Stdout = stdout
	w.Close()

	// Wait for the goroutine to finish and get the output
	output := <-outputChan

	// Close the read end of the pipe
	r.Close()

	return output
}

// createTestFile creates a temporary Go file with given content
func createTestFile(t *testing.T, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	workDir = tmpDir // Set workDir for relative path calculations

	filename := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(filename, []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestProcessFile(t *testing.T) {
	tests := []struct {
		name           string
		code           string
		includePrivate bool
		includeValues  bool
		want           []string
	}{
		{
			name: "exported functions and types",
			code: `package test
				func privateFunc() {}
				func PublicFunc(a string) int { return 0 }
				type privateType struct{}
				type PublicType interface{}`,
			includePrivate: false,
			includeValues:  false,
			want: []string{
				"func PublicFunc(a string) int",
				"type PublicType",
			},
		},
		{
			name: "variables with values",
			code: `package test
				var Private = "hidden"
				var Public = "visible"
				var LongValue = "this is a very long string that should be truncated"`,
			includePrivate: true,
			includeValues:  true,
			want: []string{
				"var Private = \"hidden\"",
				"var Public = \"visible\"",
				"var LongValue = \"this is a very long string th...",
			},
		},
		{
			name: "const declarations",
			code: `package test
				const (
					private = 1
					Public = 2
					VeryLong = "this is a very long constant value that should be truncated"
				)`,
			includePrivate: true,
			includeValues:  true,
			want: []string{
				"var private = 1",
				"var Public = 2",
				"var VeryLong = \"this is a very long constant ...",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up test environment
			includePrivate = tt.includePrivate
			includeValues = tt.includeValues
			maxValueLength = 30

			// Create temporary test file
			filename := createTestFile(t, tt.code)

			// Capture output
			output := captureOutput(func() {
				fset := token.NewFileSet()
				err := processFile(filename, fset)
				if err != nil {
					t.Fatal(err)
				}
			})

			// Process output
			lines := strings.Split(strings.TrimSpace(output), "\n")
			if len(lines) != len(tt.want) {
				t.Errorf("got %d lines, want %d lines\nOutput:\n%s", len(lines), len(tt.want), output)
				return
			}

			// Check each line contains expected content
			for i, want := range tt.want {
				if i >= len(lines) {
					break
				}
				got := lines[i]
				if idx := strings.LastIndex(got, ": "); idx != -1 {
					got = got[idx+2:]
				}
				if !strings.Contains(got, want) {
					t.Errorf("line %d:\ngot:  %s\nwant: %s", i, got, want)
				}
			}
		})
	}
}

func TestProcessPath(t *testing.T) {
	tests := []struct {
		name           string
		files          map[string]string
		includePrivate bool
		includeValues  bool
		want           []string
		wantErr        bool
	}{
		{
			name: "basic exported declarations",
			files: map[string]string{
				"main.go": `package main
					func Main() {}`,
				"subdir/helper.go": `package helper
					type Helper struct{}`,
				"notgo.txt": "not a go file",
			},
			includePrivate: false,
			includeValues:  false,
			want: []string{
				"main.go: func Main()",
				"subdir/helper.go: type Helper",
			},
		},
		{
			name: "private and public declarations",
			files: map[string]string{
				"code.go": `package test
					func private() {}
					func Public() {}
					type private struct{}
					type Public struct{}`,
			},
			includePrivate: true,
			includeValues:  false,
			want: []string{
				"code.go: func private()",
				"code.go: func Public()",
				"code.go: type private",
				"code.go: type Public",
			},
		},
		{
			name: "variables with values",
			files: map[string]string{
				"vars.go": `package test
					var private = "hidden"
					var Public = "visible"
					const Long = "this is a very long string that should be truncated"`,
			},
			includePrivate: true,
			includeValues:  true,
			want: []string{
				`vars.go: var private = "hidden"`,
				`vars.go: var Public = "visible"`,
				`vars.go: var Long = "this is a very long string th...`,
			},
		},
		{
			name: "multiple files in different directories",
			files: map[string]string{
				"pkg1/file1.go": `package pkg1
					func Func1() {}`,
				"pkg1/file2.go": `package pkg1
					func Func2() {}`,
				"pkg2/file.go": `package pkg2
					type Type1 struct{}`,
			},
			includePrivate: false,
			includeValues:  false,
			want: []string{
				"pkg1/file1.go: func Func1()",
				"pkg1/file2.go: func Func2()",
				"pkg2/file.go: type Type1",
			},
		},
		{
			name: "invalid go file",
			files: map[string]string{
				"invalid.go": `package test
					this is not valid go code`,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary test directory
			tmpDir := t.TempDir()

			// Set global variables for this test
			includePrivate = tt.includePrivate
			includeValues = tt.includeValues
			maxValueLength = 30
			workDir = tmpDir

			// Create test files
			createdFiles := make([]string, 0, len(tt.files))
			for path, content := range tt.files {
				fullPath := filepath.Join(tmpDir, path)
				dir := filepath.Dir(fullPath)
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				err := os.WriteFile(fullPath, []byte(content), 0644)
				if err != nil {
					t.Fatal(err)
				}
				createdFiles = append(createdFiles, fullPath)
			}

			// Debug info
			t.Logf("Working directory: %s", workDir)
			t.Logf("Created files: %v", createdFiles)

			// Run the test
			var gotErr error
			output := captureOutput(func() {
				fset := token.NewFileSet()
				// Process each file individually
				for _, file := range createdFiles {
					if strings.HasSuffix(file, ".go") {
						err := processFile(file, fset)
						if err != nil {
							gotErr = err
							return
						}
					}
				}
			})

			// Check error cases
			if (gotErr != nil) != tt.wantErr {
				t.Errorf("processPath() error = %v, wantErr %v", gotErr, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			// Split output into lines and clean
			lines := strings.Split(strings.TrimSpace(output), "\n")
			if len(lines) == 1 && lines[0] == "" {
				lines = nil
			}

			// Verify output
			if len(lines) != len(tt.want) {
				t.Errorf("got %d lines, want %d lines\nOutput:\n%s\nWant:\n%s",
					len(lines), len(tt.want), output, strings.Join(tt.want, "\n"))
				return
			}

			// Sort both slices to ensure consistent ordering
			sort.Strings(lines)
			want := make([]string, len(tt.want))
			copy(want, tt.want)
			sort.Strings(want)

			// Compare each line
			for i := range lines {
				if !strings.Contains(lines[i], want[i]) {
					t.Errorf("line mismatch:\ngot:  %s\nwant: %s", lines[i], want[i])
				}
			}
		})
	}
}
