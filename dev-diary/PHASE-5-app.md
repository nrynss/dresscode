# P5: App

```yaml
id:       P5
size:     L
requires: [T1.5]
blocks:   P6
parallel: P3, P4
```

**Goal:** The phone app judges see: a mobile-first installable web app, Svelte 5 + Chaaya 0.5.0, built as a static single-page app so Capacitor can wrap it later. Built against the T1.5 contract and `testdata/api/` until the server lands.

Rules for every task here: no score or probability on screen, ever; reasons worded as suggestions; layouts checked at 375 px; dialogs and sheets from Bits UI. No SvelteKit form actions or `+server.ts` routes.

---

### T5.1: Shell, theme and API client ★
```yaml
requires:   T1.5
fixture-ok: yes
size:       M · mid
owns:       web/src/app/, web/src/lib/api/, web/static/manifest.webmanifest
status:     done
```
The app shell, Dresscode's own look on Chaaya's theme roles (light, dark, system), the installable manifest and icons, and Chaaya's API client with its Keel adapter so every `wire` error arrives typed. A mock mode serving `testdata/api/`.

**Done when:** the app installs on a phone from a local tunnel; every recorded error code renders a human message.

Notes:

- The shell is `web/src/app/`. Dresscode's colours sit on Chaaya's theme roles: warm paper, wine, and gold, in the four blocks the token checker expects (light root, system dark, forced light, forced dark). The appearance control calls Chaaya's `theme.set` with `light`, `dark`, or `system`. The head script is Chaaya's `themeScript`, so a stored mode paints before the first frame.
- The JSON client is Chaaya's Keel client from `@nrynss/chaaya/keel`, with cookies (`credentials: "include"`) and no bearer token. A wire envelope arrives as `ApiError` with the stable code, the detail record, and `Retry-After` when the header names whole seconds. The generic client at `@nrynss/chaaya/api` does not read the envelope. A job state `error` is the envelope's error member, not wrapped again. A stream `error` frame wraps it a second time, and `parseJobEvent` reads that. A private media miss is plain text, so the client reports `http_error` rather than inventing a wire code.
- Mock mode serves every file under `testdata/api`. It is on in dev and in tests. A production build calls the network unless `VITE_DRESSCODE_API=mock`, and the fixture chunk is emitted but not loaded. `VITE_DRESSCODE_API=live` forces the network in dev. An unrecorded `/api/` path answers with the recorded `not_found` envelope. An unrecorded `/media/` path answers with the recorded plain-text miss. The story default is the finished family-function look, and recall with history off. `choice` `switch` and a verdict body select the other recorded file. `preferFixture` pins a file for a path.
- Every code in `testdata/api/errors/` has its own sentence. The screen branches on the code and does not show the wire phrase for those codes. The sentences contain no digits, so a byte count or a wait is not shown as a figure. `renders_paused` and `job_failed` have no HTTP status; they are still parsed as wire envelopes. The proof is `web/src/lib/api/errors.test.ts`.
- The manifest is installable on paper: name, `start_url`, `display: standalone`, a 192 icon and a 512 icon. The mark is a hanger, not a person. Vite serves `web/static/` because its public directory is `static`.
- Boot files outside `owns`, because the shell has to replace the placeholder: `web/index.html`, `web/src/main.ts`, `web/src/vite-env.d.ts`, `web/vite.config.ts`. The placeholder page is gone. The typecheck has no Node types package, so `vite-env.d.ts` declares the `node:` imports the fixture loader and the tests use.
- The phone install from a local tunnel was not done. No tunnel was opened and no phone was used. That clause is unmet.

---

### T5.2: Occasion and opt-in menu ★
```yaml
requires:   T5.1
fixture-ok: yes
size:       S · mid
owns:       web/src/routes/occasion/
status:     done
```
Pick an occasion; tick the fixes you're open to (remembered per occasion). Nothing asks or infers gender.

**Done when:** the menu saves and reloads per occasion.

Notes:

- The screen is `web/src/routes/occasion/`. It lists the five occasions from `GET /api/occasions`, then loads `GET /api/occasions/{id}` and `GET /api/occasions/{id}/opt-in`. Save sends `PUT` with `{"ticked":[...]}`. A well-formed 200 means that set replaced the occasion's ticks, so this session keeps the set it sent, keyed by occasion id. Beard, lip, and jewellery are one list. Nothing is ticked until a stored set says so. Nothing on the screen asks who the fixes are for.
- The recorded opt-in read is the empty first visit, and the recorded write is one example (three family-function ticks). Neither is applied over a choice this session already saved, or a different tick list would snap back to that example. A later read of an occasion already saved here does not ask again, so switching away and back reloads that occasion's set. A full reload asks the API again. The proof is `web/src/routes/occasion/menu.test.ts`: family function saves a set the recording does not return, date saves another, festival saves an empty list, and a reload in another order makes no further request. A missed write does not replace another occasion.
- Only family function has a recorded menu. The other four names are on the list; opening one answers the recorded `not_found` until those responses exist. The date and festival writes in the test are pinned on the mock so a second occasion can answer 200. Their pinned bodies differ from the sets that reload.
- The shell had nowhere to go. `web/src/app/app.svelte` is the smallest change: a button sets `#/occasion`, and that hash swaps the home placeholder for this screen. There is no router in Chaaya.

---

### T5.3: Bring the look ★
```yaml
requires:   T5.1
fixture-ok: yes
size:       M · mid
owns:       web/src/routes/capture/
status:     done
```
One selfie through Chaaya `capture` (front camera, frame grab, resize before upload), with `<input type="file" accept="image/*" capture="user">` as the fallback; products by photo, camera or link; Chaaya resumable upload with progress. Clear refusals: no face, too small, uploads paused.

