# API

The phone app and the server share this contract. Neither side is built here. The product is in [`product.md`](../product.md). Where this file and the product disagree, the product wins.

Every path the app calls is listed below. One example response for each path is in [`testdata/api/`](../testdata/api/). A second file for the same path is an alternate of that path, named in the section, not a second path. Each fixture names the method and the path. `status` and `headers` are the response around the body. `body` is the response body. An error file has no path, because the same code can answer more than one path.

The shape is normative: field names, value kinds, stage names, and error codes. The ids in the fixtures are a single family-function story the app can serve as a mock. A live server mints its own ids.

## Common

The app is served by the same binary as the API. Requests are same-origin and send cookies. There is no bearer token on the phone, and no Google sign-in.

A first request with no session cookie creates a guest user and a server-side session. The cookie is `identity_session`. It is `HttpOnly`, `Secure`, and `SameSite=Lax`. Its value is the signed session id and nothing else. No response body carries the session id, the cookie value, or an email address.

Public ids that the server mints are 32 lowercase hex characters, Keel `id`. Occasion ids, criterion ids, fix ids, catalog product ids, and sample ids are the slugs already in `testdata/occasions/`.

JSON responses use `Content-Type: application/json` and `Cache-Control: no-store`. Keel writes one JSON value and a newline. The fixtures are pretty-printed so they can be read; a parser must accept either form.

Request and response bodies are JSON objects unless a section says otherwise. On Dresscode's own routes, a request field the section does not name is `invalid_request`. Upload, sign-in, and account deletion are Keel's handlers, and they follow Keel's decoding. Keel ignores a JSON field it does not decode. It does not reject that field. A sign-in `choice` other than empty or `switch` is `invalid_request`. Account deletion answers `not_found` when this user does not hold the address. A guest does not hold it.

### Nothing that judges

No response, and no event, carries a probability, a score, a percentage, an age, a gender, a face ratio, or a skin analysis score. The Clef wording of a criterion (`clef`) stays on the server. The phone receives the suggestion.

The fix picker may use face shape, hair type, hair length, and colour tones. Those values are not on this API. The phone does not receive the allowlist.

Upload byte counts, erase progress counts, and a digest are not judgements. The app does not show them as a score.

### Errors

A failed JSON request is Keel's `wire` envelope and a newline:

```json
{"error":{"code":"budget_refused","message":"budget refused"}}
```

`code` is the stable snake_case identifier. The app branches on it. `message` is a sentence or a short phrase the app may show and must not branch on. `detail` is optional. `Cache-Control` is `no-store`. A `429` also sets `Retry-After` to whole seconds, at least 1, from `detail.retry_after_seconds` when that member is present.

The phrases the product names, and the codes the app branches on:

| Code | Status | Message in the fixture |
|---|---|---|
| `budget_refused` | 409 | budget refused |
| `no_face_in_photo` | 422 | no face in photo |
| `uploads_paused` | 403 | uploads paused |

The rest of the codes the app can receive:

