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

**Current status:** P0 not started.

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
| **P4** Server | [PHASE-4-server.md](PHASE-4-server.md) | P1; P3 for the look routes | P3, P5 | P5 wiring, P6 |
| **P5** App | [PHASE-5-app.md](PHASE-5-app.md) | T1.5 (the API contract) | P3, P4 | P6 |
| **P6** Ship | [PHASE-6-ship.md](PHASE-6-ship.md) | P3, P4, P5 | none | submission |

```text
  P0 ──▶ P1 ──┬──▶ P2 clients ──▶ P3 pipeline ──┐
              ├──▶ P4 server (accounts first) ──┼──▶ P6 ship
              └──▶ P5 app (against the contract)┘
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
  -> deploy (T6.1) -> README (T6.2) -> video (T6.4) -> submit (T6.5)
```

The demo is built around one story, **the second visit** (product.md). First visit: a family function, the look misses two criteria, the jhumkas render is kept and the red lip dismissed. Second visit, same occasion: Dresscode recalls both, offers the jhumkas first, never the red lip, and the render lands on the user's own photo with every product labelled. Protect everything that moment depends on before anything else.

---

## Status Board

| Phase | Tasks complete | Status |
|---|---|---|
| P0 | 4 / 7 | T0.4 waits on the user's photos. T0.5 and T0.6 wait on T0.4. |
| P1 | 1 / 5 | T1.1 done. T1.2, T1.3 and T1.4 are claimed. |
| P2 | 0 / 4 | Waits for P1. |
| P3 | 0 / 9 | Waits for P2. |
| P4 | 0 / 7 | Accounts (T4.1–T4.3) start after P1. |
| P5 | 0 / 8 | Starts after T1.5. |
| P6 | 0 / 6 | Waits for P3–P5. |

### Units and spend

| Service | Budget | Spent |
|---|---|---|
| YouCam | 1040 credits, they do not expire; 400 held back for judging | 0 |
| Workers AI | Free plan until T6.6; then Workers Paid | 5 embeddings (the access check, plus four in the T0.7 probe) |

---

## Handoff Log

Newest first. One entry per closed task, finding, or split.

- **T1.1 The domain model.** `internal/domain` loads `testdata/occasions` and validates it. Fidelity labels are exact product, approximate match, and visual simulation. The catalog tokens `exact` and `approximate` decode as the first two. `Allowlist` is face shape, hair type, hair length, and colour tones, with no photo. A bad file returns `FileError` with the base name and the reason. Interview and funeral share `lip-quiet`. A review of `a0cb3cd` came back clean and the gate passed.
- **T0.3 Occasions, fix menus and the look vocabulary.** Five occasions in `testdata/occasions/`, prose in `docs/occasions.md`. Each has criteria, an opt-in menu, and a basket. Fixes use printed YouCam paths from Skin, Beauty, Fashion, Jewelry and Watch, and Hair and Beard. The family-function story is gold jhumkas kept and the red lip dismissed. Light, watches, rings, and hair colour have no render. No units spent. Landed without a separate sign-off because the user said not to wait.
- **T0.7 Lambo probe.** `docs/lambo.md` lists the calls Dresscode needs: initialize, notifications/initialized, `lambo_derive`, `lambo_stats` with the receipt, `lambo_recall`, and `POST /admin/s/<session>/erase`. Recorded in `testdata/lambo/` against Lambo 0.8.0 at `127.0.0.1:7720`. Two scratch sessions each stored a different family-function look. Recall in one did not return the other's. The query "family function" returned the kept fix and the dismissed fix. Erasing a session made the next recall and initialize return JSON-RPC `-32003` with nothing read, and a second erase reported `already_absent` true. The other session was unchanged, then erased. Four `bge-m3` embeddings. Surprised: recall returns this session's nearest concepts even for a query that names the other look, and an erased session answers HTTP 200 `application/json` rather than an empty hit list. Erased ids `dc-u-probe-a` and `dc-u-probe-b` must not be reused.
- **T0.2 Keys, accounts and settings.** `docs/keys.md` names every key and where it lives. YouCam console balance is 1040 credits and they do not expire. Fix selection is Clef. Dresscode's Lambo is its own process on `127.0.0.1:7720` with its own sqlite file, embedder Workers AI `bge-m3`. The dogfood server is untouched.
- **Review and remediation from P1.** On P0 the orchestrator checks a result and lands it. From P1 a review agent reads the branch and runs the gate, then a remediation agent fixes what the review found. Those two repeat until a review comes back clean, and only then does the work land.
- **T0.1 Repository on git footing.** Module `github.com/nrynss/dresscode`, Go 1.27.1, Keel v0.9.0, no toolchain line. `version` prints `dresscode 0.0.0`; `serve` exits without listening, dialing, or reading a key. The app is a Vite and Svelte 5 static page on `@nrynss/chaaya@0.5.0` and `bits-ui@^2`, with eslint, svelte-check, and Vitest. `./tools/check.sh` is the gate, and CI runs it on push to `main` and on pull requests. `.env.example` is a comment until T0.2 names the variables. Surprised: the Chaaya root export is only `packageName`; `@sveltejs/kit` is an optional peer and was not added; pnpm's minimum release age rejects the exact `@nrynss/chaaya@0.5.0` pin, so `web/pnpm-workspace.yaml` excludes that version; `go mod tidy` added no indirect modules because `keel/id` uses only the standard library.
- **Fix selection is Clef.** Jev on OpenRouter stays available so the two can be compared on the same pick requests. That comparison is still required. Clef remains the picker until it is done.
- **Repository started.** Product, architecture, agent rules and this diary. Decided before the first task: guests plus emailed-code sign-in, no Google; style memory is text only, with Clef describing each render in a fixed vocabulary; Keel v0.9.0, Chaaya 0.5.0, Lambo v0.8.0. Checked: the YouCam key authenticates on the India server (an upload slot, no units), and a Workers AI token lists Clef, Clef-flash and bge-m3 and ran one bge-m3 call.
