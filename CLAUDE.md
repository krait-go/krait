# CLAUDE.md

## Project: krait — Unified Go codebase health analyzer

### Build & Test
- `go build ./cmd/krait/` — build the binary
- `go test ./...` — run all tests
- `go vet ./...` — vet the code
- `./krait check` — dog-food: run krait on itself

### Architecture
- `cmd/krait/` — CLI entry point only, no business logic
- `pkg/analyzer/` — shared types (Finding, Result, Config, Analyzer interface)
- `pkg/{deadcode,duplication,complexity,architecture,deps}/` — one package per analyzer, each implements the Analyzer interface
- `pkg/reporter/` — output formatting (JSON, SARIF, text, markdown)
- `pkg/config/` — config file loading
- `internal/parser/` — shared AST parsing utilities used by all analyzers

### Key types
- `analyzer.Analyzer` interface: `Name() string`, `Description() string`, `Analyze(project *Project, cfg *Config) (*Result, error)`
- `analyzer.Finding`: rule, category, severity, message, location, related_locations, meta
- `analyzer.Result`: analyzer name, duration, findings slice, stats map

### Git
- Never add `Co-Authored-By` lines to commit messages

### Adding a new analyzer
1. Create `pkg/newanalyzer/newanalyzer.go`
2. Implement the `analyzer.Analyzer` interface
3. Add it to `allAnalyzers()` in `cmd/krait/main.go`
4. Add a subcommand in the commands slice
5. Add test fixtures in `testdata/`
