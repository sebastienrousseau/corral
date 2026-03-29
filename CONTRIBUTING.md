# Contributing

Corral is a single-file Bash tool that clones and organises GitHub repositories by visibility and language. Contributions are welcome.

## Getting Started

1. Fork and clone the repository.
2. Install development dependencies:

   **macOS:**
   ```bash
   brew install bash shellcheck bats-core
   ```

   **Ubuntu / Debian / WSL2:**
   ```bash
   sudo apt install shellcheck bats
   ```

3. Set up the project:
   ```bash
   make init
   ```
4. Create a branch:
   ```bash
   git checkout -b feat/my-change
   ```
5. Make changes.
6. Verify everything passes:
   ```bash
   make check
   make test
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

- [ ] `make check` passes
- [ ] `make test` passes
- [ ] README updated if behaviour changed
- [ ] All commits are signed (`git log --show-signature`)

## Code Style

- Indent with tabs.
- Scope variables with `local`.
- Quote all variable expansions.
