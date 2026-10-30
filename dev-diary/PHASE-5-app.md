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
status:     done
```
Chaaya's job stream through the stages (checking → choosing a fix → rendering → checking the render), paused while the tab is hidden and resumed on return. The verdict: the criteria the look misses as reasons, the suggested fix, and why.

**Done when:** a look survives backgrounding the app mid-render; no number appears anywhere on screen.

Notes:

- The screen is `web/src/routes/look/verdict/`. The shell link "See the verdict" is on the home screen in `web/src/app/app.svelte` and sets `#/look/verdict`. There is no router in Chaaya.
- It follows the recorded family-function look, the one the fixtures already have at render. Chaaya's keel `JobStream` subscribes to `GET /api/jobs/{id}/events` and then reads the state. `job_id` is mapped onto `jobId`. `current` and `total` are not copied. The stage is shown as Checking, Choosing a fix, Rendering, or Checking the render. Measure uses the same words as pick. Shop has no word. A `next_id` opens that attempt. The look id does not change.
- Chaaya pauses that stream while the document is hidden and resumes it when the document is visible again. The keel options do not include `pauseWhenHidden`. Leaving it unset keeps the core default, which is on.
- The verdict is the ready look. Each missed criterion is its suggestion. The fix is its changes. Why is the fix's why. The check booleans, the ids, the counters, and the shop list are not written out. A failure is shown from its code's sentence, not from the wire phrase.
- The test holds the recorded event stream open after the render frame, hides the document, and shows it again. The stream stays paused and does not reconnect while it is hidden. The same job and the same look resume. The verdict lines have no digit.
- Landed after a second review came back clean. A terminal status the screen cannot narrate (a bare `cancelled` or `interrupted`, or an `error` with no error envelope) ends the wait through Chaaya's `isTerminalStatus` and shows the `job_failed` sentence — "This look could not be finished." — instead of leaving the last stage word up. Fixtures for both bare endings are in `testdata/api/` in the recorded shape. The second review accepted the bare-`error` coverage as the same hang class, not scope creep.

---

### T5.5: The render and shop the look ★
```yaml
requires:   T5.4
fixture-ok: yes
size:       M · mid
owns:       web/src/routes/look/render/
status:     done
```
Before and after as a swipe, the original one tap away; shop the look as a bottom sheet with every product's label shown plainly; keep, dismiss, bought. When no render passed the check, the suggestion and products stand without one, and say so.

**Done when:** each label reads clearly at 375 px; keep / dismiss / bought reach the server.

Notes:

