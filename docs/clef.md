# Clef

What Workers AI's Clef takes, what it returns, and what it costs, measured on the sample set. The recordings are in [`testdata/clef/`](../testdata/clef/), and they are the T3.9e re-record: the same 55 exchanges T0.6 recorded, recorded again against the service of today on the same sample photos and the same request shape. Every number here is a raw probability as Clef returned it on that fresh pass. None of them is a threshold, and none is ever shown to a user; the thresholds are the lines chosen in [`docs/evaluation.md`](evaluation.md).

**Two rounds live in this file.** Every measurement below is the fresh corpus, except [`## Spend`](#spend) and [`## The replay format`](#the-replay-format). Those two also carry the history of the T0.6 round that built the corpus and of how its files were converted, and the fresh pass is stated beside that history rather than over it.

## The call

`POST https://api.cloudflare.com/client/v4/accounts/<account id>/ai/run/@cf/cloudflare/clef` (or `.../@cf/cloudflare/clef-flash`), `Authorization: Bearer <WORKERS_AI_API_TOKEN>`, `Content-Type: application/json`. Source: the model pages and their raw schemas at `developers.cloudflare.com/workers-ai/models/clef/` and `.../clef-flash/` (`schema-input.json`, `schema-output.json`).

### Request body

| Field | Shape |
|---|---|
| `model` | Required. `"clef"` or `"clef-flash"`, and it must match the model in the URL path. |
| `state` | Required. A string, or any JSON object or array. Long text state is truncated to fit. |
| `questions` | Required. Object of 1 to 64 questions keyed by id. Ids use letters, digits, `_`, `.`, `-`, at most 100 characters. Answers come back under the same ids. |
| `images` | Optional. At most 4. Each is a base64 data URL string (`data:image/jpeg;base64,...`) or an object `{"content_type": "image/jpeg", "base64": "..."}`. PNG, JPEG or WebP. 4 MiB and 16 megapixels each, 8 MiB decoded in all, the whole body at most 13 MiB. No URLs. Placed before the state. |

The three question types:

| Type | Fields | Answer |
|---|---|---|
| `noul` | `instructions` (a string, or an object or array holding the question), optional `criteria` `{"true": ..., "false": ...}` describing a yes and a no | `{"type": "noul", "noul": p}`, p the probability of yes |
| `choice` | `instructions`, `criteria`: object of 2 to 255 option ids to a description (string, object, array or null) | `{"type": "choice", "choice": id, "probabilities": {id: p, ...}, "confidence": c}` |
| `score` | `instructions`, `criteria`: array of 2 to 10 level descriptions, lowest first | `{"type": "score", "score": s, "legend": {"0": ...}, "probabilities": {...}, "confidence": c}` |

### Response

The REST API wraps the model's output in Cloudflare's envelope:

```json
{"result": {"model": "clef", "answers": {"<id>": {...}}, "usage": {"input_tokens": 1963, "output_tokens": 0}},
 "success": true, "errors": [], "messages": []}
```

Every probability came back with at most four decimals. `output_tokens` was 0 on every call: Clef bills input tokens only.

## The requests recorded

The wording is the data's, verbatim: each criterion's `clef` text (with `family-hair-finished` as reworded in the T0.6 round, below), the occasion's `biggest_issue.question` and option labels, and the vocabulary's questions and options from `testdata/occasions/`. The re-record sent the same wording — the fresh recordings' `instructions` are these strings, read back out of the committed requests — and the same shape. No YouCam analysis is sent in any request: each carries only the sample photo, or the two photos of a check pair, and its state carries nothing but the occasion's id and name (plus, on a check, the change and the criterion it is checking).

**Judge** (`model: clef`, one image, the sample JPEG as committed, 896 × 1200):

```json
{"model": "clef",
 "state": {"occasion": "family-function", "name": "Family function"},
 "images": [{"content_type": "image/jpeg", "base64": "..."}],
 "questions": {
   "family-earring-festive": {"type": "noul", "instructions": "Are gold jhumkas visible, rather than a bare ear or a plain stud?"},
   "...": "one noul per criterion, the criterion id as the question id",
   "biggest_issue": {"type": "choice", "instructions": "Which missed part of this dress code is the biggest issue?",
                     "criteria": {"family-earring-festive": "the earring", "...": "...", "none": "none", "cant-tell": "can't tell"}}}}
```

**Check** (two images: the original, then the render):

```json
{"model": "clef",
 "images": [<original>, <render>],
 "state": {"occasion": "family-function", "name": "Family function",
           "images": "The first image is the original photo. The second image is the render. Answer every question about the render.",
           "requested_change": "Adds gold jhumkas and paints the lip red.",
           "criterion": "family-earring-festive"},
 "questions": {
   "change_made": {"type": "noul", "instructions": "Was the requested change made in the render?"},
   "same_person": {"type": "noul", "instructions": "Is the person in the render the same person as in the original, with the face shape and skin texture unchanged?"},
   "criterion_met": {"type": "noul", "instructions": "<the criterion's clef text>"},
   "look.<attribute>": {"type": "choice", "instructions": "<vocabulary question>", "criteria": {"<option>": "<option>", "...": "..."}}}}
```

One `look.<attribute>` choice per attribute in `vocabulary.json` (eleven). The three check `noul` wordings are this probe's; T3.5 owns the final wording, and a recording only replays for a byte-identical request (below).

## Recordings

| Path | What |
|---|---|
| `testdata/clef/judge-<variant>.json` | 37 judge calls on `clef`, one per sample variant |
| `testdata/clef/check-<model>-V04-to-V05.json` | Check proxy (a), on `clef` and `clef-flash` |
| `testdata/clef/check-<model>-V04-to-woman-white.json` | Check proxy (b), the same-person negative, on both |
| `testdata/clef/stability/stability-V04-family-function-NN.json` | Ten repeats of the V04 judge request |
| `testdata/clef/cost/cost-V04-family-function-<size>.json` | The V04 judge at 1024, 768 and 512 px on the long side, and with no image |
| `testdata/clef/cost/images/<sha256>.jpg` | The three resized photos those cost requests sent (765 × 1024, 573 × 768 and 382 × 512), published by the recorder under the digest of their bytes |

Each file is the envelope `internal/systemone/replay.go` loads:

```json
{"exchange": "<file name without .json>",
 "request": {"method": "POST", "path": "/ai/run/@cf/cloudflare/clef", "body": <the request body, images by file>},
 "response": {"status": 200, "elapsed_ms": 3641, "body": <Cloudflare's envelope>}}
```

The path leaves out `/client/v4/accounts/<id>`, so no account id is stored, and no request header is stored at all. `systemone.Open("testdata/clef")` loads the 41 top-level recordings and `Ask` serves each one. `Open` does not descend into folders: `stability/` and `cost/` are opened on their own. The ten stability repeats are one request, so `Ask` on that folder answers `Ambiguous` by design and `Reply(<exchange>)` serves each; they sit in their own folder because they are the same request as `judge-V04-family-function`, and `Ask` on the top level must not turn ambiguous. `cost-*` differ in image bytes, so each is served.

A recorded image names a file instead of carrying its bytes: `{"content_type": "image/jpeg", "file": "../samples/woman/V04-family-function.jpg"}`, the path relative to the recording's own folder. `Open` reads the file and puts its bytes inline before indexing, so the request key (which hashes the image bytes) and the content key are the same as for an inline recording, and a live request carrying the bytes still matches. A referenced file that is missing or unreadable fails `Open` with the recording's name and the path. Inline images (the object form or a data URL) still load. The bytes sent to Clef were the committed files exactly; the recorder sent them inline and wrote the reference. The reference string is the one piece of a stored request that depends on where the recording sits: the ten stability repeats name `../../samples/woman/V04-family-function.jpg` because their folder is one level deeper, and their bytes, questions, state and model are equal to `judge-V04-family-function`'s, so the key they index under is the same. `testdata/clef` is about 0.6 MB (599,676 bytes over 58 files: 41 recordings, 10 stability repeats, 4 cost recordings and 3 cost photos).

## Cost

Workers AI bills Clef at 21,818 neurons per million input tokens and Clef-flash at 3,455. The free plan allows 10,000 neurons a day.

Clef's documented image tokenizer: keep the aspect ratio, round each side to a multiple of 32, scale up below about 65,000 px² and down above about 1 MP, then one token per 32 × 32 block plus 3 markers, at most 1,024 per image. The measurement matches it exactly once the text is subtracted.

The V04 family-function judge request, the same text each time:

| Image | Input tokens | Image tokens | Neurons on clef | Neurons on clef-flash |
|---|---|---|---|---|
| none (text only) | 961 | 0 | 21.0 | 3.3 |
| 896 × 1200 (as committed) | 1,963 | 1,002 | 42.8 | 6.8 |
| 765 × 1024 | 1,732 | 771 (768 × 1024 → 24 × 32 + 3) | 37.8 | 6.0 |
| 573 × 768 | 1,396 | 435 (576 × 768 → 18 × 24 + 3) | 30.5 | 4.8 |
| 382 × 512 | 1,156 | 195 (384 × 512 → 12 × 16 + 3) | 25.2 | 4.0 |

The Clef-flash column is computed at its rate. Clef-flash counted the same tokens as Clef on both check requests (4,092 and 4,065), so the tokenizer is the same.

The 896 × 1200 image is over 1 MP and is scaled down to 999 blocks + 3. The 1536 × 2048 anchor counted 972 + 3. Resized with ImageMagick `-resize`, JPEG quality 92, metadata stripped in the first round; the re-record sent those same committed bytes, and the three photos it published hash exactly as the files the first round left (`e79a9b8a…` for 1024, `ae95368c…` for 768, `4faa1b79…` for 512).

The text is a large share. The judge's questions cost 942 to 1,079 input tokens depending on the occasion (date 1,944 total, family function 1,963, interview 1,973, festival 2,064, funeral 2,081 with the full-size image). The check, with two images and fourteen questions, counted 4,092 tokens (V05 render) and 4,065 (the anchor render): about 2,090 of that is text, mostly the eleven vocabulary choices.

**Per look** (one judge plus one check, images as committed): 1,963 + 4,092 = 6,055 tokens. That is 132 neurons with the check on `clef`, or 57 with the check on `clef-flash` (42.8 + 14.1). The free 10,000 neurons a day then cover about 75 looks all-Clef at full size, about 175 with a Clef-flash check, and more if the text is trimmed. A fix-pick call (text only, no image) is not in these figures. The same arithmetic at 768 px is not in them either: this corpus records the judge at 768 (1,396 tokens) but never a two-image check at that scale, so a shrunken per-look figure cannot be read off it.

**Do the flags change at smaller sizes?** Not at a 0.5 reading on V04: no `noul` crosses 0.5 at any of the three sizes. The moves themselves are larger than a bare reading of the table suggests. The two biggest are `family-lip-colour` at 0.1227 (0.3101 at full size, 0.2608 at 1024, 0.2569 at 768, 0.1874 at 512) and `family-expression-present` at 0.1089 (0.368 full, then 0.4769, 0.3099 and 0.3321); the other five `noul` stay within 0.06 of their full-size reading. At the line the product actually reads — `[judge].met = 0.35265` in `dresscode.toml.example` — the size does move one flag: `family-expression-present` reads met at full size and at 1024 (0.368, 0.4769) and flagged at 768 and 512 (0.3099, 0.3321). It is the cell sitting nearest that line. The biggest-issue pick does not hold across sizes either: `family-earring-festive` at full size (0.234) becomes `family-garment-festive` at 1024 (0.2095) and comes back at 768 and 512 (0.2411, 0.2557), each pick within 0.05 of the others. The calls nearest any line are the ones that move, and on this request the choice question is the least settled part of the answer. One photo is not enough to settle the size; the whole set would have to be judged at each size to do it.

**Without an image**, the same questions still answer, and no longer as a block of yeses: the seven `noul` read 0.3220 to 0.6360 (`family-beard-even` 0.6360, `family-expression-present` 0.6281, `family-hair-finished` 0.6139 and `family-light-even` 0.5214 above a bare half; `family-earring-festive` 0.3825, `family-lip-colour` 0.3741 and `family-garment-festive` 0.3220 below it), and the biggest issue spreads with no clear pick (top `family-garment-festive` 0.2679, then `family-earring-festive` 0.1795 and `none` 0.1372). A judge request that loses its image does not fail; it answers. The pipeline must refuse to judge without the image rather than trust such an answer.

## Stability

Ten repeats of the V04 family-function judge request on `clef`, plus the original judge call: all eleven returned the same answers to the last digit. Spread, every probability:

| Probability | Min | Max | Mean | SD |
|---|---|---|---|---|
| `family-earring-festive` | 0.0455 | 0.0455 | 0.0455 | 0 |
| `family-lip-colour` | 0.3101 | 0.3101 | 0.3101 | 0 |
| `family-garment-festive` | 0.8243 | 0.8243 | 0.8243 | 0 |
| `family-hair-finished` | 0.8327 | 0.8327 | 0.8327 | 0 |
| `family-beard-even` | 0.8927 | 0.8927 | 0.8927 | 0 |
| `family-expression-present` | 0.368 | 0.368 | 0.368 | 0 |
| `family-light-even` | 0.8258 | 0.8258 | 0.8258 | 0 |
| `biggest_issue` → `family-earring-festive` | 0.234 | 0.234 | 0.234 | 0 |
| `biggest_issue` → `family-lip-colour` | 0.1375 | 0.1375 | 0.1375 | 0 |
| `biggest_issue` → `family-garment-festive` | 0.1535 | 0.1535 | 0.1535 | 0 |
| `biggest_issue` → `family-hair-finished` | 0.1588 | 0.1588 | 0.1588 | 0 |
| `biggest_issue` → `family-beard-even` | 0.0202 | 0.0202 | 0.0202 | 0 |
| `biggest_issue` → `family-expression-present` | 0.119 | 0.119 | 0.119 | 0 |
| `biggest_issue` → `family-light-even` | 0.0405 | 0.0405 | 0.0405 | 0 |
| `biggest_issue` → `none` | 0.0923 | 0.0923 | 0.0923 | 0 |
| `biggest_issue` → `cant-tell` | 0.0442 | 0.0442 | 0.0442 | 0 |
| `biggest_issue` confidence | 0.0428 | 0.0428 | 0.0428 | 0 |

The choice was `family-earring-festive` all eleven times, and its distribution is flat: no option above 0.234, and `biggest_issue` reports confidence 0.0428 on every one of the eleven answers while the seven `noul` span 0.0455 to 0.8927. Clef is deterministic for one request body; the variation that matters is between photos and between image sizes, not between calls. A repeat costs as much as the first call, so retrying for a second opinion buys nothing. What a repeat cannot promise is a decisive choice answer — determinism here means the same flat distribution every time, not a confident one.

## The judge on the sample set

Each cell is the expected answer from `testdata/samples/README.md` (Y should pass, N should fail) and the raw `noul` Clef returned on the fresh pass. **≠** marks a cell where the probability is on the other side of 0.5 from the expected answer. 0.5 is used only to count agreement in this document. It is not a threshold: the shipped lines sit elsewhere, chosen per criterion off the curves in [`docs/evaluation.md`](evaluation.md).

The two halves of a cell move differently. The letter is the sample set's ground truth, and a re-record does not touch it; the probability and the mark are observations of one pass of the service, and both moved here — 19 of the 273 cells sit on the wrong side of 0.5 on this corpus, where the T0.6 corpus had 13.

### Date

| Variant | Person | `date-collar-open` | `date-garment-easy` | `date-hair-clear` | `date-beard-even` | `date-lip-clear` | `date-expression-engaged` | `date-light-see` | Biggest issue (p) |
|---|---|---|---|---|---|---|---|---|---|
| V01 | man | Y 0.7543 | Y 0.9004 | Y 0.9289 | Y 0.4503 **≠** | Y 0.8158 | Y 0.848 | Y 0.7398 | `date-beard-even` 0.4902 |
| V02 | man | N 0.0831 | N 0.0473 | Y 0.9496 | Y 0.2087 **≠** | Y 0.9112 | N 0.1504 | N 0.6666 **≠** | `date-beard-even` 0.576 |
| V03 | woman | Y 0.8803 | Y 0.8995 | N 0.032 | Y 0.9587 | N 0.0942 | Y 0.8898 | Y 0.7652 | `date-lip-clear` 0.4519 |
| V21 | man | Y 0.76 | Y 0.8738 | Y 0.9318 | N 0.2869 | Y 0.902 | Y 0.7804 | Y 0.6247 | `date-beard-even` 0.5719 |
| P01 | woman-white | Y 0.7734 | Y 0.7953 | Y 0.8246 | Y 0.9084 | Y 0.7254 | Y 0.7851 | Y 0.7351 | `none` 0.3754 |
| P02 | woman-white | N 0.1745 | N 0.0591 | N 0.2651 | Y 0.8968 | N 0.0571 | Y 0.7776 | Y 0.8015 | `date-lip-clear` 0.5722 |
| P09 | man-eastasian | Y 0.8105 | Y 0.9092 | Y 0.9339 | Y 0.1965 **≠** | Y 0.8551 | Y 0.7531 | Y 0.7259 | `date-beard-even` 0.6296 |
| P10 | man-eastasian | Y 0.87 | Y 0.8896 | Y 0.9307 | N 0.1922 | Y 0.9226 | N 0.2031 | N 0.1126 | `date-beard-even` 0.4486 |

### Family function

| Variant | Person | `family-earring-festive` | `family-lip-colour` | `family-garment-festive` | `family-hair-finished` | `family-beard-even` | `family-expression-present` | `family-light-even` | Biggest issue (p) |
|---|---|---|---|---|---|---|---|---|---|
| V04 | woman | N 0.0455 | N 0.3101 | Y 0.8243 | Y 0.8327 | Y 0.8927 | Y 0.368 **≠** | Y 0.8258 | `family-earring-festive` 0.234 |
| V05 | woman | Y 0.88 | Y 0.8231 | Y 0.8132 | Y 0.7558 | Y 0.8421 | Y 0.5027 | Y 0.7413 | `none` 0.5172 |
| V06 | woman | Y 0.9472 | Y 0.9401 | Y 0.7998 | N 0.0242 | Y 0.9422 | N 0.3458 | Y 0.784 | `family-hair-finished` 0.5617 |
| V07 | man | N 0.0676 | N 0.1159 | Y 0.6955 | Y 0.5991 | Y 0.5341 | Y 0.5628 | Y 0.6214 | `none` 0.416 |
| V08 | man | N 0.0199 | N 0.0551 | N 0.0563 | Y 0.8636 | N 0.574 **≠** | N 0.2089 | N 0.2636 | `family-garment-festive` 0.5824 |
| P03 | woman-white | Y 0.8744 | Y 0.8578 | Y 0.795 | Y 0.7102 | Y 0.8686 | Y 0.4709 **≠** | Y 0.7571 | `none` 0.6084 |
| P04 | woman-white | N 0.0451 | N 0.1108 | N 0.1105 | N 0.0738 | Y 0.9025 | N 0.2584 | N 0.132 | `family-garment-festive` 0.2218 |
| P11 | man-eastasian | N 0.0205 | N 0.0339 | Y 0.4358 **≠** | Y 0.8143 | Y 0.2058 **≠** | Y 0.7403 | Y 0.8991 | `family-beard-even` 0.4225 |
| P12 | man-eastasian | N 0.019 | N 0.0769 | N 0.0778 | Y 0.7183 | N 0.1306 | N 0.3702 | Y 0.8807 | `family-beard-even` 0.3781 |

### Interview

| Variant | Person | `interview-collar-buttoned` | `interview-garment-plain` | `interview-hair-close` | `interview-beard-short` | `lip-quiet` | `interview-expression-calm` | `interview-light-plain` | Biggest issue (p) |
|---|---|---|---|---|---|---|---|---|---|
| V09 | man | Y 0.3842 **≠** | Y 0.9394 | Y 0.871 | Y 0.2175 **≠** | Y 0.8315 | Y 0.8107 | Y 0.6428 | `interview-beard-short` 0.6416 |
| V10 | man | N 0.0596 | N 0.0425 | Y 0.8664 | N 0.0773 | Y 0.801 | N 0.0845 | N 0.1396 | `interview-garment-plain` 0.7318 |
| V11 | woman | Y 0.4634 **≠** | Y 0.9434 | N 0.0334 | Y 0.9549 | N 0.0449 | Y 0.4215 **≠** | Y 0.7791 | `lip-quiet` 0.2764 |
| V12 | woman | Y 0.1656 **≠** | Y 0.8391 | Y 0.7117 | Y 0.8483 | Y 0.7247 | Y 0.5276 | Y 0.6456 | `none` 0.409 |
| P05 | woman-white | Y 0.2583 **≠** | Y 0.9429 | N 0.0397 | Y 0.9429 | Y 0.8956 | Y 0.3199 **≠** | Y 0.8075 | `interview-hair-close` 0.537 |
| P13 | man-eastasian | N 0.0369 | N 0.0382 | Y 0.8636 | N 0.1588 | Y 0.8982 | N 0.0742 | N 0.1212 | `interview-garment-plain` 0.5503 |

### Festival

| Variant | Person | `festival-garment-festive` | `festival-earring` | `festival-necklace` | `festival-lip` | `festival-hair` | `festival-beard` | `festival-expression` | `festival-light` | Biggest issue (p) |
|---|---|---|---|---|---|---|---|---|---|---|
| V13 | woman | Y 0.8799 | Y 0.9296 | Y 0.6929 | Y 0.8856 | Y 0.7265 | Y 0.9037 | Y 0.8922 | Y 0.6178 | `none` 0.6963 |
| V14 | woman | N 0.056 | N 0.0836 | N 0.069 | N 0.0693 | N 0.0501 | Y 0.9238 | N 0.0812 | N 0.1209 | `festival-hair` 0.3498 |
| V15 | man | Y 0.8682 | N 0.0351 | N 0.0775 | N 0.0785 | Y 0.8416 | Y 0.492 **≠** | Y 0.8719 | Y 0.6357 | `festival-beard` 0.3197 |
| V16 | man | N 0.0647 | N 0.0361 | N 0.0715 | N 0.076 | Y 0.8346 | N 0.0677 | Y 0.6256 | N 0.3245 | `festival-garment-festive` 0.3077 |
| P06 | woman-white | Y 0.9104 | Y 0.9194 | Y 0.7564 | Y 0.9008 | N 0.0389 | Y 0.9534 | N 0.0827 | Y 0.691 | `festival-expression` 0.2099 |
| P14 | man-eastasian | Y 0.9175 | N 0.034 | N 0.1455 | N 0.0942 | Y 0.8857 | N 0.0601 | Y 0.9503 | Y 0.7196 | `festival-beard` 0.5774 |

### Funeral

| Variant | Person | `funeral-garment-dark` | `funeral-collar-plain` | `funeral-jewellery-quiet` | `lip-quiet` | `funeral-hair-plain` | `funeral-beard-close` | `funeral-expression-still` | `funeral-light-plain` | Biggest issue (p) |
|---|---|---|---|---|---|---|---|---|---|---|
| V17 | man | Y 0.9291 | Y 0.9078 | Y 0.8534 | Y 0.8278 | Y 0.8359 | Y 0.3595 **≠** | Y 0.9309 | Y 0.7973 | `funeral-beard-close` 0.5047 |
| V18 | man | N 0.0468 | N 0.0435 | Y 0.708 | Y 0.7757 | Y 0.8431 | N 0.0376 | N 0.0496 | N 0.0864 | `funeral-garment-dark` 0.6855 |
| V19 | woman | Y 0.9034 | Y 0.8747 | Y 0.8065 | Y 0.8632 | Y 0.8248 | Y 0.8905 | Y 0.9159 | Y 0.7056 | `none` 0.51 |
| V20 | woman | N 0.038 | N 0.0676 | N 0.0437 | N 0.0462 | N 0.1951 | Y 0.8526 | N 0.0324 | N 0.0887 | `funeral-expression-still` 0.4948 |
| P07 | woman-white | Y 0.906 | Y 0.9036 | Y 0.8262 | Y 0.8463 | Y 0.6473 | Y 0.8994 | Y 0.906 | Y 0.7534 | `none` 0.5295 |
| P08 | woman-white | Y 0.8998 | N 0.2605 | N 0.0352 | N 0.0672 | N 0.0781 | Y 0.922 | Y 0.8866 | Y 0.9076 | `funeral-jewellery-quiet` 0.757 |
| P15 | man-eastasian | Y 0.919 | Y 0.914 | Y 0.859 | Y 0.816 | Y 0.8262 | Y 0.2982 **≠** | Y 0.9264 | Y 0.8209 | `funeral-beard-close` 0.7107 |
| P16 | man-eastasian | Y 0.9258 | Y 0.9286 | Y 0.9076 | Y 0.7785 | Y 0.9076 | N 0.2387 | N 0.0231 | N 0.0434 | `funeral-expression-still` 0.6788 |

### Agreement per criterion

"Agree" counts the variants whose probability sits on the expected side of 0.5. "Lowest yes" is the smallest probability among the variants expected to pass, "highest no" the largest among those expected to fail. "Apart" says whether the two groups do not overlap on this set, so that some line would split them; it says nothing about where.

| Criterion | Agree | Lowest yes | Highest no | Apart | Off the expected side |
|---|---|---|---|---|---|
| `date-collar-open` | 8/8 | 0.7543 | 0.1745 | yes |  |
| `date-garment-easy` | 8/8 | 0.7953 | 0.0591 | yes |  |
| `date-hair-clear` | 8/8 | 0.8246 | 0.2651 | yes |  |
| `date-beard-even` | 5/8 | 0.1965 | 0.2869 | no | P09 (Y, 0.1965), V01 (Y, 0.4503), V02 (Y, 0.2087) |
| `date-lip-clear` | 8/8 | 0.7254 | 0.0942 | yes |  |
| `date-expression-engaged` | 8/8 | 0.7531 | 0.2031 | yes |  |
| `date-light-see` | 7/8 | 0.6247 | 0.6666 | no | V02 (N, 0.6666) |
| `family-earring-festive` | 9/9 | 0.8744 | 0.0676 | yes |  |
| `family-lip-colour` | 9/9 | 0.8231 | 0.3101 | yes |  |
| `family-garment-festive` | 8/9 | 0.4358 | 0.1105 | yes | P11 (Y, 0.4358) |
| `family-hair-finished` | 9/9 | 0.5991 | 0.0738 | yes |  |
| `family-beard-even` | 7/9 | 0.2058 | 0.574 | no | P11 (Y, 0.2058), V08 (N, 0.574) |
| `family-expression-present` | 7/9 | 0.368 | 0.3702 | no | P03 (Y, 0.4709), V04 (Y, 0.368) |
| `family-light-even` | 9/9 | 0.6214 | 0.2636 | yes |  |
| `interview-collar-buttoned` | 2/6 | 0.1656 | 0.0596 | yes | P05 (Y, 0.2583), V09 (Y, 0.3842), V11 (Y, 0.4634), V12 (Y, 0.1656) |
| `interview-garment-plain` | 6/6 | 0.8391 | 0.0425 | yes |  |
| `interview-hair-close` | 6/6 | 0.7117 | 0.0397 | yes |  |
| `interview-beard-short` | 5/6 | 0.2175 | 0.1588 | yes | V09 (Y, 0.2175) |
| `interview-expression-calm` | 4/6 | 0.3199 | 0.0845 | yes | P05 (Y, 0.3199), V11 (Y, 0.4215) |
| `interview-light-plain` | 6/6 | 0.6428 | 0.1396 | yes |  |
| `lip-quiet` (interview and funeral) | 14/14 | 0.7247 | 0.0672 | yes |  |
| `festival-garment-festive` | 6/6 | 0.8682 | 0.0647 | yes |  |
| `festival-earring` | 6/6 | 0.9194 | 0.0836 | yes |  |
| `festival-necklace` | 6/6 | 0.6929 | 0.1455 | yes |  |
| `festival-lip` | 6/6 | 0.8856 | 0.0942 | yes |  |
| `festival-hair` | 6/6 | 0.7265 | 0.0501 | yes |  |
| `festival-beard` | 5/6 | 0.492 | 0.0677 | yes | V15 (Y, 0.492) |
| `festival-expression` | 6/6 | 0.6256 | 0.0827 | yes |  |
| `festival-light` | 6/6 | 0.6178 | 0.3245 | yes |  |
| `funeral-garment-dark` | 8/8 | 0.8998 | 0.0468 | yes |  |
| `funeral-collar-plain` | 8/8 | 0.8747 | 0.2605 | yes |  |
| `funeral-jewellery-quiet` | 8/8 | 0.708 | 0.0437 | yes |  |
| `funeral-hair-plain` | 8/8 | 0.6473 | 0.1951 | yes |  |
| `funeral-beard-close` | 6/8 | 0.2982 | 0.2387 | yes | P15 (Y, 0.2982), V17 (Y, 0.3595) |
| `funeral-expression-still` | 8/8 | 0.8866 | 0.0496 | yes |  |
| `funeral-light-plain` | 8/8 | 0.7056 | 0.0887 | yes |  |

In all, 254 of 273 criterion answers agree at 0.5, and 32 of 36 criteria have their yes and no apart on this set. The four that are not apart are `date-beard-even`, `date-light-see`, `family-beard-even` and `family-expression-present`: on this corpus no single line splits their yes from their no, whatever line is chosen.

### Agreement per person

| Person | Agree | Off the expected side |
|---|---|---|
| Indian woman (`woman`, 10 variants) | 70/74 | V04 expression, V11 collar, V11 expression, V12 collar |
| White woman (`woman-white`, 8 variants) | 56/59 | P03 expression, P05 collar, P05 expression |
| Indian man (`man`, 11 variants) | 73/81 | V01 beard, V02 beard, V02 light, V08 beard, V09 collar, V09 beard, V15 beard, V17 beard |
| East Asian man (`man-eastasian`, 8 variants) | 55/59 | P09 beard, P11 garment, P11 beard, P15 beard |

The misses gather on the men — 12 of the 19 are on the two male groups — and on the beard question, which takes 9 of them. `interview-collar-buttoned` takes 4, the expression questions 4, light 1 and `family-garment-festive` 1. Five rows carry two misses each (V02, P11, V09, V11, P05); every other row carries one or none.

- **`family-hair-finished` was reworded in the T0.6 round** (user decision), and both corpora since are recorded on the reworded question. The first round asked "Is the hair tied into one shape, rather than loose across the eyes?", and all four men's short hair, expected yes, read 0.0377 to 0.2203 on that recording — Clef answered the word "tied". The question now asks "Does the hair look finished and off the eyes, either tied back or cut short and neat?" On the fresh corpus the men expected yes read V07 0.5991, V08 0.8636, P11 0.8143, P12 0.7183. The women expected yes read V04 0.8327, V05 0.7558, P03 0.7102; the two expected no, hair loose across the eyes, read V06 0.0242 and P04 0.0738. 9 of 9 agree, and the two groups are far apart: lowest yes 0.5991 against highest no 0.0738.
- **Rewording one question moved the others.** That is the T0.6 round's measurement, on the recording it replaced: the same photos, with only that one question's text changed, gave different answers to the other six — V04's expression went from 0.648 to 0.5943, P12's from 0.4616 to 0.612, P11's beard from 0.6431 to 0.5106, P04's expression from 0.5057 to 0.4531. Clef reads the questions together, so any change of wording invalidates every recording of that occasion, not just the one criterion, and the reading has to be redone on the final wording. The fresh pass is a live demonstration of the last half: those same four cells now read V04 0.368, P12 0.3702, P11 0.2058 and P04 0.2584, and three of the four have crossed 0.5 since that reading — V04's expression has moved off its expected side, P12's expression has moved onto it, and P11's beard has moved off it.
- **`interview-collar-buttoned`** reads low on every variant the README expects to pass: V12 0.1656, P05 0.2583, V09 0.3842 (shirt and tie) and V11 0.4634, while the two expected no read 0.0369 and 0.0596. Both buttoned variants are buttoned in the README; a tie may hide the top button for Clef too. The criterion still separates — its lowest yes is above its highest no — but it separates far below mid-scale, which is one of the reasons it carries a line of its own.
- **Beard calls are the weakest group.** Nine of the 19 misses on this corpus are beard cells, and eight of those nine are expected-yes beards read low: `date-beard-even` V01 0.4503, V02 0.2087 and P09 0.1965, `family-beard-even` P11 0.2058, `interview-beard-short` V09 0.2175, `festival-beard` V15 0.492, `funeral-beard-close` V17 0.3595 and P15 0.2982. The ninth is V08's patchy beard at 0.574, the one expected-no beard still read as yes. The East Asian man's light beard is the clearest case: expected even it reads P09 0.1965, P11 0.2058 and P15 0.2982; expected patchy it reads P10 0.1922, P13 0.1588 and P16 0.2387. The two groups span 0.1588 to 0.2982 between them, and on this set no line at all splits even from patchy on `date-beard-even` or `family-beard-even`. Four of the five beard criteria carry a line of their own in `dresscode.toml.example`, and they are four of its six overrides; `date-beard-even`'s sits on the thinnest interval of the 36 — 0.19435 on a plateau running 0.1922 to 0.1965, a margin of 0.0021 either side.
- **The biggest-issue choice** names one of the variant's expected misses (or `none` when there are none) on 28 of 37. Eight of the nine that miss it name a beard question anyway: V01, V09, P09, P15 and V17 have no expected miss at all and Clef says the beard, and V02, V15 and P11 say the beard where a different miss was expected. Only V07 goes the other way, answering `none` where the earring and the lip are the misses. Its top probability is often low: under 0.4 on nine variants, under 0.3 on four.

### The weakest calls the README names

The README flags the light misses on V08 and P10 as moderate shadows, the weakest ground truth in the set.

- **V08** (`family-light-even`, expected N): 0.2636, on the expected side — the moderate shadow does not read as light on this corpus. Its beard (expected N) reads 0.574, still on the yes side, and is the row's only miss. Biggest issue `family-garment-festive` at 0.5824.
- **P10** (`date-light-see`, expected N): 0.1126, on the expected side and no longer near the middle. Its beard (N) 0.1922 and expression (N) 0.2031 are on the expected side too, and P10 agrees with the README on all seven of its cells. Biggest issue `date-beard-even` at 0.4486.
- **V02**, not flagged in the README: its flat dull frontal light (expected N for `date-light-see`) reads 0.6666, and it is the only light miss left on the set. "Can the eyes be seen" is a yes for flat light; the criterion's wording may not catch "dull".

## The check: proxies only

YouCam renders exist in the tree (T0.5, `testdata/youcam/results/`), but none of them is in this corpus: both "renders" here are committed sample photos, so these four check calls are proxies, not renders.

- **(a)** original `woman/V04-family-function.jpg`, "render" `woman/V05-family-function.jpg`: the same look with gold jhumkas and a red lip added by the edit model. Requested change "Adds gold jhumkas and paints the lip red.", criterion `family-earring-festive`.
- **(b)** original V04, "render" `anchors/woman-white.jpg`: a different person. It must read as not the same person.

| Answer | (a) clef | (a) clef-flash | (b) clef | (b) clef-flash |
|---|---|---|---|---|
| `change_made` | 0.982 | 0.9437 | 0.0136 | 0.035 |
| `same_person` | 0.9246 | 0.9434 | 0.0352 | 0.0269 |
| `criterion_met` | 0.9744 | 0.9748 | 0.0228 | 0.0188 |
| `jewellery` | jhumkas 0.9377 | jhumkas 0.9601 | none 0.8058 | none 0.6973 |
| `jewellery_colour` | gold 0.9658 | gold 0.9535 | none 0.6357 | none 0.4428 |
| `necklace` | none 0.9587 | none 0.8713 | none 0.9654 | none 0.8156 |
| `necklace_colour` | none 0.9683 | none 0.9618 | none 0.9631 | none 0.9606 |
| `lip_colour` | red 0.9191 | red 0.8952 | nude 0.3467 | nude 0.3235 |
| `collar` | mandarin 0.6755 | mandarin 0.7049 | open 0.7445 | buttoned 0.3341 |
| `hair` | tied-back 0.799 | tied-back 0.8001 | loose 0.6683 | loose 0.7098 |
| `hair_colour` | black 0.9456 | black 0.9388 | red 0.9054 | red 0.9514 |
| `beard` | none 0.9741 | none 0.9466 | none 0.9767 | none 0.9594 |
| `expression` | flat 0.6555 | flat 0.6216 | flat 0.6704 | flat 0.597 |
| `garment` | festive-top 0.8804 | festive-top 0.7895 | plain-shirt 0.5903 | plain-shirt 0.5964 |

Each vocabulary cell is the chosen option and its probability; the full distributions are in the recordings. Both models separate (a) from (b) on all three `noul` checks by a wide margin: on `clef`, 0.9820 / 0.9246 / 0.9744 on the honest pair against 0.0136 / 0.0352 / 0.0228 on the different-person pair; on `clef-flash`, 0.9437 / 0.9434 / 0.9748 against 0.0350 / 0.0269 / 0.0188. Clef-flash no longer contradicts itself on the anchor: it answers `none` for `jewellery_colour` (0.4428) as well as for `jewellery` (0.6973), though `gold` is its runner-up at 0.3012 and its confidence on that cell is only 0.1960. It still spreads its collar answer thinly — `buttoned` 0.3341 where `clef` says `open` 0.7445. On this one pair, Clef-flash looks enough for the three gating checks and weaker on the vocabulary. One pair is not enough to settle a model, which is why the choice is made on every recorded read in [`docs/evaluation.md`](evaluation.md).

`look.expression` read `flat` for V05 (0.6555 on `clef`, 0.6216 on `clef-flash`), whose README expression is calm and present. The vocabulary has no "calm" option between `flat` and `closed-mouth-smile`.

## Spend

**This file carries two spends.** The first block below is the T0.6 round that built the corpus this one replaced, kept as that round reported it. The second is the T3.9e re-record, and it is the spend of the recordings now in `testdata/clef/`.

*The T0.6 round, the corpus this one replaces.* 78 calls. The first round was 55: 37 judge (the first was V04, run alone to read its cost), 10 stability, 4 cost and 2 check on `clef`, and 2 check on `clef-flash`. The rewording of `family-hair-finished` added 23 on `clef`: the 9 family-function judges, the 10 stability repeats and the 4 cost runs. 149,571 input tokens on `clef` (3,263 neurons at the per-token rate) and 8,157 on `clef-flash` (28 neurons): 3,292 neurons computed from the reported tokens, under the 8,000 cap and inside the free 10,000 a day. The dashboard figure was not read.

*The T3.9e re-record, the corpus in the tree.* 55 calls over 45 distinct requests: the same 53 on `clef` (37 judge, 10 stability, 4 cost, 2 check) and the same 2 check calls on `clef-flash` as the first round's first pass, with no rewording pass behind them. The 45 is those 55 with the V04 judge request and its ten stability repeats collapsed onto the one key they share — 11 calls, 1 request. The pass booked **2,365 neurons, $0.026018869, with zero retry spend**: the plan's ceiling was 78 calls / 3,300 neurons / $0.0363, the 78-against-55 headroom is `workersai.go`'s retry budget, and no call reached it. The run was cut once and resumed, and the resume is the reason the headroom matters: 37 calls were paid before it stopped and 18 were made after, and the 37 cost nothing twice because a recording already on disk is refused by `AlreadyRecorded` before the inner `Ask` and books nothing. The corpus agrees with the booked figure read on its own terms: summing `usage.input_tokens` over the 55 shipped recordings gives 107,121 tokens on `clef` (2,337.2 neurons) and 8,157 on `clef-flash` (28.2), 2,365.3 neurons summed, `output_tokens` 0 on every one of the 55. So 3,292 against 2,365 is the 23 extra calls of the rewording round, not a price movement: the two corpora book identical `input_tokens` on every one of their 55 shared recordings, token for token, and the requests cost today exactly what they cost then. The calls answered in 717 to 4,187 ms (`elapsed_ms`, the fresh files' own range; the files this one replaced report 395 to 4,155).

