# Git Hooks

## Pre-commit Hook

The pre-commit hook automatically runs `make check` before allowing commits. This ensures code quality by running:
- `make manifests` - Generate CRDs and RBAC
- `make generate` - Generate DeepCopy methods
- `make fmt` - Format code
- `make vet` - Run go vet
- `make lint` - Run golangci-lint
- `make test` - Run unit tests

### Installation

```bash
make install-hooks
```

This copies `scripts/pre-commit` to `.git/hooks/pre-commit` and makes it executable.

### Usage

Once installed, the hook runs automatically before every commit:

```bash
git commit -m "your message"
# Running 'make check' before commit...
# ... check output ...
# ✅ 'make check' passed. Proceeding with commit.
```

If checks fail, the commit is aborted:

```bash
git commit -m "your message"
# Running 'make check' before commit...
# ... error output ...
# ❌ 'make check' failed. Commit aborted.
# Fix the issues and try again, or use 'git commit --no-verify' to skip this check.
```

### Skipping the Hook

To bypass the hook (not recommended):

```bash
git commit --no-verify -m "your message"
```

### Uninstalling

```bash
rm .git/hooks/pre-commit
```

## Why Use Pre-commit Hooks?

✅ **Catch issues early** - Before they reach CI/CD
✅ **Consistent quality** - All commits pass checks
✅ **Save time** - No failed CI builds due to formatting/linting
✅ **Team alignment** - Everyone runs the same checks
