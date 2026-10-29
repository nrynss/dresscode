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
status:     not-started
```
One Clef request: the normalised photo, the occasion, its criteria as `noul` questions, the biggest-issue `choice`. Turn probabilities into flags with thresholds from settings (placeholders until T3.9 sets them). Output: the flagged criteria with the user-facing wording, and the biggest issue.

**Done when:** on the recordings, each sample's flags come out; a test proves no YouCam analysis is in the request.

---

### T3.2: Pick a fix ★
```yaml
requires:   T2.1, T3.1
fixture-ok: yes
size:       M · frontier
owns:       internal/pipeline/pick.go
status:     not-started
```
One System One request on text only: flagged criteria, occasion, the user's opt-in menu, the allowlisted attributes (only those the cached analysis already holds), and recall if history is on. Output: fix and runner-up, both from the opt-in menu. A dismissed fix in recall is never offered first.

**Done when:** a test proves no image and no attribute outside the allowlist reaches the request; a fix outside the opt-in menu can never be returned.

---

### T3.3: Measure, only for that fix
```yaml
requires:   T2.2, T3.2
fixture-ok: yes
size:       S · mid
owns:       internal/pipeline/measure.go
status:     not-started
```
Run only the analysis the chosen fix names (colour tones for a shade; face shape, hair type and length for a hairstyle; nothing for clothes, jewellery, beard, lighting or smile). Cache by the normalised photo's hash; a cached result costs no unit. Ask Face Attributes for face shape only.

**Done when:** a second look on the same photo spends no analysis units; fixes that need none call nothing.

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