**Done when:** a selfie from a phone's camera and one from the gallery both upload; a dropped connection resumes.

Notes:

- The screen is `web/src/routes/capture/`. The shell link "Bring the look" uses the hash `#/capture`, and the shell renders that route. A static page has no server path for it.
- A selfie opens Chaaya `CameraSession` on the front camera, grabs a frame through `sessionBackend`, and runs `prepareImage` before upload. The long side is 4096, Keel's default, and the byte ceiling is 4 MiB, Clef's image cap. The gallery input is Chaaya `fileBackend`. The fallback input is `accept="image/*"` and `capture="user"`.
- A product is a gallery file, the rear camera, or `POST /api/products/link`. A photo sends `name` and `category` to `POST /api/products`.
- The upload is Chaaya `ChunkUploader` on `/api/uploads`, private, one 65536-byte chunk size, owner `ignored`. The bar copies stored bytes against the bytes handed over. No count is written on screen. A later visit calls `resume` and can finish what a drop left in the store.
- `no_face_in_photo`, `too_small`, and `uploads_paused` go through `ErrorNotice`, which uses `humanMessage`. A link that answers `upload_a_photo` uses the same path.
- Tests against the mock: a gallery file of one chunk completes begin, the chunk, complete, and `POST /api/photos`. A chunk write that throws as a dropped connection fails that uploader; `resume` then sends the missing chunk and complete succeeds. The phone's physical camera was not used. That clause is unmet.
- Chaaya 0.5.0 `beginBody` has no `group`. Omitted means selfie, so a product photo opens as a selfie and `uploads_paused` refuses it. Not worked around. The issue text is in the handoff.
- The recorded complete fixture is one 65536-byte chunk, and the gallery test matches it. The mock has no second chunk, so a larger file in mock mode does not complete. A live server returns the real size.

---

### T5.4: Progress and the verdict ★
```yaml
requires:   T5.3
fixture-ok: yes
size:       M · mid
owns:       web/src/routes/look/verdict/
status:     claimed:grok-4.7
```
Chaaya's job stream through the stages (checking → choosing a fix → rendering → checking the render), paused while the tab is hidden and resumed on return. The verdict: the criteria the look misses as reasons, the suggested fix, and why.

**Done when:** a look survives backgrounding the app mid-render; no number appears anywhere on screen.

---

### T5.5: The render and shop the look ★
```yaml
requires:   T5.4
fixture-ok: yes
size:       M · mid
owns:       web/src/routes/look/render/
status:     not-started
```
Before and after as a swipe, the original one tap away; shop the look as a bottom sheet with every product's label shown plainly; keep, dismiss, bought. When no render passed the check, the suggestion and products stand without one, and say so.

**Done when:** each label reads clearly at 375 px; keep / dismiss / bought reach the server.

---

### T5.6: History and the second visit ★
```yaml
requires:   T5.5
fixture-ok: yes
size:       S · mid
owns:       web/src/routes/history/
status:     not-started
```
The history switch, off by default, with a plain sentence of what is kept and where. The second visit shows what memory did: "Last family function you kept the gold jhumkas." Past looks by occasion.

**Done when:** with history off, nothing mentions memory; with it on, the second-visit story plays on mock data.

---

### T5.7: Sign-in and deletion
```yaml
requires:   T5.1
fixture-ok: yes
size:       M · mid
owns:       web/src/routes/account/
status:     done
```
Sign in by emailed code (looks kept); delete a photo, a look, history, or the account (fresh code, then progress until every part confirms). What YouCam and Cloudflare receive, said plainly.

**Done when:** account deletion shows progress to the end on mock data; the privacy text matches product.md.

Notes:

- The screen is `web/src/routes/account/`. The shell's Account link is `href="#account"`, and that hash is the only shell change. It shows this route instead of the home slot.
- Sign-in posts an empty `choice`. A new address upgrades the guest in place, so this device's looks stay. `guest_data_conflict` leaves the same code live. Switch posts `choice` `switch` and tells the user the guest's looks stay on the guest and are not copied. No other choice is sent.
- Account deletion asks for a fresh code, then `POST /api/account/deletion`. Chaaya's Keel `JobStream` follows that job. The screen lists stage `delete` before `rows_done`, then `rows_done`, then `done`: "Deleting the account.", "Every target is confirmed and the identity rows are gone.", "Every part has confirmed." It does not show a count, a fraction, or the user id. A photo, look, or history deletion uses stage `erase` and the same rule: a sentence, not a count. The recorded stream for the shared deletion job id is the account story, so those three buttons also land on stage `delete` under mock data.
- The privacy lines are product.md's own sentences, read from that file: what Clef on Cloudflare Workers AI receives, what YouCam receives, and what is never sent. The account lines ("Signing in keeps the guest's looks.", the deletion sentence, and the rest) are taken from the same file. No probability figure is shown. The product's sentence that probabilities are never shown is one of the lines.
- The session fixture stays a guest after verify. The screen reports the session it read and still states the upgrade rule. No live mail was sent. No photo was used.

---

### T5.8: Accessibility and contrast
```yaml
requires:   T5.4, T5.5
fixture-ok: yes
size:       S · light
owns:       web/tests/a11y/
status:     not-started
```
Chaaya's contrast and accessibility gates run on every screen, in light and dark, at 375 px, in the gate.

**Done when:** the gates pass in `./tools/check.sh`.
