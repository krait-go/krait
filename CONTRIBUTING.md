# Contributing to krait

Thank you for your interest in contributing. This document explains how to get set up, how the project is structured, and what we expect from contributions.

---

## Prerequisites

- **Go 1.23 or later** — the module is declared at `go 1.23.0`
- **Task** (optional but recommended) — `brew install go-task` or see [taskfile.dev](https://taskfile.dev)

If you do not have Task installed, all commands have plain `go` equivalents listed below.

---

## Getting Started

```bash
git clone https://github.com/krait-go/krait.git
cd krait

# Verify everything builds and tests pass
go build ./...
go test ./...
go vet ./...
```

Or, with Task:

```bash
task check
```

Dog-food the tool on itself:

```bash
go build -o krait ./cmd/krait/
./krait check
```

---

## Development Workflow

| Task command | Equivalent | Description |
|---|---|---|
| `task build` | `go build ./cmd/krait/` | Build the binary |
| `task test` | `go test ./...` | Run all tests |
| `task lint` | `go vet ./... && goimports -l .` | Vet and check imports |
| `task check` | build + test + lint | Full pre-push check |

For a fast feedback loop while working on a single package:

```bash
go test ./pkg/complexity/... -v -run TestComplexity
```

---

## Project Structure

```
krait/
├── cmd/
│   └── krait/
│       └── main.go          # CLI entry point — flags, commands, wiring only
├── internal/
│   └── parser/
│       └── parser.go        # Shared AST parsing, produces *analyzer.Project
├── pkg/
│   ├── analyzer/
│   │   ├── analyzer.go      # Analyzer interface, Finding, Result, Config, Report types
│   │   └── project.go       # Project type — parsed ASTs for all in-scope files
│   ├── architecture/        # Layer violation and coupling metrics analyzer
│   ├── complexity/          # Cyclomatic and cognitive complexity analyzer
│   ├── deadcode/            # Unused exported symbol analyzer
│   ├── deps/                # Dependency hygiene analyzer
│   ├── duplication/         # AST-based code clone detection analyzer
│   ├── config/              # Config file loading (.krait.json, .krait.jsonc)
│   └── reporter/            # Output formatters: text, JSON, SARIF, markdown
└── testdata/                # Fixture projects used by analyzer tests
    ├── simple/
    └── clean-arch/
```

**Key constraint:** `cmd/krait/main.go` wires everything together but contains no analysis logic. All business logic lives in `pkg/` or `internal/`. Keep it that way.

---

## How to Add a New Analyzer

1. **Create the package**

   ```bash
   mkdir pkg/myanalyzer
   touch pkg/myanalyzer/myanalyzer.go
   touch pkg/myanalyzer/myanalyzer_test.go
   ```

2. **Implement the interface**

   ```go
   package myanalyzer

   import "github.com/krait-go/krait/pkg/analyzer"

   type Analyzer struct{}

   func New() *Analyzer { return &Analyzer{} }

   func (a *Analyzer) Name() string        { return "my-analyzer" }
   func (a *Analyzer) Description() string { return "Detects ..." }

   func (a *Analyzer) Analyze(project *analyzer.Project, cfg *analyzer.Config) (*analyzer.Result, error) {
       result := &analyzer.Result{Analyzer: a.Name()}
       // ... build result.Findings
       return result, nil
   }
   ```

3. **Register it in `cmd/krait/main.go`**

   Add `myanalyzer.New()` to the slice returned by `allAnalyzers()`, and add a dedicated subcommand to the `Commands` slice.

4. **Add test fixtures**

   Create `testdata/myanalyzer/` with minimal Go source files that exercise both the happy path and violation cases. Reference them from `myanalyzer_test.go`.

5. **Update the config defaults** if the analyzer needs thresholds or rule names. Add them to `DefaultConfig()` and `defaultRules()` in `pkg/analyzer/analyzer.go`, and update `Validate()` in `pkg/config/config.go`.

---

## Commit Convention

We use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add my-analyzer for detecting X
fix: handle nil pointer in duplication scanner
docs: update CONTRIBUTING with task commands
refactor: simplify complexity threshold logic
test: add edge case for empty file in dead code analyzer
chore: bump Go version to 1.24
```

The type must be one of: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`.

Include a body when the change is non-obvious — explain **why**, not what.

---

## Pull Request Guidelines

- CI must pass — `go build`, `go test ./...`, and `go vet ./...` must all be green
- Every new exported function or analyzer method requires a test
- One logical change per PR — split unrelated changes into separate PRs
- For new analyzers, include at least one test fixture project in `testdata/`
- Update `CHANGELOG.md` under `## [Unreleased]` with a one-line description of the change
- Do not bump version numbers in PRs — releases are handled by maintainers

---

## Code Style

- Follow the patterns already established in the package you are modifying
- Use `goimports` with the local module prefix: `goimports -local github.com/krait-go/krait`
- Wrap errors with context: `fmt.Errorf("analyzing file %s: %w", path, err)`
- Do not add comments that restate the code — comments should explain *why*, not *what*
- Keep `cmd/krait/main.go` thin — if you find yourself adding logic there, it belongs in a package

---

## Reporting Issues

Before opening an issue:
- Check existing issues to avoid duplicates
- If you found a bug, include the krait version (`krait --version`), your Go version, and the smallest reproduction case you can create

For security vulnerabilities, do not open a public issue — see [SECURITY.md](SECURITY.md).
