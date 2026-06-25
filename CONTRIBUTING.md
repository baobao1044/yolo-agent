# Contributing to YOLO Agent

Thank you for your interest in contributing!

## How to Contribute

1. **Fork the repository** and clone your fork.
2. **Create a feature branch**: `git checkout -b feature/my-feature`
3. **Write code** that follows the existing style and conventions.
4. **Add tests** for new functionality.
5. **Run tests**: `go test ./...`
6. **Commit** with a clear message.
7. **Open a pull request** to the `main` branch.

## Development Setup

```bash
git clone https://github.com/baobg/yolo-agent.git
cd yolo-agent
go mod download
go build -o yolo-agent ./cmd/agent
```

## Code Style

- Go code should be formatted with `go fmt`.
- Keep the binary lightweight; prefer pure-Go dependencies.
- Match the comment density and naming conventions of existing code.
- Add documentation for new features in `docs/`.

## Testing

```bash
go test ./...
go vet ./...
```

## Reporting Issues

Please open an issue with:
- A clear description of the bug or feature request
- Steps to reproduce (for bugs)
- Expected vs actual behavior
- Go version and OS

## Security

- Do not submit exploits or malicious code.
- If you find a security vulnerability, please report privately via email or GitHub Security Advisories.
