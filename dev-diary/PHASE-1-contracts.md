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
status:     claimed:grok-4.7
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
status:     in-review
```
- `SystemOne`: images (optional, embedded bytes), a state object, questions (`choice`, `noul`, `score`) in; per-option probabilities and confidence out. Model by name.
- `YouCam`: upload, create task, poll, fetch result; typed errors for no face, bad file, expired, out of units.
- `Memory`: remember a look, recall for an occasion, erase a session; one session per account.
- Each has a replay or fake implementation that serves `testdata/` (T0.5–T0.7) by a stable request key, so P3 and P5 run with no network.

**Done when:** the replay twins serve every recorded P0 reply; a missing recording fails loudly with its key, never a silent default.

Notes:

- `SystemOne.Ask` takes a model name, optional embedded images, a JSON state, and `choice`, `noul`, and `score` questions. Choice and score come back with a probability per option and a confidence. Noul comes back as the probability the answer is yes. A missing confidence stays nil. `clef` and `clef-flash` are named; the caller names Jev.
- `YouCam` is upload, create, poll, and fetch. `ErrNoFace`, `ErrBadFile`, `ErrExpired`, and `ErrOutOfUnits` match on kind, so a provider detail still matches the sentinel. Poll returns a refusal as that error, not as a success status.
- `Memory` remembers concepts, recalls by the occasion's query text, and erases a session. `Session` builds the one `dc-u-` id for an account. `ErrErased` is JSON-RPC `-32003`: nothing was read, and it is not an empty hit list. The recorded derive stays `pending`; the stats exchange is its own recording.
- Recordings are the lambo envelope: `exchange`, `request`, `response`. `Reply` uses the exchange id. `Serve` hashes the HTTP method, path, and canonical body. Headers are not in that hash. A missing key is `Missing` and the error quotes the key. Two recordings of one request are `Ambiguous`; the replay does not pick.
- The twenty Lambo exchanges replay. Eighteen have a unique HTTP body. `15-erase-a` and `18-erase-a-again` are the same request, so `Serve` and `Erase` name both instead of choosing. Recall of "silver studs" in session A returns A's look. The same query in two recordings is ambiguous, not a chosen hit list.
- `testdata/clef` and `testdata/youcam` are not there. Those replays open empty and fail the call with the key. A recording in the same envelope is served, including a Clef body carried as an HTTP body. No fixtures were added.
- Tests live next to the six files. The gate runs `go test`, and the done-when needs a recorded hit and a missing key.
- Did not import Keel `cache` for the digest. That import would add `golang.org/x/sync` to `go.mod`, which this task does not own. The key is still a SHA-256 of canonical JSON.
- Surprised that the two erase calls are byte-for-byte the same request with different replies, and that a poor recall query still returns only that session's concepts.

---

### T1.4: Settings, budgets and flags
```yaml
requires:   T1.1
fixture-ok: yes
size:       S · mid
owns:       internal/config/, dresscode.toml.example
status:     claimed:grok-4.7
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
