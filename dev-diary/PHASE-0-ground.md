# P0: Ground

```yaml
id:       P0
size:     M
requires: []
blocks:   everything
parallel: partly (the user's tasks run beside T0.1)
```

**Goal:** Put the repository on git footing, settle every key and account, write the occasions down, take the sample photos, and record what YouCam, Clef and Lambo actually return for them. Every later track then builds offline.

**Why this phase comes first:** YouCam units are scarce, so every reply is recorded once and replayed after. Without the occasions there is nothing to ask Clef. Without the sample set there is nothing to record, test or demo.

Three tasks are the user's: T0.2, T0.3 (sign-off), T0.4. Agents prepare everything around them and stop where an account, a key, a photo or a decision is needed.

Read first: [`../product.md`](../product.md), [`../docs/architecture.md`](../docs/architecture.md), [`README.md`](README.md).

---

### T0.1: Repository on git footing ★
```yaml
requires:   []
fixture-ok: yes
size:       S · light
owns:       .gitignore, README.md (stub), go.mod, go.sum, cmd/dresscode/main.go, web/ (scaffold), tools/check.sh, .github/workflows/check.yml, .env.example
status:     done
```
The first commit (product, architecture, agent rules, this diary, licence) is in. Add:

- `go.mod` pinned to `github.com/nrynss/keel v0.9.0`, and a stub `cmd/dresscode` with `serve` and `version` that exits cleanly.
- `web/`: a Svelte 5 + Vite static single-page app with `@nrynss/chaaya@0.5.0` and Bits UI, pnpm, one placeholder route, lint, `svelte-check` and Vitest wired.
- `tools/check.sh`: gofmt, go vet, `go test -race ./...`, then the app's lint, check and test. It needs no key and no network.
- `.github/workflows/check.yml` running the same script on push to `main` and on pull requests.
- `.gitignore`: `.env*` except `.env.example`, `dresscode.toml`, `data/`, `*.db`, `web/build/`, `node_modules/`, worktree folders.
- `.env.example` with names only (T0.2 fills the list).

**Done when:** a fresh clone passes `./tools/check.sh`; CI runs it on push; `git ls-files` shows no `.env` and no `data/`.

Notes:

- Built the module `github.com/nrynss/dresscode` on `go 1.27.1` with no toolchain line, requiring Keel v0.9.0. `version` prints `dresscode 0.0.0` after `id.Valid` accepts a fixed id, so the require is a real call. `serve` prints one line and exits. It does not listen, dial, or read a key. The test re-executes the test binary and checks that both commands exit 0.
- The app is Vite and Svelte 5, one page at `web/src/placeholder.svelte`, not a Kit app. That page imports `packageName` from `@nrynss/chaaya` and `Dialog` from `bits-ui`. `tools/check.sh` runs gofmt, go vet, `go test -race ./...`, then eslint, svelte-check, and Vitest. Vite's `outDir` is `web/build`.
- Chose `github.com/nrynss/dresscode` so the module sits beside Keel. Did not add `@sveltejs/kit`: Chaaya marks that peer optional, and nothing imports `@nrynss/chaaya/sveltekit`. Did not declare `axe-core` or `@internationalized/date`; pnpm `autoInstallPeers` still records them for Chaaya and Bits UI. `.env.example` is a comment only, because T0.2 owns the names. `.gitignore` already ignored `.claude/worktrees/`, and no other in-repo worktree folder was missing.
- Surprised that the Chaaya root export is only `packageName`. Surprised that pnpm's minimum release age rejects the exact `@nrynss/chaaya@0.5.0` pin; `web/pnpm-workspace.yaml` excludes that version so a frozen install can run. `go mod tidy` added no indirect modules, because `keel/id` uses only the standard library.

---

### T0.2: Keys, accounts and settings ★
```yaml
requires:   []
fixture-ok: no
size:       S · user
owns:       docs/keys.md, .env.example
status:     done
```
The user confirms each account; an agent writes `docs/keys.md` (where each key lives, by name only) and `.env.example`.

- **YouCam:** key in the keyring (`service youcam … kind api-key`), already proved on the India server. The console balance is 1040 credits. They do not expire.
- **Workers AI:** token in the keyring (`service cloudflare … kind workers-ai`), already proved. Record the account id's variable name.
- **Resend:** an API key and a verified sending domain or address for sign-in codes. Reprise's settings name `RESEND_API_KEY` and `RESEND_FROM`; decide whether Dresscode gets its own key.
- **OpenRouter (Jev):** the key is available. Fix selection is Clef. A comparison of Clef and Jev on the same pick requests is still required.
- **Lambo:** its own v0.8.0 process on this machine, port 7720, sqlite at `~/lambo-dresscode/dresscode.db`, Workers AI `bge-m3`, multi-session. Not the dogfood server.

**Done when:** `docs/keys.md` lists every key by name and location, no value anywhere; the handoff log records the YouCam balance and the Clef-or-Jev decision.

Notes:

- The values were already in `.env`. This task wrote the names into `docs/keys.md` and `.env.example`.
- YouCam's console shows 1040 credits, and they do not expire. The earlier "90 days from redemption" does not apply.
- Dresscode's Lambo is `lambo-dresscode.service`, port 7720, database `~/lambo-dresscode/dresscode.db`. A request without the bearer token is refused. The first start needed `lambo provision` on that new file.

