# Agent protocol

Rules for any agent working in this repository. The product is in [`product.md`](product.md), the architecture in [`docs/architecture.md`](docs/architecture.md), and the work in [`dev-diary/`](dev-diary/README.md). The orchestrator's loop is in [`dev-diary/ORCHESTRATOR.md`](dev-diary/ORCHESTRATOR.md).

## Task rules

- Work only on a task from a phase file in `dev-diary/`, claimed for you by the orchestrator.
- One implementer edits a task at a time. On P0 the orchestrator checks the result and lands it, or sends it back. From P1 a review agent reads the work, then a remediation agent fixes what the review found, and that repeats until a review is clean. You do not review your own task, and you do not mark it done.
- Meet every **Done when** clause, with evidence in your report. A clause you could not meet is reported as unmet, never glossed.
- A decision the task names as the user's goes back to the orchestrator. Don't make it yourself.
- Never schedule. No dates, day windows, target days or timelines in any document, commit or report. Work is ordered by `requires` only.

## Stack

- **Go**, the version on the `go` line of `go.mod`. **Keel v0.9.0.** Use a Keel package before writing the same thing again.
- **The app:** Svelte 5 with runes, on Vite, built as a static single-page app under `web/`. **Chaaya 0.5.0.** Bits UI for dialogs, sheets and tooltips. Never use SvelteKit form actions or `+server.ts` routes: they need a Node server and don't run inside a Capacitor app.
- **Node and pnpm.** Node 26, pnpm.
- **SQLite** through Keel. No other database.
- **Lambo v0.8.0** runs as its own process; Dresscode talks to it over MCP, never links it.
- The standard library first. A new dependency needs a stated reason in the handoff log.

## Worktrees

- One task, one branch: `task-<id>-<slug>`, in its own worktree. Two agents never edit one checkout.
- Commit on your branch. Never push, merge, or touch the main checkout; the orchestrator lands.
- A worktree has no gitignored files (`.env`, `dresscode.toml`, `data/`). Don't assume they exist.

## File ownership

- Edit only the paths in your task's `owns` field.
- `go.mod`, `go.sum`, `web/package.json` and the lockfile are shared: add only the dependency your task names.
- Only the orchestrator edits `dev-diary/README.md`.

## The gate

- `./tools/check.sh` must pass on every commit: gofmt, go vet, `go test -race ./...`, and the app's lint, type check and tests.
- CI runs the same script on push to `main` and on pull requests.
- The gate needs no key, no network and no running service. Tests run on `testdata/` and recorded replies.

## Live tests

- A live test calls a real service: YouCam, Workers AI, OpenRouter, Resend or a running Lambo. It sits behind the `live` build tag (`//go:build live`) and runs only with `go test -tags live ./...`.
- Live tests never run in the gate or in CI.
- **YouCam units are scarce.** Every live task names its unit budget. Run one case first, read the units it cost, and stop and report if the next would go over. Prefer calls that spend nothing (an upload slot) to prove a key.
- A live run is evidence: commit the test, and put its transcript (no key, no personal data, no photo of a real person) in your report.

## Secrets

- Keys come from the keyring or the environment, never a committed file. `.env` and `dresscode.toml` are gitignored; `.env.example` holds names only. Setup is in `docs/keys.md` (T0.2).
- Never print, log, commit or paste a key value, including in test output, fixtures and recordings.

## Photos and people

- Fixtures, the demo and the video use only the two AI-generated sample people (T0.4) and product images we may use. No photo of a real person, the user included.
- A sample person is an original design: never generated from a real person's photo, and never prompted to look like anyone.
- No third-party logos or brand imagery in the video.
- Never send a model anything the "Who sees what" table in product.md forbids. Never show a probability to the user.

## Commits and pull requests

- Plain messages.
- No `Co-Authored-By` trailer.
- No Claude or Anthropic attribution in commits or PR text.

## Dev-diary

- Set your task to `in-review` in its phase file when you report, and add your notes under the task: what you built, what you decided and why, what surprised you.
- Put your handoff-log entry in your report; the orchestrator adds it to `dev-diary/README.md`.
- Anything YouCam, Workers AI, Resend or a library does that surprises you goes in [`dev-diary/feedback.md`](dev-diary/feedback.md) when you hit it.
- The repository may go public. Nothing private in the diary: no key, no account detail, no private repository's name.

## New work only

- The event takes new projects. Write Dresscode's code here. Keel, Chaaya and Lambo are published dependencies; never copy code from Reprise, Before Yes or any earlier project.
