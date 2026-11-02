# Dresscode

**The dress-code mirror.** Every occasion has a dress code. Dresscode lets you see it before they do.

Pick an occasion, bring one selfie. Dresscode reads the look against the occasion's written dress code and flags what it misses — as reasons, worded as suggestions, **never a score** — picks one fix from the fixes you ticked as open to, renders it on your own photo, checks the render is still you before showing it, and labels every product honestly: **exact product**, **approximate match** or **visual simulation**. With style history on, it remembers what you kept and dismissed for next time.

Dresscode is an entry for the YouCam API Skin AI & eCommerce VTO Hackathon, and it is live at **https://dresscode.nryn.dev**.

## For judges: start here

| | |
|---|---|
| **The app** | https://dresscode.nryn.dev — served from the author's own machine behind a Cloudflare Tunnel, HTTPS, installable on a phone. Opening a cached sample story is free and spends nothing; a fresh selfie and its renders are capped by a per-session judge lease, and spend guards refuse before they spend. |
| **Sample stories** | Five cached looks, one per occasion — **date**, **family function**, **festival**, **funeral**, **interview**. Family function is the demo's spine, the second visit: the first visit misses the festive earrings and the lip colour, the jhumka fix is picked, rendered and checked, and that kept look is what style memory remembers. **Every person in the samples is AI-generated and does not exist.** No photo of a real person was used anywhere, and no sample was generated to look like anyone. |
| **The video** | The video shows the hosted app on its intended device, a phone, through the same second visit, and names each YouCam API as it works. |
| **Evaluation** | Measured on 37 sample variants — 273 criterion answers — against the sample set's ground truth, replayed from recorded model calls ([docs/evaluation.md](docs/evaluation.md)). These are evaluation numbers, not product output: the product shows reasons, never scores or probabilities. **Judging: 269 of 273 criterion answers correct (98.5%)** at the shipped decision lines; a flat 0.5 line reads 254. **Render check: every known bad render discarded, no honest render lost** (4 of 4 recorded reads). **Biggest issue, two labelled reads**: the service's raw choice names a miss on **28 of 37**; gated on the look's own flags — the read the app reports — **32 of 37**. |

## The YouCam APIs, and why each

Each render task puts one chosen fix on the user's own photo (the original is always one tap away), and each is gated by the render check: a render that is not the same person, did not make the change, or beautifies the face is discarded, never shown.

| API | Set | Why it is there | Units |
|---|---|---|---|
| `2d-vto/earring` | Jewelry & Watch | Renders the chosen earring on the user's ears — the gold jhumkas of the second-visit story. | 1 |
| `2d-vto/necklace` | Jewelry & Watch | Renders the chosen necklace for the festival dress code. | 1 |
| `makeup-vto` | Beauty | Renders the lip-colour fix. `skin_smooth` is sent at 0 — the API otherwise defaults it to 50 — so the render never smooths skin. | 1 |
| `cloth-v4` | Fashion | Renders the garment fix: the festive top, the plain dark shirt, whatever the occasion's code asks. | 2 |
| `hair-transfer` | Hair & Beard | Renders the hairstyle fix when hair over the eyes breaks a criterion. | 2 |
| `beard-style` | Hair & Beard | Renders the beard fix, trimmed to the occasion's code. | 2 |
| `ai-smile` | — | Renders the expression fix (a closed-mouth smile). | 1 |
| `face-attr-analysis` | Skin AI | The one analysis that runs, and only when the chosen fix needs it: face shape, so the pick suits the face. Cached per photo — the same face never pays twice. Age, gender and facial ratios are never requested, read, stored or sent on. | 10 |

That covers all five API sets the hackathon names, each used on the path a real look takes. One thing is deliberately **not** YouCam: the judge that reads the dress code, the pick, and the render-check judge all run on **Clef** (`@cf/cloudflare/clef`) on Cloudflare Workers AI — the check that keeps YouCam's renders honest is a separate model that never sees YouCam's analysis. Style memory is Lambo over MCP, embedded on Workers AI.

## Run it from a fresh clone — no keys needed