---

### T0.3: Occasions, fix menus and the look vocabulary ★
```yaml
requires:   []
fixture-ok: yes
size:       M · frontier
owns:       testdata/occasions/, docs/occasions.md
status:     claimed:grok-4.7
```
The content the whole product runs on. The agent drafts; **the user signs off.**

- **Five occasions:** date, family function, interview, festival, funeral. For each, 5–8 yes/no criteria about clothing, grooming, lighting and expression only, each worded twice: as a Clef question, and as a suggestion the user reads ("the collar reads formal; try it open"). Plus the biggest-issue choice list.
- **Fix menus:** each fix names what it changes, the YouCam task that renders it, the analysis it needs (if any), and the criteria it addresses. Only fixes YouCam can render from the qualifying sets (Skin, Beauty, Fashion, Jewelry & Watch, Hair & Beard).
- **The look vocabulary:** one choice question per attribute (jewellery, jewellery colour, lip colour, collar, hair, …) with a closed option list including "none" and "can't tell". Every fix's change must be expressible in it, so memory can tell "kept the jhumkas" apart from "dismissed the red lip".
- **Retail basket:** the product categories each occasion draws from, and the catalog entries T0.4 will photograph or license.
- Nothing about age, body, skin tone as a judgement, or attractiveness. No gender inference: the menu is opt-in.

Write it as data (`testdata/occasions/*.json`, the schema T1.1 will load) and as prose (`docs/occasions.md`).

**Done when:** every occasion has criteria, a menu and a basket; every fix maps to a YouCam task and a vocabulary change; the user has signed off.

---

### T0.4: The sample set ★
```yaml
requires:   T0.3
fixture-ok: no
size:       S · user
owns:       testdata/samples/ (the user adds the photos), testdata/samples/README.md
status:     not-started
```
The user takes the photos; an agent writes the README and the shot list from T0.3.

- **Selfies:** the user's own, taken for this purpose: about 20, across the five occasions, with deliberate misses (formal collar on a date, flat light, untidy beard) so each criterion has a yes and a no. The README records which criteria each photo should fail; that is the evaluation's ground truth (T3.9).
- **Products:** photographs the user took, or images we may use. No brand logos visible.
- **Known bad renders** for the render check: produced later by T0.5 and by hand edits (reshaped face, smoothed skin, change missing).

**Done when:** the photos are in place, the README holds the expected flags per photo, and the user confirms consent for the demo and the public video.

---

### T0.5: YouCam probe ★
```yaml
requires:   T0.2, T0.3, T0.4
fixture-ok: no
size:       M · mid
owns:       testdata/youcam/, docs/youcam.md
status:     not-started
```
Turn the API docs into recorded replies the clients are built against. **Unit budget: 150.** Run the cheapest case first, read the units it cost, stop and report before going over.

- For every YouCam task the fix menus name (T0.3), plus Facial Color Tones and Face Attributes (face shape only): upload through the File API, create the task, poll, fetch the result. Record each request and response (headers without the key), the units each cost, and the result image.
- Record the error replies: no face, too small, bad file, unknown task, expired file.
- Measure: how long each task takes, how long a result link lives, the rate limit.
- `docs/youcam.md`: the calls, their shapes, units per call, the timings, the surprises (also in `feedback.md`).

**Done when:** every task the menus name has a recorded success and its unit cost; the error shapes are recorded; the units spent are in the handoff log.

---

### T0.6: Clef probe ★
```yaml
requires:   T0.2, T0.3, T0.4
fixture-ok: no
size:       M · mid
owns:       testdata/clef/, docs/clef.md
status:     not-started
```
Record what Clef returns, and what it costs, on the sample set. Workers AI spend: within the free daily allowance.

- The judge request: one photo, one occasion's criteria as `noul`, the biggest-issue `choice`. Run it on every sample photo for its occasion.
- The check request: original plus a T0.5 render, the three checks and the look-vocabulary `choice` questions.
- **Stability:** one photo, ten repeats; record the spread of each probability.
- **Cost:** the input tokens Workers AI counts for an embedded image, at 1,024 px and smaller. That sets the real cost per look and whether Clef-flash is enough for the check.
- Fix selection is Clef. This probe records Clef. The Jev comparison on the same pick request is still required and is not part of this probe.

**Done when:** replies for every sample photo are recorded; the stability spread and tokens per image are in `docs/clef.md` and the handoff log.

---

### T0.7: Lambo probe
```yaml
requires:   T0.2
fixture-ok: no
size:       S · light
owns:       testdata/lambo/, docs/lambo.md
status:     claimed:grok-4.7
```
Against the Dresscode Lambo server (T0.2), in scratch sessions:

- Open two sessions in one serve; derive a look in each; confirm recall in one never returns the other's.
- Recall by occasion with a text query ("family function"), and confirm the kept and dismissed fixes come back.
- Erase a session, and confirm recall then returns nothing and a second erase reports gone.
- Record every MCP request and response as JSON (no key).

**Done when:** `docs/lambo.md` lists the calls Dresscode needs with their shapes; isolation, recall and erase were each seen to work, or the gap is written as issue text in the handoff log.
