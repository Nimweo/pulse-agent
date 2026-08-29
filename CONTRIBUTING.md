# Contributing to Pulse Agent

Thank you for helping improve Pulse Agent. Contributions are welcome through
issues and pull requests.

## Before opening an issue

- Search existing issues first.
- Include the Pulse Agent version, operating system, configuration with secrets
  removed, and relevant logs.
- Do not disclose API keys or security-sensitive details publicly. Follow
  [SECURITY.md](SECURITY.md) for vulnerability reports.

## Development setup

Pulse Agent requires Go 1.26. Clone the repository, make the requested change,
and run the complete validation suite:

```bash
go test ./...
go test -race ./...
go vet ./...
bash -n install.sh
go mod verify
```

Keep comments and user-facing messages in English. Add or update tests for
behavioral changes and preserve cross-platform support for Linux, macOS, and
Windows where applicable.

## Pull requests

- Keep pull requests focused and explain the user-visible impact.
- Use clear commit messages with the project prefixes, for example
  `(feat)`, `(fix)`, `(test)`, or `(docs)`.
- Do not commit credentials, local configuration, generated binaries, or the
  ignored `test_web_server` directory.
- Describe compatibility, migration, and release-version implications.
- Ensure CI is green before requesting review.

By submitting a contribution, you agree that it may be distributed under the
project's [Apache License 2.0](LICENSE).
