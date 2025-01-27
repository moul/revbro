# `revbro`

A command-line tool that helps explore and list declarations in Go source code.

## Installation

```bash
go install moul.io/revbro@latest
```

## Usage

```bash
# List only exported declarations
revbro path/to/code/...

# Include private declarations
revbro -private path/to/code/...
```
