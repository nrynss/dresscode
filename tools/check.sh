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
go test -race ./...

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
