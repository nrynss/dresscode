# P3: Pipeline

```yaml
id:       P3
size:     L
requires: [P2]
blocks:   T4.4, P6
parallel: P4, P5
```

**Goal:** The seven steps of a look (product.md), each its own file, run as one Keel job with progress streamed, and an evaluation that sets every threshold from measurement.

All of P3 is `fixture-ok: yes`: it runs on the replay twins and the T0.4–T0.6 recordings. Only T3.9b spends.

**Read first:** product.md, "Who sees what". Each step's test pins its boundary: what the model received and what it must never receive.

---

### T3.1: Judge ★
```yaml
requires:   T2.1
fixture-ok: yes
size:       M · frontier
owns:       internal/pipeline/judge.go
status:     done
```
One Clef request: the normalised photo, the occasion, its criteria as `noul` questions, the biggest-issue `choice`. Turn probabilities into flags with thresholds from settings (placeholders until T3.9 sets them). Output: the flagged criteria with the user-facing wording, and the biggest issue.

**Done when:** on the recordings, each sample's flags come out; a test proves no YouCam analysis is in the request.

Notes:

- Built `internal/pipeline/judge.go`: `Judge(ctx, client, photo, occ, thresholds) (Judgement, error)` — one `systemone.Request` to Clef with the photo as the only image, a two-field state, one `noul` per criterion in the data's wording, and the `biggest_issue` choice. The replay answers all 37 judge recordings through the existing key machinery; no live call.
- The no-YouCam proof is structural. The state is `judgeState{Occasion, Name}` — a two-field struct, so an analysis has no field to travel in — and the questions are copied verbatim from `domain.Criterion.Clef` and `BiggestIssue`, never composed. The package imports the System One seam and the domain, not the YouCam client, so an analysis cannot even be named in it. The test pins it twice: typed assertions on the captured request (one image equal to the photo given, state decodes strictly as the two fields, question set exactly criteria plus the choice with the data's wording and options), and canonical equality of judge's whole request with the recorded probe request on every one of the 37 samples — the probe docs/clef.md records as sending no YouCam analysis.
- Thresholds: `Thresholds{Met, PerCriterion}` is an argument, not a package constant. A criterion flags when its yes-probability is under its line; the line is `PerCriterion[id]` when one is set, else `Met`. Values must sit in [0, 1] (NaN refused) and an override for an id off the occasion is an error, so a stale setting fails loudly instead of quietly stopping to apply. Config owns no threshold yet, so the placeholders are proposed, not taken (report holds the exact diff): a `[judge]` section in `Settings` (`met`, `per_criterion`), whose values T3.9 measures; run.go maps it onto `pipeline.Thresholds`.
- Output: `Judgement{Flags []domain.Criterion, Issue}`. Flags are the flagged criteria carried whole, in the occasion's criteria order, so the app renders the data's own suggestion (`testdata/api/look.json` renders id, kind, suggestion) and pick (T3.2) reads the same criteria. `Issue` is the chosen option's id and the occasion's label for it (`none` and `cant-tell` pass through). No probability in the output: they steer this decision and die here.
- The flags test table pins all 37 samples at a 0.5 reading — the reading docs/clef.md counts agreement at — with expected flags and issues read off its tables. Thirteen cells read on the other side of 0.5 from the README's ground truth (V02 light; V21, P09, P13, P16 beards; V06, P12 expressions; V08 light and beard; P11 garment; V09, V12 collars; V16 light); the table pins the read, and T3.9 owns moving the lines.
- The judge refuses an empty photo before any call: docs/clef.md measured that Clef answers an imageless judge request confidently, and that answer must not be trusted.
- For T3.2 (pick): you inherit `Judgement` (flags carry the criteria whole; `Issue.ID` is a criterion id, `none`, or `cant-tell` — it can name a criterion the flags do not hold, as V21 and P15 do) and `Thresholds` as the shape settings fill. The pick request is text only; the seam's `Request` takes zero images, so the boundary there is the same argument-shaped one.
- Surprised that the replay's resolved recording (with images read inline) is retrievable by exchange id, so the test compares judge's request against the recorded probe request byte for byte without re-resolving file references. Surprised that `filepath.Glob` on `judge-*.json` counts exactly 37 — the stability and cost sets live in their own folders, so the top-level count is the judge set.
- Also surprised, while reading T0.6's notes again: rewording one question moved the other answers on the same photo. That is why the questions are copied from the data and never composed here — a wording change belongs to `testdata/occasions/` and T0.3's owner, with a re-recording, not to a pipeline string.
- Remediation (review F1, F2). F1: the twelve-cells note above was wrong on the count — docs/clef.md's 260 of 273 leaves thirteen cells off the line, and the test's own comment listed all thirteen; the note now says thirteen and names V16's festival-light. F2: the no-photo refusal test now pins the ordering, not just the wording — it runs the judge against a `captureClient` over a canned stub and asserts zero requests went out, so a judge that asked the model on an empty photo fails the test; the typed refusal (the error names the photo) is unchanged. `judge.go` untouched; the config proposal stays deferred to T3.6 per the review ruling.

---

### T3.2: Pick a fix ★
```yaml
requires:   T2.1, T3.1
fixture-ok: yes
size:       M · frontier
owns:       internal/pipeline/pick.go
status:     done
```
One System One request on text only: flagged criteria, occasion, the user's opt-in menu, the allowlisted attributes (only those the cached analysis already holds), and recall if history is on. Output: fix and runner-up, both from the opt-in menu. A dismissed fix in recall is never offered first.

**Done when:** a test proves no image and no attribute outside the allowlist reaches the request; a fix outside the opt-in menu can never be returned.

Notes:

- Built `internal/pipeline/pick.go`: `Pick(ctx, client, occ, j Judgement, optin []string, attrs domain.Allowlist, recall *Recall) (Choice, error)` — one `systemone.Request` to Clef with no image, one `choice` question (`fix`) whose options are the candidates in the data's own `changes` wording plus `none`, and a state of four fields: occasion id and name, the flags (id plus the data's suggestion, in the judgement's order), the held attributes, and the recall when history is on. Output `Choice{Fix, RunnerUp *domain.Fix}`, the menu entries carried whole so measure and render need no second lookup. The runner-up is the best-probability candidate under the model's own answer, menu order breaking ties; the probabilities rank this one decision and go no further.
- The text-only boundary is the argument-shaped one. `Pick` takes no image parameter, so the request's `Images` stays nil and the key `images` never appears on the wire (the test checks the canonical request JSON for the key's absence, not just an empty list). The state is `pickState` — four fields, so a photo has no field to travel in — and the test decodes it with unknown fields refused. This package imports the System One seam and the domain, not the YouCam client.
- The allowlist gate: attributes enter only as `domain.Allowlist`, and `held` projects it by marshalling the type itself, so the state's attribute keys are that type's own four JSON names and a fifth key has no place to come from. Refused versus dropped, as the task asked: refused at the type (`Allowlist.UnmarshalJSON` errors on any other field, the domain's own pin, re-proven in pick_test) and dropped at the value level — an empty field is an analysis not run yet, and it is left out rather than invented. "Only those the cached analysis already holds" is exactly that omission. For T3.3: the values that fill this struct come from your `use_fields` → `pickerAttributeForYouCamField` mapping and the analysis cache; the picker forwards what is set and invents nothing.
- The opt-in menu is the candidate set, not a filter after generation. `candidates` builds it before the request: ticked fixes of this occasion's menu minus the dismissed. The request offers exactly the candidates plus `none`; `pickRead` looks the answer up in the candidate set and refuses anything else, so `Pick` returns a menu entry or an error and never an off-menu fix — and `Choice`'s pointer fields make an off-menu fix unrepresentable in the output type itself. The test sweeps all five occasions: every menu fix comes back whole when ticked, an unticked fix and an unknown id are refused, `none` declines.
- The dismissal decision (T3.8's handoff): a dismissed fix is out entirely — never first, never runner-up. product.md's second-visit moment says "skips the red lip they dismissed", the critical-path story says "offers the jhumkas first, never the red lip", and api.md's "not offered first" is only the minimum: the fallback chain renders the runner-up whenever the first check fails, so a dismissal that only barred first place would put the dismissed fix on the user's face one hop later. The exclusion happens before the request (the model is never offered the fix), and the answer lookup refuses the id anyway (a model echoing the state's dismissed list cannot get it returned). Dismissed ids still travel in the state verbatim so the model knows the preference in its own words. A stale dismissed id that names no fix here is a no-op.
- Kept-first is deliberately not forced in code: the recall line travels verbatim and the model prefers the kept fix from it. A "never" is enforceable structurally; a "first" forced by code would override the judgement the request exists to ask for. T3.8's story test will show whether the line carries it; if measurement (T3.9) says it does not, revisit with data, not with a rank override.
- Two asks are refused before any call, the judge's empty-photo lesson (docs/clef.md: Clef answers a vacuous question confidently): nothing flagged, or nothing a candidate (nothing ticked, or everything ticked dismissed). Both return an empty `Choice` with no error and no request — the look stands on its suggestions, and the test pins zero requests with a capture client over a stub that would have answered.
- Wording: fix options are the menu's `changes` verbatim; flags are the criteria's `suggestions` (the words the app renders as reasons); the `none` option's text is the occasion's own biggest-issue label for `none`. The one composed sentence is `pickQuestion`, named at the top of the file — the occasions' data has no field for a pick question. The judgement's biggest issue is not sent: product.md's who-sees-what row for pick lists flagged criteria, and the flags already carry every criterion the issue could name.
- No pick recordings exist: docs/clef.md's T0.6 notes say so in place ("a fix-pick call (text only, no image) is not in these figures") and `testdata/clef` holds judge, check, stability and cost only. The replay pin is therefore honest machinery, not a recording: the constructed pick request, asked of `testdata/clef`'s replay, returns the typed `Missing` (so it validated and hashed) and two identical picks hash to one key, so a recording made against this construction will serve it. The first live pick recording is a later task's spend decision.
- Surprised twice by Go itself: a function and a type cannot share a name in one package, so the output is `Choice` rather than `Pick`; and judge.go already owns `read` and `request` as package-level names, so this file's are `pickRead` and `pickRequest`. One real bug caught before the first run: ranging over `recall.Dismissed` on a nil `*Recall` panics — history-off is a nil, not an empty struct.
- The replay open costs about ten seconds under `-race` (it hashes every recording's inline image bytes); the landed judge tests already carry that cost and this file adds one more open. Nothing else surprised.
- Remediation (review F1, F2, F3). F1: the `fixByID` helper in pick_test.go was defined and documented but never called; removed. F2: `candidates` declared an error return that no path populated; the return is dropped and `Pick` narrows with the slice directly — the refusals stay where they were (the two vacuous checks in `Pick`, the answer lookup in `pickRead`), and no refusal was invented to justify the signature. F3 (orchestrator granted `internal/domain/load.go` and its test): the loader reserved `none`/`cant-tell` as criterion ids but not as fix ids, so a fixture fix id `none` would sit in the pick options map under the same key as the decline option and be unreachable; `validateFixes` now refuses a fix id of `none` or `cant-tell` in the criterion reservation's own wording (`fix id %q is reserved`), and `TestBrokenFile` pins both ids refusing to load with a test-constructed occasion. Every shipped fix id is prefixed, so the real fixtures are untouched and still load.

---

### T3.3: Measure, only for that fix
```yaml
requires:   T2.2, T3.2
fixture-ok: yes
size:       S · mid
owns:       internal/pipeline/measure.go
status:     in-review
```
Run only the analysis the chosen fix names (colour tones for a shade; face shape, hair type and length for a hairstyle; nothing for clothes, jewellery, beard, lighting or smile). Cache by the normalised photo's hash; a cached result costs no unit. Ask Face Attributes for face shape only.

**Done when:** a second look on the same photo spends no analysis units; fixes that need none call nothing.

Notes:

- Built `internal/pipeline/measure.go`: `Measure(ctx, runner *youcam.Runner, cache Cache, photo Photo, fix *domain.Fix) (domain.Allowlist, error)`. It reads the fix the pick carried whole, runs exactly the analyses in that fix's own `Analysis` list through the T2.2 runner, and returns the values as a `domain.Allowlist` — the picker's own argument type and the cache row's shape, so the next look's pick request and T3.4's render read the same value with no second lookup. The mapping is data, three tables at the top of the file: `allowlistFields` (keyed by the picker allowlist's four attributes — colour tones, face shape, hair type, length — the get/set pair that is also the cache column of the same name), `analysisFields` (the fix's `use_fields` name → attribute; `faceShape` only, this file's side of the bridge domain.Allowlist draws unexported), and `analysisRuns` (the analysis task name → its run; `face-attr-analysis` only). T3.4 and T3.6 inherit the tables unchanged.
- The cache is the `analysis_cache` table's own contract, as an interface this file owns and a later store task implements: `Held`/`Keep` under the `(user_id, photo_hash, task)` primary key, the four allowlist columns and nothing else. The row existing is the cache; an empty field inside it is a read that yielded nothing, and it is kept that way so a repeat never pays ten units to yield nothing again. The providertask idempotency key is `analysis/<user>/<hash>/<task>`, so two fixes naming the same analysis on one photo share one task and a restart resumes it.
- Face Attributes is asked for the fix's `use_fields` only: the shipped hair fixes ask `features: ["faceShape"]`, the recorded body byte for byte. Never agegender, never facialratio, never face_quality — the client never decodes them, and `planFields` refuses a `use_field` the mapping does not carry and lets `never_use` veto one it does, so a quietly unmeasured field is unrepresentable.
- A fix with no analysis — all twenty clothes, jewellery, lip, beard and smile fixes on the five menus — returns an empty allowlist before any scope is even checked: no call, no unit, no cache row. No lighting fix exists in any menu (light is judged, never rendered), so there is nothing a lighting fix could call.
- The one decision to flag: "colour tones for a shade" (product.md) has no run on this seam. `skin-tone-analysis` is off the menu — docs/occasions.md: "No fix runs a shade match" — off the T2.2 rate card, undecoded by the client, and without a recording, and all four are outside this task's owns. So the shipped shade fixes name no analysis and call nothing, and a fix constructed to name colour tones is refused before any call, naming the task. Refusing, never skipping silently: a skipped analysis would render a fix without the measurement it names, and the refusal is the exact hook where a future task adds the run (rate-card line, decode, recording, one table entry). The run-selection test pins all of this.
- The proof replays the recorded wire: `http-face-attr-create` and `http-face-attr-poll` served through the replay's `Serve` to the live v2 client, then the T2.2 runner over it — upload, create, poll, ten units settled, `Oval` out. The slot reply is built around the recorded upload's `file_id` (docs/youcam.md keeps one slot recording per distinct request, and this flow's slot body was never recorded; a slot spends nothing), and the PUT lands on the test's own server, so no bytes leave. The photo is the recording's own file, `testdata/samples/woman/V06-family-function.jpg`, read inline by `Open`. Zero YouCam units spent by this task.
- Tests: `TestAHairstyleAsksFaceAttributesOnce` (run selection + the done-when: second look on the same photo makes zero calls, the books untouched, the same `Oval` back), `TestTheCacheIsScopedToTheUser` (a second account's first look on the same hash runs its own analysis, because the cache key is `(user, photo_hash, task)`), `TestAFixThatNeedsNoAnalysisCallsNothing` (the sweep over every menu fix, plus the pin that the fixes naming an analysis are exactly the hair-transfer ones), `TestMeasureRefusesAnAnalysisWithNoRun` (colour tones, three-view hair type, an age ask, a `never_use` veto, an empty `use_fields`), and `TestMeasureRefusesAnUnusableCall` (nil runner/cache/fix, no user, no hash, an empty photo, no content type — the judge's empty-photo lesson, before any call).
- Surprised that the typed `face-attr-poll` recording carries no faceshape — only the `http-face-attr-poll` one does — so a runner over the typed replay caches an empty face shape; the pipeline test therefore faces the wire the way the youcam package's own wire tests do. Surprised that product.md's "colour tones for a shade" is narrower in the landed data: the shade fixes name no analysis at all (see the flagged decision above). Noted for the orchestrator: judge.go's header still says this package does not import the YouCam client; measure.go now does, and the sentence wants a one-word narrowing to "the judge" — not taken here, that file is not mine.

---

### T3.4: Render ★
```yaml
requires:   T2.2, T3.3
fixture-ok: yes
size:       M · mid
owns:       internal/pipeline/render.go
status:     not-started
```
The YouCam try-on for the fix, with the product the user brought or one from the basket, through the YouCam client (idempotent, copied into mediastore). Quote before running; a refused budget ends the step with the suggestion intact.

**Done when:** on the recordings, each fix renders; a budget refusal spends nothing and the look still returns its suggestion.

---

### T3.5: Check the render ★
```yaml
requires:   T2.1, T3.4
fixture-ok: yes
size:       M · frontier
owns:       internal/pipeline/check.go
status:     not-started
```
One Clef request with the original and the render: change made, same person, criterion met (`noul`), and one `choice` per look attribute. Join the winning attribute options into the look-in-words line ("gold jhumkas, red lip, open collar"), leaving out "none" and "can't tell". Failing change-made or same-person discards the render, whatever else it scored.

**Done when:** on the known bad renders (T0.4), each is discarded; the line is identical for identical answers.

---

### T3.6: The fallback chain ★
```yaml
requires:   T3.5
fixture-ok: yes
size:       S · mid
owns:       internal/pipeline/run.go
status:     not-started
```
The look as one Keel `job`: judge → pick → measure → render → check, then the runner-up once if the check fails, then the suggestion alone. Each stage publishes its event on `stream` in the T1.5 shapes. Resumes after a restart without repeating a paid step.

**Done when:** a look killed mid-render resumes and finishes without a second YouCam task; every stage event matches the API contract.

---

### T3.7: Shop the look
```yaml
requires:   T3.6
fixture-ok: yes
size:       S · mid
owns:       internal/pipeline/shop.go
status:     not-started
```
Products for the chosen fix from the user's own items and the occasion's basket, each labelled **exact product**, **approximate match** or **visual simulation**, with the rule for each written in the code's doc comment. Products the user kept before rank first when history is on.

**Done when:** every product carries a label; the label rules are tested.

---

### T3.8: Remember and recall ★
```yaml
requires:   T2.3, T3.6
fixture-ok: yes
size:       M · mid
owns:       internal/pipeline/memory.go
status:     not-started
```
With history on: after the user keeps, dismisses or buys, remember the look as text in the account's Lambo session. Before picking a fix: recall for the occasion and pass it to T3.2. With history off: nothing is read or written, and a test proves it.

**Done when:** the second-visit story (dev-diary README, Critical Path) runs end to end on recordings: the kept fix is offered first and the dismissed one is not offered.

---

### T3.9: The evaluation ★
```yaml
requires:   T3.1, T3.5
fixture-ok: yes
size:       L · frontier
owns:       internal/eval/, testdata/eval/, docs/evaluation.md
status:     not-started
```
On the sample set's ground truth (T0.4):

- **Judging:** per criterion, agreement between Clef's flags and the expected flags; choose each threshold from the curve, not by guess.
- **Render check:** every known bad render rejected; count good renders wrongly discarded.
- **Look vocabulary:** Clef's answer against the true attribute on every render.
- **Stability:** from T0.6's repeats, which criteria flip; widen or reword those.

Write the thresholds into the settings and the numbers into `docs/evaluation.md`. These are the numbers the README and video quote.

**Done when:** every threshold is set from a measurement shown in the report; the evaluation replays offline and gives the same numbers.

### T3.9b: Evaluation live
```yaml
requires:   T3.9
fixture-ok: no
size:       S · light
owns:       internal/eval/live_test.go
status:     not-started
```
Re-run the judging and check evaluation live once, after the criteria wording settles. Workers AI only; no YouCam units. **Done when:** the live numbers are in the report beside the recorded ones.
