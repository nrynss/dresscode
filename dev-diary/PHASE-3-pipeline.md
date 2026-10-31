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
status:     done
```
Run only the analysis the chosen fix names (colour tones for a shade; face shape, hair type and length for a hairstyle; nothing for clothes, jewellery, beard, lighting or smile). Cache by the normalised photo's hash; a cached result costs no unit. Ask Face Attributes for face shape only.

**Done when:** a second look on the same photo spends no analysis units; fixes that need none call nothing.

Notes:

- Built `internal/pipeline/measure.go`: `Measure(ctx, runner *youcam.Runner, cache Cache, photo Photo, fix *domain.Fix) (domain.Allowlist, error)`. It reads the fix the pick carried whole, runs exactly the analyses in that fix's own `Analysis` list through the T2.2 runner, and returns the values as a `domain.Allowlist` — the picker's own argument type and the cache row's shape, so the next look's pick request and T3.4's render read the same value with no second lookup. The mapping is data, three tables at the top of the file: `allowlistFields` (keyed by the picker allowlist's four attributes — colour tones, face shape, hair type, length — the get/set pair that is also the cache column of the same name), `analysisFields` (the fix's `use_fields` name → attribute; `faceShape` only, this file's side of the bridge domain.Allowlist draws unexported), and `analysisRuns` (the analysis task name → its run; `face-attr-analysis` only). T3.4 and T3.6 inherit the tables unchanged.
- The cache is the `analysis_cache` table's own contract, as an interface this file owns and a later store task implements: `Held`/`Keep` under the `(user_id, photo_hash, task)` primary key, the four allowlist columns and nothing else. The row existing is the cache; an empty field inside it is a read that yielded nothing, and it is kept that way so a repeat never pays ten units to yield nothing again. The providertask idempotency key is `analysis/<user>/<hash>/<task>`, so two fixes naming the same analysis on one photo share one task and a restart resumes it.
- Face Attributes is asked for the fix's `use_fields` only: the shipped hair fixes ask `features: ["faceShape"]`, the recorded body byte for byte. Never agegender, never facialratio, never face_quality — the client never decodes them, and `planFields` refuses a `use_field` the mapping does not carry and lets `never_use` veto one it does, so a quietly unmeasured field is unrepresentable.
- A fix with no analysis — all twenty-two clothes, jewellery, lip, beard and smile fixes on the five menus — returns an empty allowlist before any scope is even checked: no call, no unit, no cache row. No lighting fix exists in any menu (light is judged, never rendered), so there is nothing a lighting fix could call.
- The one decision to flag: "colour tones for a shade" (product.md) has no run on this seam. `skin-tone-analysis` is off the menu — docs/occasions.md: "No fix runs a shade match" — off the T2.2 rate card, undecoded by the client, and without a recording, and all four are outside this task's owns. So the shipped shade fixes name no analysis and call nothing, and a fix constructed to name colour tones is refused before any call, naming the task. Refusing, never skipping silently: a skipped analysis would render a fix without the measurement it names, and the refusal is the exact hook where a future task adds the run (rate-card line, decode, recording, one table entry). The run-selection test pins all of this.
- The proof replays the recorded wire: `http-face-attr-create` and `http-face-attr-poll` served through the replay's `Serve` to the live v2 client, then the T2.2 runner over it — upload, create, poll, ten units settled, `Oval` out. The slot reply is built around the recorded upload's `file_id` (docs/youcam.md keeps one slot recording per distinct request, and this flow's slot body was never recorded; a slot spends nothing), and the PUT lands on the test's own server, so no bytes leave. The photo is the recording's own file, `testdata/samples/woman/V06-family-function.jpg`, read inline by `Open`. Zero YouCam units spent by this task.
- Tests: `TestAHairstyleAsksFaceAttributesOnce` (run selection + the done-when: second look on the same photo makes zero calls, the books untouched, the same `Oval` back), `TestTheCacheIsScopedToTheUser` (a second account's first look on the same hash runs its own analysis, because the cache key is `(user, photo_hash, task)`), `TestAFixThatNeedsNoAnalysisCallsNothing` (the sweep over every menu fix, plus the pin that the fixes naming an analysis are exactly the hair-transfer ones), `TestMeasureRefusesAnAnalysisWithNoRun` (colour tones, three-view hair type, an age ask, a `never_use` veto, an empty `use_fields`), and `TestMeasureRefusesAnUnusableCall` (nil runner/cache/fix, no user, no hash, an empty photo, no content type — the judge's empty-photo lesson, before any call).
- Surprised that the typed `face-attr-poll` recording carries no faceshape — only the `http-face-attr-poll` one does — so a runner over the typed replay caches an empty face shape; the pipeline test therefore faces the wire the way the youcam package's own wire tests do. Surprised that product.md's "colour tones for a shade" is narrower in the landed data: the shade fixes name no analysis at all (see the flagged decision above). Noted for the orchestrator: judge.go's header still says this package does not import the YouCam client; measure.go now does, and the sentence wants a one-word narrowing to "the judge" — not taken here, that file is not mine.
- Remediation (review R1). Narrowed judge.go's package-doc sentence to "the judge does not import the YouCam client", exactly as the review endorsed: the claim is about the judge's request, and measure.go (a sibling in the package) imports internal/youcam for the T2.2 runner, so the package-level wording had gone stale. Also corrected this section's Done-when evidence from "all twenty" to "all twenty-two" — the five menus hold twenty-two such fixes (cloth 5, beard 5, makeup-vto 5, ai-smile 4, earring 2, necklace 1, of 28 total).

---

### T3.4: Render ★
```yaml
requires:   T2.2, T3.3
fixture-ok: yes
size:       M · mid
owns:       internal/pipeline/render.go
status:     in-review
```
The YouCam try-on for the fix, with the product the user brought or one from the basket, through the YouCam client (idempotent, copied into mediastore). Quote before running; a refused budget ends the step with the suggestion intact.

**Done when:** on the recordings, each fix renders; a budget refusal spends nothing and the look still returns its suggestion.

Notes:

- Built `internal/pipeline/render.go`: `Render(ctx, runner, photo Photo, fix *domain.Fix, brought, basket *Product) (domain.Render, error)` — the fix's own YouCam task run through the T2.2 runner, which quotes, reserves and settles the rate-card line, resumes a recorded task under the job's key, and copies the result into mediastore while the link is fresh. Returns `domain.Render{MediaID, FixID}` with `Check` nil: Clef's read of the render is T3.5's, and no provider link is stored anywhere.
- The fix→flow mapping is one table, `renderFlows`, keyed by the fix's own `youcam.task` in Measure's data-shaped style; T3.5 and T3.6 inherit it unchanged. The bodies are the recorded shapes, built only from the fix's data: **2d-vto/earring** and **2d-vto/necklace** share the VTO body (the ids twice — `ref_file_ids`/`object_infos` plus `source_info`; **no placement parameters** — the pipeline measures no anchor points, the hand-placed chain recording named coordinates picked for one photo, and the engine-placed necklace is exactly the known bad render T0.4 gives T3.5 to discard); **cloth-v4** (`garment_category: "upper_body"`, the recorded value, every cloth fix is a top); **makeup-vto** (`skin_smooth` at a *present* zero, lip shape `original`, no morphology, one matte palette at intensity 70); **hair-transfer** (the fix's `template` plus `hair_color: "src"` — the default is the template's colour, and hair colour is never rendered); **beard-style** (the fix's `template`); **ai-smile** (`closed_mouth_smile`).
- Two small value tables sit beside it: `lipPalettes` (the lip_colour vocabulary value → the palette a recorded makeup render painted; only `red` → `#B0172F` exists) and `smileStyles` (`closed-mouth-smile` → `closed_mouth_smile`). A lip or smile fix whose value no recorded render used is refused before any call, naming the fix and the value — that refusal is the hook where a recorded nude render (none exists; the date, interview and funeral nude lip fixes are the refused set, and they cannot render until one is recorded) adds its colour in one line. An invented hex was never on the table.
- The product rule as implemented: the render takes the user's brought product and the basket's candidate as two arguments and chooses inside `plan` — brought first, else basket, else refusal for a reference flow. Only the compositing flows (earring, necklace, cloth) take a product at all: the lip fix's catalog entry says "Shop photo only — makeup-vto paints a colour; it does not composite this picture", and hair, beard and smile are template-only, so a product passed for those is never uploaded and never enters their key. The choice lives here, not in T3.6, so the test can pin the priority.
- Idempotency key: `render/<user>/<hash>/<fix>`, plus `/<productID>` as a fifth segment exactly when the flow composites a product. Reasoning: Measure's `analysis/<user>/<hash>/<task>` names everything that decides the provider task; for a reference render the product's bytes decide it too, and without the segment a later render with the user's own jhumkas would resume the basket jhumkas' recorded task and show the wrong picture. The task's suggested shape stands unmodified for lip, hair, beard and smile. The mediastore group is `render/<user>/<hash>` (one photo's renders evict with the photo; a look is one selfie), owner the user — T3.6 and the erase wiring inherit both names.
- Quote before running: `UnitsFor` is read before the job runs, and the runner reserves the line before anything uploads. A refused budget comes back as a typed `*Refused{FixID, Task, Units, Err}` whose unwrap is `cost.ErrOverBudget` — zero calls, empty books, no providertask claim left behind, no blob, the returned `domain.Render` empty, and the criterion's suggestion (the suggestion-only look's whole content) untouched. The fallback chain reads the type or the sentinel; both name the same branch.
- The proof replays the typed recordings through the T2.2 runner (not the wire): the replay looks a create up by the canonical JSON of its payload, so each recorded flow succeeding **is** the byte-equality proof — the body the fix built is the recorded body, and a wrong field, a missing `skin_smooth` zero or a guessed necklace coordinate would fail as a Missing recording. Seven flows proven end to end: earring (family-gold-jhumkas on V04 with gold-jhumkas), necklace (festival-gold-chain on V14 with gold-chain, engine placement), cloth (family-festive-top on V08 with top-festive), red lip (family-red-lip on V04), tied hair (family-tie-hair on V06, Casual Updo), short beard (family-short-beard on V08, Circle), closed smile (family-closed-smile on P12). Each: selfie uploaded first, product second and only when composited, one create, two polls (verdict, then the free fresh link), one fetch, the recorded picture kept under a sniffed content type, and the rate-card line settled once against the render key.
- The other tests: a sweep that plans all 28 fixes on the five menus without a call — 25 plan (5 cloth, 6 hair, 5 beard, 2 red lip, 2 earring, 1 necklace, 4 smile) with field-by-field body pins per task, 3 nude lips refused; the brought-vs-basket priority with two distinct keys and one settle each; a never-pays-twice repeat (no second upload, no second create, one charge, the same media id — the collected outcome polls once more and fetches again, free); and the unusable-call table (no runner/fix/user/hash/photo/content type, an unknown task, a reference render with no product, a product with no id or picture, a template fix with no template, an unrecorded lip colour or expression).
- Surprised four times. The recorded results are T0.5's re-encodes — JPEG at quality 92 for every flow, PNG or not — so the sniff reads image/jpeg on all seven; the live PNG-under-a-.jpg-name case is a live-wire matter the same sniff already handles. A second `Run` of a finished key re-collects: one poll and one fetch again, and that is the runner landing a fresh link, not a leak. The slot leg is unrecorded per distinct request (docs/youcam.md), so the engine-placed necklace's create names file ids its own uploads produced; the test hands those ids out as canned slots read from the create recording itself. And a canned slot queue is order-sensitive: the first slot lands on the first upload, so a queue of one ref id gets consumed by the selfie — the first bug the tests caught before anything ran.
- For T3.5: `Render` returns `Check` nil always, and a render that fails a check is T3.5's discard — the mappings here deliberately produce the renders T0.4 calls known bad (the engine-placed necklace, an ai-smile that smooths) rather than pre-empting them. For T3.6: `Render`'s refusals are `*Refused` (budget) and the seam's `*youcam.Error` (no face, bad file, expired) plus wrapped transport errors — the chain distinguishes only the first from the rest.
- Noted for the orchestrator, not taken: measure.go's header and Measure's doc comment both say render reads the values Measure returns; this render reads nothing from Measure (YouCam measures the face itself during a render) — the pick step is the allowlist's reader. A two-line comment narrowing in measure.go is proposed in the report.

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
