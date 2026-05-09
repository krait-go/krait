# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Dead code analyzer: detects unused exported functions, methods, types, variables, and constants across packages
- Duplication analyzer: AST-based clone detection that catches renamed variables and normalized literals
- Complexity analyzer: cyclomatic and cognitive complexity scoring with configurable thresholds
- Architecture analyzer: layer violation detection with coupling metrics and instability scores
- Dependency analyzer: detects unused go.mod dependencies and unlisted imports
- Four output formats: text, JSON, SARIF 2.1, and GitHub-flavored markdown
- Configuration via `.krait.json` with JSONC comment support
- `krait init` command for config file scaffolding
- `krait check` as the default command running all analyzers
- Individual analyzer commands: `dead`, `dupes`, `complexity`, `arch`, `deps`
- CI mode with `--ci` flag (JSON output, exit 1 on error-severity findings)
- Threshold-based exit codes with `--threshold` flag
- Test file inclusion with `--tests` flag
- JSON Schema for `.krait.json` editor autocompletion
- GitHub Action for CI integration
