# Contributing

Thank you for considering a contribution to this project.

## Getting started

1. Fork and clone the repo.
2. Set up the project (enables Git hooks):
   ```bash
   make init
   ```
3. Create a feature branch from `main`:
   ```bash
   git checkout -b feat/my-change
   ```
4. Make your changes.
5. Run all checks:
   ```bash
   make check
   ```
6. Commit, push, and open a pull request.

## Commit guidelines

- **Sign all commits.** Unsigned commits will not be accepted.
  ```bash
  # Configure once:
  git config --global commit.gpgsign true

  # Then commit as usual:
  git commit -m "feat: add dry-run mode"
  ```
  See [GitHub's guide to signing commits](https://docs.github.com/en/authentication/managing-commit-signature-verification/signing-commits) if you haven't set up a signing key.

- Write clear, imperative commit messages (e.g., "Add dry-run flag", not "Added dry-run flag").

## Pull request checklist

- [ ] `shellcheck clone-gh-repos.sh` passes with no warnings
- [ ] `bash -n clone-gh-repos.sh` confirms valid syntax
- [ ] README updated if behaviour changed
- [ ] Commit(s) are signed (`git log --show-signature`)

## Code style

- Indent with tabs (matches the existing script).
- Use `local` for function-scoped variables.
- Quote all variable expansions.
