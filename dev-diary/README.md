# Development Diary: Phase Handoffs

This document breaks down the work for **Dresscode**, the YouCam API Skin AI & eCommerce VTO Hackathon entry.

Reference specification:
- [`../product.md`](../product.md): the product, occasions, who sees what, accounts, style memory, the hackathon rules.
- [`../docs/architecture.md`](../docs/architecture.md): the domain model, the pipeline, the seams, budgets, dependencies, and the facts the design rests on.
- [`ORCHESTRATOR.md`](ORCHESTRATOR.md): how the orchestrator dispatches, validates, lands and closes, with the brief template.
- [`../AGENTS.md`](../AGENTS.md): the rules every agent follows.

Where a phase document and product.md conflict, product.md wins. Record the discrepancy in the handoff log.

Each phase document gives an engineer starting cold the complete context for its tasks.

**This diary may go public.** Nothing private goes in here: no key, no account detail, no personal path beyond `~`, no private repository's name, no photo of anyone.

**Current status:** T5.12 landed (`9949451`) — the front door is the occasion picker with one-tap judge samples. T5.13 (vanity mirror capture + arched comparator) dispatches next; T5.14 closes P5B. T2.2 (YouCam client) in flight here; P3 opens when it lands.

---

## Swarm Coordination Rule

> **Phase-level dependencies are advisory. Task-level dependencies are binding.**

Do not wait for a whole phase to finish. If a task requires only `T1.1`, start it the moment `T1.1` closes.

**Fixtures make most work offline.** P0 records YouCam, Clef and Lambo replies for the sample photos into `testdata/`. Tasks marked `fixture-ok: yes` run against those files with no key, no running service and no spend. Only `b` tasks call a live service, and every YouCam unit they spend is counted.

---

## Phase Graph

| Phase | Document | Requires | Runs parallel with | Blocks |
|---|---|---|---|---|
| **P0** Ground | [PHASE-0-ground.md](PHASE-0-ground.md) | none | none | everything |
| **P1** Contracts | [PHASE-1-contracts.md](PHASE-1-contracts.md) | T0.1, T0.3 | P0 probes | P2–P6 |
| **P2** Clients | [PHASE-2-clients.md](PHASE-2-clients.md) | P1 | P4 accounts | P3 |
| **P3** Pipeline | [PHASE-3-pipeline.md](PHASE-3-pipeline.md) | P2 | P4, P5 | P4 routes, P6 |
| **P4** Server | [PHASE-4-server.md](PHASE-4-server.md) | P1; P3 for the look routes | P3, P5, P5B | P5 wiring, P6 |
| **P5** App | [PHASE-5-app.md](PHASE-5-app.md) | T1.5 (the API contract) | P3, P4 | P5B, P6 |
| **P5B** UX Overhaul | [PHASE-5B-ux.md](PHASE-5B-ux.md) | P5 | P2, P3, P4 | P6 |
| **P6** Ship | [PHASE-6-ship.md](PHASE-6-ship.md) | P3, P4, P5, P5B | none | submission |

```text
  P0 ──▶ P1 ──┬──▶ P2 clients ──▶ P3 pipeline ─────────────┐
              ├──▶ P4 server (accounts first) ─────────────┼──▶ P6 ship
              └──▶ P5 app ──▶ P5B UX & writing overhaul ───┘
```

P0 and P1 are the serial bottleneck. Once the API contract (T1.5) freezes, the app is built against recorded responses while the pipeline and server fill in behind it.

---

## Parallel Tracks

| Track | Phase | Network required? | First task | Notes |
|---|---|---|---|---|
| **A: Clients** | P2 | `b` tasks only | T2.1 | System One, YouCam, Lambo, Resend behind interfaces with replay twins |
| **B: Pipeline** | P3 | No | T3.1 | The product's judgement lives here, verified by the evaluation |
| **C: Accounts** | P4 | No | T4.1 | Guests, code sign-in, deletion; independent of the pipeline |
| **D: App** | P5 | No | T5.1 | Against the frozen API contract and recorded responses |

If capacity is short, run B and D first: B makes the advice right and D makes it something a judge can watch.

---

## Task Pattern

```markdown
### T3.5: Check the render ★
requires:   T2.1, T3.4
fixture-ok: yes
size:       M · frontier
owns:       internal/pipeline/check.go
status:     not-started
```