| Code | Status | When |
|---|---|---|
| `lease_refused` | 409 | The judge session cap refuses another fresh selfie or another render. `detail.cap` is `fresh_selfies` or `renders`. Sample opens do not use this code. |
| `samples_only` | 403 | The samples-only switch is on. A fresh selfie, a fresh product, a fresh link, or a fresh look is refused. Opening a sample is not. |
| `renders_paused` | none on its own | The renders switch is on, or a render cannot be spent. It is a render-stage detail, not a failed look. The suggestion still stands. The fixture message is `renders paused`. |
| `too_small` | 422 | The photo cannot be judged because it is too small. The bound is the server's, not a number in this contract. |
| `upload_a_photo` | 422 | A product link could not be used (the retailer is blocked, or the page yielded no image). The app asks for a photo instead. |
| `invalid_request` | 400 | Malformed body, unknown field on a Dresscode route, or a value this path does not honour. `detail.field` and `detail.reason` name the field when there is one. |
| `invalid_code` | 401 | The sign-in or deletion code is unknown, from another session, expired, used, or past its attempts. One code covers every case. |
| `guest_data_conflict` | 409 | Signing in would strand this device's looks. Nothing changes. |
| `send_limited` | 429 | Too many code requests. Known and unknown addresses share it. |
| `not_found` | 404 | The caller may not see that resource. A missing id and an id owned by someone else are the same answer. |
| `rate_limited` | 429 | The spending gate on a fresh look. A refusal takes no token. |
| `unsupported_format` | 422 | `photo` could not decode the bytes. |
| `heic` | 422 | The bytes are HEIC. This product does not convert HEIC. |
| `too_many_pixels` | 422 | `photo` refused the pixel count. |
| `too_many_input_bytes` | 422 | `photo` refused the input size. |
| `output_cannot_fit` | 422 | `photo` could not fit the image under its byte cap. |
| `chunk_mismatch` | 409 | A chunk's bytes do not match `X-Chunk-SHA256`. |
| `hash_mismatch` | 409 | The assembled upload does not match the declared SHA-256. |
| `incomplete` | 409 | Complete was called with chunks missing. |
| `limit_exceeded` | 413 | The upload or the owner is over the byte cap. `detail.limit` is `upload` or `owner`. |
| `unsupported_type` | 415 | The content type cannot be stored. |
| `conflict` | 409 | That upload id already names stored bytes. |
| `storage_error` | 500 | The upload could not be advanced. |
| `fetch_blocked_address` | 422 | The link resolved to an address the fetch policy refuses. The message does not echo the address. |
| `fetch_bad_scheme` | 422 | The link is not https. |
| `fetch_bad_url` | 422 | The link does not parse. |
| `fetch_bad_port` | 422 | The port is not allowed. |
| `fetch_too_many_redirects` | 422 | The link redirected too many times. |
| `fetch_too_large` | 422 | The page or image was over the cap. |
| `fetch_bad_type` | 422 | The content type was not allowed. |
| `fetch_timeout` | 422 | The fetch timed out. |
| `fetch_bad_status` | 422 | The final response was not successful. |
| `fetch_unreachable` | 422 | The name did not resolve, or the connection failed. |
| `job_failed` | 200 on the job | A job failed and named no more specific code. The stream carries it. The state read carries it. The look request itself already returned. |
| `internal_error` | 500 | A dependency failed. |

Each code has a file in `testdata/api/errors/`. The file name is the code with each underscore written as a hyphen. Those files are the envelope the app uses for the message. They are not extra paths.

A private media miss is not this envelope. See Media.

## Flows

| Product | Paths |
|---|---|
| A visitor is a guest and can do everything | `GET /api/session` |
| Sign in by emailed code, and keep this device's looks | `POST /api/session/code`, `POST /api/session/code/verify` |
| An address that already has an account | verify returns `guest_data_conflict`; the app repeats with `choice` `switch`. The guest's looks stay on the guest |
| No Google sign-in | no such path |
| Pick an occasion and read its dress code | `GET /api/occasions`, `GET /api/occasions/{id}` |
| Tick the fixes you are open to. Nothing infers gender | `GET` and `PUT /api/occasions/{id}/opt-in` |
| One selfie, resumable | `POST /api/uploads` and the chunk paths, then `POST /api/photos` |
| A product by photo | the same upload, then `POST /api/products` |
| A product by link. A blocked retailer or a page with no image asks for a photo | `POST /api/products/link` |
| A shared link or a shared image | no share path. The app reads the share on the device, then calls the link path or the upload path |
| Start a look | `POST /api/looks` |
| Check the dress code, pick a fix, measure only if needed, render, check the render, shop | `GET /api/jobs/{id}/events` |
| The verdict: missed criteria as suggestions, the fix, and why. No score | `GET /api/looks/{id}` |
| The original and the render | `GET /api/looks/{id}/renders`, `GET /media/{id}` |
| A render that fails the check is never shown. The runner-up runs once. Then the suggestion stands alone | the `render` and `check` stages, then `look-suggestion.json` |
| Shop the look, each product labelled | `GET /api/looks/{id}/shop` |
| Keep, dismiss, bought | `PUT /api/looks/{id}/verdict` |
| History off until the user turns it on. What is kept, in words | `GET` and `PUT /api/history` |
| The second visit: recall for the occasion, and past looks | `GET /api/history/recall`, `GET /api/looks` |
| Remember only after a verdict, and only when history is on | the verdict response, not a stage |
| Delete a photo, a look, history, or the account | the four deletion paths, then the job stream |
| Account deletion needs a fresh code and reports when every part has confirmed | `POST /api/account/deletion`, then the returned job: stage `delete`, then `done` |
| Judges open a sample and spend nothing | `GET /api/samples`, `POST /api/samples/{id}/looks` |
| Budget, lease, upload pause, render pause, samples only, no face, too small | the error codes above |

