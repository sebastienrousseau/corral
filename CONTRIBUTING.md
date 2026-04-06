# Contributing

Corral is a compiled Go application with a Bubble Tea terminal user interface that clones and organises GitHub repositories by visibility and language. Contributions are welcome.

## Getting Started

1. Fork and clone the repository.
2. Install development dependencies:
   - **Go 1.21+**
   - **Make**
   - **Git**

3. Set up the project hooks (optional but recommended):
   ```bash
   git config core.hooksPath .githooks
   ```
4. Create a branch:
   ```bash
   git checkout -b feat/my-change
   ```
5. Make changes.
6. Verify everything passes:
   ```bash
   make format
   make test
   make build
   ```
7. Commit, push, and open a pull request.

## Commits

Sign every commit. Unsigned commits are not accepted.

```bash
# Enable signing once:
git config --global commit.gpgsign true
```

Need a signing key? Follow [GitHub's guide to signing commits](https://docs.github.com/en/authentication/managing-commit-signature-verification/signing-commits).

Use imperative commit messages: "Add dry-run flag", not "Added dry-run flag."

## Pull Request Checklist

- [ ] `make test` passes
- [ ] `make build` succeeds
- [ ] README updated if behaviour changed
- [ ] All commits are signed (`git log --show-signature`)

## Code Style

- Use standard Go formatting (`gofmt -w .`).
- Ensure all exported functions and types are documented.
- Follow idiomatic Go guidelines.