- **requires:** Binding task ids.
- **fixture-ok:** Whether the task can be built and tested against `testdata/` alone.
- **size:** Scope from XS to XL, then the class: the kind of work it is. The user assigns the agent and model.
- **owns:** The paths this task alone edits.
- **status:** `not-started`, `claimed:<agent>`, `in-review`, `done`, `blocked`, or `superseded` (the user replaced the task's plan; the task says by what).

| Class | Send it for |
|---|---|
| **frontier** | A decision others build on. Prompt and judgement work verified by measurement. A spec that needs interpreting. |
| **mid** | Well specified implementation, or a spec precise enough to follow literally. |
| **light** | Mechanical, low ambiguity work that its own test verifies. |
| **user** | Only the user can do it: an account, a key, a photo, a recording, a submission, a decision. |

A star (★) marks a task on the critical demo path.

A `b` task (T2.2b) is the live sibling of its parent. It owns only its live test, behind the `live` build tag. A live test never runs in CI and never gates a build. A live call is evidence: commit the test, and paste its transcript into the handoff log. A `b` task that calls YouCam names its unit budget.

---

## Developer Guidelines

1. **Claim before starting.** The orchestrator sets `status: claimed:<agent>` in the phase document on `main`. On P0 the orchestrator checks the result and lands it. From P1 a review agent reads the work, then a remediation agent fixes what that review found, and the two repeat until a review is clean.
2. **Stay inside `owns`.** A change outside it is a new task or a handoff note.
3. **Contracts freeze at the end of P1.** A change to a domain type, an interface or the API contract updates its fixtures in `testdata/` in the same commit, and is the orchestrator's call.
4. **Who sees what is enforced in code.** The judge and the check never receive YouCam analysis. The fix picker never receives the photo. Only allowlisted attributes cross. A test pins each boundary.
5. **No score reaches the user.** Probabilities steer the pipeline; the app shows reasons, never numbers.
6. **A failed render is never shown.** Change not made, or not the same person: discard it, whatever else it scored.
7. **Every paid call is quoted, reserved and settled once.** Through Keel `cost`; YouCam tasks through `providertask`. Nothing pays twice after a restart.
8. **Thresholds come from measurement.** No threshold on a Clef probability is written down before the evaluation (T3.9) sets it.
9. **New work only.** Write it here. Never copy code from Reprise, Before Yes or any earlier project; depend on Keel, Chaaya and Lambo as published.
10. **Gaps in Keel, Chaaya or Lambo become issues.** Write the issue text in the handoff log. Don't work around it here. The user files it.
11. **Document handoffs.** A task without a handoff entry is unfinished. Record what exists, what surprised you, and what the next agent should not have to work out again.

---

## Scope

Every task here ships. A task that turns out to be blocked becomes a finding, not a cut. Record it, name every task that depends on it, and mark it `blocked`. If a task is too large, split it into `T3.1a` beside `T3.1` and record the split.

---

## Critical Path

```
keys and settings (T0.2) + occasions, menus, vocabulary (T0.3) + sample set (T0.4)
  -> YouCam probe (T0.5) + Clef probe (T0.6) + Lambo probe (T0.7)
  -> domain (T1.1) -> store (T1.2) -> interfaces (T1.3) -> settings and budgets (T1.4) -> API contract (T1.5)
  -> System One client (T2.1) + YouCam client (T2.2) + Lambo client (T2.3)
  -> judge (T3.1) -> pick (T3.2) -> measure (T3.3) -> render (T3.4) -> check (T3.5) -> fallback (T3.6)
  -> shop (T3.7) + remember and recall (T3.8) -> evaluation (T3.9)
  -> look routes (T4.4) + samples for judges (T4.6)
  -> capture (T5.3) -> verdict and render (T5.4, T5.5) -> second visit (T5.6)
  -> UX writing & mock media (T5.10) -> shell & account UX (T5.11) -> front door (T5.12) -> mirror capture & look UX (T5.13) -> visual verification (T5.14)
  -> deploy (T6.1) -> README (T6.2) -> video (T6.4) -> submit (T6.5)
```

The demo is built around one story, **the second visit** (product.md). First visit: a family function, the look misses two criteria, the jhumkas render is kept and the red lip dismissed. Second visit, same occasion: Dresscode recalls both, offers the jhumkas first, never the red lip, and the render lands on her own photo with every product labelled. The user in the demo is the AI sample woman (T0.4). Protect everything that moment depends on before anything else.

---

## Status Board

| Phase | Tasks complete | Status |
|---|---|---|
| P0 | 8 / 8 | Done. |
| P1 | 5 / 5 | Done. The API contract is frozen. |
| P2 | 6 / 9 | T2.1 landed. T2.2 (YouCam client) is ready now that T0.5 landed; T2.2b follows it. |
| P3 | 0 / 9 | Waits for P2. |
| P4 | 4 / 7 | T4.4 waits on the pipeline. T4.6 waits on the evaluation. |
| P5 | 9 / 9 | **Complete.** The app is finished on mock data: shell, capture, verdict, render, history, account, accessibility gates, and the 3:1 control boundaries. Phone camera and phone install still need you. |
| P5B | 3 / 5 | T5.12 landed — the front door is the picker, with judge samples. T5.13 (vanity mirror capture + comparator) next. |
| P6 | 0 / 6 | Waits for P3–P5B. |

### Units and spend

| Service | Budget | Spent |
|---|---|---|
| YouCam | 1040 credits in two pools that expire (event pool after judging closes); 400 held back for judging | 24 (T0.5), balance 1,016 read from the credit endpoint |
| FAL (sample images, Chalachithram's key) | $10 for T0.4, listed prices | $5.70 (T0.4: 12 Seedream at $0.135, 51 nano-banana-2/edit at $0.08) |
| Workers AI | Free plan until T6.6; then Workers Paid | 7 embeddings (the access check, four in the T0.7 probe, two in the T2.3b live check); T0.6: 78 Clef calls, 3,292 neurons in one day (76 on clef, 2 on clef-flash) |

---

## Handoff Log

Newest first. One entry per closed task, finding, or split.

- **T5.12 Front door.** `/` is now the product: five editorial occasion cards immediately interactive, selectable fix chips with custom check indicators, the second-visit recall banner above the fixes when history is on (runtime-only memory words, fetched only when an occasion is selected and the switch is on; a failed or unmet answer leaves the menu without the line), one-tap judge samples from the recorded `/api/samples`, and a primary `Bring the look` into capture. The gate discipline paid before landing: `--line` over `--raised` computes 2.81:1 in dark, so the raised cards take the soft edge and the banner is sunken — and the review endorsed remediating T5.11's `.info-card` to the same sunken recipe (its raised drawing failed dark). Four POST fixtures recorded so all five judge samples work in mock. The slimmed lead sentence was ratified as authored. Accent/accent-soft joined the boundary table (8.81/8.00); the a11y matrix grew to 26 mounted tests over 13 states. Two review rounds; the second re-ran the whole gate on a patched copy at the remediated tip. Landed as `7c9b121`+`4324fcf`+`9949451`, gate green in the worktree and the main checkout. Note for T5.13: the judge flow intentionally never reads the POST response — the render screen follows the recorded story look.

- **Decision: the agent-picked templates are approved.** The user accepted `all_garibaldi` (Garibaldi) for festival-full-beard and `female_crewcut` (Crewcut, keeps the hair colour) for interview-short-hair, without a live render first. Neither has been rendered yet; T3.9 will show how Clef reads them.

- **T0.3a YouCam templates for the hair and beard fixes** (split from T0.3). A fix now carries `template`, the YouCam template id; the loader requires it for hair-transfer and beard-style and refuses it elsewhere, naming the file and the fix (a contract change on `domain.Fix`, the orchestrator's call; nothing outside `internal/domain` reads it yet). **User decisions:** tie-hair fixes render `female_casual_updo` and set hair to `bun`; short-beard fixes render `all_circle` and set beard to `goatee`, so memory describes what the render shows. User-facing wording is unchanged. **Agent picks awaiting the user, not yet rendered:** festival-full-beard `all_garibaldi` (beard `full`) and interview-short-hair `female_crewcut` (hair `short`, keeps the hair colour; `all_crewcut` is the swap but may recolour). YouCam's template id prefixes do not match their listing category.

- **T0.5 YouCam probe** (`aaf9866`). A recorded success and measured unit cost for every menu task: `ai-smile` 1, `makeup-vto` 1, `2d-vto/earring` 1, `2d-vto/necklace` 1, `cloth-v4` 2, `hair-transfer` v2.1 2, `beard-style` 2, `face-attr-analysis` (faceShape only) 10, each read from the balance before and after: **`GET /s2s/v1.0/client/credit` answers to the v2 key**, so there is a balance endpoint after all (feedback.md corrected), and the orchestrator re-read it at 1,016. **Spend: 24 of 150 units.** 149 recordings in `testdata/youcam/` with uploads and results as files (2.9 MB); `internal/youcam/replay.go` loads them with keys unchanged, and its guard against any `testdata/youcam` is removed. Error shapes recorded, all at 0 net units. Tasks finish in 3.7–12.6 s; result links are signed for two hours and a re-poll gives a fresh one for free; no rate-limit headers. **Corrected:** units do expire (docs/keys.md fixed); makeup must send `skin_smooth` at 0, since leaving it out smooths at 50 (docs/occasions.md fixed). **Renders:** lip at 0 smoothing, earring, placed necklace and the kurta are clean; the boys' kurta renders fine on an adult chest-up. AI Smile and hair transfer smooth the skin, and the hair updo drops the earrings, so the render check will likely reject them. The necklace needs anchor and wearing points. **Open for the user:** which hair template stands for `tied-back` (no template is titled so; Casual Updo reads as a bun) and which beard template stands for `short` (Circle reads as a goatee). **Contract change (orchestrator's call):** T2.2 adds a `Status` field for analysis results carrying `faceShape` only. Not observed: a real expired upload, and running out of units.

- **T5.11 Brand mark, editorial shell, button system and Account/History overhaul.** The arched vanity-mirror SVG mark sits beside the wordmark and in all four regenerated app icons (`rsvg-convert`, machine tooling, no dependency; the manifest's SVGs rewritten too so installed apps don't ship the old mark). The header is one slim row — mark, wordmark, three quiet links; `Appearance` lives in Account as a segmented pill defaulting to `system`, its fieldset/legend/radios byte-identical for keyboard and Chaaya. theme.css carries the four button tiers (primary/secondary/ghost/danger) with micro-press and transitions only under `prefers-reduced-motion: no-preference`; seven contrast pairs were computed before being added and all pass in both themes. History's notice is a neutral `.info-card` and its switch is a real sliding toggle behind the untouched label structure. Account: the four canonical copy swaps, one shared `sessionLine` with no dead branch, 6-cell OTP frames behind the exact original inputs, `Who sees what` in an open disclosure, and destructive triggers in danger — including "Delete the account", whose class swap the orchestrator ruled at remediation after the implementer correctly proposed rather than took it (the phase had named only three triggers). The h1 became a wordmark span; every screen still leads with a heading. Review came back CLEAN, reproduced all seven ratios, re-did the break-proof, and confirmed the icon and manifest handling. Landed as `366be74`+`e0c4dc2`+`5075c93`, gate green in the worktree and the main checkout, 30 a11y tests. Two cosmetic notes recorded in the task: maskable icon's bar end can overshoot the safe zone by ~2.5px, and the `.appearance` legend's clearance is visual only.

- **T5.10 UX writing, occasion fixtures and real mock media.** The overhaul's first task: product.md's four jargon sentences rewritten with privacy.ts's readers moved atomically (account.test.ts renders product.md's own sentences and stays green); fifteen `Renders …` fix descriptions replaced across the occasion fixtures (clef questions byte-untouched — the 37 recordings depend on them); three error sentences polished including the user's final `lease_refused`. Mock mode now serves the real sample photographs (V04, V05, the gold jhumkas) behind the recorded media ids — base64 through a virtual module, dynamically imported so the photos never enter the production bundle — and the recall story answers `recall.json` once the history switch is on, so dev shows the second-visit sentence. Two out-of-owns edits (one `preferFixture` line in history.test.ts, two ambient type lines) were taken mid-task, disclosed, verified minimal by review, and ratified at landing with the approval record corrected — the note had said "approved by the orchestrator mid-task", which was not accurate. Review came back CLEAN. Landed as `1dadce7`+`580b01e`, gate green in the worktree and the main checkout. `docs/occasions.md` needed no change — its fix column was already human.

- **New phase P5B: UX, Component System & Writing Overhaul (`PHASE-5B-ux.md`, `T5.10`–`T5.14`).** Following the UI exploration and screenshot review (`/tmp/ui-explore/`), the user decided on a radical overhaul before shipping: unify the fragmented screens into a 3-step Mirror journey (Front Door with occasion cards, selectable fix chips, recall banner and one-tap judge samples → Arched Vanity Mirror Capture with button-styled file pickers, collapsible product drawer and `Check the dress code` CTA → Look screen with 4-step live pipeline stepper, arched Before/After comparator with visible split line/handle/chips, Fix hero card, 3-column verdict bar, and interactive fidelity popovers); add an arched vanity-mirror SVG brand mark and slim the header (`Appearance` moves into `Account` as a segmented 3-way pill bar, defaulting to `system`); upgrade `History` with a tactile toggle switch and neutral `.info-card` styling; upgrade `Account` with a 6-cell OTP code input, collapsible `Who sees what`, and `.danger` buttons; serve real sample JPEGs from `testdata/samples/` at `/media/*` in mock mode; and clean up the engineering/pipeline jargon across `product.md`, `testdata/occasions/` (`fixes[].changes` only, leaving `criteria[].clef` untouched), and `web/src/`. Split into `T5.10` → `T5.11` → `T5.12` → `T5.13` → `T5.14`. `T5.10` is ready now.

- **Mail story closed: the sender is the verified domain.** Root cause of T2.4b's refused send: `RESEND_FROM` named `dresscode@nryn.dev` while the account's verified domain is `send.nryn.dev` — Resend refuses with an unnamed 403. The from-address is now `dresscode@send.nryn.dev` in `.env` and docs/keys.md. The production path was proven with one accepted send (401 ms) to an external, non-account-owner mailbox (Proton) — arrival confirmation pending from the user. The `onboarding@resend.dev` quick path is no longer needed anywhere.

- **T5.9 Control boundaries meet 3:1 — and with it, P5 is complete.** The user's T5.8 decision landed: `--line` is `#827c70` light and `#706659` dark, the nearest channel-scaled step clearing 3:1 with a cushion (3.11–4.08 across every drawn adjacency, old values 1.21–2.02); `--line-soft` stays soft by name, excused in the new drawn-boundary guard. The suite gained the boundary list at 3:1 through Chaaya's per-call minimum (two calls: text 4.5, boundaries 3), with `--sunken` added because the swipe frame draws it and it is light's binding pair. Break-proven: reverting the old light hex fails six named pairs in one error. The review reproduced every ratio independently to 1e-9, re-did the break, and left two advisory items taken as disclosure: a side-specific border shorthand would slip the sweep's property filter (none exists; widen in a later pass if one appears) and the line-soft binding-pair figure corrected in the notes. Landed as `6914cca`, gate green in the worktree and the main checkout, 30 a11y tests. Note for the next theme change: every drawn border role must join a measured pair or be excused by name.

- **T2.1 The System One client.** `WorkersAI` and `OpenRouter` in `internal/systemone/`, both behind `SystemOne`. One Ask is one metered call — reserve the estimate, retry the POST through Keel `throttle` (429 three attempts, 5xx/timeout two, Retry-After honoured, never a 4xx), settle the measured `input_tokens` at the model's neuron rate (constants sourced in-code from docs/clef.md's T0.6 measurements). Oversize/unreadable images die typed before any request and before any reserve; 16 MP comes from header-only dimension reads. All 45 clef/cost recordings replay through the real client over HTTP, content-keyed; the T1.3 replay twin needed no change. No OpenRouter recording exists — the decisions shape is pinned by a labelled inline fixture, and the comparison task must record the real exchange and re-check Jev pricing. The first review found two LOWs and two INFOs (a Retry-After test that couldn't catch its own regression; unscrubbed envelope notes; a dead prefix check; missing zero-charge pins); the remediation fixed all four — the new 3s ceiling derived from keel's own `hintWait` spread — and the second review verified it against the keel source. Landed as `d764228`+`16cbd91`, gate green in the worktree and the main checkout, `go test -race -count=1` clean. P3's judge interface now exists.

- **T5.8 Accessibility and contrast.** Chaaya's own `a11yGate` and `contrastGate` run on every screen inside `./tools/check.sh`: 11 screen states (dialogs and the shop sheet included, portals measured) × forced light and dark at a 375 px viewport, plus all eleven text pairs the screens draw at 4.5 across all four theme blocks, plus honesty guards (every colour outside the theme blocks must be a role; every drawn pair must be measured) and a 375 px width scan. jsdom entered as a dev dependency with the user's approval — the DOM host Chaaya's a11y gate needs. No genuine failure on any landed screen; the routed WCAG 1.4.11 finding (control boundaries under 3:1) became the user's decision and then T5.9. The first review verified the matrix; the remediation exposed that the colour-declaration parser had wrong capture groups — **both colour guards had passed vacuously** — and made them live (73 declarations now parsed; the old parser collected zero), extending them to svg paints, semicolon-less final declarations, and a whole-screen width sweep, each with a /tmp break-proof; the second review reproduced all three breaks and confirmed the old parser collected nothing. Two residual documentation LOWs (a stale line citation, two theoretical blind spots) were fixed by the orchestrator under the docs exception and recorded in the task's notes. Landed as `9d0d934`+`6f7d40a`+`7f6581c`, gate green in the worktree and the main checkout. Lesson recorded with it: a guard that has never failed is not yet a guard.

- **Two decisions on T5.8's findings (user, 2026-10-30).** The jsdom dev dependency was confirmed by the user directly ("You asked me and I said yes") — the record in T5.8's task notes stands. And on the WCAG 1.4.11 finding (control boundaries `--line` at 1.39–1.95 against ground/surface, guideline 3:1), the user accepted the recommendation to strengthen the line colour and gate it. That work is T5.9, dispatched once T5.8 lands (their branches would both touch web/tests/a11y/).

- **T0.4a The catalog product images** (`2ed898f`). `testdata/samples/products/` holds one reference per catalog id: six real product photos from retailer product pages (the user's fair-use decision) and two lip swatches made locally (red `#B0172F`, nude `#B8836F`). The README gives each source page and every edit. **User decisions:** the three shirts had the store's woven neck label painted out locally, nothing else changed, accepted; the festive top is a boys' maroon zari kurta, the only model-free festive kurta found, accepted, to be replaced by an adult one if T0.5 shows it renders short or tight. The gold chain is listed by its store as gold-plated silver. No spend, no keys. **Next agent should know:** ethnic-wear stores show adult kurtas only on models; western shirt stores shoot without one but show their label inside the collar; stock sites watermark.

- **T0.6 Clef probe** (`98a5f95`). Clef's request and response shapes are in `docs/clef.md`. 37 judge recordings on `clef`, one per sample variant (photo, the criteria as `noul`, the biggest-issue `choice`, no YouCam analysis), in `testdata/clef/` in the replay format. The check is recorded on proxies only (V04 against V05 reads 0.91–0.99 on change made, same person and criterion met; V04 against the white woman's anchor reads 0.02–0.06), on clef and clef-flash; redo it after T0.5. Clef is deterministic: ten repeats identical to the last digit. Tokens per image: 1,002 at 896×1200, 771/435/195 at 1024/768/512 px, exactly the documented tokenizer; the question text costs as much as the photo. One look is about 132 neurons all on Clef, 57 with a Clef-flash check (flash is weaker on the vocabulary). Agreement with the README at 0.5, counted only, not a threshold: 260/273; misses gather on the men's beard and light; V08 is the weakest row. **User decision:** `family-hair-finished` reworded to accept short, neat hair (the old wording failed every man); men now read 0.84–0.89. **Contract change (orchestrator's call):** the System One replay loads a recorded image by file, relative to the recording, with request keys unchanged, so `testdata/clef` is 0.6 MB instead of 20 MB; T1.3's guard against any `testdata/clef` is removed. Spend: 78 calls, 3,292 neurons. **Next agent should know:** Clef answers a judge with no image instead of failing, so the judge (T3.1) must refuse to call it without the photo. Rewording one question shifts the answers to the others, so a wording change re-records the whole occasion and thresholds must be measured on final wording. A recording replays only for a byte-identical body. Open findings: `interview-collar-buttoned` reads low on a shirt and tie (V09); the vocabulary has no "calm" expression.

