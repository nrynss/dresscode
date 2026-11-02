#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

unformatted="$(
  find . \
    \( -name .git -o -name node_modules -o -name build \) -prune \
    -o -name '*.go' -print0 |
    xargs -0 -r gofmt -l
)"
if [[ -n "${unformatted}" ]]; then
  printf 'gofmt would change:\n%s\n' "${unformatted}" >&2
  exit 1
fi

go vet ./...
# Also type-check the live-tagged files: they build no binary, run no call
# and read no key, and without this line a break in the paid harness lands
# green and is only found by someone paying for a run.
go vet -tags live ./...
# The eval package under -race runs 466-594s against go test's default
# 600s per-package timeout — one loaded runner away from a false red.
# 45m keeps the ceiling far from the floor; a genuine hang still fails.
go test -race -timeout 45m ./...

if [[ ! -d web/node_modules ]]; then
  pnpm install --frozen-lockfile --dir web
fi

(
  cd web
  pnpm exec eslint .
  pnpm exec svelte-check --tsconfig ./tsconfig.json
  pnpm exec vitest run
  pnpm exec vitest run --config tests/a11y/vitest.config.ts
)
