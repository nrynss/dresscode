# The evaluation

Every threshold in the settings file, chosen from measurement on the sample set's ground truth (T0.4), read back through the recorded Clef calls (T0.6) on the shipped pipeline code. These are the numbers the README and the video quote. The evaluation replays offline: `go test ./internal/eval/` re-runs all of it from committed recordings — no key, no network, no spend — and a second run returns the identical report (the test proves it).

The machinery is `internal/eval/`; the ground truth is `testdata/eval/ground-truth.json`, transcribed from [`testdata/samples/README.md`](../testdata/samples/README.md). Probabilities appear here because this document is the threshold record. None of them reaches a user: probabilities steer the pipeline and die there.

## Headline numbers

- **Judging: 269 of 273 criterion answers correct (98.5%)** against the sample set's ground truth, at the shipped lines. A flat 0.5 line reads 260 (95.2%). Every line is on its own curve's measured best plateau.
- **Render check: every known bad render discarded, no honest render lost.** On the recorded proxies — the different-person negative and the honest second-visit pair, on both models — the shipped lines decide all four reads correctly, with at least 0.42 of margin on every gate.
- **Look vocabulary: 19 of 22 attribute answers true** on the honest render and the discarded proxy with the chosen model (clef). The three misses are words the vocabulary cannot say yet, not misreadings.
- **Stability: eleven recorded answers of one judge request are identical to the last digit.** Nothing flips across calls; what varies is between photos, not between calls.

## Method