- **T5.6 History and the second visit.** The history screen holds the switch (off by default), the API's sentence of what is kept and where, and, with history on, the second visit: the recorded recall sentence verbatim and past looks grouped by occasion. Off, absence is pinned at three levels — no screen's markup anywhere in the app carries a word of memory, the reader refuses any memory member beside a closed switch, and the second-visit read fetches nothing at all. One fixture added: `recall-empty.json`, the contract's empty-memory example at the date occasion. The review found one minor hole (a stray `occasion` beside the closed switch was tolerated); `08eb3b4` refuses it, with a pin proven against the old reader. Second review clean. Landed as `fd8b525` + `87989ec`, gate green in the worktree and the main checkout. No physical phone (the standing unmet-note class). Surprises worth keeping: a Svelte 5 state variable named `state` breaks svelte-check while vitest passes (feedback.md, Svelte); and a seam for a later task — the account screen's history deletion turns the stored switch off server-side, and this screen's session-held value learns of it only on a reload.

- **T2.4b Resend live — landed, done-when met on the user's confirmation.** The live test (`internal/mail/live_test.go` behind the `live` tag; reads `RESEND_API_KEY`/`RESEND_FROM`/`RESEND_TO`, skips without them; one send, no retry; the key cannot reach output on any path — verified path by path in review). The first send from the configured from-address was refused (Resend `403`, no error name; see feedback.md). With the user's approval, the account-owner quick path sent one code from `onboarding@resend.dev` at 2026-10-30T14:33:40Z, accepted in 415 ms; the user confirmed arrival within about a minute — in Gmail's spam (its own deliverability finding, feedback.md). The configured from-domain `nryn.dev` remains unverified: production-sender proof and a spam-free demo wait on the console fix. A review of the test came back clean; landed as `d1e7681` over the T2.5 landing, diary conflict resolved keeping both tasks' notes, gate green in the worktree and the main checkout.