## Session

`GET /api/session`

The boot call. It also mints the guest when the cookie is absent, and the response sets the cookie.

```json
{"user_id":"a11ce0000000000000000000000000a1","kind":"guest"}
```

`kind` is `guest` or `owner`. Fixture: `session.json`.

## Sign-in

`POST /api/session/code`

```json
{"address":"person@example.com"}
```

`202` and `{"ok":true}` whether or not the address already has an account. The same answer is `send_limited` for a known address and an unknown one. Fixture: `session-code.json`.

`POST /api/session/code/verify`

```json
{"address":"person@example.com","code":"000000","choice":""}
```

`choice` is omitted, empty, or `switch`. Any other value, including `merge`, is `invalid_request`. Keel v0.9.0 `VerifySignInCode` accepts only an empty choice or `switch`.

`200` and `{"ok":true}`. The response sets a new session cookie. The app then reads `GET /api/session` and sees `owner`.

A new address upgrades this guest in place. The user id does not change, so the looks stay. That is how signing in keeps this device's looks.

An address that already has an account, when this guest has no looks, joins that account. The device then uses the account's session.

An address that already has an account, when this guest has looks, returns `409` `guest_data_conflict` and changes nothing. The code stays live. The app repeats the same code with `"choice":"switch"`.

`switch` moves the device onto that account. The guest's rows, including its looks, stay on the guest. The account's looks are not overwritten, and the guest's looks are not attached to the account. Mounting Keel's verify handler cannot accept `merge` or copy those rows. After `switch`, `GET /api/session` reports the account's user id.

The code in the fixtures is `000000` so the file is obviously not a live code. A live code is six digits. One code is single use. Request a new code for a deletion; the verify code is already spent. A conflict does not spend the code. The `switch` repeat uses the same code.

Fixtures: `session-verify.json` (empty choice), `session-verify-switch.json` (the `switch` repeat).

## Occasions

`GET /api/occasions`

The five occasions, in product order: `date`, `family-function`, `interview`, `festival`, `funeral`. Each item is `id` and `name` only. Fixture: `occasions.json`.

`GET /api/occasions/{id}`

The dress code and the fix menu for one occasion. Criteria are `id`, `kind`, and `suggestion`. `kind` is `clothing`, `grooming`, `lighting`, or `expression`. Fixes are `id`, `changes`, and `criteria` (the criterion ids that fix addresses). The YouCam task, the analysis, the vocabulary, and the Clef question are not on this response.

An unknown id is `not_found`. Fixture: `occasion.json` (family function).

## Opt-in

`GET /api/occasions/{id}/opt-in`

```json
{"occasion":"family-function","ticked":[]}
```

No row means not ticked. A new guest has an empty list. The menu does not fill itself in. Fixture: `opt-in.json`.

`PUT /api/occasions/{id}/opt-in` answers `200`.

```json
{"ticked":["family-gold-jhumkas","family-red-lip","family-festive-top"]}
```

Replaces the set for that occasion. An empty list is valid. An id that is not a fix on that occasion is `invalid_request`. The response is the stored set. Fixture: `opt-in-put.json`.

## Upload

Resumable upload is Keel's protocol, mounted at `/api/uploads`. Chaaya's uploader speaks this without a Dresscode-specific client.

| Method and path | Status | Fixture |
|---|---|---|
| `POST /api/uploads` | 201 | `upload-begin.json` |
| `PUT /api/uploads/{id}/chunks/{n}` | 200 | `upload-chunk.json` |
| `GET /api/uploads/{id}` | 200 | `upload-state.json` |
| `POST /api/uploads/{id}/complete` | 201 | `upload-complete.json` |

Open:

```json
{"owner":"ignored","content_type":"image/jpeg","visibility":"private","chunk_size":65536,"group":"selfie"}
```

The server sets `owner` to the session user and ignores the body's owner. `visibility` is stored `private`. `group` is `selfie` or `product`. Omitted means `selfie`.

`fresh_uploads_off` refuses a selfie open with `uploads_paused`. A `product` open is still accepted. `samples_only` refuses both.