- The screen is `web/src/routes/look/render/`. The shell link "See the render" is on the home screen in `web/src/app/app.svelte` and sets `#/look/render`, on T5.4's accepted wiring pattern: an import, a branch in `currentRoute`, a hash setter, a render branch, and the home button. There is no router in Chaaya.
- The screen reads the ready look once. A pending look is the sentence that says it is still being finished, with the verdict screen named for following it. The swipe is one range input laid over the two pictures, so the drag is the browser's own slider and stays keyboard reachable; the position lives in state and is never written on the screen as a figure. "Show the original" covers the frame with the original in one tap, and "Show the render" goes back.
- Guideline six is enforced twice. The server withholds a discarded render; the reader discards again: a `shown_render` whose check is not all three true, or whose check cannot be read, becomes no render. The screen then says "No render passed the check. The suggestion and the products stand without one." A test pins a recorded body with `same_person` false and the reader returns no render.
- Shop the look is a Bits UI `Dialog` styled as a bottom sheet; Bits UI v2 ships no Sheet component, dialogs are its parts. The list is read from `GET /api/looks/{id}/shop` the first time the sheet opens. Every product's `fidelity` is validated against the three domain spellings before the sheet shows it, and a list that cannot be read throws into the usual failure sentence. A product with no url shows no picture box. Keep, dismiss, and bought are the look's verdict; each sends the one-field `PUT` and the screen says what is stored.
- Fixtures added: `shop-none.json` (the suggestion-only look's shop list, the same recorded product), and `media-render.json` with `media-product.json`, the empty 200s at the media ids `look.json` and `shop.json` already name, in the shape of `media.json`.
- Surprised: the shell test bans score, probability, percent, gender, attractiveness, and ratio across every source file, and the banned pattern matches inside `aspect-ratio`, so the swipe's portrait frame is a padding spacer. Classes carried through Bits UI props read as unused selectors to Svelte's scoper and are stripped, so the sheet's own classes are `:global()`. The recorded media fixtures carry no bytes, so in mock mode every picture, on this screen and the others, shows as a miss; the server serves the real bytes.
- Landed after a clean review. The review accepted the 375 px clause on the pinned styles plus the browser-viewport pass (no physical phone, the same unmet-note class as T5.1 and T5.3), and accepted the empty 200 media fixtures as the recorded shape rather than a wrong one. One note for later screens: the sheet's `:global(.sheet)` name is generic — a more specific name when another dialog lands.
- The 375 px clause: the gate asserts the layout the clause needs — the sheet spans `min(40rem, 100%)`, names and labels wrap, the rows and frames carry `min-inline-size: 0`, and nothing pins a line against wrapping. On top of the gate, the screen and the sheet were opened in a browser at a 375 px viewport on mock data and every label read clearly; the screenshots are in the task report. No physical phone was used.

---

### T5.6: History and the second visit ★
```yaml
requires:   T5.5
fixture-ok: yes
size:       S · mid
owns:       web/src/routes/history/
status:     done
```
The history switch, off by default, with a plain sentence of what is kept and where. The second visit shows what memory did: "Last family function you kept the gold jhumkas." Past looks by occasion.

**Done when:** with history off, nothing mentions memory; with it on, the second-visit story plays on mock data.

Notes:

- The screen is `web/src/routes/history/`. The shell link "Style history" is on the home screen in `web/src/app/app.svelte`, on T5.4 and T5.5's accepted wiring pattern: an import, a branch in `currentRoute`, a hash setter, a render branch, and the home button. There is no router in Chaaya.
- The switch is a checkbox, off until a `PUT` stores on. The recorded read is the fresh first visit, so a session that turned the switch on keeps the value it sent until a full reload asks again — the accepted opt-in rule from T5.2. The notice is the API's own sentence for each state; the screen adds no sentence of its own about memory, per the contract's "the app must not add a memory sentence of its own" when off.
- Every word of memory arrives from the API through an expression: the notice, the recall sentence, the looks in words, the occasion names. The screen's own markup names only the switch and the sections, so a fresh screen's static markup carries none of the memory words. The test pins that absence across every `.svelte` file under `src`, and pins it at the network too: with the switch off, the second-visit read refuses before it fetches, so no call about memory is made at all.
- With history on, the second visit reads past looks per occasion in the occasions' order, and recall for each occasion that has looks. An occasion whose list is not recorded (the mock answers its `not_found` envelope) carries no group and no line; any other failure surfaces through `ErrorNotice` and `humanMessage`. The reader refuses a word of memory beside a closed switch, and another occasion's memory beside the asked one, so neither can reach the screen however the wire misbehaves.
- Fixture added: `recall-empty.json`, the contract's empty-memory recall example ("enabled true, the occasion, an empty line, and empty kept and dismissed"), recorded at the date occasion, which has no memory in the story. It is the only fixture added; the switch, the story, the off recall, and the past-looks examples were already recorded. A second occasion's look in the grouping test is pinned on the mock in the recorded item shape, the accepted T5.2 pattern.
- The recorded story sentence is the recall fixture's own line, shown verbatim: "Last family function you kept the gold jhumkas and dismissed the red lip." The kept and dismissed fix ids ride along for the picker and reach no markup.
- Surprised twice. A Svelte 5 state variable named `state` breaks `svelte-check` — svelte2tsx's generated code has its own `state` binding, and the rune then reads as "used before its declaration" — so the screen's state is `switchState`. And `lastFixtureFile` after a read that makes several calls is timing-fragile across a parallel suite run; the test asserts the recorded call list instead of the last file.
- The 375 px clause: the screen was opened in a browser at a 375 px viewport on mock data, off and on; the off screen shows the switch and the notice and nothing else, the on screen shows Past looks with the Family function group and the look in words, and nothing overflows. The recall sentence does not show in the browser because the mock's story default for the recall path is the off recording; the on-world sentence is proven in the tests through the mock's documented `preferFixture` pin. No physical phone was used, the same unmet-note class as T5.1, T5.3 and T5.5.
- A seam left for a handoff note: the account screen's "Delete style history" turns the stored switch off on the server, but this screen's session-held value learns of it only on a full reload. Both screens are landed; crossing that seam is a later task's call, not a change here.
- Remediation of the review finding: the off-body refusal in `recallFromResponse` named only `line`, `kept` and `dismissed`, so a stray `occasion` beside the closed switch was tolerated, though the contract's off body is `{"enabled":false}` and nothing else and the occasion names are words of memory. The refused-member list now names `occasion` too, same throw; the on world is untouched because the check lives in the off branch only, and an on body's `occasion` is still read where it belongs. One assertion added to the refusal test pinning that `{enabled:false, occasion}` throws; it was run against the unfixed reader first and failed, then the fix was applied and it passed. Nothing else changed.

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
status:     done
```
Chaaya's contrast and accessibility gates run on every screen, in light and dark, at 375 px, in the gate.

**Done when:** the gates pass in `./tools/check.sh`.

Notes:

- The gates are Chaaya's own, from `@nrynss/chaaya/testing`: `contrastGate` over `web/src/app/theme.css`, and `a11yGate` over the rendered page. No accessibility logic lives in this repository. The suite is `web/tests/a11y/`; the one line added to `tools/check.sh` runs it (`pnpm exec vitest run --config tests/a11y/vitest.config.ts`).
- The gate matrix: eleven screen states, each mounted as the whole app (shell, theme control, and the route) on the recorded mock data, in forced light and forced dark, at a 375 px viewport — 22 gate runs. The states: home with the session read; home with the About dialog open; the occasion screen with the fix menu open; capture; the verdict screen with the recorded look finished; the render screen with the swipe and verdict buttons; the render screen with the shop sheet open; history off; history on with the second visit shown; the account screen with the privacy text; the account screen with the delete-history dialog open. Each run waits for the state a reader would see, then points `a11yGate` at `document.body`, so the Bits UI portals (the dialogs and the sheet) sit inside the measured container.
- What `a11yGate` asserts per run: axe finds no serious or critical violation (names and labels ride inside axe — `button-name`, `link-name`, `image-alt`, `label` and their neighbours are serious or critical rules, so an unnamed control fails the gate); no element carries a native `title` attribute; every button, link, input, select and textarea can take focus (no `tabindex="-1"`; disabled controls are exempt). Chaaya's own gate disables axe's `color-contrast` and `region` rules because jsdom has no layout engine — that is the tool's documented design, not a local override.
- What `contrastGate` asserts: eleven text pairs — every foreground/background pair the screens draw — at Chaaya's default 4.5, in every theme block the stylesheet defines: system light, system dark, forced light, forced dark. Three guards keep the pair list honest and able to fail: the stylesheet carries all four blocks (asserted); every colour declaration outside the theme blocks is a `var()` of a theme role, so a raw hex, `rgb()` or a colour name on any screen or on the shell fails the suite; and every role drawn as text or background must appear in a measured pair, so a new drawn pair cannot skip the gate.
- 375 px: jsdom lays nothing out, so the width duty is carried two ways. Every gate run sits in a 375 px viewport (`window.innerWidth = 375`, asserted per run), and a static gate asserts the two rules the clause rests on: the shell and the sheet span `min(40rem, 100%)`, and no style rule pins a fixed px width wider than 375. The same pinned-styles class as T5.5's accepted 375 px clause.
- jsdom entered as a dev dependency with the user's approval (asked through the orchestrator's question): Chaaya's a11y gate needs a DOM host, jsdom is vitest's own optional DOM peer and the same DOM Chaaya's package tests its gates with. Reason stated in the handoff log; no other dependency was added.
- The gates can fail, proven on a copy of the tree in `/tmp` with both breaks applied there and never committed: light `--text` dropped to `#767676` and the whole `check.sh` went red with the contrast gate naming each failing pair and block ("system light: --text (#767676) on --ground (#f3eee4) is 3.93 to 1, below 4.5" and its eleven companions); a `title` attribute injected on the home screen's lead failed `a11yGate` with `title attribute on <p>: pointer only help`, red in both themes.
- No genuine failure on a landed screen: all 22 states pass axe serious and critical, the title rule, and focusability; all eleven pairs pass 4.5 in all four blocks. Findings for the orchestrator, not gate failures: (1) WCAG 1.4.11 non-text contrast sits outside Chaaya's gate's authored coverage, and the control boundaries do not meet 3:1 — `--line` over `--ground` is 1.39 to 1 light and 1.95 to 1 dark, `--line` over `--surface` 1.58 and 1.80 (theme.css lines 12 and 95 light, 40 and 68 dark) — and the text inputs and buttons it draws sit on `--surface` fills 1.14 to 1 from the page in light and 1.08 to 1 in dark, so that border is the only boundary the control has; routing this (gate it and remediate the theme, or accept it) is the orchestrator's call, no screen was touched. (2) `--focus-ring` is a compound value, so Chaaya's gate, which reads hex-only tokens, cannot measure it; the ring's colour equals `--accent`, and accent over ground passes 3:1 in both modes by a wide margin.
- Surprised four times: vitest's jsdom environment routes module fetches through vite's serving allow list while its node environment does not, so the suite's config adds the repository root to `server.fs.allow` — `product.md?raw`, which the account screen reads by T5.7's design, sits above `web/`; vitest resolves Svelte to its server build unless the config carries the browser condition, and `mount` then refuses with `lifecycle_function_unavailable`; jsdom 30 carries no `matchMedia`, no `ResizeObserver`, and no `scrollIntoView`, and the setup file stubs each with only the behaviour the app reads; and the verdict screen's job stream runs to done under the mock inside jsdom exactly as the node-environment tests showed. The library surprises are also feedback.md material at the orchestrator's hand.
- Remediation of the review findings: (1) the diary arithmetic is corrected — the `--surface` fills sit 1.14 to 1 from the page in light and 1.08 to 1 in dark (computed 1.1375 and 1.0848), not "about 1.04 to 1"; the direction and the conclusion stand, and the boundary figures in the note above were re-computed and held. (2a) `fill` and `stroke` joined the colour-property allowlist, and a new scan reads the svg `fill`/`stroke` attributes in every screen's markup over the same sweep the contrast guard uses, so a raw-colour paint attribute fails the suite the way a raw CSS colour does; the failure names the attribute, the value, and the file. (2b) the declaration parser now reads a declaration to the next `;`, the closing `}`, or the end of the text — the last declaration of a block may carry no trailing semicolon — and anchors at the start of a rule body, not only after `{` or `;`. (2c) layout-375's fixed-px scan now sweeps theme.css plus every screen's whole text (markup and style blocks alike) through one shared sweep in `helpers.ts`, and names the file in the failure. Surprised once, and it is the big one: making (2b) real exposed that the landed parser read the wrong capture groups — it tested the value against the property allowlist and read a third group that does not exist — so both colour guards that consume it had been passing vacuously on every tree so far, including the one this task landed. With the parser live, the clean tree needed one tolerance: a size rides beside the colour it qualifies (`border: 1px solid var(--line)`), so a number with a unit is stripped before the raw-value test; a bare hex, `rgb()` or a colour name still fails. All three extensions were proven to fail on /tmp copies and never committed: a scratch svg with `fill="#123456"` and `stroke="rebeccapurple"` failed the paint guard naming both; a raw `#123456` as the last declaration of an occasion-screen rule with no trailing semicolon failed the raw-colour guard; `width: 400px` in the same rule failed the width guard naming the file. The clean tree passes all 28 a11y tests.
- Disclosed blind spots, recorded at the second review's ask: an unquoted svg paint attribute (`fill=red`) would slip past the paint scan, and an inline `style="…"` markup attribute escapes the colour sweeps (the width scan reads whole text, so inline widths are covered). Neither occurs in the tree; if either pattern appears, widen the sweep before trusting the green.

### T5.9: Control boundaries meet 3:1
```yaml
requires:   T5.8
fixture-ok: yes
size:       S · light
owns:       web/src/app/theme.css, web/tests/a11y/
status:     in-review
```
The user accepted the 1.4.11 recommendation: control boundaries must be visible. `--line` reaches at least 3:1 against both `--ground` and `--surface`, in light and dark, with the smallest visual change that gets there. The contrast gate gains the non-text pairs (line vs ground, line vs surface, both modes) at 3:1, so the boundary cannot quietly fade again.

**Done when:** the new pairs pass at 3:1 in all four theme blocks and the gates still bite (break-proof in /tmp).

*(User decision on T5.8's routed finding; see the handoff log.)*

Notes:

- The theme change is one role: `--line` in all four blocks — light `#d5cbb8` → `#827c70` (each channel times 0.61), dark `#4a433b` → `#706659` (each channel times about 1.51). The warm channel proportions are kept, so the line stays the same warm grey, darker in light and lighter in dark; no other role, screen or sentence moved.
- The measured set grew one pair beyond the two the decision names: the render swipe's frame draws `--line` against `--sunken`, and in light `--sunken` (`#e6dfd2`) is darker than `--ground`, so sunken — not ground — is light's binding pair. The gate measures line vs ground, surface and sunken, plus the notice's `--stop` vs `--stop-soft` and `--stop` vs `--ground` (the notice border is a drawn boundary; it already cleared 3:1 — 7.03 and 7.43 light, 8.17 and 10.56 dark — so measuring it costs nothing).
- Ratios by Chaaya's own maths, old → new: light vs ground 1.39 → 3.58, vs surface 1.58 → 4.08, vs sunken 1.21 → 3.13; dark vs ground 1.95 → 3.38, vs surface 1.80 → 3.11, vs sunken 2.02 → 3.50.
- Every candidate's ratios were computed before choosing. The mathematical minimum hexes that clear 3:1 sit razor-thin — light `#857f73` (channel scale 0.626, worst pair 3.00), dark `#6e6458` (scale 1.486, worst pair 3.02) — so the chosen values are the nearest candidates with a real cushion: scale 0.61 (worst pair 3.13) and 1.515 (worst pair 3.11). The one-step-nearer light candidate `#847e72` would pass at 3.04.
- `--line-soft` is deliberately unchanged. It edges the product cards, the fidelity chip and the recall line — containers whose identification is carried by their own text, each text colour already in a measured pair — not a control boundary; gating it at 3:1 would demand moving it from 1.24 to 3.0, a redesign the decision did not ask for. The new honesty guard names it in `softEdges` with that reason.
- Gate wiring: Chaaya's `contrastGate` takes the minimum per call, not per pair, so the boundary list is its own call — `contrastGate(themeCss, boundaryPairs, 3)` — the same measurement, the same four-block discovery, the same failure shape that names the block, both hexes, the ratio and the minimum. The text pairs stay a separate call at the default 4.5. A second guard, built on the same sweep as the drawn-pair test, reads every border declaration for roles and fails when a drawn boundary role sits in no measured pair (or an excuse), so the list cannot rot away from what the app draws and a future fade fails the suite naming the pair.
- Break proof on a /tmp copy, never committed: light `--line` reverted to `#d5cbb8` in both light blocks and `./tools/check.sh` went red with six failures — "system light: --line (#d5cbb8) on --ground (#f3eee4) is 1.39 to 1, below 3" and its five companions (surface 1.58, sunken 1.21, both light blocks). The other 29 tests stayed green, so the assertion that bit is precisely the new one.
- Clean tree: `./tools/check.sh` green end to end — go vet, `go test -race`, eslint, svelte-check, 50 unit tests, 30 a11y tests (28 plus the boundary gate and the boundary sweep).
- Surprised twice. Chaaya's minimum rides per call, so a suite carrying two minima carries two calls; wrapping the gate to fake a per-pair minimum would have left the library's own failure shape behind. And the smallest change that reaches 3:1 is not small: a line drawn near the page colour cannot triple its contrast within a shade or two — the light line drops about 61 percent down its channels. The control fills stay 1.14 to 1 from the page in light and 1.08 to 1 in dark, so the border is still the only boundary the control has, and it now carries its own contrast.
