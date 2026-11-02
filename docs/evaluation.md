# The evaluation

Every threshold in the settings file, chosen from measurement on the sample set's ground truth (T0.4), read back through the recorded Clef calls (T0.6) on the shipped pipeline code. These are the numbers the README and the video quote. The evaluation replays offline: `go test ./internal/eval/` re-runs all of it from committed recordings — no key, no network, no spend — and a second run returns the identical report (the test proves it).

The machinery is `internal/eval/`; the ground truth is `testdata/eval/ground-truth.json`, transcribed from [`testdata/samples/README.md`](../testdata/samples/README.md). Probabilities appear here because this document is the threshold record. None of them reaches a user: probabilities steer the pipeline and die there.

## Headline numbers

- **Judging: 269 of 273 criterion answers correct (98.5%)** against the sample set's ground truth, at the shipped lines. A flat 0.5 line reads 260 (95.2%). Every line is on its own curve's measured best plateau.
- **Render check: every known bad render discarded, no honest render lost.** On the recorded proxies — the different-person negative and the honest second-visit pair, on both models — the shipped lines decide all four reads correctly, with at least 0.42 of margin on every gate.
- **Look vocabulary: 19 of 22 attribute answers true** on the honest render and the discarded proxy with the chosen model (clef). Three misses sit on the discarded proxy render whose words die with it; of the honest render's misses, the expression gap is a word the vocabulary cannot say yet — the hair and lip reads are misreadings the vocabulary can say (the table below is exact).
- **Stability: eleven recorded answers of one judge request are identical to the last digit.** Nothing flips across calls; what varies is between photos, not between calls.

## Method

