# Dresscode

**The dress-code mirror.** Every occasion has a dress code. Dresscode lets you see it before they do.

Pick an occasion, bring one selfie. Dresscode checks the look against the occasion's dress code, suggests one fix with its reasons, renders it on your own photo with the YouCam API, checks the render is still you, and labels every product honestly. With style history on, it remembers what you kept and dismissed for next time.

> **Status:** entry for the YouCam API Skin AI & eCommerce VTO Hackathon, in development. Nothing below is implemented yet; see [`product.md`](product.md) and the [development diary](dev-diary/README.md).

## Run

Print the version:

```sh
go run ./cmd/dresscode version
```

`serve` exits without listening. Check the web app (svelte-check) with:

```sh
pnpm --dir web check
```

The full gate is `./tools/check.sh`: gofmt, go vet, `go test -race ./...`, then the web app's lint, svelte-check, and Vitest.

## Licence

Apache-2.0. See [`LICENSE`](LICENSE).