## The replay format

- **Images by file.** The first round stored every photo inline (about 20 MB, the same V04 photo 15 times). `replay.go` now lets a recorded image name a file relative to the recording, reads it on `Open`, and keys it exactly as if inline. Every recording here references the committed photos under `testdata/samples/` or the resized ones under `testdata/clef/cost/images/`. Converting the first round's 55 recordings to file references left every request key and content key unchanged (the before and after key lists were identical).
- **One request recorded many times.** Identical requests share one key, so the stability repeats are `Ambiguous` to `Ask` and live in their own folder. That is the right refusal for a replay; it means a stability set can only be read back by exchange id.
- **The fresh files were written by the recorder, not converted.** The T3.9e corpus came out of `internal/systemone/recorder.go` on the way in: it sent the bytes inline and stored the file reference itself, so nothing here is a rewrite of the first round's files. The image index it used pointed at the samples tree only, so the three resized photos the cost requests send — the only photos not in that index — took the fallback: the recorder published each one beside its recording under the digest of its bytes, and the three references now read `images/<sha256>.jpg` where the first round's read `images/V04-family-function-<size>.jpg`. The fourth cost request sends no image at all, in either round. Those are renames, not new bytes: the three jpgs hash identically to the files the first round committed (`e79a9b8a…` at 1024, `ae95368c…` at 768, `4faa1b79…` at 512), so the same resized photos came back out of the same request shapes a round later.
