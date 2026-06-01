# Contributing to gron

Thank you for your interest in contributing to **gron**! Whether you're fixing a bug, adding a feature, improving documentation, or reporting an issue, your help is deeply appreciated.

This guide will help you get started, understand our workflow, and ensure your contributions align with the project's goals.

---

## Code of Conduct

By participating in this project, you agree to abide by our [Code of Conduct](CODE_OF_CONDUCT.md). Please be respectful and constructive in all interactions.

---

## Getting Started

### Prerequisites

- **Go 1.21+** (or latest stable)
- **Git**
- **Make** (for local development commands)
- **golangci-lint** (optional, falls back to `go vet`)

### Setup

1. Fork the repository and clone it locally:

   ```bash
   git clone https://github.com/ismayelalam/gron.git
   cd gron
   ```

2. Install dependencies & verify setup:
   ```bash
   go mod tidy
   make help
   ```

---

## Reporting Issues

Before opening a new issue, please:

1. Search [existing issues](https://github.com/ismayelalam/gron/issues) to avoid duplicates.
2. Use our **[Bug Report](https://github.com/ismayelalam/gron/issues/new?template=bug_report.yml)** or **[Feature Request](https://github.com/ismayelalam/gron/issues/new?template=feature_request.yml)** template.
3. Include:
   - Go version & OS
   - Minimal reproducible example
   - Expected vs actual behavior
   - Logs/stack traces (if applicable)

---

## Development Workflow

### 1. Create a Branch

Use descriptive branch names:

```bash
git checkout -b feature/add-metrics
git checkout -b fix/cron-parser-range-bug
```

### 2. Write Code

- Follow idiomatic Go conventions (`effective_go`)
- Keep the package **zero-dependency** (no external imports allowed in `go.mod`)
- Use meaningful names, avoid over-engineering
- Add `//go:build` tags for platform-specific code

### 3. Run Local Checks

Before committing, run the full CI pipeline locally:

```bash
make precommit  # tidies, formats, vets, lints, and tests
```

### 4. Commit & Push

Use [Conventional Commits](https://www.conventionalcommits.org/) or clear imperative messages:

```bash
git commit -m "feat(parser): support step ranges in cron fields"
git push origin feature/add-metrics
```

### 5. Open a Pull Request

- Use the **[PR Template](.github/pull_request_template.md)**
- Link related issues (`Fixes #123`)
- Ensure all CI checks pass
- Request review from maintainers

---

## Code Standards

| Requirement       | Tool/Command                                           |
| ----------------- | ------------------------------------------------------ |
| Formatting        | `make fmt` (`gofmt` + `goimports`)                     |
| Static Analysis   | `make vet` + `make lint` (`golangci-lint`)             |
| Race Safety       | `go test -race ./...`                                  |
| Coverage          | `make cover` (aim for `>80%` on new code)              |
| Zero Dependencies | No new imports in `go.mod` without maintainer approval |

---

## Testing Guidelines

- Write **table-driven tests** where applicable
- Use **deterministic assertions** (avoid flaky `time.Sleep` when possible)
- Always run with the race detector: `go test -race ./...`
- Add **example tests** (`Example*`) in `example_test.go` for public APIs
- Mock external interfaces (Store, Locker, Logger) instead of relying on real services

---

## Documentation

- Every exported type, function, variable, and constant must have a **GoDoc comment**
- Package-level doc should live in `doc.go` or the top of `gron.go`
- Update `README.md` for public-facing changes
- Add runnable examples to `example_test.go` with `// Output:` comments
- Keep documentation concise, accurate, and up-to-date

---

## Release Process

1. Merges to `main` are automatically tested via GitHub Actions
2. Maintainers tag releases using [Semantic Versioning](https://semver.org/)
3. Update `CHANGELOG.md` before tagging
4. Push tag: `git tag v1.x.0 && git push origin v1.x.0`
5. `pkg.go.dev` auto-indexes new versions within minutes

---

## Need Help?

- Read the [README](README.md) & [GoDoc](https://pkg.go.dev/github.com/ismayelalam/gron)
- Ask questions via **[GitHub Discussions](https://github.com/ismayelalam/gron/discussions)** or open a **[Question Issue](https://github.com/ismayelalam/gron/issues/new?template=question.yml)**
- Follow us for updates: `@ismayelalam`

---

## License

By contributing, you agree that your contributions will be licensed under the project's [MIT License](LICENSE).

Thank you for making `gron` better!