`size_bytes` may be omitted. Chaaya omits it, and the begin fixture omits it. The state then has no `size_bytes` and no `chunk_count`. `received` and `missing` are arrays. `missing` is empty until a hole exists below the chunks already seen.

A chunk is raw bytes. The header `X-Chunk-SHA256` is the lowercase hex SHA-256 of those bytes. A chunk may be repeated with the same digest. A different digest is `chunk_mismatch`.

Complete:

```json
{"sha256":"d924940046edbe3fbbcf957afe1f564576f6dd481b6911e36e44e0135f2585a3"}
```

The response id is the stored blob. The digest in the fixtures is an example, not a photograph.

`expires_at` is RFC3339 UTC. The instant in the fixtures is only an example of that field.

## Photos

`POST /api/photos` answers `201`.

```json
{"upload_id":"5e1f1e0000000000000000000000000a"}
```

The upload must already be complete, and it must be this user's. The server normalises with Keel `photo`: orientation applied, metadata stripped, resized under YouCam's long side and Clef's byte cap. It stores the normalised JPEG, drops the raw blob, and returns the normalised id. Later paths use that id, not the upload id.

```json
{"media_id":"5e1f1e00000000000000000000000001","url":"/media/5e1f1e00000000000000000000000001","content_type":"image/jpeg","sha256":"d924940046edbe3fbbcf957afe1f564576f6dd481b6911e36e44e0135f2585a3"}
```

`sha256` is the normalised bytes. It is the analysis cache key. The app does not display it.

Refusals from this path: `uploads_paused`, `samples_only`, `no_face_in_photo`, `too_small`, `heic`, `unsupported_format`, `too_many_pixels`, `too_many_input_bytes`, `output_cannot_fit`, `not_found`.

`no_face_in_photo` is returned here when the server can already see that the photo has no usable face, including a cached miss. If the server cannot know yet, the look job fails with the same code instead of showing a verdict.

Fixture: `photo.json`.

## Products

`POST /api/products` answers `201`.

A product photograph. Same pause rules as a product upload (`samples_only` refuses it; `uploads_paused` does not, unless `samples_only` is also on).

```json
{"upload_id":"b20d0c7000000000000000000000000a","name":"Gold jhumkas","category":"earrings"}
```

`name` and `category` are non-empty text. The server normalises the same way as a selfie. At intake the fidelity is `exact product`, because the bytes are the item the user brought. Shop may show a different label later. The label the screen reads is the shop response, not this one.

`origin` is `user`. Fixture: `product-photo.json`.

`POST /api/products/link` answers `201`.

```json
{"url":"https://example.com/gold-jhumkas"}
```

The server fetches the page with Keel `fetch` and stores the image preview. A private address is `fetch_blocked_address`. A blocked retailer, or a page with no usable image, is `upload_a_photo`. Other fetch failures use the `fetch_*` codes. `samples_only` refuses the path.

At intake the fidelity is `approximate match`, because the picture is whatever the page offered. `origin` is `user`. Fixture: `product-link.json`.

A user product:

| Field | Kind |
|---|---|
| `id` | 32 hex |
| `category` | string |
| `name` | string |
| `fidelity` | `exact product`, `approximate match`, or `visual simulation` |
| `origin` | `user` or `catalog` |
| `media_id` | 32 hex, omitted when there is no picture |
| `url` | `/media/{media_id}`, omitted with `media_id` |

## Looks

`POST /api/looks`

```json
{"occasion":"family-function","selfie_id":"5e1f1e00000000000000000000000001","product_ids":[]}
```

`product_ids` may be omitted. Each id is a user product or a catalog id. `occasion` is one of the five slugs. The selfie must be this user's normalised photo.

`202`:

```json
{"look_id":"100c0000000000000000000000000001","job_id":"10b00000000000000000000000000001"}
```

The spending gate is on this path (`rate_limited`). The judge lease and the budgets are checked before any paid call. A lease miss is `lease_refused`. A budget that cannot reserve the look is `budget_refused`. Nothing is spent on that refusal. `samples_only` and `uploads_paused` also refuse a fresh look (`samples_only` wins when both are on). `renders_paused` does not refuse the start.

The response does not wait for the pipeline. Fixture: `look-start.json`.

`GET /api/looks?occasion=family-function`

Past looks for one occasion, newest first. This list is the user's looks, not style memory. It is returned whether or not history is on. Each item is `id`, `occasion`, `verdict`, `ready`, `fix` (a fix id, or empty), and `words` (empty until the check has a line). No recall sentence here. Fixture: `looks.json`.

