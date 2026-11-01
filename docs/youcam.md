# YouCam v2: what the API returned

Recorded by T0.5 against the India server, `https://yce-api-01.makeupar.com`, with the API key as a Bearer token. The recordings are in [`testdata/youcam/`](../testdata/youcam/); the result images are in `testdata/youcam/results/`. Request and response shapes below are copied from those recordings, not from the docs.

## What is recorded, and how

Two kinds of recording sit side by side in `testdata/youcam/`. Both load with `youcam.Open`.

| Kind | Names | What it holds | Served by |
|---|---|---|---|
| Typed | `<flow>-upload-src`, `-upload-ref`, `-create`, `-poll`, `-fetch` | The seam's own types: `File`, `Task`, `PollRef`, `ResultRef` in; `Slot`, `Created`, `Status`, `Result` or a typed `Error` out | `Upload`, `Create`, `Poll`, `Fetch` |
| HTTP | `http-*` | The wire: method, path, headers, body, status, response headers, response body, `elapsed_ms` | `Serve` for API calls; `Reply` for the two signed-URL legs (PUT upload, GET result), which have a URL instead of a path |

- **No image bytes in a recording.** A typed upload names its file (`"file": "../samples/woman/V04-family-function.jpg"`) and a typed result names its picture (`"file": "results/lip-red-V04.jpg"`), relative to the recording. `Open` reads the file and keys the recording exactly as if the bytes were inline; a missing file fails `Open` naming the recording and the path.
- **Result pictures are re-encoded.** The provider's bytes (PNG for makeup, earring and necklace; JPEG for the rest) are stored as JPEG quality 92 with metadata stripped. `Fetch` serves those bytes, not the provider's.
- **No key, no signed query.** The `Authorization` header is never recorded; `auth` says how the call was authorised. Every signed upload or result URL is recorded with its query string cut off, because the query carries the signature and an AWS access key id. The URL path carries the provider's literal segments `ACCOUNT/<id>/v2/USER/<uuid>`: the id after `ACCOUNT/` is a per-upload identifier, not the account number — the same account shows different values across eras (the T0.5 recordings and this project's live runs), and the object names sit behind a 30-minute TTL that grants nothing without the cut query. The account number appears nowhere in a recording. Pool and ledger ids in the two credit recordings are zeroed.
- **Final poll only.** A running poll and the finished poll of one task are the same request, so only the finished one is recorded. A running poll answers `{"status":200,"data":{"error":null,"results":null,"task_status":"running"}}`.
- **One slot recording per distinct request.** The File API body names no task, so uploading the same picture twice is the same request with a different `file_id`. Only the first is kept as `http-*-slot-*`. Likewise a second flow on the same picture and task (the default-smoothing lip, the engine-placed necklace, the ponytail, the never-uploaded file) has no typed upload; its create, poll and fetch are recorded under its own `file_id`.
- **The typed `Status` has no field for analysis results.** `face-attr-poll` records success only; `faceshape` is in `http-face-attr-poll`. The seam needs a place for it before the face-shape step can use the replay.
- **Typed task names drop the version.** `hair-transfer` is called at `/s2s/v2.1/`; every other task at `/s2s/v2.0/`.

## The calls

Every call: `Authorization: Bearer <key>`, `Content-Type: application/json`. A GET without `Content-Type` gets `415 UnsupportedMediaType` on the credit history (the other GETs did not need it). Each API call took 0.85 to 1.1 s from this machine; every response carries `Connection: close`.

### 1. Upload slot (no units)

`POST /s2s/v2.0/file` with `{"files":[{"content_type":"image/jpeg","file_name":"P12-family-function.jpg","file_size":219774}]}`. `POST /s2s/v2.0/file/ai-smile` (the form `docs/architecture.md` names) answers the same.

```json
{"status":200,"data":{"files":[{"content_type":"image/jpeg","file_name":"P12-family-function.jpg",
 "file_id":"1CtI2li5…wuc2K",
 "requests":[{"method":"PUT","url":"https://yce-us.s3-accelerate.amazonaws.com/ttl30/ACCOUNT/…/USER/<uuid>.jpg",
   "headers":{"Content-Length":"219774","Content-Type":"image/jpeg"}}]}]}}
```

The slot validates nothing: `text/plain`, a declared 50 MB and a declared 0 bytes all get a slot (`http-slot-*`). Only a missing `files` is refused (`400 InvalidParameters`, "files is required but wasn't included in your request.").

### 2. PUT the bytes

`PUT` to `requests[0].url` with exactly the two headers given, no `Authorization`. Answers `200` with an empty body, an `ETag`, and `x-amz-expiration: … rule-id="TTL_30"`. The signed URL is valid for 7,200 s (`X-Amz-Expires`). The bucket is `yce-us` (AWS `us-west-2`) behind CloudFront, although the API host is the India server.