- **T2.5 The memory client survives session expiry.** `internal/memory.Client` reads a 404 on a call that carried an `Mcp-Session-Id` as an idle session the serve expired: it drops the dead id, runs the same initialize and notifications/initialized open a first call runs, and replays the tool call once; a second 404 is the typed `ErrSessionExpired`, no third attempt, one re-initialize per session in flight. Recorded `21-call-on-expired-session.json` in the T0.7 envelope. The first review found two LOWs — a 404 on the notifications/initialized leg left a poisoned gate that never re-initialized, and the doc comment understated where `ErrSessionExpired` can come from. The remediation (`15e8114`) drops the id on a notify 404 so the next call recovers, and states both Remember legs; the second review came back clean. Landed as `d7f366d` + `1cab332`, gate green in the worktree and the main checkout, `go test -race -count=1` clean.

- **Decision: product images come from web image search** (user decision: fair use). T0.4a takes real product photos for the six exact products, never generated ones, because an exact-product label must stand for a real item; the lip colours are swatches. Each image's source URL is recorded; none may show a logo, a watermark or a person. Changed: T0.4a, docs/occasions.md (Catalog).

- **T0.4 The sample set** (`40e9cd4`). Four AI-generated sample people, each an original Seedream v5 pro design the user approved: an Indian man and woman, a white woman (red hair) and an East Asian man. 37 nano-banana-2/edit variants as JPEG in `testdata/samples/<person>/`; `testdata/samples/README.md` holds each variant's expected flags (the T3.9 ground truth), prompt and cost. Every criterion has a yes and a no, each from at least two people. The second-visit pair is V04 (bare ears and lip) and V05. The orchestrator's review had V02 (read as a monochrome filter) and V18 (magenta-dyed skin) redone, and the PNGs converted to JPEG q92 (35 MB to 11 MB); the branch was squashed so the PNGs and unchosen candidates never enter `main`'s history. Weakest calls: the light misses in V08 and P10 are moderate shadows, flagged in the README. FAL: $5.70 of $10. **Split:** the catalog product images became T0.4a; T0.5 now requires it. **Next agent should know:** Seedream v5 pro takes no seed, so the anchor files are the anchors. The edit model keeps the reference's beard (start a patchy beard from a thin one), leaves a man's top button open unless asked for a tie, resists a hard one-sided shadow, and dyes the skin when coloured light is asked for on the face (put it on the wall). Set `CHALACHITHRAM_ENV` to Chalachithram's own `.env`, or its search finds Dresscode's first.