### Job state

`GET /api/jobs/{id}`

Read this after the event stream has opened, and again after a reconnect. Chaaya deduplicates the terminal event on `job_id`. The JSON is snake_case. The app maps `job_id` onto the job client's `jobId`.

| Field | Kind |
|---|---|
| `job_id` | the id that was returned at the start |
| `root_id` | the first attempt. Equal to `job_id` until a resume replaces an attempt |
| `status` | `queued`, `running`, `done`, `error`, `cancelled`, `interrupted` |
| `stage` | the latest stage, omitted before the first one |
| `next_id` | the replacement attempt, omitted when there is none |

A failed job adds `error` with `code`, `message`, and optional `detail`. That object is the envelope's `error` member, not the envelope wrapped again.

The id returned at the start is the one the app holds. A resume may publish `interrupted` on the attempt that died and continue under `next_id`. The app follows `next_id` when the state names one. A deletion or a look that the server is still resuming reports `running` or `queued` on the id the app holds once the replacement exists.

Fixtures: `job.json` (running), `job-done.json`, `job-error.json`.

### Event stream

`GET /api/jobs/{id}/events`

Server-sent events, Keel's frames. `Content-Type: text/event-stream`, `Cache-Control: no-store`, `X-Accel-Buffering: no`.

A frame is `event`, `id`, one `data` line of compact JSON, and a blank line. Ids increase. A reconnect sends `Last-Event-ID` when the last id was not 0. A comment frame is a heartbeat (`: ping`) and is ignored. The app subscribes first, then reads the job state.

Event names are `progress`, `done`, `error`, `cancelled`, and `interrupted`. `done`, `cancelled`, and `interrupted` are:

```json
{"job_id":"<id>","status":"<same as the event name>"}
```

`error` carries the wire envelope a second time, because that is what Keel writes:

```json
{"job_id":"<id>","status":"error","error":{"error":{"code":"no_face_in_photo","message":"no face in photo"}}}
```

The state read is not double-wrapped. The stream is. The app's Keel parser accepts the stream form.

`progress`:

```json
{"job_id":"<id>","stage":"judge","detail":{}}
```

`current` and `total` are omitted on a look, and on the account-deletion job. They are present on a photo, look, or history deletion. The app maps the stage to a screen word and does not show a fraction. An account deletion's stage is `delete`. Its `detail` is Keel's deletion snapshot, not a look detail. See Deletion.

| Stage | Screen word | `detail` |
|---|---|---|
| `judge` | Checking | `flags`: `{id, suggestion}` for each missed criterion, in order. |
| `pick` | Choosing a fix | `fix` and `runner_up`, fix ids. `runner_up` is empty when there is none. `recall` is the memory sentence, and it is omitted when history is off. |
| `measure` | Choosing a fix | `{"skipped":true}` when the fix names no analysis. Otherwise `{"cached":true}` or `{"cached":false}`. No analysis values. |
| `render` | Rendering | `fix`, and `attempt` `fix` or `runner_up`. A refusal adds `code` and `message` (`budget_refused` or `renders_paused`) and no media id. |
| `check` | Checking the render | `fix`, and `shown`. When `shown` is true, also `media_id` and `words`. When `shown` is false, no media id. |
| `shop` | none | `products`, the same array as shop the look. |

The order is judge, pick, measure, render, check, and, when the first check is not shown, render and check once more for the runner-up. Shop is last. `done` follows shop. A discarded render is never given a media id, so the phone cannot show it.

`no_face_in_photo` ends the job with `error` during `judge` when the photo path did not already refuse it. There is no verdict.

A budget refusal or `renders_paused` on `render` does not fail the job. The suggestion stands, shop still runs, and `done` still arrives.

Fixtures: `job-events.json` (the family-function render that is shown), `job-events-suggestion.json` (both checks discarded), `job-events-error.json` (`no_face_in_photo`), `job-events-erase.json` (account deletion, stage `delete`).

Screen words the progress view uses, and no others: Checking, Choosing a fix, Rendering, Checking the render.

## Verdict

`GET /api/looks/{id}`

While the job is unfinished, `ready` is false and the body is the ids only. Fixture: `look-pending.json`.

When `ready` is true:

| Field | Kind |
|---|---|
| `id`, `job_id`, `occasion`, `selfie_id` | strings |
| `selfie_url` | `/media/{selfie_id}` |
| `ready` | true |
| `flags` | `{id, kind, suggestion}` for each miss. `kind` matches the occasion. |
| `fix` | `{id, changes, why}`. `why` is the suggestion for the criterion the fix addresses. |
| `runner_up` | a fix id, or empty |
| `shown_render` | the render object, or null |
| `suggestion_only` | true when no render was shown |
| `products` | the shop array |
| `verdict` | empty, `kept`, `dismissed`, or `bought` |
| `words` | the look in words, or empty |

A shown render is `media_id`, `url`, `fix_id`, and `check`. `check` is `change_made`, `same_person`, `criterion_met`, and `attributes` (`{id, value}`). Attributes that are `none` or `can't tell` are left out, and so are they left out of `words`. The app does not display the three booleans. It displays the picture, the suggestions, the fix, and `why`.

`words` in the fixture is `gold jhumkas`.

When the first check fails and the runner-up is shown, `fix` stays the first pick and `shown_render.fix_id` is the runner-up. The screen talks about the render's fix.

Fixtures: `look.json`, `look-suggestion.json`.

## Renders

`GET /api/looks/{id}/renders`

```json
{"selfie_url":"/media/<selfie>","shown":[],"suggestion_only":false}
```

`shown` lists the renders the phone may display, in the order to offer them. A discarded render is absent. The original is `selfie_url`, one tap away, and it is not inside `shown`.

Each shown item is `media_id`, `url`, `fix_id`, `changes`, and `words`.

Fixtures: `renders.json`, `renders-none.json`.

## Shop the look

`GET /api/looks/{id}/shop`

```json
{"products":[{"id":"gold-jhumkas","category":"earrings","name":"Gold jhumkas","fidelity":"exact product","origin":"catalog","media_id":"<id>","url":"/media/<id>"}]}
```

`fidelity` is exactly `exact product`, `approximate match`, or `visual simulation`. Those spellings are the domain labels, not the catalog file's short tokens `exact` and `approximate`.

A template with nothing to photograph (hair, beard, smile) uses the fix id as `id`, `origin` `catalog`, `fidelity` `visual simulation`, and no `media_id`. Kept products rank first when history is on.

The shop stage and this path return the same array.

Fixtures: `shop.json` (exact product), `shop-simulation.json` (visual simulation). The link intake fixture carries `approximate match`.

## Keep, dismiss, bought

`PUT /api/looks/{id}/verdict`

```json
{"verdict":"kept"}
```

`verdict` is `kept`, `dismissed`, or `bought`. One field, one stored value. The path is the same for all three.

The response is the ready verdict, plus:

| Field | Kind |
|---|---|
| `remembered` | true only when history is on and the memory write landed |

The verdict is stored before the memory write. A memory failure still returns `200`, with `remembered` false. History off does not read or write memory, and `remembered` is false.

Fixtures: `verdict.json` (`kept`), `verdict-dismissed.json`, `verdict-bought.json`.

## History

`GET /api/history`

History is off when the user has never set it.

```json
{"enabled":false,"notice":"Style history is off. Turn it on to remember, in words only, the occasion, the fixes you keep or dismiss, and the products. No photo is kept for this. It stays until you delete it."}
```

`notice` is the sentence the screen shows for what is kept and where. It does not name a vendor. When `enabled` is false the app must not add a memory sentence of its own. Fixture: `history.json`.

`PUT /api/history`

```json
{"enabled":true}
```

The response is the same shape as the GET, with `enabled` as stored. Fixture: `history-put.json`.

`GET /api/history/recall?occasion=family-function`

When history is off, the body is `{"enabled":false}` and nothing else. No line, no fix ids. Fixture: `recall-off.json`.

When history is on:

```json
{"enabled":true,"occasion":"family-function","line":"Last family function you kept the gold jhumkas and dismissed the red lip.","kept":["family-gold-jhumkas"],"dismissed":["family-red-lip"]}
```

`line` is the sentence the second visit shows. `kept` and `dismissed` are fix ids. A dismissed fix is not offered first on the next pick. The pick stage's `recall` is this `line`. Fixture: `recall.json`.

An occasion with no memory yet, while history is on, returns `enabled` true, the occasion, an empty `line`, and empty `kept` and `dismissed`.