### 3. Create

`POST /s2s/v2.0/task/<task>` (hair: `/s2s/v2.1/task/hair-transfer`). Answers `{"status":200,"data":{"task_id":"…"}}`. No polling interval is returned. The task id starts with the source `file_id`'s prefix.

### 4. Poll

`GET /s2s/v2.0/task/<task>/<task_id>`. Finished:

```json
{"status":200,"data":{"error":null,"results":{"url":"https://yce-us.s3-accelerate.amazonaws.com/ttl30/ACCOUNT/…/<uuid>.jpg"},"task_status":"success"}}
```

Engine failure (still HTTP 200):

```json
{"status":200,"data":{"error":"error_no_face","error_message":"No face was detected in the source image.","results":null,"task_status":"error"}}
```

### 5. Fetch the result

`GET` the result URL, no `Authorization`. The makeup, earring and necklace results come back as PNG with `Content-Type: binary/octet-stream` under a `.jpg` name; sniff the bytes, not the header or the extension.

## Request bodies used

| Task | Body (file ids shortened) |
|---|---|
| `ai-smile` | `{"src_file_id":…,"expression_type":"closed_mouth_smile"}` |
| `makeup-vto` | `{"src_file_id":…,"version":"1.0","effects":[{"category":"skin_smooth","skinSmoothStrength":0,"skinSmoothColorIntensity":0},{"category":"lip_color","shape":{"name":"original"},"palettes":[{"color":"#B0172F","texture":"matte","colorIntensity":70}],"style":{"type":"full"}}]}` |
| `2d-vto/earring` | `{"src_file_id":S,"ref_file_ids":[R],"source_info":{"name":S},"object_infos":[{"name":R}]}` |
| `2d-vto/necklace` | as earring, plus `"parameter":{"necklace_anchor_point":[[275,120],[925,145]],"necklace_wearing_location":[[352,785],[558,785]]}` in the object |
| `cloth-v4` | `{"src_file_id":…,"ref_file_id":…,"garment_category":"upper_body"}` |
| `hair-transfer` (v2.1) | `{"src_file_id":…,"template_id":"female_casual_updo","hair_color":"src"}` |
| `beard-style` | `{"src_file_id":…,"template_id":"all_circle"}` |
| `face-attr-analysis` | `{"src_file_id":…,"features":["faceShape"]}` |