The sample set is 37 variants of four generated people across the five occasions — 273 criterion answers (each variant's occasion judged on every criterion; `lip-quiet` rides two occasions). For each criterion the evaluation:

1. replays its recorded judge calls through the real `pipeline.Judge` and keeps the raw yes-probabilities (the replay hit is also the byte-equality proof: the shipped request equals the recorded probe request, which carried no YouCam analysis);
2. sweeps the met line over the intervals between observed probabilities, scoring agreement with the ground truth;
3. takes the **widest plateau with the most agreement** — every line inside it reads the same cells, and the plateau's midpoint is the reading farthest from any observed probability, so a stray digit moves nothing;
4. chooses the default `met` as the single line reading the most cells across all criteria (ties: fewest overrides, then closest to 0.5), and gives a criterion a `per_criterion` override only where the default reads it below its own curve's best. Every criterion then performs at its maximum with the fewest overrides.

The same sweep runs per gate on the render check, over the honest pair (V04 against V05 — the second-visit story's kept look — as the positive control) and the known bad render (V04 against a different person entirely, the proxy verdict T0.4 recorded on both models). The chosen lines are the chosen model's plateau midpoints.

## Judging

**At 0.5** — the reading docs/clef.md counts, an anchor and not a setting — 260 of 273 cells agree with the ground truth, and the thirteen off-side cells are exactly the ones docs/clef.md names: V02's light; V21's, P09's, P13's and P16's beards; V06's and P12's expressions; V08's light and beard; P11's garment; V09's and V12's collars; V16's light. The curves show them: every one of the thirteen sits inside a plateau where the right line reads it correctly, and the sweep moves nine of them.

**The chosen settings** (`[judge]` in `dresscode.toml`):

```toml
[judge]
# The default met line: the single line that reads the most cells
# correctly across all 36 criteria (260 of 273) with the fewest
# per-criterion overrides. Measured by T3.9 (docs/evaluation.md).
met = 0.628

[judge.per_criterion]
# Lines from each criterion's own curve, where the criterion's best
# point sits off the default (docs/evaluation.md).
family-expression-present = 0.5466
family-garment-festive = 0.2394
interview-collar-buttoned = 0.11395
interview-expression-calm = 0.36925
```

**What they read: 269 of 273.** The four cells no line can save, because a misread probability sits inside the other group's own range:

| Cell | Reads | Should be | What happened |
|---|---|---|---|
| `date-beard-even` on P09 | 0.4508 | pass | the East Asian man's even beard under-reads, under the line that flags V21's patchy one |
| `family-beard-even` on P11 | 0.5106 | pass | the same face's neat short beard, under the line that flags V08's |
| `family-expression-present` on P12 | 0.6120 | flag | the flat stare reads calmer than three genuinely calm faces |
| `date-light-see` on V02 | 0.7913 | flag | flat dull frontal light reads brighter than real light misses |

Three of the four are the men's beard and light readings; the sample README already flags V08 and P10's shadows as the weakest ground truth, and V02's wording gap was open in docs/clef.md. These four are a wording problem, not a threshold problem — see Recommendations.

**Per criterion** — n = cells, best = the most agreement any line reads, plateau = the widest best interval, line = the shipped line (the default `met` = 0.628 unless an override carries it), margin = the shipped line's distance to the nearest observed probability, 0.5 = the cells off the expected side at 0.5 (the anchors):

| Criterion | n | best | apart | plateau | line | margin | 0.5 |
|---|---|---|---|---|---|---|---|
| date-collar-open | 8 | 8/8 | yes | (0.0509, 0.8095) | 0.628 | 0.1815 | — |
| date-garment-easy | 8 | 8/8 | yes | (0.0376, 0.8085) | 0.628 | 0.1805 | — |
| date-hair-clear | 8 | 8/8 | yes | (0.4052, 0.7847) | 0.628 | 0.1567 | — |
| date-beard-even | 8 | 7/8 | no | (0.5985, 0.7217) | 0.628 | 0.0295 | V21, P09 |
| date-lip-clear | 8 | 8/8 | yes | (0.0925, 0.6691) | 0.628 | 0.0411 | — |
| date-expression-engaged | 8 | 8/8 | yes | (0.3786, 0.7236) | 0.628 | 0.0956 | — |
| date-light-see | 8 | 7/8 | no | (0.3259, 0.6337) | 0.628 | 0.0057 | V02 |
| family-earring-festive | 9 | 9/9 | yes | (0.0351, 0.9422) | 0.628 | 0.3142 | — |
| family-lip-colour | 9 | 9/9 | yes | (0.2094, 0.9390) | 0.628 | 0.3110 | — |
| family-garment-festive | 9 | 9/9 | yes | (0.0363, 0.4425) | **0.2394** | 0.2031 | P11 |
| family-hair-finished | 9 | 9/9 | yes | (0.0312, 0.7673) | 0.628 | 0.1393 | — |
| family-beard-even | 9 | 8/9 | no | (0.6090, 0.8676) | 0.628 | 0.0190 | V08 |
| family-expression-present | 9 | 8/9 | no | (0.5431, 0.5501) | **0.5466** | 0.0035 | V06, P12 |
| family-light-even | 9 | 9/9 | yes | (0.6223, 0.8574) | 0.628 | 0.0057 | V08 |
| interview-collar-buttoned | 6 | 6/6 | yes | (0.0426, 0.1853) | **0.11395** | 0.0713 | V09, V12 |
| interview-garment-plain | 6 | 6/6 | yes | (0.0307, 0.9297) | 0.628 | 0.3017 | — |
| interview-hair-close | 6 | 6/6 | yes | (0.0412, 0.8563) | 0.628 | 0.2283 | — |
| interview-beard-short | 6 | 6/6 | yes | (0.5771, 0.7586) | 0.628 | 0.0509 | P13 |
| interview-expression-calm | 6 | 6/6 | yes | (0.1603, 0.5782) | **0.36925** | 0.2090 | — |
| interview-light-plain | 6 | 6/6 | yes | (0.1864, 0.6679) | 0.628 | 0.0399 | — |
| lip-quiet (interview + funeral) | 14 | 14/14 | yes | (0.0462, 0.8262) | 0.628 | 0.1982 | — |
| festival-garment-festive | 6 | 6/6 | yes | (0.0866, 0.8575) | 0.628 | 0.2295 | — |
| festival-earring | 6 | 6/6 | yes | (0.0490, 0.9567) | 0.628 | 0.3287 | — |
| festival-necklace | 6 | 6/6 | yes | (0.0304, 0.8437) | 0.628 | 0.2157 | — |
| festival-lip | 6 | 6/6 | yes | (0.0715, 0.9364) | 0.628 | 0.3084 | — |
| festival-hair | 6 | 6/6 | yes | (0.0464, 0.8211) | 0.628 | 0.1931 | — |
| festival-beard | 6 | 6/6 | yes | (0.2369, 0.8214) | 0.628 | 0.1934 | — |
| festival-expression | 6 | 6/6 | yes | (0.1154, 0.7474) | 0.628 | 0.1194 | — |
| festival-light | 6 | 6/6 | yes | (0.5734, 0.7533) | 0.628 | 0.0546 | V16 |
| funeral-garment-dark | 8 | 8/8 | yes | (0.0443, 0.9049) | 0.628 | 0.2769 | — |
| funeral-collar-plain | 8 | 8/8 | yes | (0.1014, 0.8866) | 0.628 | 0.2586 | — |
| funeral-jewellery-quiet | 8 | 8/8 | yes | (0.0554, 0.8438) | 0.628 | 0.2158 | — |
| funeral-hair-plain | 8 | 8/8 | yes | (0.2700, 0.7195) | 0.628 | 0.0915 | — |
| funeral-beard-close | 8 | 8/8 | yes | (0.5737, 0.6420) | 0.628 | 0.0140 | P16 |
| funeral-expression-still | 8 | 8/8 | yes | (0.0588, 0.8732) | 0.628 | 0.2452 | — |
| funeral-light-plain | 8 | 8/8 | yes | (0.0873, 0.7755) | 0.628 | 0.1475 | — |

32 of 36 criteria separate perfectly (apart). Four notes on the choices:

- **The tie the sweep had to break.** `date-beard-even` and `family-beard-even` have two plateaus with the same best agreement; the wider one wins, and it is the conservative direction — it flags a patchy beard (V21 0.5985, V08 0.609) at the cost of flagging one genuinely even one (P09 0.4508, P11 0.5106). A false flag offers a fix; a false pass hides one.
- **The thin lines.** `family-expression-present`'s plateau is 0.007 wide and its line carries a 0.0035 margin — the curve has no room, because calm faces read 0.5501–0.5943 and one flat stare reads 0.612. `date-light-see` rides the default with a 0.0057 margin. Both are honest lines at the curve's best point, and both are on the rewording list below; neither should be nudged by hand.
- **`interview-collar-buttoned` reads 6/6 only at 0.11395.** Clef under-reads a shirt-and-tie collar (V09 0.1853, V12 0.401); the low line recovers both, with 0.0713 of margin. A tie hiding the top button is the wording's problem (docs/clef.md noted it); the line is the widening until a rewording is recorded.
- **`family-garment-festive` at 0.2394** recovers P11's navy festive kurta (0.4425) that 0.5 flagged.

**The biggest-issue choice** names one of the variant's misses — or `none` when it misses nothing — on 31 of 37 variants, matching docs/clef.md. The six it misses: V07 and V15 (the men's earring and lip misses, where it says none or the garment), P11 (same), V21 (names the collar, not the beard), P09 and P15 (names the beard, where none is expected). The issue steers the pick's wording only; the flags carry the decision.

## Render check

**The chosen settings** (`[check]` in `dresscode.toml`):

```toml
[check]
# Lines from the chosen model's gate curves; the model that reads the
# look most truthfully. Measured by T3.9 (docs/evaluation.md).
change_made = 0.50085
same_person = 0.48355
criterion_met = 0.50315
model = "clef"
```

The recorded reads — the honest pair (a) and the different-person proxy (b), per model:

| Read | Model | `change_made` | `same_person` | `criterion_met` | Decision at the shipped lines |
|---|---|---|---|---|---|
| (a) V04 → V05 (honest) | clef | 0.9866 | 0.9066 | 0.9806 | shown |
| (a) V04 → V05 (honest) | clef-flash | 0.9385 | 0.9345 | 0.9737 | shown |
| (b) V04 → woman-white (bad) | clef | 0.0151 | 0.0605 | 0.0257 | discarded |
| (b) V04 → woman-white (bad) | clef-flash | 0.0338 | 0.0546 | 0.0215 | discarded |

**Every known bad render rejected; good renders wrongly discarded: zero.** Each gate's separating interval is wide, and all three hold 0.5 inside them, so the shipped lines are also safe for the model that is not chosen:

| Gate | Model | Separating interval | Shipped line | Margin |
|---|---|---|---|---|
| `change_made` | clef | (0.0151, 0.9866) | 0.50085 | 0.4858 |
| `change_made` | clef-flash | (0.0338, 0.9385) | — | — |
| `same_person` | clef | (0.0605, 0.9066) | 0.48355 | 0.4230 |
| `same_person` | clef-flash | (0.0546, 0.9345) | — | — |
| `criterion_met` | clef | (0.0257, 0.9806) | 0.50315 | 0.4774 |
| `criterion_met` | clef-flash | (0.0215, 0.9737) | — | — |

Joint intervals (safe for both models at once): `change_made` (0.0338, 0.9385), `same_person` (0.0605, 0.9066), `criterion_met` (0.0257, 0.9737).

**The model: `clef`.** Both recorded models gate perfectly on the proxies, so the vocabulary decided: clef reads 19 of 22 attribute answers true, clef-flash 17 of 22. The trade-off is cost — a check on clef costs about 132 neurons a look against about 57 with a clef-flash check (docs/clef.md) — and the free plan covers roughly 75 all-clef looks a day. If cost ever dominates, `[check].model` flips to `"clef-flash"` with no code change; the gates held on both models at the shipped lines.

## Look vocabulary

Clef's answer against the true attribute on every recorded render, 11 attributes × 4 reads = 44 cells; 36 true. The eight misses:

| Read | Attribute | Answered | True | Why |
|---|---|---|---|---|
| honest, both models | `hair` | tied-back | bun | V05's hair is a low bun; the vocabulary has both words and the checker says tied-back. The data's own word for tied hair is bun (the tie-hair fixes set it), so memory would store a different word than the fix sets. |
| honest, both models | `expression` | flat | closed-mouth-smile | Calm and present has no vocabulary value between flat and closed-mouth-smile (docs/clef.md flagged this in T0.6; it still holds). |
| bad proxy, both models | `lip_colour` | nude | none | Bare lips read "nude" more readily than "none" — at low confidence (0.36), on a render that is discarded anyway, so the line dies with it. |
| bad proxy, clef-flash | `jewellery_colour` | gold | none | The bare ear called gold while the earring read none — flash's vocabulary weakness, on a discarded render. |
| bad proxy, clef-flash | `collar` | buttoned | open | The anchor's simple collar; flash spreads its collar answers thin. |

The discarded proxy's words never reach the user (a failed check's line dies with the render), so the misses that matter are the honest render's two — both vocabulary gaps, both already on the wording list. Both models join the honest render's line identically ("jhumkas, gold, red, mandarin, tied-back, black, flat, festive-top"), which is the same-look-same-line promise holding.

