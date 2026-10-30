# Clef

What Workers AI's Clef takes, what it returns, and what it costs, measured on the sample set (T0.6). The recordings are in [`testdata/clef/`](../testdata/clef/). Every number here is a raw probability as Clef returned it. None of them is a threshold, and none is ever shown to a user: T3.9 sets the thresholds from these and later recordings.

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
{"result": {"model": "clef", "answers": {"<id>": {...}}, "usage": {"input_tokens": 1959, "output_tokens": 0}},
 "success": true, "errors": [], "messages": []}
```

Every probability came back with at most four decimals. `output_tokens` was 0 on every call: Clef bills input tokens only.

## The requests recorded

The wording is the data's, verbatim: each criterion's `clef` text (with `family-hair-finished` as reworded in this task, below), the occasion's `biggest_issue.question` and option labels, and the vocabulary's questions and options from `testdata/occasions/`. No YouCam analysis was sent in any request (none exists yet).

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
| `testdata/clef/cost/images/V04-family-function-<size>.jpg` | The three resized photos those cost requests sent |

Each file is the envelope `internal/systemone/replay.go` loads:

```json
{"exchange": "<file name without .json>",
 "request": {"method": "POST", "path": "/ai/run/@cf/cloudflare/clef", "body": <the request body, images by file>},
 "response": {"status": 200, "elapsed_ms": 1416, "body": <Cloudflare's envelope>}}
```

The path leaves out `/client/v4/accounts/<id>`, so no account id is stored, and no request header is stored at all. `systemone.Open("testdata/clef")` loads the 41 top-level recordings and `Ask` serves each one. `Open` does not descend into folders: `stability/` and `cost/` are opened on their own. The ten stability repeats are one request, so `Ask` on that folder answers `Ambiguous` by design and `Reply(<exchange>)` serves each; they sit in their own folder because they are also byte-identical to `judge-V04-family-function`, and `Ask` on the top level must not turn ambiguous. `cost-*` differ in image bytes, so each is served.

A recorded image names a file instead of carrying its bytes: `{"content_type": "image/jpeg", "file": "../samples/woman/V04-family-function.jpg"}`, the path relative to the recording's own folder. `Open` reads the file and puts its bytes inline before indexing, so the request key (which hashes the image bytes) and the content key are the same as for an inline recording, and a live request carrying the bytes still matches. A referenced file that is missing or unreadable fails `Open` with the recording's name and the path. Inline images (the object form or a data URL) still load. The bytes sent to Clef were the committed files exactly; the recorder sent them inline and wrote the reference. `testdata/clef` is about 0.6 MB.

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

The 896 × 1200 image is over 1 MP and is scaled down to 999 blocks + 3. The 1536 × 2048 anchor counted 972 + 3. Resized with ImageMagick `-resize`, JPEG quality 92, metadata stripped.

The text is a large share. The judge's questions cost 942 to 1,079 input tokens depending on the occasion (date 1,944 total, family function 1,963, interview 1,973, festival 2,064, funeral 2,081 with the full-size image). The check, with two images and fourteen questions, counted 4,092 tokens (V05 render) and 4,065 (the anchor render): about 2,090 of that is text, mostly the eleven vocabulary choices.

**Per look** (one judge plus one check, images as committed): 1,963 + 4,092 = 6,055 tokens. That is 132 neurons with the check on `clef`, or 57 with the check on `clef-flash` (42.8 + 14.1). At 768 px on the long side for both calls the estimate is about 1,396 + 2,958 tokens: 95 neurons on `clef`, 41 with the check on `clef-flash`. The free 10,000 neurons a day then cover about 75 looks all-Clef at full size, about 175 with a Clef-flash check, and more if the text is trimmed. A fix-pick call (text only, no image) is not in these figures.

**Do the flags change at smaller sizes?** Not at any 0.5 reading on V04. Every noul moves by less than 0.06, except `family-expression-present`: 0.5943 at full size, then 0.6219, 0.5312 and 0.5816 at 1024, 768 and 512. The biggest-issue pick stays `family-earring-festive`: 0.8456 at full size, then 0.8563, 0.839 and 0.7497. The calls nearest any line are the ones that move. One photo is not enough to settle the size; T3.9 should judge it on the whole set.

**Without an image**, the same questions answer yes at 0.67 to 0.86 on every criterion, and the biggest issue spreads (top pick `family-garment-festive` at 0.3323). A judge request that loses its image does not fail; it answers. The pipeline must refuse to judge without the image rather than trust such an answer.

## Stability

Ten repeats of the V04 family-function judge request on `clef`, plus the original judge call: all eleven returned the same answers to the last digit. Spread, every probability:

| Probability | Min | Max | Mean | SD |
|---|---|---|---|---|
| `family-earring-festive` | 0.0351 | 0.0351 | 0.0351 | 0 |
| `family-lip-colour` | 0.2094 | 0.2094 | 0.2094 | 0 |
| `family-garment-festive` | 0.9308 | 0.9308 | 0.9308 | 0 |
| `family-hair-finished` | 0.9253 | 0.9253 | 0.9253 | 0 |
| `family-beard-even` | 0.9518 | 0.9518 | 0.9518 | 0 |
| `family-expression-present` | 0.5943 | 0.5943 | 0.5943 | 0 |
| `family-light-even` | 0.9061 | 0.9061 | 0.9061 | 0 |
| `biggest_issue` → `family-earring-festive` | 0.8456 | 0.8456 | 0.8456 | 0 |
| `biggest_issue` → `family-lip-colour` | 0.0386 | 0.0386 | 0.0386 | 0 |
| `biggest_issue` → `family-garment-festive` | 0.0138 | 0.0138 | 0.0138 | 0 |
| `biggest_issue` → `family-hair-finished` | 0.0124 | 0.0124 | 0.0124 | 0 |
| `biggest_issue` → `family-beard-even` | 0.0052 | 0.0052 | 0.0052 | 0 |
| `biggest_issue` → `family-expression-present` | 0.0273 | 0.0273 | 0.0273 | 0 |
| `biggest_issue` → `family-light-even` | 0.0076 | 0.0076 | 0.0076 | 0 |
| `biggest_issue` → `none` | 0.0341 | 0.0341 | 0.0341 | 0 |
| `biggest_issue` → `cant-tell` | 0.0154 | 0.0154 | 0.0154 | 0 |
| `biggest_issue` confidence | 0.6839 | 0.6839 | 0.6839 | 0 |

The choice was `family-earring-festive` all ten times. Clef is deterministic for one request body; the variation that matters is between photos and between image sizes, not between calls. A repeat costs as much as the first call, so retrying for a second opinion buys nothing.

## The judge on the sample set

Each cell is the expected answer from `testdata/samples/README.md` (Y should pass, N should fail) and the raw `noul` Clef returned. **≠** marks a cell where the probability is on the other side of 0.5 from the expected answer. 0.5 is used only to count agreement in this document. It is not a threshold, and T3.9 may well pick per-criterion lines elsewhere.

### Date

| Variant | Person | `date-collar-open` | `date-garment-easy` | `date-hair-clear` | `date-beard-even` | `date-lip-clear` | `date-expression-engaged` | `date-light-see` | Biggest issue (p) |
|---|---|---|---|---|---|---|---|---|---|
| V01 | man | Y 0.8095 | Y 0.8085 | Y 0.7847 | Y 0.7217 | Y 0.6944 | Y 0.7236 | Y 0.6337 | `none` 0.4917 |
| V02 | man | N 0.0403 | N 0.037 | Y 0.924 | Y 0.7355 | Y 0.9096 | N 0.1923 | N 0.7913 **≠** | `date-expression-engaged` 0.307 |
| V03 | woman | Y 0.9622 | Y 0.9296 | N 0.0398 | Y 0.9477 | N 0.0925 | Y 0.8054 | Y 0.8341 | `date-lip-clear` 0.5553 |
| V21 | man | Y 0.8593 | Y 0.8236 | Y 0.8882 | N 0.5985 **≠** | Y 0.8273 | Y 0.8644 | Y 0.6388 | `date-collar-open` 0.3019 |
| P01 | woman-white | Y 0.8995 | Y 0.8651 | Y 0.8451 | Y 0.8625 | Y 0.6691 | Y 0.7957 | Y 0.7587 | `none` 0.5515 |
| P02 | woman-white | N 0.0509 | N 0.0376 | N 0.4052 | Y 0.946 | N 0.0775 | Y 0.7439 | Y 0.8057 | `date-lip-clear` 0.7029 |
| P09 | man-eastasian | Y 0.9085 | Y 0.9098 | Y 0.9079 | Y 0.4508 **≠** | Y 0.8344 | Y 0.7565 | Y 0.8408 | `date-beard-even` 0.5363 |
| P10 | man-eastasian | Y 0.9525 | Y 0.9061 | Y 0.914 | N 0.4486 | Y 0.852 | N 0.3786 | N 0.3259 | `date-beard-even` 0.4168 |

### Family function

| Variant | Person | `family-earring-festive` | `family-lip-colour` | `family-garment-festive` | `family-hair-finished` | `family-beard-even` | `family-expression-present` | `family-light-even` | Biggest issue (p) |
|---|---|---|---|---|---|---|---|---|---|
| V04 | woman | N 0.0351 | N 0.2094 | Y 0.9308 | Y 0.9253 | Y 0.9518 | Y 0.5943 | Y 0.9061 | `family-earring-festive` 0.8456 |
| V05 | woman | Y 0.9517 | Y 0.939 | Y 0.9103 | Y 0.9012 | Y 0.9041 | Y 0.5608 | Y 0.8574 | `none` 0.7908 |
| V06 | woman | Y 0.9798 | Y 0.9746 | Y 0.8925 | N 0.0263 | Y 0.9631 | N 0.5431 **≠** | Y 0.929 | `family-hair-finished` 0.9008 |
| V07 | man | N 0.0249 | N 0.037 | Y 0.8987 | Y 0.8446 | Y 0.8676 | Y 0.7219 | Y 0.9009 | `none` 0.3765 |
| V08 | man | N 0.0219 | N 0.0193 | N 0.0342 | Y 0.8906 | N 0.609 **≠** | N 0.3287 | N 0.6223 **≠** | `family-garment-festive` 0.5698 |
| P03 | woman-white | Y 0.9422 | Y 0.9444 | Y 0.8356 | Y 0.7673 | Y 0.9143 | Y 0.5501 | Y 0.9025 | `none` 0.6472 |
| P04 | woman-white | N 0.025 | N 0.1337 | N 0.0363 | N 0.0312 | Y 0.9342 | N 0.4531 | N 0.4152 | `family-garment-festive` 0.2674 |
| P11 | man-eastasian | N 0.0201 | N 0.0283 | Y 0.4425 **≠** | Y 0.8544 | Y 0.5106 | Y 0.7279 | Y 0.9208 | `family-garment-festive` 0.349 |
| P12 | man-eastasian | N 0.0161 | N 0.0234 | N 0.0281 | Y 0.8829 | N 0.2652 | N 0.612 **≠** | Y 0.9361 | `family-garment-festive` 0.4434 |

### Interview

| Variant | Person | `interview-collar-buttoned` | `interview-garment-plain` | `interview-hair-close` | `interview-beard-short` | `lip-quiet` | `interview-expression-calm` | `interview-light-plain` | Biggest issue (p) |
|---|---|---|---|---|---|---|---|---|---|
| V09 | man | Y 0.1853 **≠** | Y 0.9297 | Y 0.8563 | Y 0.7586 | Y 0.8437 | Y 0.6607 | Y 0.6679 | `none` 0.4553 |
| V10 | man | N 0.0426 | N 0.0267 | Y 0.9375 | N 0.2603 | Y 0.9248 | N 0.1121 | N 0.1864 | `interview-garment-plain` 0.8586 |
| V11 | woman | Y 0.5524 | Y 0.9729 | N 0.032 | Y 0.9552 | N 0.0295 | Y 0.6101 | Y 0.7197 | `lip-quiet` 0.725 |
| V12 | woman | Y 0.401 **≠** | Y 0.948 | Y 0.8597 | Y 0.9257 | Y 0.8262 | Y 0.672 | Y 0.7546 | `none` 0.3807 |
| P05 | woman-white | Y 0.6087 | Y 0.9678 | N 0.0412 | Y 0.9404 | Y 0.8921 | Y 0.5782 | Y 0.8412 | `interview-hair-close` 0.8352 |
| P13 | man-eastasian | N 0.024 | N 0.0307 | Y 0.9116 | N 0.5771 **≠** | Y 0.8955 | N 0.1603 | N 0.1785 | `interview-garment-plain` 0.8398 |

### Festival

| Variant | Person | `festival-garment-festive` | `festival-earring` | `festival-necklace` | `festival-lip` | `festival-hair` | `festival-beard` | `festival-expression` | `festival-light` | Biggest issue (p) |
|---|---|---|---|---|---|---|---|---|---|---|
| V13 | woman | Y 0.9298 | Y 0.9621 | Y 0.8437 | Y 0.9562 | Y 0.8433 | Y 0.9216 | Y 0.9466 | Y 0.7703 | `none` 0.8016 |
| V14 | woman | N 0.0401 | N 0.049 | N 0.0304 | N 0.0715 | N 0.0464 | Y 0.9357 | N 0.0844 | N 0.2935 | `festival-hair` 0.3792 |
| V15 | man | Y 0.8575 | N 0.0146 | N 0.0195 | N 0.0214 | Y 0.8211 | Y 0.8214 | Y 0.9004 | Y 0.7898 | `none` 0.5078 |
| V16 | man | N 0.0866 | N 0.0211 | N 0.0211 | N 0.0248 | Y 0.8633 | N 0.2369 | Y 0.7474 | N 0.5734 **≠** | `festival-garment-festive` 0.5569 |
| P06 | woman-white | Y 0.8831 | Y 0.9567 | Y 0.9434 | Y 0.9364 | N 0.033 | Y 0.9592 | N 0.1154 | Y 0.7533 | `festival-hair` 0.7009 |
| P14 | man-eastasian | Y 0.9307 | N 0.0157 | N 0.0297 | N 0.015 | Y 0.8285 | N 0.1742 | Y 0.96 | Y 0.7755 | `festival-beard` 0.7546 |

### Funeral

| Variant | Person | `funeral-garment-dark` | `funeral-collar-plain` | `funeral-jewellery-quiet` | `lip-quiet` | `funeral-hair-plain` | `funeral-beard-close` | `funeral-expression-still` | `funeral-light-plain` | Biggest issue (p) |
|---|---|---|---|---|---|---|---|---|---|---|
| V17 | man | Y 0.9546 | Y 0.8917 | Y 0.9117 | Y 0.8756 | Y 0.8603 | Y 0.812 | Y 0.9303 | Y 0.803 | `none` 0.694 |
| V18 | man | N 0.0287 | N 0.0604 | Y 0.8438 | Y 0.8729 | Y 0.8599 | N 0.2102 | N 0.0588 | N 0.0623 | `funeral-garment-dark` 0.8386 |
| V19 | woman | Y 0.9604 | Y 0.8902 | Y 0.9047 | Y 0.9014 | Y 0.8733 | Y 0.9268 | Y 0.9443 | Y 0.7755 | `none` 0.7127 |
| V20 | woman | N 0.0443 | N 0.0853 | N 0.0554 | N 0.0462 | N 0.27 | Y 0.9366 | N 0.0475 | N 0.0873 | `funeral-garment-dark` 0.5527 |
| P07 | woman-white | Y 0.9656 | Y 0.9273 | Y 0.9184 | Y 0.8911 | Y 0.7797 | Y 0.9311 | Y 0.9361 | Y 0.8654 | `none` 0.7329 |
| P08 | woman-white | Y 0.9049 | N 0.1014 | N 0.033 | N 0.0431 | N 0.1337 | Y 0.952 | Y 0.9314 | Y 0.9026 | `funeral-jewellery-quiet` 0.8077 |
| P15 | man-eastasian | Y 0.9465 | Y 0.8866 | Y 0.8799 | Y 0.8603 | Y 0.7195 | Y 0.642 | Y 0.8732 | Y 0.8684 | `funeral-beard-close` 0.4155 |
| P16 | man-eastasian | Y 0.9316 | Y 0.9199 | Y 0.8606 | Y 0.8612 | Y 0.809 | N 0.5737 **≠** | N 0.0393 | N 0.0392 | `funeral-expression-still` 0.8096 |


### Agreement per criterion

"Agree" counts the variants whose probability sits on the expected side of 0.5. "Lowest yes" is the smallest probability among the variants expected to pass, "highest no" the largest among those expected to fail. "Apart" says whether the two groups do not overlap on this set, so that some line would split them; it says nothing about where.

| Criterion | Agree | Lowest yes | Highest no | Apart | Off the expected side |
|---|---|---|---|---|---|
| `date-collar-open` | 8/8 | 0.8095 | 0.0509 | yes | |
| `date-garment-easy` | 8/8 | 0.8085 | 0.0376 | yes | |
| `date-hair-clear` | 8/8 | 0.7847 | 0.4052 | yes | |
| `date-beard-even` | 6/8 | 0.4508 | 0.5985 | no | V21 (N, 0.5985), P09 (Y, 0.4508) |
| `date-lip-clear` | 8/8 | 0.6691 | 0.0925 | yes | |
| `date-expression-engaged` | 8/8 | 0.7236 | 0.3786 | yes | |
| `date-light-see` | 7/8 | 0.6337 | 0.7913 | no | V02 (N, 0.7913) |
| `family-earring-festive` | 9/9 | 0.9422 | 0.0351 | yes | |
| `family-lip-colour` | 9/9 | 0.939 | 0.2094 | yes | |
| `family-garment-festive` | 8/9 | 0.4425 | 0.0363 | yes | P11 (Y, 0.4425) |
| `family-hair-finished` | 9/9 | 0.7673 | 0.0312 | yes | |
| `family-beard-even` | 8/9 | 0.5106 | 0.609 | no | V08 (N, 0.609) |
| `family-expression-present` | 7/9 | 0.5501 | 0.612 | no | V06 (N, 0.5431), P12 (N, 0.612) |
| `family-light-even` | 8/9 | 0.8574 | 0.6223 | yes | V08 (N, 0.6223) |
| `interview-collar-buttoned` | 4/6 | 0.1853 | 0.0426 | yes | V09 (Y, 0.1853), V12 (Y, 0.401) |
| `interview-garment-plain` | 6/6 | 0.9297 | 0.0307 | yes | |
| `interview-hair-close` | 6/6 | 0.8563 | 0.0412 | yes | |
| `interview-beard-short` | 5/6 | 0.7586 | 0.5771 | yes | P13 (N, 0.5771) |
| `interview-expression-calm` | 6/6 | 0.5782 | 0.1603 | yes | |
| `interview-light-plain` | 6/6 | 0.6679 | 0.1864 | yes | |
| `lip-quiet` (interview and funeral) | 14/14 | 0.8262 | 0.0462 | yes | |
| `festival-garment-festive` | 6/6 | 0.8575 | 0.0866 | yes | |
| `festival-earring` | 6/6 | 0.9567 | 0.049 | yes | |
| `festival-necklace` | 6/6 | 0.8437 | 0.0304 | yes | |
| `festival-lip` | 6/6 | 0.9364 | 0.0715 | yes | |
| `festival-hair` | 6/6 | 0.8211 | 0.0464 | yes | |
| `festival-beard` | 6/6 | 0.8214 | 0.2369 | yes | |
| `festival-expression` | 6/6 | 0.7474 | 0.1154 | yes | |
| `festival-light` | 5/6 | 0.7533 | 0.5734 | yes | V16 (N, 0.5734) |
| `funeral-garment-dark` | 8/8 | 0.9049 | 0.0443 | yes | |
| `funeral-collar-plain` | 8/8 | 0.8866 | 0.1014 | yes | |
| `funeral-jewellery-quiet` | 8/8 | 0.8438 | 0.0554 | yes | |
| `funeral-hair-plain` | 8/8 | 0.7195 | 0.27 | yes | |
| `funeral-beard-close` | 7/8 | 0.642 | 0.5737 | yes | P16 (N, 0.5737) |
| `funeral-expression-still` | 8/8 | 0.8732 | 0.0588 | yes | |
| `funeral-light-plain` | 8/8 | 0.7755 | 0.0873 | yes | |

In all, 260 of 273 criterion answers agree at 0.5, and 32 of 36 criteria have their yes and no apart on this set.

### Agreement per person

| Person | Agree | Off the expected side |
|---|---|---|
| Indian woman (`woman`, 10 variants) | 72/74 | V06 expression, V12 collar |
| White woman (`woman-white`, 8) | 59/59 | |
| Indian man (`man`, 11) | 75/81 | V02 light, V08 beard and light, V09 collar, V16 light, V21 beard |
| East Asian man (`man-eastasian`, 8) | 54/59 | P09 beard, P11 garment, P12 expression, P13 beard, P16 beard |

The misses gather on the men, and on beard and light.

- **`family-hair-finished` was reworded in this task** (user decision). The first round asked "Is the hair tied into one shape, rather than loose across the eyes?", and all four men's short hair, expected yes, read 0.0377 to 0.2203: Clef answered the word "tied". It now asks "Does the hair look finished and off the eyes, either tied back or cut short and neat?", and every family-function recording was redone on it. The men now read V07 0.8446, V08 0.8906, P11 0.8544, P12 0.8829. The women expected yes read V04 0.9253, V05 0.9012, P03 0.7673; the two expected no, hair loose across the eyes, read V06 0.0263 and P04 0.0312. 9 of 9 agree, and the two groups are far apart.
- **Rewording one question moved the others.** The same photos, with only that one question's text changed, gave different answers to the other six: V04's expression went from 0.648 to 0.5943, P12's from 0.4616 to 0.612 (now on the wrong side), P11's beard from 0.6431 to 0.5106, P04's expression from 0.5057 to 0.4531 (now on the right side). Clef reads the questions together. Any change of wording invalidates every recording of that occasion, not just the one criterion, and T3.9 must measure on the final wording.
- **`interview-collar-buttoned`** reads low on V09 (shirt and tie, 0.1853) and V12 (0.401). Both are buttoned in the README. A tie may hide the top button for Clef too.
- **Beard calls are the weakest group.** The East Asian man's light beard sits near the middle whether it is expected even (P09 0.4508, P11 0.5106, P15 0.642) or patchy (P10 0.4486, P13 0.5771, P16 0.5737). V21 and V08, the Indian man's patchy beard, read 0.5985 and 0.609 against an expected no.
- **The biggest-issue choice** names one of the variant's expected misses (or `none` when there are none) on 31 of 37. The six that do not: V07, V15 and P11 (the men's earring and lip misses, where Clef says `none` or the garment), V21 (`date-collar-open` at 0.3019 instead of the beard), and P09 and P15 (the beard, where none is expected). Its top probability is often low: under 0.4 on seven variants.

### The weakest calls the README names

The README flags the light misses on V08 and P10 as moderate shadows, the weakest ground truth in the set.

- **V08** (`family-light-even`, expected N): 0.6223, on the yes side. Its beard (expected N) reads 0.609, also on the yes side. Biggest issue `family-garment-festive` at 0.5698. V08 is the least reliable row in the set.
- **P10** (`date-light-see`, expected N): 0.3259, on the expected side. Its beard (N) 0.4486 and expression (N) 0.3786 are also on the expected side, though near the middle. Biggest issue `date-beard-even` at 0.4168.
- **V02**, not flagged in the README: its flat dull frontal light (expected N for `date-light-see`) reads 0.7913. "Can the eyes be seen" is a yes for flat light; the criterion's wording may not catch "dull".

## The check: proxies only

There is no YouCam render yet (T0.5 comes later). These are proxies, not renders, and the real check recordings are redone after T0.5.

- **(a)** original `woman/V04-family-function.jpg`, "render" `woman/V05-family-function.jpg`: the same look with gold jhumkas and a red lip added by the edit model. Requested change "Adds gold jhumkas and paints the lip red.", criterion `family-earring-festive`.
- **(b)** original V04, "render" `anchors/woman-white.jpg`: a different person. It must read as not the same person.

| Answer | (a) clef | (a) clef-flash | (b) clef | (b) clef-flash |
|---|---|---|---|---|
| `change_made` | 0.9866 | 0.9385 | 0.0151 | 0.0338 |
| `same_person` | 0.9066 | 0.9345 | 0.0605 | 0.0546 |
| `criterion_met` | 0.9806 | 0.9737 | 0.0257 | 0.0215 |
| `jewellery` | jhumkas 0.9636 | jhumkas 0.9573 | none 0.8882 | none 0.8815 |
| `jewellery_colour` | gold 0.9661 | gold 0.9494 | none 0.736 | **gold 0.7073** |
| `necklace` | none 0.9594 | none 0.7841 | none 0.9438 | none 0.789 |
| `necklace_colour` | none 0.9655 | none 0.9508 | none 0.9545 | none 0.949 |
| `lip_colour` | red 0.915 | red 0.8893 | nude 0.3603 | nude 0.3645 |
| `collar` | mandarin 0.7121 | mandarin 0.7525 | open 0.7177 | buttoned 0.3391 |
| `hair` | tied-back 0.8167 | tied-back 0.8347 | loose 0.6741 | loose 0.7547 |
| `hair_colour` | black 0.9438 | black 0.9471 | red 0.8889 | red 0.9566 |
| `beard` | none 0.9815 | none 0.9206 | none 0.9825 | none 0.9532 |
| `expression` | flat 0.7573 | flat 0.5912 | flat 0.7747 | flat 0.7052 |
| `garment` | festive-top 0.8666 | festive-top 0.8028 | plain-shirt 0.6566 | plain-shirt 0.5565 |

Each vocabulary cell is the chosen option and its probability; the full distributions are in the recordings. Both models separate (a) from (b) on all three `noul` checks by a wide margin. Clef-flash calls the anchor's bare ear "gold" for `jewellery_colour` while also saying the earring is `none`, and spreads its collar answer thinly. On this one pair, Clef-flash looks enough for the three gating checks and weaker on the vocabulary. One pair is not enough to decide; the decision belongs to the evaluation on the real renders.

`look.expression` read `flat` for V05, whose README expression is calm and present. The vocabulary has no "calm" option between `flat` and `closed-mouth-smile`.

## Spend

78 calls. The first round was 55: 37 judge (the first was V04, run alone to read its cost), 10 stability, 4 cost and 2 check on `clef`, and 2 check on `clef-flash`. The rewording of `family-hair-finished` added 23 on `clef`: the 9 family-function judges, the 10 stability repeats and the 4 cost runs. 149,571 input tokens on `clef` (3,263 neurons at the per-token rate) and 8,157 on `clef-flash` (28 neurons): 3,292 neurons computed from the reported tokens, under the 8,000 cap and inside the free 10,000 a day. The dashboard figure was not read.

## The replay format

- **Images by file.** The first round stored every photo inline (about 20 MB, the same V04 photo 15 times). `replay.go` now lets a recorded image name a file relative to the recording, reads it on `Open`, and keys it exactly as if inline. Every recording here references the committed photos under `testdata/samples/` or the resized ones under `testdata/clef/cost/images/`. Converting the first round's 55 recordings to file references left every request key and content key unchanged (the before and after key lists were identical).
- **One request recorded many times.** Identical requests share one key, so the stability repeats are `Ambiguous` to `Ask` and live in their own folder. That is the right refusal for a replay; it means a stability set can only be read back by exchange id.