- **Makeup must zero `skin_smooth`, not leave it out.** The makeup page says an omitted `skin_smooth` applies a default of 50. Recorded both ways on V04: with it zeroed the pores stay; without it the skin is visibly smoothed and lightened (`lip-red-default-smooth-V04.jpg`). docs/occasions.md's "must not use `skin_smooth`" therefore means sending it at 0. No `morphology`, and the lip shape is `original`.
- **`garment_category` enum** (the cloth page's OpenAPI bundle): `full_body`, `lower_body`, `upper_body`, `shoes`, `auto`, `outer`. A top is `upper_body`.
- **`hair_color` defaults to `ref`**, the template's colour. Hair colour is never rendered (docs/occasions.md), so send `src`.
- **2D VTO** needs the ids twice: in `src_file_id`/`ref_file_ids` and again as `source_info.name`/`object_infos[].name`.

## Templates

`GET /s2s/v2.1/task/template/hair-transfer?page_size=20` (six pages, 116 templates) and `GET /s2s/v2.0/task/template/beard-style?page_size=20` (one page, 15). Each has `id`, `thumb`, `title`, `category_name` and, for hair, `keep_users_color`. Full lists are in `http-templates-*`. Listing spends nothing.

- **Hair, tied back:** no title says "tied back". The candidates are `female_casual_updo` (Casual Updo: a high bun, no fringe, all hair off the face), `all_high_pony_tail` (High Pony Tail: tied, but long strands fall down the front), `female_messy_bun` and `female_messy_bun_brown` (both with a fringe), and `all_dark_kitty_bun`. Both of the first two were run. Casual Updo is the one that meets "tied and off the eyes"; the look vocabulary would likely read it as `bun`, not `tied-back`.
- **Beard, short and even:** no template is a short boxed or even full-cheek beard. The fifteen are Shaved, Anchor, Circle, Garibaldi, Mustache, Soul Patch, Long Bandholz, Ducktail, Bandholz, Chin Curtain, Walrus, Mutton Chops, French Fork, Goatee, Horse Shoe. `all_circle` (moustache joined to a chin beard, cheeks clear) is the shortest even full shape; the vocabulary would read it as `goatee`, not `short`. `all_garibaldi` and `all_bandholz` are the full beards for `festival-full-beard`.

## Units

Prices come from each task's doc page and agree with `GET /s2s/v2.0/credit/feature-cost` (six pages, no units). Measured is the balance from `GET /s2s/v1.0/client/credit` read immediately before and after each task; the ledger at `GET /s2s/v1.0/client/credit/history` shows the same charges, one entry per task, named `api_<task>`.

| Task | Documented | Measured per call |
|---|---|---|
| `ai-smile` | 1 | 1 |
| `makeup-vto` | 1 | 1 |
| `2d-vto/earring` (single) | 1 (stacked: 2) | 1 |
| `2d-vto/necklace` | 1 | 1 |
| `cloth-v4` | 2 | 2 |
| `hair-transfer` v2.1, preset | 2 | 2 |
| `beard-style` | 2 | 2 |
| `face-attr-analysis`, 1 to 5 features | 10 (6–14: 20; 15–28: 30) | 10 |
| A task that ends in `error` | 0 | 0 net |
| A `400` at create | 0 | 0 |
| Slot, PUT, poll, fetch, templates, feature cost, credit | 0 | 0 |

- **The v1 credit endpoint takes the v2 key.** `GET /s2s/v1.0/client/credit` with the same Bearer key answers the balance per pool: `{"status":200,"results":[{"id":…,"type":"ApiPaygToken","amount":1000,"amount_dec":1000.0,"expiry":…},{"…":"…","amount":40,…}]}`. That is a balance endpoint, and Keel `cost` can settle against it.
- **A failed task is charged, then refunded.** The ledger shows `api_ai_smile` at −1 when the task is created and `api_ai_smile_failed` at +1 a second later. The debit came from the 1,000-unit pool and the refund went to the 40-unit trial pool, so failures move units between pools with different expiries.
- **Both pools carry an `expiry`.** docs/keys.md says the units do not expire; the API says each pool does.

T0.5 spent 24 units: 1,040 to 1,016.

## Errors

| Case | How it was made | Where it fails | Reply | Units | Typed kind |
|---|---|---|---|---|---|
| No face | the jhumka product photo as the `ai-smile` source | poll | `error_no_face`, "No face was detected in the source image." | 0 | `no face` |
| Bad file | 58 bytes of text uploaded as `image/jpeg` | poll | `error_decode_image`, "The source image file could not be read." | 0 | `bad file` |
| Too small | V04 scaled to 64×86, `makeup-vto` | poll | `error_below_min_image_size`, "The input image is smaller than the minimum size (320x320px)." | 0 | `bad file` |
| File not in storage | a slot taken, nothing PUT | poll, at once | `error_download_image`, "The source image could not be downloaded from the provided URL or file ID." | 0 | `expired` |
| Unknown file id | `src_file_id` that was never issued | create | `400 InvalidParameters`, "One or more parameters in this request are invalid." | 0 | none |
| Bad parameter | `expression_type: "grin"` | create | `400 InvalidParameters`, "expression_type is not one of the accepted values." | 0 | none |
| Unknown task | `POST /s2s/v2.0/task/no-such-task` | create | `404 NotFound`, "This endpoint doesn't exist." | 0 | none |
| Unknown or expired task id | `GET /s2s/v2.0/task/ai-smile/not-a-real-task-id` | poll | `400 InvalidTaskId`, "This task ID doesn't exist, or the task has expired." | 0 | none |
| Wrong key | a placeholder Bearer value | any | `401 InvalidApiKey`, "This API key isn't recognized." | 0 | none |

- **An expired file was not observed.** Files live 30 days. The closest reachable case is a file id whose bytes are not in storage, which is what an expired file should look like to the engine; it is recorded as `expired`. An expired task id answers the same `InvalidTaskId` as an unknown one.
- **Out of units** (`CreditInsufficiency`, per the docs) was not provoked.
- The makeup page says the minimum is 100 px; the engine said 320×320.
- Size ceilings were looser than documented. Hair (documented long side ≤ 1024) and beard (< 1024) accepted the 896×1200 sample; beard returned 764×1024, every other task returned the input size. Makeup (documented height < 1080) accepted 1200.

## Timings

Polled every 2 s after a 0.9 s request, so the step is about 2.9 s. "Done" is the first poll that saw `success` or `error`, counted from the create call's response.

| Task | Done at |
|---|---|
| `2d-vto/earring`, `2d-vto/necklace` (engine placement), `beard-style` | 3.7 s |
| `ai-smile`, `makeup-vto`, `2d-vto/necklace` (placed), `hair-transfer` updo | 6.6 s |
| `makeup-vto` (default smoothing) | 9.4 s |
| `cloth-v4`, `face-attr-analysis`, `hair-transfer` ponytail | 12.3–12.6 s |
| Engine errors | 3.7 s; a file not in storage, 0.9 s |

A full look (slot, PUT, create, polls, fetch) is about 5 s plus the task time. The PUT took 0.3–1.4 s and the fetch 0.8–1.7 s.

## Result links and rate limit

- **Result links:** the URL is signed for `X-Amz-Expires=7199` (two hours). The smile result link fetched `200` at 14.8 minutes and `403 AccessDenied`, "Request has expired", at 239.7 minutes (`http-err-expired-result-link`). The exact cut-off between those was not measured; the signature says two hours. Polling the finished task again (free) returned a freshly signed link to the same 302,007-byte picture, so a lapsed link can be recovered while the task id lives. The object behind it is kept 30 days (`x-amz-expiration … TTL_30`). Copy the bytes at once and never store the link. The live client should map that `403` to `expired`, then poll once for a fresh link.
- **Rate limit:** no response, success or error, carried a rate-limit header (`X-RateLimit-*`, `Retry-After` or similar). The documented limit is 250 requests per 300 s per IP and per key, refused with `429`. One look is about seven calls, so a judge session is nowhere near it; the limit was not provoked.

## Face shape only

`face-attr-analysis` was asked for `["faceShape"]` only (10 units). The response still has every group, empty:

```json
{"nose":{},"agegender":{},"eyelid":{},"eyebrow":{},"color":{},
 "face_quality":{"has_face":true,"area":"good","frontal":"good","lighting":"good","faceangle":"good"},
 "cheekbone":{},"faceshape":"Oval","facialratio":{}}
```

**Only `faceshape` may be read.** `agegender` (age and gender) and `facialratio` (ratios) are never requested, read, stored or sent on, even when a request would fill them. Ask for `faceShape` alone, which also keeps the price at the 1–5 feature tier.

## The renders, looked at

Each result was compared with its input at full size and in a close crop of the face. These are the render-check inputs.

| Result | Change made? | Same person? | Skin smoothed? | Face reshaped? | Notes |
|---|---|---|---|---|---|
| `smile-P12.jpg` | yes, closed-mouth smile, eyes narrowed with it | yes | **yes**: pores softened, tone evened and warmed across the face | the cheeks lift with the smile, nothing else | The whole face is regenerated. Expect the render check to discard it as smoothed. |
| `lip-red-V04.jpg` | yes, even red within the lip line | yes | no, pores kept | no | The one to keep. `skin_smooth` at 0. |
| `lip-red-default-smooth-V04.jpg` | yes | yes | **yes**: pores gone, skin lighter, under-eye shadow lifted | no | A known bad render: what an omitted `skin_smooth` does. |
| `earring-jhumka-V04.jpg` | yes, a jhumka on each ear (the second is mirrored) | yes | no, face untouched | no | Flat, slightly pasted lighting; hangs a little off the lobe. Exact product composited. |
| `necklace-chain-V14-auto.jpg` | wrong: a long thin line over the hair and shirt, ends off the neck | yes | no | no | A known bad render: the engine misplaced the flat product shot on a dark photo with hair over the neck. |
| `necklace-chain-V14.jpg` | yes, a chain from the neck to mid-chest, the left side behind the hair | yes | no | no | Needed explicit anchor and wearing points. A stray gold fleck (the clasp) floats on the neck. |
| `cloth-kurta-V08.jpg` | yes, maroon kurta with mandarin collar and zari placket | yes | no; the face is **regenerated with harsher, over-sharpened texture** | no | Reads brighter red than the product. See the kurta verdict. |
| `hair-updo-V06.jpg` | yes, high bun, hair off the eyes | yes | **yes**, clearly | possibly narrower cheeks; hair covered the jaw in the input, so not certain | **The jhumkas were removed.** |
| `hair-ponytail-V06.jpg` | yes, tied high, but long loose waves down both sides | yes | **yes**, less than the updo | no | Jhumkas kept. Reads as loose as much as tied. |
| `beard-circle-V08.jpg` | yes, moustache and chin beard, patchy cheeks cleared | yes | slightly softer, bumps kept | no | Output downscaled to 764×1024. Reads as a goatee. |

**The kurta on an adult:** in a chest-up selfie it renders fine. It is not visibly short or tight, because the frame stops at the chest and its length never shows; the collar and shoulders sit naturally on a grown man. Its colour reads brighter than the maroon product shot.

## Surprises

- The v1 credit endpoint answers to the v2 key, so there is a balance endpoint after all.
- Failed tasks are charged and refunded, across pools.
- Uploads and results go to a US bucket from the India API host.
- `skin_smooth` defaults to 50 when omitted.
- Hair transfer and AI Smile regenerate and smooth the face; hair transfer also removed earrings. The cloth task regenerated the face with harsher texture.
- PNG results arrive as `binary/octet-stream` under `.jpg` names.
- Documented size limits were looser than the engine, and the minimum was stricter (320 px, not 100).
- No rate-limit headers.