## Deletion

Each deletion is `202` and `{"job_id":"<id>"}`. The app follows that job. Repeating a deletion whose resource is already gone is `not_found`. The work itself is safe to repeat.

Photo, look, and history deletion are Dresscode jobs. Progress uses stage `erase`. `detail` is `{targets, confirmed}`. `current` is how many targets are confirmed. `total` is how many there are. `done` means every target confirmed.

| Path | Targets | Fixture |
|---|---|---|
| `POST /api/photos/{id}/deletion` | `files`, `looks` | `delete-photo.json` |
| `POST /api/looks/{id}/deletion` | `look`, `files` | `delete-look.json` |
| `POST /api/history/deletion` | `memory` | `delete-history.json` |

`files` is the stored blobs. `looks` is the looks that used that photo. `look` is that look's rows. `memory` is the account's style memory.

Photo deletion removes that blob and the looks that cannot be shown without it. Look deletion removes that look and its render blobs. The selfie blob stays until a photo deletion. History deletion clears style memory and turns the switch off. Looks stay.

Account deletion is Keel's `DeleteHandler`. The body is the address and a fresh code:

```json
{"address":"person@example.com","code":"000000"}
```

The address must already be an identity of this user. A caller who does not hold it, including a guest, receives `not_found`. An unknown account is the same answer. A malformed body, or an empty address or code, is `invalid_request`. A code that is unknown, expired, used, or from another session is `invalid_code`. The check is the holder of the address, not the caller's kind.

The response clears the session cookie, because the deletion revokes every session. The job id in the body is the capability for the stream after that: `GET /api/jobs/{id}` and the event stream for that deletion do not need the cookie. Any other job still needs the session. Fixture: `delete-account.json`. Fixture of the stream: `job-events-erase.json`.

The returned job streams stage `delete`. `detail` is `{version, user, rows_done}`. `version` is `1`. `user` is the user id being deleted. `rows_done` is omitted until the identity rows are gone, and then it is true. `current` and `total` are omitted. This job id never carries stage `erase`.

The per-target ledger is a different, inner erasure job. Keel starts it under kind `erase` and does not return its id. That job's progress has `stage` as an empty string, plus `current`, `total`, and `detail` `{ref, targets, confirmed}`. `ref` is the user id. The targets Dresscode registers there are `files`, `rows`, and `memory`: the stored blobs, the account's remaining rows (including the sign-in rows), and the style memory. The app does not follow that job.

`rows_done` becomes true only after that inner erasure has confirmed every target and the identity rows are gone. `done` on the returned job is the report that every part has confirmed. The app branches on stage `delete` and on that `done`.

## Samples

`GET /api/samples`

The sample looks a judge can open. Items are `id`, `name`, and `occasion`. No image bytes. Fixture: `samples.json`.

| Id | Occasion |
|---|---|
| `sample-date` | `date` |
| `sample-family` | `family-function` |
| `sample-interview` | `interview` |
| `sample-festival` | `festival` |
| `sample-funeral` | `funeral` |

`POST /api/samples/{id}/looks`

`202` and the same body as a fresh look: `look_id` and `job_id`. The look is the cached sample. The job is already `done` (fixture `job-done.json`). Opening it spends no YouCam unit and makes no Workers AI call. It does not count against the judge lease. The spending gate is not on this path.

The verdict is the ready shape. The sample's analysis, judgement, and render are the cache, not a live call.

Fixture: `sample-look.json`.

## Media

`GET /media/{id}`

The bytes of one private blob, with the stored content type. `Cache-Control` is `private, no-store`. Range requests are honoured. The fixture records the headers and does not carry bytes: `media.json`. There is no photograph in `testdata/api/`.

An unknown id, a malformed id, and a blob this session may not read are the same `404`. The body is the plain text `404 page not found` and a newline, not the wire envelope. `Cache-Control` stays `private, no-store`. Fixture: `media-missing.json`.

## Switches

The operator switches are not a phone path. They change what the paths above return.

| Switch | Effect on the next request |
|---|---|
| `fresh_uploads_off` | `uploads_paused` on a selfie upload, on `POST /api/photos`, and on `POST /api/looks` |
| `renders_off` | `renders_paused` on the render stage. The look still finishes with the suggestion |
| `samples_only` | `samples_only` on fresh uploads, photos, products, links, and fresh looks. Samples still open |
