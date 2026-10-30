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
status:     done
```
`Occasion`, `Criterion`, `Fix`, `LookAttribute`, `Look`, `Render`, `Product` and the fidelity label (architecture.md, "The domain model"). Load the occasions, menus and vocabulary from `testdata/occasions/` and validate them: every fix names a YouCam task and the criteria it addresses; every vocabulary question has a closed list with "none" and "can't tell"; no criterion is shared by two occasions under different wording.

Also here: the **attribute allowlist** for the fix picker as a type, so anything else cannot be passed by mistake.

**Done when:** the T0.3 data loads and validates; a broken file fails with the file and the reason; JSON round-trips are tested.

Notes:

- Added `internal/domain`. `Load` reads `testdata/occasions` into occasions, the look vocabulary, and the catalog. `Look` and `Render` are the attempt and the mediastore copy. A render check is nil until Clef has answered. Selfie storage is the mediastore id, not the photo.
- The fidelity values are `exact product`, `approximate match`, and `visual simulation`. The catalog file writes `exact` and `approximate`; those decode as the first two labels, and the domain writes the labels back. Visual simulation is a template with nothing to photograph. The catalog has no such row. `Basket.TemplateOnly` is where the menus name those templates. The loader does not rewrite the files.
- A fix must name a YouCam task from Skin, Beauty, Fashion, Jewelry & Watch, or Hair & Beard, the criteria on that occasion it addresses, and a vocabulary change whose value is in the closed list. It must be opt-in. An occasion has 5 to 8 criteria. Every vocabulary question must include `none` and `can't tell`. The same criterion id on two occasions must keep the same kind, question, and suggestion. The same question under two ids is refused. Interview and funeral share `lip-quiet`. The five occasion ids are the product's closed set.
- A bad file returns `FileError`: the base name and the reason. Unknown JSON fields are refused.
- `Allowlist` is what the fix picker can take. The fields are face shape, hair type, hair length, and colour tones. There is no photo field, and any other JSON field is refused. The menus pass only `faceShape`. Hair type, hair length, and colour tones stay on the type because the product allows them, and they stay empty until a fix asks. `face-attr-analysis` must list age, gender, and ratios in `never_use`.
- Did not require `path_verified`. The flag records whether a page prints the path; a false value should still load.
- Surprised that the biggest-issue id for "can't tell" is `cant-tell`, while the vocabulary option is the words `can't tell`.

---

### T1.2: The store ★
```yaml
requires:   T1.1
fixture-ok: yes
size:       M · mid
owns:       internal/store/
status:     done
```
SQLite through Keel `sqlite`, with namespaced migrations: looks, renders and their checks, the analysis cache keyed by the normalised photo's hash, products and labels, the user's opt-in menu per occasion, the history switch. Keel `identity` brings its own tables; don't duplicate them.

Every row a user owns carries the user id, so deletion (T4.3) can find all of it.

**Done when:** migrations run on a fresh file and again with no change; a test lists every table holding user data, and T4.3 reads that list.

Notes:

- Added `internal/store`. `Migrate` applies six files under the Keel namespace `store`. `UserTables` is the list T4.3 deletes by `user_id`. The same list is how a guest's rows are found when they attach to an account: every owned row carries `user_id`, not only the parent.
- Looks, flags, renders, checks, check attributes, the user's products, and the labelled products on a look are separate tables. A render row stays when the check rejects it, so the mediastore id remains findable. The check row is absent until Clef has answered. Catalog products stay in `testdata`; a look records the id and the label it showed. Fidelity is the domain label (`exact product`, `approximate match`, `visual simulation`), not the catalog file's short spelling.
- The analysis cache key is the normalised photo's hash, scoped to the user, plus the YouCam task. The value columns are the picker allowlist and nothing else, so age, gender, ratios and scores have no place to go. An opt-in row is one ticked fix on one occasion. No history row means the switch is off.
- No foreign key to identity's `users`. The store migrates on a fresh file, and again beside identity, without creating `users`, `sessions`, `identities` or `sign_in_codes`. Child tables cascade, so walking `UserTables` in either order deletes one user and leaves the other.
- `go.mod` gained the indirect modules Keel sqlite imports. No new direct dependency.

---

### T1.3: The interfaces and their replay twins ★
```yaml
requires:   T1.1
fixture-ok: yes
size:       M · frontier
owns:       internal/systemone/systemone.go, internal/systemone/replay.go, internal/youcam/youcam.go, internal/youcam/replay.go, internal/memory/memory.go, internal/memory/fake.go
status:     claimed:grok-4.7
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
status:     in-review
```
One TOML file through Keel `config`, keys resolved from the environment or the keyring, never written into it. Budgets: YouCam units as a `cost` credit budget with a share held for judging; Workers AI dollars per day; the judge lease (fresh selfies and renders per session). Flags: fresh uploads off, renders off, samples only.

**Done when:** the example file loads; a missing key fails naming the variable, never its value.

Notes:

- Added `internal/config` and `dresscode.toml.example`. The file loads through Keel `config`. Each key is a reference. The environment is tried first. YouCam's key, YouCam's secret, and the Workers AI token then fall back to the keyring: the three names `docs/keys.md` places there, looked up by service and kind. The other names are environment only. The example quotes only those names, `env`, and `keyring`.
- YouCam is a `cost` credit budget, denomination `youcam.credit`, period none, because the credits do not expire. The ceiling is 1040. Owner `judging` can spend 400. Owner `development` can spend 640. The pool has no third owner, so nothing else can spend the judging share. A test reserves 640 and 400 on one pool and refuses one more unit to either owner.
- Workers AI is a dollar budget with period daily. No dollar ceiling was recorded. The example sets one dollar a day. The number lives in the file. The in-memory budget has no window; the period is what a durable store opens with.
- The judge lease caps one session at 5 fresh selfies and 10 renders. Five is one fresh selfie for each occasion. Ten is a render and a runner-up for each. A cached sample counts against neither. The caps are counts. They are not a `lease.Quota` of open sessions.
- The flags are `fresh_uploads_off`, `renders_off`, and `samples_only`. True is the restriction. The example leaves all three false, so the open position is the default and a flip is what turns a restriction on.
- A missing variable fails with Keel's resolve error and the variable name. The tests inject stand-ins and check that the error, the plan, and the formatted settings do not contain them. A literal where a reference belongs is refused the same way.
- Keel treats a missing settings file as empty, which would fail a required key without naming its variable. Load refuses a missing path first.
- `go mod tidy` recorded the sums for the TOML parser Keel `config` imports.

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