- **New task T2.5: the memory client survives session expiry.** Found while recording landings in Lambo: idle MCP sessions die with HTTP `404` `Not Found: Session not found` after roughly 10–20 idle minutes (two orchestrator sessions lost this way; see feedback.md under Lambo), and `internal/memory/lambo.go` has no 404 handling at all. A user idling past that before their second visit would fail recall. T2.5 (S · light, beside its siblings in PHASE-2) adds re-initialize-and-replay-once to the client, pinned by a recorded 404 exchange; a second 404 fails typed. T3.8 and the live demo depend on it.

- **T5.5 The render and shop the look.** The render screen follows the recorded family-function look: a before/after swipe over one range input with the original one tap away, a Bits UI Dialog styled as the bottom sheet (v2 ships no Sheet) listing every product with its fidelity label validated against the three domain spellings, and keep / dismiss / bought as one-field verdict PUTs. Guideline six is enforced twice — the server withholds a discarded render and the reader discards again, so a failed render cannot reach a picture frame even from the wire. With no render passed, the plain sentence stands with the suggestion and the products. A review of `f4e8065` came back clean; the 375 px clause holds on pinned styles plus a browser-viewport pass, no physical phone (same unmet-note class as T5.1 and T5.3). Bits UI prop-carried classes must be `:global()`; the banned-words shell test matches inside `aspect-ratio`. Landed with the gate green in the worktree and the main checkout.

