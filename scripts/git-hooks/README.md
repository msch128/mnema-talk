# Git hooks

Versioned git hooks for this repository. Enable them once per clone:

```sh
make install-hooks   # = git config core.hooksPath scripts/git-hooks
```

| Hook         | What it does                                                                 |
|--------------|------------------------------------------------------------------------------|
| `pre-commit` | `gitleaks protect --staged` on the staged diff, and `gofmt -l` on staged `.go` files |

Notes:

- If `gitleaks` is not installed the secret scan is skipped with a warning.
  CI (`.github/workflows/ci.yml`) scans every push and pull request anyway, but
  a secret caught before the commit never reaches the public history.
  Install: <https://github.com/gitleaks/gitleaks#installing>.
- The hook runs no tests; run `make check` before pushing.
- `git commit --no-verify` bypasses the hook, including the secret scan. Use it
  only for a confirmed false positive, and prefer allowlisting it in
  `.gitleaks.toml`.
- On Windows the hook runs through Git for Windows' bash.