The sample set is 37 variants of four generated people across the five occasions — 273 criterion answers (each variant's occasion judged on every criterion; `lip-quiet` rides two occasions). For each criterion the evaluation:

1. replays its recorded judge calls through the real `pipeline.Judge` and keeps the raw yes-probabilities (the replay hit is also the byte-equality proof: the shipped request equals the recorded probe request, which carried no YouCam analysis);
2. sweeps the met line over the intervals between observed probabilities, scoring agreement with the ground truth;
3. takes the **widest plateau with the most agreement** — every line inside it reads the same cells, and the plateau's midpoint is the reading farthest from any observed probability, so a stray digit moves nothing;
4. chooses the default `met` as the single line reading the most cells across all criteria (ties: fewest overrides, then closest to 0.5), and gives a criterion a `per_criterion` override only where the default reads it below its own curve's best. Every criterion then performs at its maximum with the fewest overrides.

The same sweep runs per gate on the render check, over the honest pair (V04 against V05 — the second-visit story's kept look — as the positive control) and the known bad render (V04 against a different person entirely, the proxy verdict T0.4 recorded on both models). The chosen lines are the chosen model's plateau midpoints.

## Judging

**At 0.5** — the reading docs/clef.md counts, an anchor and not a setting — 254 of 273 cells agree with the ground truth, and the nineteen off-side cells are exactly the ones docs/clef.md names: the nine beards (V01's, V02's and P09's on `date-beard-even`, P11's and V08's on `family-beard-even`, V09's on `interview-beard-short`, V15's on `festival-beard`, P15's and V17's on `funeral-beard-close`); P05's, V09's, V11's and V12's collars; P03's and V04's family expressions and P05's and V11's interview ones; V02's light; P11's garment. The curves show them: seventeen of the nineteen sit inside a plateau where the right line reads them correctly, and the sweep moves exactly those seventeen. The two it cannot are V02's light and V08's beard, both in the irreducible table below. Two cells that 0.5 reads correctly are wrong at the shipped lines instead — P12's flat stare and V21's patchy beard — which is how 254 becomes 269.

**The chosen settings** (`[judge]` in `dresscode.toml`):

```toml
[judge]
# The default met line: the single line that reads the most cells
# correctly across all 36 criteria (262 of 273) with the fewest
# per-criterion overrides. Measured on the re-recorded corpus by
# T3.9e (docs/evaluation.md).
met = 0.35265

[judge.per_criterion]
# Lines from each criterion's own curve, where the criterion's best
# point sits off the default (docs/evaluation.md).
date-beard-even = 0.19435
family-beard-even = 0.1682
funeral-beard-close = 0.26845
interview-beard-short = 0.18815
interview-collar-buttoned = 0.1126
interview-expression-calm = 0.2022
```

**What they read: 269 of 273.** The four cells no line can save, because a misread probability sits inside the other group's own range:

| Cell | Reads | Should be | What happened |
|---|---|---|---|
| `family-expression-present` on P12 | 0.3702 | flag | the flat stare reads 0.0022 above V04's genuinely present face (0.3680); the curve's two equally good plateaus save one of the pair each, and the default sits on V04's side |
| `date-light-see` on V02 | 0.6666 | flag | flat dull frontal light still reads brighter than every real light miss but P10's 0.1126; the curve's other equally good plateau flags it and loses V21's genuinely lit 0.6247 instead |
| `family-beard-even` on V08 | 0.5740 | flag | the patchy beard reads above six of the seven even ones; flagging it means flagging V07's even 0.5341 with it, and the 8-of-9 plateau sits under P11's even 0.2058 instead |
| `date-beard-even` on V21 | 0.2869 | flag | the even beards read so low (P09 0.1965, V02 0.2087) that the only 7-of-8 plateau — between P10's patchy 0.1922 and P09's even 0.1965 — passes V21 with them |

All four sit on the two male groups, and two of them are beard readings. None of them is the README's weakest ground truth: on this corpus the shadows the README flags read V08 0.2636 and P10 0.1126, both on the expected side, and V08 is in this table for its beard, not its light. V02's wording gap is open in docs/clef.md still. These four are a wording problem, not a threshold problem — see Recommendations.

**Per criterion** — n = cells, best = the most agreement any line reads, plateau = the widest best interval, line = the shipped line (the default `met` = 0.35265 unless an override carries it), margin = the shipped line's distance to the nearest observed probability, 0.5 = the cells off the expected side at 0.5 (the anchors):

| Criterion | n | best | apart | plateau | line | margin | 0.5 |
|---|---|---|---|---|---|---|---|
| date-collar-open | 8 | 8/8 | yes | (0.1745, 0.7543) | 0.35265 | 0.17815 | — |
| date-garment-easy | 8 | 8/8 | yes | (0.0591, 0.7953) | 0.35265 | 0.29355 | — |
| date-hair-clear | 8 | 8/8 | yes | (0.2651, 0.8246) | 0.35265 | 0.08755 | — |
| date-beard-even | 8 | 7/8 | no | (0.1922, 0.1965) | **0.19435** | 0.00215 | P09, V01, V02 |
| date-lip-clear | 8 | 8/8 | yes | (0.0942, 0.7254) | 0.35265 | 0.25845 | — |
| date-expression-engaged | 8 | 8/8 | yes | (0.2031, 0.7531) | 0.35265 | 0.14955 | — |
| date-light-see | 8 | 7/8 | no | (0.1126, 0.6247) | 0.35265 | 0.24005 | V02 |
| family-earring-festive | 9 | 9/9 | yes | (0.0676, 0.8744) | 0.35265 | 0.28505 | — |
| family-lip-colour | 9 | 9/9 | yes | (0.3101, 0.8231) | 0.35265 | 0.04255 | — |
| family-garment-festive | 9 | 9/9 | yes | (0.1105, 0.4358) | 0.35265 | 0.08315 | P11 |
| family-hair-finished | 9 | 9/9 | yes | (0.0738, 0.5991) | 0.35265 | 0.24645 | — |
| family-beard-even | 9 | 8/9 | no | (0.1306, 0.2058) | **0.1682** | 0.0376 | P11, V08 |
| family-expression-present | 9 | 8/9 | no | (0.3702, 0.4709) | 0.35265 | 0.00685 | P03, V04 |
| family-light-even | 9 | 9/9 | yes | (0.2636, 0.6214) | 0.35265 | 0.08905 | — |
| interview-collar-buttoned | 6 | 6/6 | yes | (0.0596, 0.1656) | **0.1126** | 0.053 | P05, V09, V11, V12 |
| interview-garment-plain | 6 | 6/6 | yes | (0.0425, 0.8391) | 0.35265 | 0.31015 | — |
| interview-hair-close | 6 | 6/6 | yes | (0.0397, 0.7117) | 0.35265 | 0.31295 | — |
| interview-beard-short | 6 | 6/6 | yes | (0.1588, 0.2175) | **0.18815** | 0.02935 | V09 |
| interview-expression-calm | 6 | 6/6 | yes | (0.0845, 0.3199) | **0.2022** | 0.1177 | P05, V11 |
| interview-light-plain | 6 | 6/6 | yes | (0.1396, 0.6428) | 0.35265 | 0.21305 | — |
| lip-quiet (interview + funeral) | 14 | 14/14 | yes | (0.0672, 0.7247) | 0.35265 | 0.28545 | — |
| festival-garment-festive | 6 | 6/6 | yes | (0.0647, 0.8682) | 0.35265 | 0.28795 | — |
| festival-earring | 6 | 6/6 | yes | (0.0836, 0.9194) | 0.35265 | 0.26905 | — |
| festival-necklace | 6 | 6/6 | yes | (0.1455, 0.6929) | 0.35265 | 0.20715 | — |
| festival-lip | 6 | 6/6 | yes | (0.0942, 0.8856) | 0.35265 | 0.25845 | — |
| festival-hair | 6 | 6/6 | yes | (0.0501, 0.7265) | 0.35265 | 0.30255 | — |
| festival-beard | 6 | 6/6 | yes | (0.0677, 0.492) | 0.35265 | 0.13935 | V15 |
| festival-expression | 6 | 6/6 | yes | (0.0827, 0.6256) | 0.35265 | 0.26995 | — |
| festival-light | 6 | 6/6 | yes | (0.3245, 0.6178) | 0.35265 | 0.02815 | — |
| funeral-garment-dark | 8 | 8/8 | yes | (0.0468, 0.8998) | 0.35265 | 0.30585 | — |
| funeral-collar-plain | 8 | 8/8 | yes | (0.2605, 0.8747) | 0.35265 | 0.09215 | — |
| funeral-jewellery-quiet | 8 | 8/8 | yes | (0.0437, 0.708) | 0.35265 | 0.30895 | — |
| funeral-hair-plain | 8 | 8/8 | yes | (0.1951, 0.6473) | 0.35265 | 0.15755 | — |
| funeral-beard-close | 8 | 8/8 | yes | (0.2387, 0.2982) | **0.26845** | 0.02975 | P15, V17 |
| funeral-expression-still | 8 | 8/8 | yes | (0.0496, 0.8866) | 0.35265 | 0.30305 | — |
| funeral-light-plain | 8 | 8/8 | yes | (0.0887, 0.7056) | 0.35265 | 0.26395 | — |

32 of 36 criteria separate perfectly (apart). Five notes on the choices:

- **The ties the sweep had to break have moved.** On this corpus two curves carry two plateaus with the same best agreement, and the wider one wins in both: `family-expression-present` (0.3458, 0.3680) 0.0222 wide against (0.3702, 0.4709) 0.1007, and `date-light-see` (0.1126, 0.6247) 0.5121 wide against (0.6666, 0.7259) 0.0593. The beard criteria, where the T0.6 corpus had its tie, now have exactly one interval at their best each: `date-beard-even` 7/8 in (0.1922, 0.1965) and `family-beard-even` 8/9 in (0.1306, 0.2058).
- **Neither beard line is the conservative direction any more.** Both read the group's best, and both pay for it with a false *pass*: V21's patchy beard (0.2869) and V08's (0.5740) sit above the plateau the even beards need. A false flag offers a fix; a false pass hides one — and on this set the rule that maximises agreement has no room to choose the flagging side. That is the wording's problem, not the line's, and it is why the beard pair heads Recommendations.
- **`family-expression-present` is the one row whose shipped line sits outside its listed plateau.** The default reads the curve at its 8-of-9 best from the *other* tied interval, 0.35265 between V06's 0.3458 and V04's 0.3680, so the table prints the wider plateau the curve alone would choose and the line the product actually ships. Its margin is 0.00685, to V06's 0.3458.
- **The thin lines.** `date-beard-even` is now the thinnest line in the product: 0.19435 on a plateau 0.0043 wide, a margin of 0.00215, bounded by P10's patchy 0.1922 and P09's even 0.1965. After it come `family-expression-present` at 0.00685, then `festival-light` 0.02815, `interview-beard-short` 0.02935, `funeral-beard-close` 0.02975 and `family-beard-even` 0.0376. Four of the six overrides are beard lines (`date-beard-even`, `family-beard-even`, `funeral-beard-close`, `interview-beard-short`) — four of the five beard criteria need a line of their own. None of these should be nudged by hand. `date-light-see`, thin on the T0.6 corpus at 0.0057, rides the default here with 0.24005 of margin.
- **`interview-collar-buttoned` reads 6/6 only far below the default.** Its plateau is (0.0596, 0.1656) and the override 0.1126 is its midpoint, with 0.053 of margin. Clef under-reads every buttoned collar on this set — V12 0.1656, P05 0.2583, V09 0.3842 in the shirt and tie, V11 0.4634 — against the two open ones at 0.0369 and 0.0596, so the low line recovers four cells where the T0.6 line recovered two. A tie hiding the top button is still the wording's problem (docs/clef.md notes it); the line is the widening until a rewording is recorded. **`family-garment-festive` needs no override at all now:** P11's festive kurta reads 0.4358 and the default 0.35265 passes it, where the T0.6 corpus needed 0.2394.

**The biggest-issue choice** names one of the variant's misses — or `none` when it misses nothing — on 28 of 37 variants, matching docs/clef.md. The nine it misses: V01, V09, P09, P15 and V17 miss nothing on the shipped lines and Clef names a beard anyway; V02, V15 and P11 name a beard where the expected misses lie elsewhere; and V07 answers `none` where the earring and the lip are the misses. Eight of the nine are the beard pick. The issue steers the pick's wording only; the flags carry the decision.

## Render check

**The chosen settings** (`[check]` in `dresscode.toml`):

```toml
[check]
# Lines from the chosen model's gate curves; the model that reads the
# look most truthfully. Measured on the re-recorded corpus by T3.9e
# (docs/evaluation.md).
change_made = 0.4978
same_person = 0.4799
criterion_met = 0.4986
model = "clef"
```

The recorded reads — the honest pair (a) and the different-person proxy (b), per model:

| Read | Model | `change_made` | `same_person` | `criterion_met` | Decision at the shipped lines |
|---|---|---|---|---|---|
| (a) V04 → V05 (honest) | clef | 0.9820 | 0.9246 | 0.9744 | shown |
| (a) V04 → V05 (honest) | clef-flash | 0.9437 | 0.9434 | 0.9748 | shown |
| (b) V04 → woman-white (bad) | clef | 0.0136 | 0.0352 | 0.0228 | discarded |
| (b) V04 → woman-white (bad) | clef-flash | 0.0350 | 0.0269 | 0.0188 | discarded |

**Every known bad render rejected; good renders wrongly discarded: zero** — the shipped lines decide 4 of 4 recorded reads, and the evaluation counts the outcomes through the real `pipeline.Check`, not by eye. Each gate's separating interval is wide, and all three hold 0.5 inside them, so the shipped lines are also safe for the model that is not chosen:

| Gate | Model | Separating interval | Shipped line | Margin |
|---|---|---|---|---|
| `change_made` | clef | (0.0136, 0.9820) | 0.4978 | 0.4842 |
| `change_made` | clef-flash | (0.0350, 0.9437) | — | — |
| `same_person` | clef | (0.0352, 0.9246) | 0.4799 | 0.4447 |
| `same_person` | clef-flash | (0.0269, 0.9434) | — | — |
| `criterion_met` | clef | (0.0228, 0.9744) | 0.4986 | 0.4758 |
| `criterion_met` | clef-flash | (0.0188, 0.9748) | — | — |

Joint intervals (safe for both models at once): `change_made` (0.0350, 0.9437), `same_person` (0.0352, 0.9246), `criterion_met` (0.0228, 0.9744). The thinnest of clef's three shipped margins is `same_person` at 0.4447, so every line holds its gate by at least 0.44; clef-flash's own plateau midpoints would leave it 0.4543, 0.4582 and 0.4780.

**The model: `clef`.** Both recorded models gate perfectly on the proxies, so the vocabulary decided: clef reads 19 of 22 attribute answers true, clef-flash 18 of 22. The trade-off is cost — a check on clef costs about 132 neurons a look against about 57 with a clef-flash check (docs/clef.md) — and the free plan covers roughly 75 all-clef looks a day. If cost ever dominates, `[check].model` flips to `"clef-flash"` with no code change; the gates held on both models at the shipped lines.

## Look vocabulary

Clef's answer against the true attribute on every recorded render, 11 attributes × 4 reads = 44 cells; 37 true — clef 19 of 22, clef-flash 18 of 22. The seven misses:

| Read | Attribute | Answered | True | Why |
|---|---|---|---|---|
| honest, both models | `hair` | tied-back | bun | V05's hair is a low bun; the vocabulary has both words and the checker says tied-back (0.7990 on clef, 0.8001 on clef-flash). The data's own word for tied hair is bun (the tie-hair fixes set it), so memory would store a different word than the fix sets. |
| honest, both models | `expression` | flat | closed-mouth-smile | Calm and present has no vocabulary value between flat and closed-mouth-smile (docs/clef.md flagged this in T0.6; it still holds): flat answers 0.6555 on clef and 0.6216 on clef-flash. |
| bad proxy, both models | `lip_colour` | nude | none | Bare lips read "nude" more readily than "none" — at low confidence (0.3467 on clef, 0.3235 on clef-flash), on a render that is discarded anyway, so the line dies with it. |
| bad proxy, clef-flash | `collar` | buttoned | open | The anchor's simple collar; flash spreads its collar answers thin — `buttoned` at 0.3341 where clef says `open` at 0.7445. |

The discarded proxy's words never reach the user (a failed check's line dies with the render), so the misses that matter are the honest render's two per model — the expression, which the vocabulary has no word for, and the hair, which it does. Both are already on the wording list. Both models join the honest render's line identically ("gold jhumkas, red, mandarin, tied-back, black, flat, festive-top" — the phrase table's line, the jhumkas carrying the colour), which is the same-look-same-line promise holding. The T0.6 corpus's eighth miss does not survive the re-record: there clef-flash called the proxy's bare ear `gold` while its earring read `none`; on this recording it answers `none` (0.4428) and is correct, with `gold` only its runner-up at 0.3012.

## Stability

Eleven recorded answers of the same V04 family-function judge request — the ten repeats and the original — compared cell by cell: 18 cells each (seven criterion probabilities, the biggest-issue choice, its nine option probabilities, its confidence), **zero differences**. Clef is deterministic for one request body; no criterion flips across calls, so no rewording is forced by instability, and retrying for a second opinion buys nothing. The stability *result* is what the two corpora share — eleven answers, eighteen cells each, no difference, and one choice (`family-earring-festive`) repeated all eleven times — while the probabilities under it moved with the service: V04's expression reads 0.3680 on this recording against 0.5943 on the T0.6 one.

What varies is between photos and between sizes, and the curves carry it:

- **Between photos** the readings nearest a line are the beard group and `family-expression-present` — margins of 0.00215 and 0.00685 — then `festival-light` 0.02815, `interview-beard-short` 0.02935, `funeral-beard-close` 0.02975 and `family-beard-even` 0.0376. That is what the per-criterion lines and the four irreducible cells above measure.
- **Between sizes**: the same photo re-recorded at 1024, 768 and 512 px, read at the shipped lines, moves one flag — `family-expression-present` — and moves it at two of the three sizes, not one. It reads met at full size and at 1024 px (0.3680, 0.4769) and flagged at 768 and 512 (0.3099, 0.3321), against the default `met` = 0.35265 it rides; no other criterion's flag moves at any size. The T0.6 corpus moved the same criterion at 768 only, under the 0.5466 override it carried then. The pipeline ships full-size photos, so the shipped lines are measured where they run; **if a later task resizes to save neurons, re-measure first.** (The no-image recording in the same folder is the evidence behind the judge's empty-photo refusal. What this document claimed of it off the T0.6 corpus — that every criterion answers yes to a photo that is not there — is not what this recording says: the seven answer 0.3220 to 0.6360, six of them met at the shipped default and `family-garment-festive` (0.3220) not. A request that lost its image still answers, and the pipeline still has to refuse it. docs/clef.md carries the numbers.)

## The live re-run

The evaluation above replays recordings. T3.9b re-ran the same two measurements against Workers AI itself: one call per judge recording (37) and one per check recording (4), the committed photos, the lines read from `dresscode.toml.example`.

```
go test -tags live ./internal/eval/ -run TestLiveProbe      # two calls: price the sweep first
go test -tags live ./internal/eval/ -run TestLiveEvaluation  # 41 calls: the re-run
```

Both need `WORKERS_AI_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID` in the environment; with either missing they skip and spend nothing. Workers AI only — the run builds no YouCam client and reads no YouCam key.

The transcripts of the three paid runs are committed with the code: [`testdata/eval/live/probe.txt`](../testdata/eval/live/probe.txt) (the 2-call probe), [`sweep-1.txt`](../testdata/eval/live/sweep-1.txt) and [`sweep-2.txt`](../testdata/eval/live/sweep-2.txt) (the two 41-call passes, identical bar their timing lines). Every number in this subsection is in them. They hold no key and no account id: the test scrubs both out of every line before it prints.

**What it cost.** 41 calls — 39 on clef, 2 on clef-flash — 90,403 input tokens, 1,823 neurons, **$0.0200 booked**, inside the $0.08 the run set for itself with $0.0600 spare, and under a fifth of the free plan's 10,000 neurons a day. The probe's two calls priced it first — one judge call at 42.83 neurons, one clef check at 89.28, $0.011 per 1,000 neurons ([`internal/systemone/workersai.go`](../internal/systemone/workersai.go) books at that rate; docs/clef.md states the two neuron rates and the day allowance) — and projected the sweep at 1,791 neurons / $0.0197. It came in at 1,823 / $0.0200: a judge call costs 42.41–45.40 depending on the occasion's token count, and the two clef-flash checks, priced from the documented rate at 14.14 in the projection, booked 14.14 and 14.04. That the request bytes equalled the recordings' is proven by the offline replay, not by the live run: `internal/systemone/replay.go` keys each recording by the SHA-256 of its canonical request — model, image bytes, state and questions — and serves it only to a request that hashes the same, so a hit says the shipped request equals the recorded one, and T3.9's run is that hit on all 41 exchanges (`internal/eval/judge.go:28-33` states the rule). `liveAssertShipped` shows the thresholds that replay read are the shipped ones, which is how one offline hit covers the live requests too: the lines change only how an answer is read, never the request. Corroborating it, every call reported the recording's own input-token count — date 1,944, family function 1,963, interview 1,973, festival 2,064, funeral 2,081, the honest check pair 4,092, the proxy 4,065. Re-running the sweep returned identical cells to the last digit.

**Judging: the method reproduces, the probabilities do not.** Live against recorded, 273 cells:

| Reading | Recorded | Live |
|---|---|---|
| cells agreeing at 0.5 | 260/273 | 254/273 |
| cells agreeing at the shipped lines | 269/273 | 254/273 |
| the curves' best total | 269/273 | 269/273 |
| criteria separating perfectly | 32/36 | 32/36 |
| biggest issue names a miss (or none) | 31/37 | 28/37 |
| cells off the expected side at 0.5 | 13 | 19 |
| irreducible cells at the shipped line | 4 | 19 |

272 of the 273 live probabilities differ from the recorded ones (the one that does not is V20's `lip-quiet`, 0.0462 on both sides): of all 273 cells the mean move is 0.08, 140 stay inside 0.05, 74 move 0.10 or more and three move past 0.45 — the largest `interview-beard-short` on V09, 0.7586 → 0.2175, with `funeral-beard-close` on V17 at 0.8120 → 0.3595. 17 cells end up on the other side of the shipped line: sixteen leave the correct side and one arrives (P12's flat stare drops under its 0.5466 line), so the lines that read 269 recorded cells read 254 live ones. The shape holds where it matters: the live curves' best total is again 269 of 273, and the same 32 of 36 criteria separate perfectly, so a re-measurement lands on the same plateau — the lines, not the method, are what a single call moves. The drift concentrates in the groups the thin margins already named: `family-expression-present` reads 6/9 live against 8/9 recorded, `date-beard-even` 5/8 against 7/8, `funeral-beard-close` 6/8 against 8/8, and twelve criteria lose a cell in all.

**The check held.** All four live reads decide the same way as the recorded ones: every known bad render rejected, no honest render lost, zero of each on both sides, and the honest render's line is the recorded one, word for word, on both models (`jhumkas, gold, red, mandarin, tied-back, black, flat, festive-top`).

| Read | Model | `change_made` | `same_person` | `criterion_met` | Decision |
|---|---|---|---|---|---|
| (a) V04 → V05 (honest) | clef | 0.9820 | 0.9246 | 0.9744 | shown |
| (a) V04 → V05 (honest) | clef-flash | 0.9437 | 0.9434 | 0.9748 | shown |
| (b) V04 → woman-white (bad) | clef | 0.0136 | 0.0352 | 0.0228 | discarded |
| (b) V04 → woman-white (bad) | clef-flash | 0.0350 | 0.0269 | 0.0188 | discarded |

Each gate separates at the shipped line and at 0.5 on both models, and the separating intervals stay the same shape as the recorded ones (the recorded `change_made` interval was (0.0151, 0.9866) on clef; live (0.0136, 0.9820)) — the six margins move by 0.004 to 0.018, the largest clef `same_person` (0.4230 recorded, 0.4410 live), never enough to put a line outside the interval.

**The vocabulary.** clef reads 19 of 22 true live, exactly as recorded; clef-flash 18 of 22 against 17 recorded. Seven of the eight recorded misses reproduce: the honest render's `hair` (tied-back for a bun) and `expression` (flat for calm and present) on both models, the proxy's bare lips read `nude` on both, and flash's buttoned collar on the open anchor. The eighth did not — flash called the proxy's bare ear `gold` in the recording and `none` live, so that one recorded miss does not survive a re-run, and it sits on a render the check discards anyway.

**What this says about the numbers above.** The thresholds are measured, and a measurement is a property of the service at the moment it runs. The same 41 calls made twice returned identical cells to the last digit, so the gap to the recorded set is not call-to-call noise — it is a service-side move in the model since `testdata/clef` was recorded, and it is reproducible. The lines shipped here were chosen on one recorded pass; the live pass reads 254 of 273 with them against the 269 the recorded pass reads, and finds the same 269 available on its own curves. Nothing here argues for hand-nudging a line — the recorded set, the live set and the size sweep agree on where the thin margins are, and Recommendations already names those criteria. What it does raise is a re-measurement question: the numbers the README and the video quote (269 of 273) are the recorded ones, and a live pass with the same lines reads 254 while its own curves still top out at 269.

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
