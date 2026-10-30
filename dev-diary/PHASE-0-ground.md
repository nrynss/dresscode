# P0: Ground

```yaml
id:       P0
size:     M
requires: []
blocks:   everything
parallel: partly (the user's tasks run beside T0.1)
```

**Goal:** Put the repository on git footing, settle every key and account, write the occasions down, make the sample people, and record what YouCam, Clef and Lambo actually return for them. Every later track then builds offline.

**Why this phase comes first:** YouCam units are scarce, so every reply is recorded once and replayed after. Without the occasions there is nothing to ask Clef. Without the sample set there is nothing to record, test or demo.

Two tasks are the user's: T0.2 and T0.3 (sign-off). T0.4 needs the user's approval of the two sample people. Agents prepare everything around them and stop where an account, a key or a decision is needed.

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
status:     done
```
The content the whole product runs on. The agent drafts; **the user signs off.**

- **Five occasions:** date, family function, interview, festival, funeral. For each, 5–8 yes/no criteria about clothing, grooming, lighting and expression only, each worded twice: as a Clef question, and as a suggestion the user reads ("the collar reads formal; try it open"). Plus the biggest-issue choice list.
- **Fix menus:** each fix names what it changes, the YouCam task that renders it, the analysis it needs (if any), and the criteria it addresses. Only fixes YouCam can render from the qualifying sets (Skin, Beauty, Fashion, Jewelry & Watch, Hair & Beard).
- **The look vocabulary:** one choice question per attribute (jewellery, jewellery colour, lip colour, collar, hair, …) with a closed option list including "none" and "can't tell". Every fix's change must be expressible in it, so memory can tell "kept the jhumkas" apart from "dismissed the red lip".
- **Retail basket:** the product categories each occasion draws from, and the catalog entries T0.4 will photograph or license.
- Nothing about age, body, skin tone as a judgement, or attractiveness. No gender inference: the menu is opt-in.

Write it as data (`testdata/occasions/*.json`, the schema T1.1 will load) and as prose (`docs/occasions.md`).

**Done when:** every occasion has criteria, a menu and a basket; every fix maps to a YouCam task and a vocabulary change; the user has signed off.

Notes:

- Drafted five occasions in `testdata/occasions/` and the prose in `docs/occasions.md`. Each file has a `kind` so a loader can tell an occasion from the shared vocabulary and the catalog. Every criterion is a yes/no Clef question plus a suggestion. Interview and funeral share `lip-quiet` with the same question and the same suggestion. Every other Clef line is its own rule. The user said not to wait, so this landed without a separate sign-off.
- The family-function story is two vocabulary changes: kept `family-gold-jhumkas` (`jewellery` jhumkas, `jewellery_colour` gold), dismissed `family-red-lip` (`lip_colour` red). `festival-red-lip` is a different fix, so a dismissal there is not the family one.
- Render tasks are only ones a Perfect Corp page prints: `cloth-v4`, `2d-vto/earring`, `2d-vto/necklace`, `makeup-vto` (`lip_color` only), `hair-transfer`, `beard-style`, `ai-smile` (`closed_mouth_smile`). Hair fixes ask for `face-attr-analysis` and may use `faceShape` only. Age, gender, and ratios are named as never used. No YouCam call was made.
- Light is judged and not rendered. The public lighting task is with Creators, outside the qualifying sets. Watches and rings need a wrist. Hat, scarf, shoes, and bag ask for a gender value, which this menu will not send or infer. Nothing removes jewellery or a smile. Hair colour is in the vocabulary and not a fix: one target colour would be a guess, and the hair-colour overview does not print a path.
- Surprised that the clothes page says both "full body" and "chest upward, shoulders visible". Surprised that `garment_category` is required in the example (`full_body`) but the enum is not listed. Surprised that `hair-type-detection` wants three views, so it cannot run on the one selfie. Surprised that the smile overview names `closed_mouth_smile` and does not print the JSON field. Surprised that the earring page prefers a side view, and the necklace page wants a worn reference. Template ids are not invented; T0.5 lists them.

---

### T0.4: The sample set ★
```yaml
requires:   T0.3
fixture-ok: no
size:       M · mid
owns:       testdata/samples/, testdata/samples/README.md
status:     done
```
Four sample people, generated with AI (user decision): an Indian man and woman (the first pair), then a white woman and an East Asian man (the second pair, about 8 variants each). The user approves every anchor. No photo of a real person is in the samples, the demo or the video, the user included.

- **The people:** one anchor still each, an original text-to-image design made with Chalachithram's character lock (`bytedance/seedream/v5/pro`). Never generated from a real person's photo or prompted to look like anyone. **The user approves both anchors** before any variant is made.
- **Selfies:** about 20 variants composed from the anchors (`fal-ai/nano-banana-2/edit`), across the five occasions. Chest up with the collar and shoulders in frame, ears and neck clear (docs/occasions.md). Deliberate misses (formal collar on a date, flat light, patchy beard, hair over the eyes, smeared lip) so each criterion has a yes and a no. The man covers the beard criteria, the woman the jewellery and lip criteria, both the rest. The woman also carries the second-visit story: a family-function look that misses the earring and lip criteria.
- **Phone-selfie realism:** ordinary light, skin texture kept, no retouched look. A variant that smooths skin or reshapes the face is regenerated: the render check rejects exactly that, so a polished sample would skew the evaluation.
- **The README** records, per image: the person, the occasion, the criteria it should pass and fail (the evaluation's ground truth, T3.9), the model and the prompt.
- **Products:** generated or photographed product shots of the catalog (docs/occasions.md): no logo, no text, no person.
- **Known bad renders** for the render check: produced later by T0.5 and by hand edits (reshaped face, smoothed skin, change missing).
- **Spend:** FAL through Chalachithram's own key, never copied into this repository. The brief names a dollar cap; stop and report at it.

**Done when:** the user has approved both anchors; the images are in place; the README holds the expected flags, the model and the prompt per image; every criterion has at least one yes and one no.

Notes:

- Part 1 of 2: the anchors only. `testdata/samples/anchors/` holds three candidates per person, `man-1` to `man-3` and `woman-1` to `woman-3`. Each is a text-only `bytedance/seedream/v5/pro/text-to-image` render through Chalachithram's `txt2img`, 1536 × 2048 JPEG as returned, under 0.5 MB. No variant is made until the user picks one man and one woman.
- `testdata/samples/README.md` records each candidate's full prompt, the size, the cost, and the shared selfie text. Its "Part 2 plan" is a 20-row shot list, ten per person: three for date, five for family function, four each for interview, festival and funeral. Each row lists the criteria it should pass and fail. Every criterion id in `testdata/occasions/*.json` has at least one yes and one no; a scratch script checked that against the JSON.
- Each prompt is one person text plus one shared selfie text: front camera, chest up, collar and shoulders in frame, both ears and the neck visible, plain wall, ordinary ceiling light, pores and unevenness kept, no makeup, no jewellery, no text, no logos, a fictional person. The three candidates per person differ in skin tone, face shape, hair and shirt colour, so the choice is a real one. The men's beards were asked to be slightly uneven at the cheeks, so a variant can push them either patchy or even. The women's hair is long and tucked behind the ears, so it can be shown loose over the face or tied back.
- The candidates were rendered at `portrait_4_3`, which returned 1536 × 2048. That is above 1536 × 1536 in area, so each cost two units, $0.135. Six candidates: $0.81 of the $2 part-1 cap.
- Surprised that the endpoint takes no seed, so an anchor cannot be reproduced from its prompt; the chosen file is the anchor. Chalachithram still sends a negative prompt the endpoint's schema does not list. The FAL key can read the listed unit price but not the usage, so the cost here is the listed price times the size tier, not a billed figure.
- Part 2: the user approved `man-1` and `woman-1`. They are now `anchors/man.jpg` and `anchors/woman.jpg`. The other four candidates were deleted, and their prompts are kept in the README under "Candidates not chosen".
- 21 variants are in `testdata/samples/man/` and `testdata/samples/woman/`, named `V<nn>-<occasion>.png`. Each is a `fal-ai/nano-banana-2/edit` call through Chalachithram's `edit` at 3:4 and 1K, which returned 896 × 1200 PNG under 2 MB. The README records, per variant, the person, the occasion, the criteria it should pass and fail, the reference image, the exact prompt and the cost. The coverage script passes: every criterion has a yes and a no.
- Every variant was checked by eye for three things: it is the same face, the skin texture and face shape are kept, and the intended misses show. Each prompt starts with a fixed preamble that asks for the same face, kept pores and no smoothing or reshaping. No variant came back smoothed or reshaped. V20, with a wide smile and hard side light, is the furthest from the anchor; I looked at it again and kept it. The ones that failed were all about the miss not showing, and those rows were regenerated.
- Regenerated: V02 and V08, whose beards stayed even, and V08's side shadow, which was too soft. V09 and V17 left the top button open on a collar meant to read closed. V13 was meant to have a red lip and came back with a bare one. V13 and V17 were fixed on the second try. V09 was fixed on the third, by adding a tie. V08 read as patchy only on the third attempt. V02 still had an even beard after three tries, so its beard is now a yes.
- Added V21 to keep a date beard miss. It edits V08's third attempt, which has the patchy beard, into a date look. V05 edits V04, so the second-visit pair is the same look with the jhumkas and the red lip added.
- Scope change: the user added a white woman and an East Asian man. Three text-only candidates of each are in `anchors/candidates/`, made exactly like part 1. They wait on the user's choice; no variant is made for them. The README plans 16 rows for them, eight each. With those rows, every criterion has its yes and its no from at least two different people. The beard rows are on the two men, the earring and lip rows on the two women.
- Not in this part: the catalog product shots (docs/occasions.md) and the known bad renders. They follow in a later step.
- Spend so far: $3.94 of the $10 T0.4 cap, at listed prices. That is twelve Seedream candidates at $0.135 and 29 edit calls at $0.08, of which 8 were discarded attempts.
- Surprised that the edit model holds on to the reference's beard. Asking it to make a full beard patchy was ignored twice; editing an image that already had a thin beard worked. It also leaves a top button open unless a tie closes the collar, and it softens a requested hard shadow. Seedream let the new women's hair fall in front of the ears although the prompt asked for it tucked behind.
- Part 3: the user approved `woman-white-3` and `man-eastasian-1`. They are now `anchors/woman-white.jpg` and `anchors/man-eastasian.jpg`. The other four candidates and the `candidates/` folder were deleted, and all eight unchosen prompts are kept in the README.
- 16 variants, P01 to P16, are in `testdata/samples/woman-white/` and `testdata/samples/man-eastasian/`, made like part 2 and checked by eye in the same way. I applied what part 2 taught. A tie closes a collar that must read buttoned on the man (P15, P16); P05 and P07 read buttoned without one. The East Asian man's patchy rows start from his own thin beard, and his even rows ask the model to fill it in. Coloured light goes on the wall, not the face.
- Regenerated: P04, whose side shadow was soft; the second try has a hard edge across the face. P16's side shadow was soft too, so the row now uses a coloured wash on the wall behind, which reads. P10's side shadow was moderate. One retry with split lighting came out evenly lit, and one with flat frontal light came out bright. I kept the first attempt and noted it as a weak light miss, beside V08.
- The review asked for two Indian-pair redos. V18's skin had been tinted magenta; the redo keeps the skin natural and puts the coloured light on the wall. V02 was nearly monochrome; the redo is a colour photo with flat, dull frontal light. Each row's yes and no lists are unchanged.
- Every variant is now JPEG at quality 92 with metadata stripped, at the size returned (896 × 1200). `testdata/samples` went from 35 MB to 11 MB. The JPEGs measure about 42 dB PSNR against the PNGs, and a close crop shows pores intact.
- The coverage script passes on all 37 rows. Every criterion has a yes and a no, and each side comes from at least two different people.
- Spend for T0.4 at listed prices: $5.70 of $10. That is 12 Seedream calls at $0.135 ($1.62) and 51 edit calls at $0.08 ($4.08).
- Follow-up, not in T0.4 as run: the catalog product images (docs/occasions.md) and the known bad renders for the render check.
- Surprised that a hard one-sided shadow is the miss the edit model resists most. Of ten attempts that asked for a hard side shadow, only V20 and P04's second came back with one. A coloured wash on the wall, a dark room, an orange cast and flat dull light all came through.

---

### T0.4a: The catalog product images
```yaml
requires:   T0.3, T0.4
fixture-ok: no
size:       S · mid
owns:       testdata/samples/products/
status:     not-started
```
Split from T0.4, which made the people. One reference image per catalog entry in docs/occasions.md (Catalog): gold jhumkas, gold chain, the red and nude lip colours, and the four shirts and tops. Generated with Chalachithram (FAL, its own key) or photographed, as the YouCam task for each fix needs it: a single jhumka front view with no ear; the chain flat, or worn on a sample person's neck only if YouCam rejects a flat shot; garments as product shots with no person; a lip colour as a swatch. No logo, no text, no third-party pattern. `testdata/samples/products/README.md` records each image's source, model and prompt or photo note, and cost. **FAL cap: $3.**

**Done when:** every catalog entry has its reference image, meeting its YouCam file spec as docs/occasions.md states it; the README holds the source and cost for each.

---

### T0.5: YouCam probe ★
```yaml
requires:   T0.2, T0.3, T0.4, T0.4a
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
status:     done
```
Against the Dresscode Lambo server (T0.2), in scratch sessions:

- Open two sessions in one serve; derive a look in each; confirm recall in one never returns the other's.
- Recall by occasion with a text query ("family function"), and confirm the kept and dismissed fixes come back.
- Erase a session, and confirm recall then returns nothing and a second erase reports gone.
- Record every MCP request and response as JSON (no key).

**Done when:** `docs/lambo.md` lists the calls Dresscode needs with their shapes; isolation, recall and erase were each seen to work, or the gap is written as issue text in the handoff log.

Notes:

- Probed the Lambo that was already running, version 0.8.0, at `127.0.0.1:7720`. Did not start, stop, or reconfigure it, and did not send a request to port 7700 or 7710.
- Opened `dc-u-probe-a` and `dc-u-probe-b` on that one serve. Each `initialize` created the session. Each session got two `observation` concepts in one `lambo_derive`: a kept fix and a dismissed fix, both worded with "family function", and different words in each session.
- `lambo_derive` returns a receipt before the write is applied. `lambo_stats` with that receipt and `wait_ms` reported `applied` for both, `2 created (2 embedded)` each. Four embeddings. Recall of "family function" then returned that session's kept fix and dismissed fix, and not the other's. A query that used the other session's words still returned only this session's concepts, at a lower score. Recall ranks the nearest concepts in the session; it does not come back empty, and it does not filter to an occasion.
- Erase is `POST /admin/s/<session>/erase` with `{"confirm":"<session>"}`, not an MCP tool. The first erase of `dc-u-probe-a` removed 2 concepts and 2 vectors (`already_absent` false). The next recall and a fresh initialize both returned HTTP 200, `application/json`, and JSON-RPC `-32003`, with no concept text: nothing was read, and the id does not come back. A second erase reported `already_absent` true. B still returned its own look, then it was erased too so the scratch text does not stay. Both ids are tombstoned.
- Recordings are `testdata/lambo/01` through `20`. The bearer token is redacted. `02`, `04` and `05` omit the SSE event id and `retry`; the JSON-RPC message in each is the one returned. `docs/lambo.md` lists the calls and the shapes.
- Surprised that a poor query still returns this session's nearest concepts, and that an erased session answers `-32003` as JSON rather than an empty hit list or an SSE body.