## Stability

Eleven recorded answers of the same V04 family-function judge request — the ten repeats and the original — compared cell by cell: 18 cells each (seven criterion probabilities, the biggest-issue choice, its nine option probabilities, its confidence), **zero differences**. Clef is deterministic for one request body; no criterion flips across calls, so no rewording is forced by instability, and retrying for a second opinion buys nothing.

What varies is between photos and between sizes, and the curves carry it:

- **Between photos** the near-line readings are the beard and expression groups — that is what the per-criterion lines and the four irreducible cells above measure.
- **Between sizes**: the same photo re-recorded at 1024, 768 and 512 px, read at the shipped lines, moves exactly one flag — `family-expression-present` at 768 px (0.5312, under its 0.5466 line) — and none at 1024 or 512. The pipeline ships full-size photos, so the shipped lines are measured where they run; **if a later task resizes to save neurons, re-measure first.** (The no-image recording in the same folder is the evidence behind the judge's empty-photo refusal: every criterion answers yes to a photo that is not there.)

## Recommendations

1. **Rewording candidates** (a re-recording decision — the criteria live in `testdata/occasions/`, T0.3's owner's; rewording one question moves the others on the same photo, so an occasion's whole set re-records): `family-expression-present` (a flat stare outranks calm faces; the curve's plateau is 0.007 wide), the beard-even family (`date-beard-even`, `family-beard-even` — a light sparse beard reads mid-scale whether it is even or patchy), and `date-light-see` (flat dull frontal light passes "can the eyes be seen" — V02).
2. **Vocabulary gaps** (same owner): no `calm` expression value between `flat` and `closed-mouth-smile`; `hair` answers `tied-back` where the fix data says `bun`; bare lips read `nude`. A phrase table or a widened question answers all three; either way the check re-records.
3. **Size**: stay at the committed full-size photos, or re-measure after any resize decision.
4. **Cost lever**: `[check].model = "clef-flash"` roughly halves the neuron cost per look with gates that held on the proxies, at a weaker vocabulary — a settings flip, held in reserve.

## Reproducing

```
go test ./internal/eval/ -run . -count=1
```

Reads only committed recordings and data. The test re-runs the whole evaluation twice and requires the two reports equal, pins the 260-of-273 anchor and the thirteen off-side cells at 0.5, pins every curve's chosen line inside its widest best plateau, and pins the settings above against the measurement. A changed recording, a changed criterion wording, or a hand-edited threshold fails it by name.