You need [Go](https://go.dev) (the version on the `go` line of `go.mod`) and Node with pnpm.

```sh
git clone https://github.com/nrynss/dresscode
cd dresscode
./tools/check.sh
```

The gate runs everything offline, on committed fixtures and recorded provider replies: gofmt, `go vet`, `go test -race ./...`, then the web app's lint, `svelte-check` and Vitest. No key, no network, no spend.

Re-run the whole evaluation the same way — it replays the recorded Clef calls and re-checks every number quoted above:

```sh
go test ./internal/eval/ -count=1
```

A second run returns the identical report; the test proves it. Build the binary and the app:

```sh
go build ./cmd/dresscode
(cd web && pnpm build)    # the built app lands in web/build
go run ./cmd/dresscode version
```

## Running a server (needs keys)

`dresscode serve` refuses to boot without keys, by design: anything paid sits behind budgets that quote, reserve and settle. Values live in a gitignored `.env` or your keyring — never in a committed file. The names, what each is for, and where they live are in [docs/keys.md](docs/keys.md):

| Name | For |
|---|---|
| `YOUCAM_API_KEY`, `YOUCAM_API_SECRET` | YouCam v2 renders and analysis (the secret is stored beside the key; the API call sends only the key) |
| `WORKERS_AI_API_TOKEN`, `CLOUDFLARE_ACCOUNT_ID` | Clef for judging, picking and the render check; `bge-m3` embeddings for memory |
| `RESEND_API_KEY`, `RESEND_FROM` | Sign-in codes by email |
| `OPENROUTER_API_KEY`, `OPENROUTER_BASE_URL` | A picker comparison kept for record; Clef is the picker |
| `LAMBO_URL`, `LAMBO_DRESSCODE_TOKEN` | The Lambo style-memory process beside the server |
| `DRESSCODE_SIGNING_KEY`, `DRESSCODE_CODE_KEY` | Session cookie and sign-in-code signing; `serve` refuses without them |

Settings are `dresscode.toml` — copy `dresscode.toml.example`, which carries the evaluation's decision lines and points at this repository's own `testdata/` data. Deploying as services, with the tunnel, health check and backups, is [deploy/README.md](deploy/README.md).

## Architecture in one picture

```
 phone (PWA, Svelte 5 + Chaaya)
   │ HTTPS through the Cloudflare Tunnel
   ▼
 dresscode (one Go binary, Keel)
   ├─ identity ── guests, code sign-in ──────────────▶ Resend
   ├─ gate · lease · cost ── refuse before spending
   ├─ upload · photo · fetch ── selfie and products in, normalised
   ├─ pipeline (job + stream, progress to the phone)
   │    judge ── pick ── measure ── render ── check ── shop ── remember
   │      │       │        │          │        │                 │
   │      ▼       ▼        ▼          ▼        ▼                 ▼
   │    Clef   System    YouCam     YouCam    Clef            Lambo ──▶ Workers AI
   │    (Workers AI) One  analysis  try-on   (Workers AI)    (MCP)      bge-m3
   ├─ providertask ── YouCam create-then-poll, resumes without paying twice
   ├─ mediastore ── selfies, products, renders (YouCam links expire in 2 h)
   ├─ erase ── photo, look, history, account: files, rows, Lambo session
   └─ sqlite ── users, sessions, looks, analysis cache, catalog, criteria
```

Each look runs as one Keel job with progress streamed to the phone: Clef flags the criteria the look misses against the occasion's written code, picks one fix from the user's opt-in menu (text only, no photo sent), measures only what that fix needs, renders it through YouCam, and checks the render with Clef seeing original and render together. A failed check discards the render and tries the runner-up fix once; after that the suggestion stands without a render. Every threshold that turns a probability into a decision is measured, not guessed — [docs/evaluation.md](docs/evaluation.md) is the record.

## Dependencies

| Dependency | Version | Why |
|---|---|---|
| [Keel](https://github.com/nrynss/keel) | v0.9.0 | The server's spine: gate, lease, cost, job, stream, providertask, mediastore, upload, photo, fetch, identity, erase, sqlite, flag, config, id, wire |
| Chaaya (`@nrynss/chaaya` on npm) | 0.5.0 | The app's behaviour: capture, upload, job stream, priced actions, share intake, theme, accessibility gates |
| Lambo | v0.8.0 | Style memory as its own published process, beside the server over MCP: multi-session serve, session erase, bearer embedders |
| Bits UI | per Chaaya's peer range | Dialogs, sheets, tooltips |

## Licence

Apache-2.0. See [LICENSE](LICENSE).