- **T2.4b Resend live — refused at the first send; parked on the console.** The live test is committed (`1f53fce`, behind the `live` tag, reads `RESEND_API_KEY`/`RESEND_FROM`/`RESEND_TO`, skips without them, one send, no retry; the message id is captured through a `slog.Handler` so the landed sender is unchanged). The one run, at 2026-10-30T13:38:42Z, was refused with `403` and no error name; no mail, no code in any log, nothing to confirm. Done-when unmet, recorded, not glossed. A 403 with no name reads as account or domain gating — the from-domain's verification or the key's send permission in the Resend console — and only the user can look there. The branch stays parked at `in-review`; review runs together with the confirmation send once the console is fixed. See `feedback.md` under Resend.

- **Decision: four sample people, not two.** The user added a white woman and an East Asian man beside the Indian pair (anchors man-1 and woman-1, approved), so no criterion's evaluation rests on one face. The second-visit story stays with the Indian woman. T0.4's $10 FAL cap is unchanged and now covers the second pair too. Changed: AGENTS.md, product.md (Sample people), T0.4.

- **T5.4 Progress and the verdict.** The verdict screen follows Chaaya's keel `JobStream` through the four stage words, pauses hidden and resumes visible, and shows the missed criteria, the fix and its why with no digit anywhere. A terminal ending the screen cannot narrate ends the wait with the `job_failed` sentence. Fixtures for bare `cancelled` and `interrupted` are in `testdata/api/` in the recorded shape. A review of `ff3eba2` found one minor hang-class gap; `54be3f4` fixed it and a second review came back clean, accepting the bare-`error` coverage. Landed as `8b9ba84` with the gate green in the worktree and in the main checkout. Chaaya pauses hidden by core default; the mock ignores `Last-Event-ID` and correctness rests on Chaaya's follower refusing replayed ids — T4.4's real stream must keep working with that client.

