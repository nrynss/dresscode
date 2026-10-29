# P1: Contracts

```yaml
id:       P1
size:     M
requires: [T0.1, T0.3]
blocks:   P2, P3, P4, P5
parallel: beside the P0 probes
```

**Goal:** Freeze the types, the store, the interfaces, the settings and the API the app talks to, so four tracks can build at once without touching each other's files.

Contracts freeze when T1.5 lands. After that, a change to anything here is the orchestrator's call and updates its fixtures in the same commit.

---

### T1.1: The domain model ★
```yaml
requires:   T0.3
fixture-ok: yes
size:       M · frontier
owns:       internal/domain/
status:     not-started
```
`Occasion`, `Criterion`, `Fix`, `LookAttribute`, `Look`, `Render`, `Product` and the fidelity label (architecture.md, "The domain model"). Load the occasions, menus and vocabulary from `testdata/occasions/` and validate them: every fix names a YouCam task and the criteria it addresses; every vocabulary question has a closed list with "none" and "can't tell"; no criterion is shared by two occasions under different wording.

Also here: the **attribute allowlist** for the fix picker as a type, so anything else cannot be passed by mistake.

**Done when:** the T0.3 data loads and validates; a broken file fails with the file and the reason; JSON round-trips are tested.

---

### T1.2: The store ★
```yaml
requires:   T1.1
fixture-ok: yes
size:       M · mid
owns:       internal/store/
status:     not-started
```
SQLite through Keel `sqlite`, with namespaced migrations: looks, renders and their checks, the analysis cache keyed by the normalised photo's hash, products and labels, the user's opt-in menu per occasion, the history switch. Keel `identity` brings its own tables; don't duplicate them.

Every row a user owns carries the user id, so deletion (T4.3) can find all of it.

**Done when:** migrations run on a fresh file and again with no change; a test lists every table holding user data, and T4.3 reads that list.

---

### T1.3: The interfaces and their replay twins ★
```yaml
requires:   T1.1
fixture-ok: yes
size:       M · frontier
owns:       internal/systemone/systemone.go, internal/systemone/replay.go, internal/youcam/youcam.go, internal/youcam/replay.go, internal/memory/memory.go, internal/memory/fake.go
status:     not-started
```
- `SystemOne`: images (optional, embedded bytes), a state object, questions (`choice`, `noul`, `score`) in; per-option probabilities and confidence out. Model by name.
- `YouCam`: upload, create task, poll, fetch result; typed errors for no face, bad file, expired, out of units.
- `Memory`: remember a look, recall for an occasion, erase a session; one session per account.
- Each has a replay or fake implementation that serves `testdata/` (T0.5–T0.7) by a stable request key, so P3 and P5 run with no network.

**Done when:** the replay twins serve every recorded P0 reply; a missing recording fails loudly with its key, never a silent default.

---

### T1.4: Settings, budgets and flags
```yaml
requires:   T1.1
fixture-ok: yes
size:       S · mid
owns:       internal/config/, dresscode.toml.example
status:     not-started
```
One TOML file through Keel `config`, keys resolved from the environment or the keyring, never written into it. Budgets: YouCam units as a `cost` credit budget with a share held for judging; Workers AI dollars per day; the judge lease (fresh selfies and renders per session). Flags: fresh uploads off, renders off, samples only.

**Done when:** the example file loads; a missing key fails naming the variable, never its value.

---

### T1.5: The API contract ★
```yaml
requires:   T1.1, T1.4
fixture-ok: yes
size:       M · frontier
owns:       docs/api.md, testdata/api/
status:     not-started
```
Every route the app calls, written down before either side is built: sign-in (start, verify), session, occasions and menus, opt-in menu, upload (resumable), product intake (photo, link), start a look, the look's event stream (stages and their payloads), the verdict, renders, shop the look, keep/dismiss/bought, history on/off and recall, deletion (photo, look, history, account), samples. Errors in Keel's `wire` envelope with stable codes ("budget refused", "no face in photo", "uploads paused").

Record one example response per route in `testdata/api/` for the app to build against.

**Done when:** every product.md flow maps to routes; every route has an example; the orchestrator freezes the contracts.
