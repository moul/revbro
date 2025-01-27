package main

import (
	"bytes"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Helper function to capture stdout during tests
func captureOutput(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

// createTestFile creates a temporary Go file with given content
func createTestFile(t *testing.T, content string) string {
	tmpDir := t.TempDir()
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
				t.Errorf("got %d lines, want %d lines", len(lines), len(tt.want))
			}

			// Check each line contains expected content
			for i, want := range tt.want {
				if i >= len(lines) {
					break
				}
				// Strip the file path from the output line
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
	// Set up test environment with default flags
	includePrivate = false // Only show exported declarations
	includeValues = false  // Don't show values
	maxValueLength = 30    // Set max value length

	// Set working directory to the temporary test directory
	tmpDir := t.TempDir()
	workDir = tmpDir // Set the global workDir

	subDir := filepath.Join(tmpDir, "subdir")
	err := os.Mkdir(subDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Create test files with proper package declarations and formatting
	files := map[string]string{
		filepath.Join(tmpDir, "main.go"): `package main

func Main() {}`,
		filepath.Join(subDir, "helper.go"): `package helper

type Helper struct{}`,
		filepath.Join(tmpDir, "notgo.txt"): "not a go file",
	}

	for path, content := range files {
		err := os.WriteFile(path, []byte(content), 0644)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Test processing directory
	output := captureOutput(func() {
		fset := token.NewFileSet()
		err := processPath(tmpDir, fset)
		if err != nil {
			t.Fatal(err)
		}
	})

	// Debug: Print the actual output
	t.Logf("Captured output:\n%s", output)

	// Verify output contains expected declarations
	lines := strings.Split(strings.TrimSpace(output), "\n")
	foundMain := false
	foundHelper := false

	for _, line := range lines {
		// Debug: Print each line being checked
		t.Logf("Checking line: %q", line)

		// Strip the file path from the output line
		if idx := strings.LastIndex(line, ": "); idx != -1 {
			line = line[idx+2:]
		}
		// Debug: Print stripped line
		t.Logf("Stripped line: %q", line)

		if strings.Contains(line, "func Main()") {
			foundMain = true
		}
		if strings.Contains(line, "type Helper struct") {
			foundHelper = true
		}
	}

	if !foundMain {
		t.Error("missing Main function declaration")
	}
	if !foundHelper {
		t.Error("missing Helper type declaration")
	}
}
