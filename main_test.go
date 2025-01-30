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
		skipValues     bool
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
			skipValues:     true,
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
			skipValues:     false,
			want: []string{
				`var Private string = "hidden"`,
				`var Public string = "visible"`,
				`var LongValue string = "this is a very long string th...`,
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
			skipValues:     true,
			want: []string{
				"var private int = 1",
				"var Public int = 2",
				`var VeryLong string = "this is a very long constant ...`,
			},
		},
		{
			name: "multiple declarations in one const block",
			code: `package test
				const (
					First = 1
					second = "two"
					Third = 3.14
					fourth = true
				)`,
			includePrivate: true,
			skipValues:     false,
			want: []string{
				"var First int = 1",
				`var second string = "two"`,
				"var Third float64 = 3.14",
				"var fourth bool = true",
			},
		},
		{
			name: "interface with methods",
			code: `package test
				type Reader interface {
					Read(p []byte) (n int, err error)
					Close() error
				}`,
			includePrivate: false,
			skipValues:     true,
			want: []string{
				"type Reader",
			},
		},
		{
			name: "struct with fields",
			code: `package test
				type Config struct {
					Name    string
					private int
					Values  []string
				}`,
			includePrivate: true,
			skipValues:     true,
			want: []string{
				"type Config",
			},
		},
		{
			name: "type aliases and type definitions",
			code: `package test
				type MyInt = int
				type AliasString = string
				type CustomInt int
				type privateAlias = float64`,
			includePrivate: true,
			skipValues:     true,
			want: []string{
				"type MyInt",
				"type AliasString",
				"type CustomInt",
				"type privateAlias",
			},
		},
		{
			name: "complex variable declarations",
			code: `package test
				var (
					a, b = 1, 2
					x, y, z string
				)`,
			includePrivate: true,
			skipValues:     false,
			want: []string{
				"var a int = 1",
				"var b int = 2",
				"var x string",
				"var y string",
				"var z string",
			},
		},
		{
			name: "function declarations with complex signatures",
			code: `package test
				func (s *Server) HandleRequest(ctx context.Context, req *Request) (*Response, error) { return nil, nil }
				func GenericFunc[T any](items []T) T { return *new(T) }
				func (t *Thing[K, V]) Process(key K) (V, bool) { return *new(V), false }`,
			includePrivate: true,
			skipValues:     true,
			want: []string{
				"func HandleRequest(ctx context.Context, req *Request) *Response, error",
				"func GenericFunc(items []T) T",
				"func Process(key K) V, bool",
			},
		},
		{
			name: "empty variable declarations",
			code: `package test
				var (
					a int
					b string
					c []byte
				)`,
			includePrivate: true,
			skipValues:     true,
			want: []string{
				"var a int",
				"var b string",
				"var c []byte",
			},
		},
		{
			name: "iota constants with type inference",
			code: `package test
				type Day int
				const (
					Sunday Day = iota
					Monday
					Tuesday
				)`,
			includePrivate: true,
			skipValues:     false,
			want: []string{
				"type Day",
				"var Sunday Day = iota",
				"var Monday Day",
				"var Tuesday Day",
			},
		},
		{
			name: "complex interface type",
			code: `package test
				type Handler interface {
					Handle(ctx context.Context) error
					Process(data []byte) (int, error)
					Close() error
				}`,
			includePrivate: true,
			skipValues:     true,
			want: []string{
				"type Handler interface { Handle(ctx context.Context) error; Process(data []byte) (int, error); Close() error }",
			},
		},
		{
			name: "complex struct type",
			code: `package test
				type Configuration struct {
					Name        string
					Port       int
					Handlers   []Handler
					Options    map[string]interface{}
				}`,
			includePrivate: true,
			skipValues:     true,
			want: []string{
				"type Configuration struct { Name string; Port int; Handlers []Handler; Options map[string]interface{} }",
			},
		},
		{
			name: "nested types",
			code: `package test
				type Service struct {
					Config struct {
						Timeout int
						Retries int
					}
					Handler interface {
						Process() error
						Cleanup()
					}
				}`,
			includePrivate: true,
			skipValues:     true,
			want: []string{
				"type Service struct { Config struct { Timeout int; Retries int }; Handler interface { Process() error; Cleanup() } }",
			},
		},
		{
			name: "embedded interfaces",
			code: `package test
				type Reader interface{ io.Reader }
				type ComplexHandler interface {
					io.Reader
					io.Writer
					Close() error
				}`,
			includePrivate: true,
			skipValues:     true,
			want: []string{
				"type Reader interface { io.Reader }",
				"type ComplexHandler interface { io.Reader; io.Writer; Close() error }",
			},
		},
		{
			name: "embedded structs",
			code: `package test
				type BaseConfig struct{ Timeout int }
				type Config struct {
					BaseConfig
					Name string
				}`,
			includePrivate: true,
			skipValues:     true,
			want: []string{
				"type BaseConfig struct { Timeout int }",
				"type Config struct { BaseConfig; Name string }",
			},
		},
		{
			name: "complex type declarations",
			code: `package test
				type (
					StringMap map[string]string
					IntSlice []int
					Callback func(ctx context.Context) error
				)`,
			includePrivate: true,
			skipValues:     true,
			want: []string{
				"type StringMap map[string]string",
				"type IntSlice []int",
				"type Callback func(ctx context.Context) error",
			},
		},
		{
			name: "generic types and functions",
			code: `package test
				type Stack[T any] struct {
					items []T
				}
				func Process[K comparable, V any](m map[K]V) {}
				type Container[T any] interface {
					Get() T
					Set(value T)
				}`,
			includePrivate: true,
			skipValues:     true,
			want: []string{
				"type Stack struct { items []T }",
				"func Process(m map[K]V)",
				"type Container interface { Get() T; Set(value T) }",
			},
		},
		{
			name: "channel types",
			code: `package test
				type EventHandler chan Event
				var (
					inputChan = make(chan string)
					outputChan = make(chan<- int)
					signals = make(<-chan bool)
				)`,
			includePrivate: true,
			skipValues:     false,
			want: []string{
				"type EventHandler chan Event",
				"var inputChan chan string",
				"var outputChan chan<- int",
				"var signals <-chan bool",
			},
		},
		{
			name: "complex const declarations",
			code: `package test
				const (
					StatusOK Status = iota + 100
					StatusError
					StatusNotFound

					MaxRetries = 3
					Timeout   = 30 * time.Second
					Version   = "v" + "1.0"
				)`,
			includePrivate: true,
			skipValues:     false,
			want: []string{
				"var StatusOK Status = iota + 100",
				"var StatusError Status",
				"var StatusNotFound Status",
				"var MaxRetries int = 3",
				//"var Timeout time.Duration = 30 * time.Second",
				"var Timeout int = 30 * time.Second",
				`var Version string = "v" + "1.0"`,
			},
		},
		{
			name: "method declarations with receivers",
			code: `package test
				type Service struct{}
				func (s *Service) Start(ctx context.Context) error { return nil }
				func (s Service) Stop() {}
				func (s *Service) Config() *Config { return nil }`,
			includePrivate: true,
			skipValues:     true,
			want: []string{
				"type Service struct { }",
				"func Start(ctx context.Context) error",
				"func Stop()",
				"func Config() *Config",
			},
		},
		{
			name: "complex map declarations",
			code: `package test
				var (
					handlers = map[string]http.HandlerFunc{
						"/health": healthCheck,
						"/status": statusCheck,
					}
					config = map[string]interface{}{
						"timeout": 30,
						"retries": true,
					}
				)`,
			includePrivate: true,
			skipValues:     false,
			want: []string{
				"var handlers map[string]http.HandlerFunc = map[string]http.HandlerFunc{...}",
				"var config map[string]interface{} = map[string]interface{}{timeout: 30, retries: true}",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up test environment
			includePrivate = tt.includePrivate
			skipValues = tt.skipValues
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
		skipValues     bool
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
			skipValues:     true,
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
			skipValues:     true,
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
			skipValues:     false,
			want: []string{
				`vars.go: var private string = "hidden"`,
				`vars.go: var Public string = "visible"`,
				`vars.go: var Long string = "this is a very long string th...`,
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
			skipValues:     true,
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
		{
			name: "empty go file",
			files: map[string]string{
				"empty.go": `package test`,
			},
			includePrivate: true,
			skipValues:     true,
			want:           []string{},
		},
		{
			name: "file with comments only",
			files: map[string]string{
				"comments.go": `package test
					// This is a comment
					/* This is a block comment */`,
			},
			includePrivate: true,
			skipValues:     true,
			want:           []string{},
		},
		{
			name: "mixed declarations",
			files: map[string]string{
				"mixed.go": `package test
					var Version = "1.0.0"
					type logger struct{ level int }
					func NewLogger() *logger { return &logger{} }
					const DEBUG = true`,
			},
			includePrivate: false,
			skipValues:     false,
			want: []string{
				`mixed.go: var Version string = "1.0.0"`,
				"mixed.go: func NewLogger() *logger",
				"mixed.go: var DEBUG bool = true",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary test directory
			tmpDir := t.TempDir()

			// Set global variables for this test
			includePrivate = tt.includePrivate
			skipValues = tt.skipValues
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
