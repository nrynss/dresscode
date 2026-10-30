# Dresscode: architecture

The product is in [`product.md`](../product.md). The work is in [`dev-diary/`](../dev-diary/README.md). This file says how Dresscode is put together. Where it and product.md disagree, product.md wins.

## Shape

One Go binary, `dresscode`, serving the API and the built app. Lambo runs beside it as its own process. Everything paid sits behind Keel's gate and cost budgets.

```
 phone (PWA, Svelte 5 + Chaaya)
   │ HTTPS through the Cloudflare Tunnel
   ▼
 dresscode (Go, Keel)
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

## The domain model

| Type | What it is |
|---|---|
| `Occasion` | An id, its criteria, its fix menu, its retail basket |
| `Criterion` | One yes/no question about clothing, grooming, lighting or expression, worded for Clef and for the user |
| `Fix` | One menu entry: what it changes, which YouCam task renders it, which analysis it needs (if any), which criterion it addresses |
| `LookAttribute` | One choice question over a fixed vocabulary (jewellery, jewellery colour, lip colour, collar, hair, …) |
| `Look` | One attempt: the selfie, the occasion, the flags, the chosen fix and runner-up, the renders with their checks, the products, the user's verdict (kept, dismissed, bought) |
| `Render` | A YouCam result copied into mediastore, with Clef's check: change made, same person, criterion met, look attributes |
| `Product` | A catalog entry or a user-supplied item, with its fidelity label |

## The pipeline

Each look runs as one Keel job, with progress streamed to the phone.

1. **Judge.** One Clef request: the photo, the occasion's criteria as `noul` questions, and one `choice` question for the biggest issue. Output: flags.
2. **Pick.** One Clef request on text only: flags, occasion, the opt-in menu, allowlisted attributes, recall. Output: fix and runner-up.
3. **Measure.** Only the analysis the fix names, cached by the normalised photo's hash.
4. **Render.** A YouCam task through `providertask`, keyed so a restart never pays twice. The result is copied into mediastore at once.
5. **Check.** One Clef request with both images: change made, same person, criterion met (`noul`), and one `choice` per look attribute. A failed first or second check discards the render; the runner-up runs once; then the suggestion stands alone.
6. **Shop.** Products for the fix, each with its label.
7. **Remember.** If history is on: one Lambo write in the account's session.

The thresholds that turn Clef's probabilities into decisions are set from the evaluation (P3), never guessed.

## The seams

- **`SystemOne`** (interface). One call: images (optional), state, questions in; per-option probabilities and confidence out. Implementations: Workers AI (Clef, Clef-flash) and a replay client serving recorded replies for tests and the offline demo.
- **`YouCam`** (interface). Upload a file, create a task, poll it, fetch the result. Implementations: the v2 REST client (Bearer key, `https://yce-api-01.makeupar.com`) and a replay client.
- **`Memory`** (interface). Remember a look, recall for an occasion, erase a session. Implementations: Lambo over MCP, and an in-memory fake.
- **`identity.Sender`** (Keel). Resend for real mail; a recorder in tests.

## Budgets

- **YouCam units** are a `cost` credit budget: the 1,000 event units, split so judging keeps its share. Each paid call quotes, reserves, and settles once.
- **Workers AI** is a dollar budget per day.
- **Judges** get a `lease`: a capped number of fresh selfies and renders per session. Sample selfies with cached analysis cost nothing to open.
- **`flag`** switches flip without a restart: fresh uploads off, renders off, samples only.

## Dependencies

| Dependency | Version | Why |
|---|---|---|
| Keel | v0.9.0 | everything server-side listed above |
| Chaaya | 0.5.0 | the app's behaviour: capture, upload, job stream, priced, share, theme, gates |
| Lambo | v0.8.0 | style memory: multi-session serve, session erase, bearer embedders |
| Bits UI | per Chaaya's peer range | dialogs, sheets, tooltips |

## Facts the design rests on (checked 30 Oct)

- YouCam v2 authenticates with the API key alone as a Bearer token; `POST /s2s/v2.0/file/<task>` returns an upload slot without spending units.
- Workers AI lists `@cf/cloudflare/clef`, `clef-flash` and `@cf/baai/bge-m3` for the project's token; bge-m3 returns 1,024 dimensions.
- Clef answers only typed questions (`choice`, `noul`, `score`), takes up to 4 images, embedded (no URLs), 4 MiB each.
- Workers AI's free plan allows 10,000 neurons a day; Clef costs 21,818 neurons per million input tokens.