- **Decision: the sample people are AI-generated.** The user ruled out their own photos. Fixtures, the demo and the video use two generated people, a man and a woman, made with Chalachithram's character lock (an original seedream anchor per person, variants composed from it). No photo of a real person anywhere, the user included. The woman carries the second-visit story (jhumkas kept, red lip dismissed); the man covers the beard criteria. T0.4 is now `M · mid` with the user approving both anchors. Changed: AGENTS.md (photos and people), product.md (Sample people), docs/occasions.md (necklace reference, catalog source), T0.4, T6.4. Risks for T0.5 and T0.6 to check: YouCam's face detection and Clef's reading on generated faces.

- **Finding against T4.3's landing: main is red.** `postJSON` is declared three times in package `server_test` (`signin_test.go:447`, `delete_test.go:467`, `intake_test.go:394`), so `go vet` fails and the server tests do not compile. The collision came in with the T4.5 and T4.3 merges: each implementer added its own helper, and each branch's gate ran on a base without the other's file. The post-merge gate of land step 4 evidently did not run on either landing. A remediation agent collapsed the two colliding helpers in `package server_test` into one canonical `postJSON` in `server_test.go` (string payloads sent verbatim, variadic `remote` for the sign-in tests' IP pinning); `intake_test.go`'s identically named helper is `package server` and was never part of the collision. A review of `41922c1` came back clean and the gate passed in the worktree and again in the main checkout after the ff-merge. Process note for every future landing: the gate runs in the main checkout after the merge, not only on the branch.
- **T4.3 Deletion.** Account deletion is one Keel erase over blobs, every user-data table, the identity rows, and the Lambo session. A job stopped halfway resumes and finishes empty. A guest gets `not_found`. A review of `7303a37` came back clean and the gate passed. Tests use fakes.
- **T4.5 Intake.** Resumable uploads go through Keel into mediastore. A photo with GPS comes out without it. A private link is refused. A page with no image asks for a photo instead. A review of `a5fd021` came back clean and the gate passed.
- **T5.7 Sign-in and deletion.** The account screen signs in by emailed code. An empty choice upgrades the guest. `switch` moves the device onto an existing account and does not copy the guest's looks. Account deletion follows the mock job to the end with no count on screen. The privacy lines are the product's sentences. A review of `c5739c4` came back clean. The shell keeps both the capture route and the account route.
- **T5.3 Bring the look.** The capture route takes a selfie or a product. A gallery file uploads and a dropped chunk resumes, both against the mock. A review of `63da8fd` came back clean aside from a physical phone camera, which was not used. A product photo is registered with `POST /api/products`. Chaaya's uploader has no upload group.
- **T5.2 Occasion and opt-in menu.** The screen lists the five occasions and saves ticks per occasion. A later read returns that occasion's set and not another's. Nothing asks or infers gender. A review of `a890c0b` came back clean and the gate passed.
- **T4.2 Sign-in by emailed code.** `MountSignIn` serves the two code routes through Keel identity. A new address upgrades the guest in place. An existing account with guest looks returns `guest_data_conflict`, then `choice` `switch` moves the device and leaves both looks where they were. `merge` is `invalid_request`. Tests cover a fresh sign-in, that switch, a wrong code, an expired code, and the send limit. A review of `2abd459` came back clean. No live mail.
- **T2.3b Lambo live.** One remember, recall, and erase on `127.0.0.1:7720`, session `dc-u-live-t23b`. Recall returned the two lines that were stored. Erase removed them. The test reads `LAMBO_URL` and `LAMBO_DRESSCODE_TOKEN` from the environment and does not open `.env`. A second review of `61d4cd9` came back clean. The gate does not run the live test.
- **T5.1 Shell, theme and API client.** The shell, Chaaya theme roles, the manifest, and the typed API client are in. Mock mode serves `testdata/api`. Every recorded error code has a human message. A review of `9c45b71` came back clean aside from installing the app on a phone, which was not done. The gate passed.
- **T2.3 The Lambo client.** `internal/memory.Client` speaks MCP streamable HTTP. A remember is confirmed through its receipt before the caller is told it was remembered. A repeat erase reports gone. `Target` is a Keel erase target named `memory`. Tests replay `testdata/lambo` and do not dial. A review of `beba7c7` came back clean and the gate passed.
- **T4.1 Guests and sessions.** A first visit mints a guest. The cookie carries only the signed session id. A revoked session does not resolve, and the next request is a new guest. `POST /api/looks` is behind Keel `gate`. A review of `f6fb783` came back clean and the gate passed. `dresscode serve` still does not listen.
- **T2.4 The Resend sender.** `internal/mail.Client` implements Keel `identity.Sender` for sign-in codes. Plain text and a minimal HTML version. The from address and the key come from settings. Keel's send ceilings apply before the post. A recorded success body is the fixture. The code is not logged. A review of `945098b` came back clean and the gate passed. No live mail was sent.
- **T1.5 The API contract.** Frozen at `1f81cf7` after a second review came back clean. Sign-in uses Keel's empty choice or `switch`. Account deletion streams stage `delete`. Every product flow has a route and an example in `testdata/api/`.
- **T1.3 The interfaces and their replay twins.** `SystemOne`, `YouCam`, and `Memory`, each with a replay over the recorded envelope. All twenty `testdata/lambo` exchanges replay. A missing recording fails with its key. The two identical erases of `dc-u-probe-a` refuse to choose. No Clef or YouCam recordings yet, so those replays fail the call with the key. A review of `8e94603` came back clean and the gate passed.
- **T1.4 Settings, budgets and flags.** `internal/config` loads `dresscode.toml.example` through Keel `config`. Keys are references. YouCam is a lifetime budget of 1040 credits, 400 held for judging. Workers AI is one dollar per day in the example, because no dollar ceiling was recorded. A judge session in the example is capped at 5 fresh selfies and 10 renders. The three flags default off. A missing variable fails by name. A review of `ca06ee4` came back clean and the gate passed. The merge kept both the sqlite and the TOML indirect modules.
- **T1.2 The store.** `internal/store.Migrate` applies six files through Keel sqlite under the namespace `store`. `UserTables()` is the deletion list. Every owned row carries `user_id`. Identity's tables are not created. The analysis cache key is the normalised photo's hash, scoped to that user, plus the YouCam task. A review of `395aff7` came back clean and the gate passed.
- **T1.1 The domain model.** `internal/domain` loads `testdata/occasions` and validates it. Fidelity labels are exact product, approximate match, and visual simulation. The catalog tokens `exact` and `approximate` decode as the first two. `Allowlist` is face shape, hair type, hair length, and colour tones, with no photo. A bad file returns `FileError` with the base name and the reason. Interview and funeral share `lip-quiet`. A review of `a0cb3cd` came back clean and the gate passed.
- **T0.3 Occasions, fix menus and the look vocabulary.** Five occasions in `testdata/occasions/`, prose in `docs/occasions.md`. Each has criteria, an opt-in menu, and a basket. Fixes use printed YouCam paths from Skin, Beauty, Fashion, Jewelry and Watch, and Hair and Beard. The family-function story is gold jhumkas kept and the red lip dismissed. Light, watches, rings, and hair colour have no render. No units spent. Landed without a separate sign-off because the user said not to wait.
- **T0.7 Lambo probe.** `docs/lambo.md` lists the calls Dresscode needs: initialize, notifications/initialized, `lambo_derive`, `lambo_stats` with the receipt, `lambo_recall`, and `POST /admin/s/<session>/erase`. Recorded in `testdata/lambo/` against Lambo 0.8.0 at `127.0.0.1:7720`. Two scratch sessions each stored a different family-function look. Recall in one did not return the other's. The query "family function" returned the kept fix and the dismissed fix. Erasing a session made the next recall and initialize return JSON-RPC `-32003` with nothing read, and a second erase reported `already_absent` true. The other session was unchanged, then erased. Four `bge-m3` embeddings. Surprised: recall returns this session's nearest concepts even for a query that names the other look, and an erased session answers HTTP 200 `application/json` rather than an empty hit list. Erased ids `dc-u-probe-a` and `dc-u-probe-b` must not be reused.
- **T0.2 Keys, accounts and settings.** `docs/keys.md` names every key and where it lives. YouCam console balance is 1040 credits and they do not expire. Fix selection is Clef. Dresscode's Lambo is its own process on `127.0.0.1:7720` with its own sqlite file, embedder Workers AI `bge-m3`. The dogfood server is untouched.
- **Review and remediation from P1.** On P0 the orchestrator checks a result and lands it. From P1 a review agent reads the branch and runs the gate, then a remediation agent fixes what the review found. Those two repeat until a review comes back clean, and only then does the work land.
- **T0.1 Repository on git footing.** Module `github.com/nrynss/dresscode`, Go 1.27.1, Keel v0.9.0, no toolchain line. `version` prints `dresscode 0.0.0`; `serve` exits without listening, dialing, or reading a key. The app is a Vite and Svelte 5 static page on `@nrynss/chaaya@0.5.0` and `bits-ui@^2`, with eslint, svelte-check, and Vitest. `./tools/check.sh` is the gate, and CI runs it on push to `main` and on pull requests. `.env.example` is a comment until T0.2 names the variables. Surprised: the Chaaya root export is only `packageName`; `@sveltejs/kit` is an optional peer and was not added; pnpm's minimum release age rejects the exact `@nrynss/chaaya@0.5.0` pin, so `web/pnpm-workspace.yaml` excludes that version; `go mod tidy` added no indirect modules because `keel/id` uses only the standard library.
- **Fix selection is Clef.** Jev on OpenRouter stays available so the two can be compared on the same pick requests. That comparison is still required. Clef remains the picker until it is done.
- **Repository started.** Product, architecture, agent rules and this diary. Decided before the first task: guests plus emailed-code sign-in, no Google; style memory is text only, with Clef describing each render in a fixed vocabulary; Keel v0.9.0, Chaaya 0.5.0, Lambo v0.8.0. Checked: the YouCam key authenticates on the India server (an upload slot, no units), and a Workers AI token lists Clef, Clef-flash and bge-m3 and ran one bge-m3 call.
