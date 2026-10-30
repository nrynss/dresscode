# P5B: UX, Component System & Writing Overhaul

```yaml
id:       P5B
size:     L
requires: [P5]
blocks:   P6
parallel: P2, P3, P4
```

**Goal:** Turn the isolated P5 task screens into one cohesive, editorial mobile mirror experience — a 3-step user journey (Front Door → Vanity Mirror Capture → Unified Look), a bespoke UI component system on `bits-ui` v2 and Chaaya's four theme blocks, real sample photographs in mock mode, and a complete UX-writing pass across `product.md`, `testdata/`, and `web/`.

Every rule from P5 still holds:
- **No score, probability, percentage, or digit** in any user-visible string or static markup (`/\d/` is banned across static screen text and error sentences).
- **No memory words in static `.svelte` markup** (`/\bmemory\b|\bremember(?:s|ing|ed)?\b|\brecall(?:s|ed|ing)?\b|jhumkas|second visit/i` is pinned by `history.test.ts` across every `.svelte` file under `web/src/`; all memory copy arrives from API responses or runtime expressions).
- **No banned grading words** (`/\b(score|probability|percent|gender|attractiveness|ratio)\b/i` is banned across all source files under `web/src/` — note that `aspect-ratio` in CSS triggers `ratio`, so use padding spacers instead).
- **Theme roles only:** every CSS colour declaration and SVG `fill`/`stroke` attribute outside the four theme blocks in `theme.css` must use `var(--<role>)`. Any new foreground/background or border role usage must be covered by `textPairs` (4.5:1) or `boundaryPairs` (3:1) in `web/tests/a11y/theme-contrast.test.ts`.
- **375 px phone width & reduced motion:** `.shell` and `.sheet` keep `width: min(40rem, 100%)`, no style rule pins a pixel width over `375px`, and every animation or transition respects `@media (prefers-reduced-motion: reduce)`.

---

### T5.10: UX writing, occasion fixtures and real mock media ★
```yaml
requires:   T5.9
fixture-ok: yes
size:       M · mid
owns:       product.md, docs/occasions.md, testdata/occasions/, testdata/api/, web/vite.config.ts, web/src/lib/api/, web/src/routes/account/privacy.ts, web/src/routes/account/account.test.ts, web/src/routes/occasion/menu.test.ts
status:     done
```
Clean up the engineering and pipeline jargon at the source (`product.md`, `testdata/occasions/`, `testdata/api/`, and `web/src/lib/api/errors.ts`), wire real sample JPEGs from `testdata/samples/` into mock mode's `/media/{id}` responses so dev mode and screenshots show real photographs instead of broken-image icons, and make recall switch to `recall.json` in mock mode once history is enabled in the session.

What to build:

1. **Source copy updates in `product.md` and `web/src/routes/account/privacy.ts`:**
   - In `product.md` (and matching readers in `privacy.ts`):
     - `The call is text only.` → `That step is text only, with no photo sent.`
     - `No Google sign-in for the event.` → `No Google sign-in, and no social login.`
     - `Account deletion fans out across stored files, rows and the Lambo session, survives a restart, and reports when every part has confirmed.` → `Account deletion removes your stored photos, looks, and style memory, continues if the server restarts, and reports when every part has confirmed.`
     - `**Cloudflare Workers AI** over REST: Clef, Clef-flash` → `**Cloudflare Workers AI** runs Clef to read the dress code and check the render` (so `privacy.ts`'s `cloudflare` line reads `Cloudflare Workers AI runs Clef to read the dress code and check the render`).
   - Keep `account.test.ts` passing against `product.md`.

2. **Natural fix descriptions in `testdata/occasions/*.json`, `docs/occasions.md`, and `testdata/api/occasion.json`:**
   - Do **not** touch `criteria[].clef` or `biggest_issue` in `testdata/occasions/*.json` (the 37 Clef recordings in `testdata/clef/` depend on byte-identical `clef` questions).
   - Replace pipeline-jargon `"Renders …"` in `fixes[].changes` across `testdata/occasions/*.json`, `docs/occasions.md`, and `testdata/api/occasion.json`:
     - `date-tie-hair`, `family-tie-hair`, `interview-tie-hair`, `festival-tie-hair`, `funeral-tie-hair`: `"Renders the hair tied back."` → `"Ties the hair back."`
     - `interview-short-hair`: `"Renders the hair short."` → `"Cuts the hair short and neat."`
     - `date-short-beard`, `family-short-beard`, `interview-short-beard`: `"Renders a short even beard."` → `"Shapes a short, even beard."`
     - `funeral-short-beard`: `"Renders a short close beard."` → `"Shapes a short, close beard."`
     - `festival-full-beard`: `"Renders a full even beard."` → `"Shapes a full, even beard."`
     - `date-closed-smile`, `family-closed-smile`, `interview-closed-smile`, `festival-closed-smile`: `"Renders a closed-mouth smile."` → `"Sets a calm, closed-mouth smile."`
   - Update the assertion in `web/src/routes/occasion/menu.test.ts` from `"Renders a short even beard."` to `"Shapes a short, even beard."`.

3. **Error sentence polish in `web/src/lib/api/errors.ts`:**
   - `chunk_mismatch`: `"A piece of the upload did not match. Start it again."`
   - `lease_refused`: `"This session cannot start another look."`
   - `invalid_request` (`field === "choice"`): `"That sign-in choice is not one Dresscode knows."`

4. **Real sample JPEGs in mock mode (`web/vite.config.ts`, `web/src/lib/api/mock.ts`, `web/src/lib/api/mock.test.ts`):**
   - Extend the Vite virtual fixture plugin (or a companion virtual module in `web/vite.config.ts`) to map the recorded `/media/{id}` paths to the real committed sample JPEGs under `testdata/samples/`:
     - `/media/5e1f1e00000000000000000000000001` → `testdata/samples/woman/V04-family-function.jpg`
     - `/media/2e4de200000000000000000000000001` → `testdata/samples/woman/V05-family-function.jpg`
     - `/media/c0a10600000000000000000000000001` and `/media/c0a10cf0000000000000000000000001` → `testdata/samples/products/gold-jhumkas.jpg`
   - When `mock.ts` serves a matched `/media/{id}` GET request, return the real JPEG bytes with `Content-Type: image/jpeg` and status `200`. Update `mock.test.ts` to assert that `/media/5e1f1e00000000000000000000000001` returns non-empty JPEG bytes (`byteLength > 0`, SOI header `ff d8`).

**Done when:** `./tools/check.sh` passes; `product.md` and `testdata/occasions/` carry the human copy; `/media/*` in mock mode serves real sample JPEGs.

Notes:

- The four `product.md` swaps and the `privacy.ts` readers moved together; `privacy.ts` throws on a missing sentence, so the pair is atomic by construction. `account.test.ts` is the proof: it renders the Account screen out of `product.md`'s own sentences, and it passes unchanged.
- The `Renders …` swaps landed in the five occasion files and the frozen `testdata/api/occasion.json` envelope — string values only, envelope shapes untouched. The diff holds only `changes` lines: no `clef` question, no `biggest_issue`, no other field. `docs/occasions.md` needed nothing: its fix tables never carried the `Renders …` sentences (the Changes column is already short human labels), so that part of the table was a no-op there. `menu.test.ts` now asserts `Shapes a short, even beard.`
- `errors.ts`: the three sentences from the table. The `lease_refused` cap-specific branches (`another selfie`, `another render`) stay; the table rewrites only the fallback.
- Media: a companion virtual module `virtual:dresscode-sample-media` in `vite.config.ts` embeds the three sample JPEGs as base64 and `mock.ts` serves those bytes for a matched GET at the recorded ids, `image/jpeg`, status 200, with the recorded headers (cache-control, accept-ranges) kept. Filenames were verified on disk first: `V04-family-function.jpg`, `V05-family-function.jpg`, `gold-jhumkas.jpg`, exactly as named. The two product ids share the jhumkas photograph. The fourth id (`c0a10cf0…`) is named by the task but appears in no recorded fixture; it is mapped anyway, ready for whichever recording names it later. `mock.test.ts` asserts the selfie id returns non-empty bytes starting `ff d8`, the render and product ids return non-empty `image/jpeg` bodies, and a media miss keeps its recorded 404 body. Because `mock.ts` is dynamically imported only in mock mode, the photographs ride the mock chunk, not the production bundle.
- Recall: the mock now stores the switch the way the server does — a `PUT /api/history` with a boolean body turns the session's world on or off, and the family-function recall story answers `recall.json` once on, `recall-off.json` fresh or switched back. `mock.test.ts` proves both worlds and the switch-back; the fresh-session default keeps `history.test.ts`'s off pins green.
- One out-of-owns line, taken mid-task without a prior orchestrator decision and ratified at landing after review verified it minimal and intent-preserving: `history.test.ts`'s "turning it on persists" test had pinned the old always-off recall story (empty line after the PUT, no pin). It now pins `recall-off.json` explicitly through the mock's documented `preferFixture`, so the test still proves exactly what its comment says — the reader honours the body's own switch over the session's — and its assertion is unchanged; the story-default change is proven in `mock.test.ts`.
- Two more paths outside owns, both forced and minimal, in `web/src/vite-env.d.ts`: the ambient declaration for the new virtual module (the pattern the fixture module already uses there), and a base64 `toString` on the declared `ReadBytes`, because the gate's typecheck narrows `node:fs` and would not accept the sample loader otherwise.
- Surprised twice, both TypeScript: TS 5.9's `Uint8Array<ArrayBufferLike>` is not a `Response` body — the lib wants `Uint8Array<ArrayBuffer>`, so the decoder builds over an explicit `ArrayBuffer` — and the narrowed `node:fs` declarations that let the gate skip `@types/node` had no base64 read. Both fixes are declarations, not runtime code.
- The digit rule held: the only digits in the diff are ids, media paths and the base64 marker. Every swapped sentence is digit-free, and the banned-word scan over the touched `web/src` files is clean.

---

### T5.11: Brand mark, editorial shell, button system and Account/History overhaul ★
```yaml
requires:   T5.10
fixture-ok: yes
size:       M · mid
owns:       web/static/, web/index.html, web/src/app/, web/src/routes/account/, web/src/routes/history/, web/tests/a11y/
status:     done
```
Replace the 180 px stacked header with a slim editorial top bar featuring an arched vanity-mirror SVG brand mark, introduce the 4-tier button system in `theme.css`, move the `Appearance` control into `Account` as a segmented 3-way pill bar (defaulting to `system`), upgrade `History` with a tactile toggle switch and neutral `.info` card styling, and upgrade `Account` with a 6-cell OTP code input, a collapsible `Who sees what` disclosure, danger buttons, and plain human copy.

What to build:

1. **Arched Mirror SVG Brand Mark & Icons (`web/src/app/shell.svelte`, `web/static/`):**
   - Draw the exact inline SVG arched vanity-mirror mark beside `Dresscode` in `shell.svelte` (`aria-hidden="true"`, using only `var(--accent)`, `var(--accent-soft)`, and `none` so `svgPaints()` in `theme-contrast.test.ts` passes):
     ```xml
     <svg viewBox="0 0 32 32" width="28" height="28" fill="none" aria-hidden="true">
       <path d="M8 14a8 8 0 0 1 16 0v9a2 2 0 0 1-2 2H10a2 2 0 0 1-2-2v-9Z" fill="var(--accent-soft)" stroke="var(--accent)" stroke-width="2"/>
       <path d="M12 11l3-3M11 16l6-6" stroke="var(--accent)" stroke-width="1.5" stroke-linecap="round"/>
       <path d="M19.5 11.5l.8 2.2 2.2.8-2.2.8-.8 2.2-.8-2.2-2.2-.8 2.2-.8.8-2.2Z" fill="var(--accent)"/>
       <path d="M16 25v3M11 28h10" stroke="var(--accent)" stroke-width="2" stroke-linecap="round"/>
     </svg>
     ```
   - Regenerate `web/static/icons/icon-192.png`, `icon-512.png`, `icon-maskable-512.png`, and `apple-touch-icon.png` (keeping exact pixel dimensions verified by `shell.test.ts`) with this arched vanity-mirror mark on `--ground` (`#f3eee4`) / `--accent` (`#6f2430`).

2. **Slim Editorial Shell & Default System Theme (`web/src/app/shell.svelte`, `web/src/app/theme-control.svelte`, `web/src/app/theme.css`):**
   - Slim top bar: left holds the Arched Mirror SVG mark + `Dresscode` wordmark (linking to `#/`); right holds quiet nav links `Bring the look` (`href={captureHash}` — kept so `upload.test.ts` pin holds), `Style history` (`href="#/history"`), and `Account` (`href="#account"`).
   - Remove `<ThemeControl />` from `shell.svelte` and place it inside `account.svelte`.
   - Ensure default theme when unset is `system` (`theme.mode`).
   - Redesign `theme-control.svelte` as a **segmented 3-way pill control** (`Light · Dark · System`) on `--sunken` with the active label styled in `--accent` / `--on-accent`, keeping the `<fieldset class="appearance">`, `<legend>Appearance</legend>`, and radio `<input>` elements so keyboard accessibility and Chaaya `theme.set` work identically.

3. **Editorial Button System & Motion (`web/src/app/theme.css`, `web/tests/a11y/theme-contrast.test.ts`):**
   - In `theme.css`:
     - **Primary (`button.primary`, `.btn-primary`):** `--accent` fill, `--on-accent` text, `border: 1px solid var(--accent)`.
     - **Secondary (default `button`):** `--surface` fill, `--text` color, `border: 1px solid var(--line)`.
     - **Ghost (`button.ghost`, `a.back`, `.back`):** `--ground` or `transparent` fill, `--accent` or `--text` color, subtle borderless/quiet styling.
     - **Danger (`button.danger`):** `--surface` fill, `--stop` text, `border: 1px solid var(--stop)`.
     - Add `@media (prefers-reduced-motion: no-preference)` micro-press (`button:active { transform: translateY(1px); }`) and smooth transitions.
   - In `web/tests/a11y/theme-contrast.test.ts`, add any newly drawn foreground/background or border pairs (such as `["stop", "surface"]`, `["stop", "ground"]` in `textPairs`, `["stop", "surface"]` and `["accent", "surface"]` / `["accent", "ground"]` / `["accent", "raised"]` in `boundaryPairs`) and verify all 4 theme blocks pass 4.5:1 text and 3:1 boundary gates.

4. **History Toggle Switch & Neutral Info Card (`web/src/routes/history/`):**
   - Replace the error-red `.notice` class on `history.svelte`'s informational notice paragraph with `.info-card` (`background: var(--raised); color: var(--text); border: 1px solid var(--line); border-radius: var(--radius-control); padding: 0.85rem 1rem`).
   - Style `label.switch` as a **tactile sliding toggle switch** (pill track + thumb indicator alongside the `Style history` label text, keeping the `<input type="checkbox">` inside `<label class="switch">Style history</label>` so `firstInputOf("Style history")` in `helpers.ts` and `history.test.ts` pass).
   - Keep zero memory words in static `.svelte` markup (`history.test.ts` pin).

5. **Account Copy, 6-Cell OTP Input, Collapsible Privacy & Danger Triggers (`web/src/routes/account/`):**
   - Replace the flagged WRITTEN strings in `account.svelte` and `account/flow.ts`:
     - `"A new address keeps this device's looks because the guest is upgraded in place."` → `"A new email address keeps the looks already on this device."`
     - `"This device is on the account. The guest's looks were not copied."` → `"This device now uses your account. Earlier guest looks stayed separate."`
     - `"The guest was upgraded in place, so this device's looks stay."` → `"You are signed in, and your looks on this device are kept."`
     - `"Every target is confirmed and the identity rows are gone."` → `"Every stored part is confirmed and your sign-in is removed."` (update `account.test.ts` if it pins this string).
   - Collapse the duplicated `sessionLine` helper so `home.svelte` and `account/flow.ts` share one source without the dead `"You have a session."` branch.
   - Upgrade the 6-digit code inputs (`signInCode` and `deletionCode`) with a **6-cell visual OTP slot frame** around/behind the `<input name="code" inputmode="numeric" maxlength="6">` (no digits in static markup).
   - Wrap the `Who sees what` section in an open-by-default collapsible `<details open class="disclosure">` card (so SSR `render(Account)` and `screens.test.ts`'s `withText("h2", "Who sees what")` still find the heading immediately, while users can collapse/expand it).
   - Style the destructive dialog triggers (`Delete this look`, `Delete this photo`, `Delete style history`) with `class="danger"`.

**Done when:** `./tools/check.sh` passes; the header is compact with the arched mirror SVG mark; `Appearance` lives in `Account` and defaults to `system`; `History` notice is neutral `--raised` instead of error red; `Account` has no engineering jargon.

Notes:

- Icons: `rsvg-convert` 2.63.4 was already on the machine (ImageMagick 7 too), so no dependency question arose; the generator script and the intermediate SVGs stayed in `/tmp/t511-icons/` and only the four PNGs were committed, at the pinned pixel sizes (192, 512, 512 maskable, 180). The mark is the phase's exact 32-box SVG centred on `--ground`/`--accent`/`--accent-soft` hexes: scale 14 for the `any` icons, scale 12 for the maskable (its content corner sits at radius 180, inside the safe zone). `icon.svg` and `icon-maskable.svg` — committed sources the manifest still serves — were rewritten to the same mark so the old jhumka mark does not ship beside the new one; they are hand-written, not generated output.
- Shell: the 180 px stacked header is gone; the bar is one flex row — brand link (arched mark + `Dresscode` wordmark) left, three quiet links right, wrapping under the brand at 375 px. The `h1` became a `span.wordmark` inside the brand link; axe's missing-heading rule is moderate, and the gate counts only serious and critical, so the mounted gates hold. The tagline survives in `index.html` and the manifest; theme.css's `.tag` rules were removed (history's own `.tag` carries its colour locally, verified before removal). The `theme-color` meta effect and the capture routing are untouched, so `upload.test.ts`'s `captureHash`/`Bring the look`/`isCaptureHash` pins and `account.test.ts`'s shell pins hold unchanged.
- Theme control: the default-when-unset is already Chaaya's own `storedMode()` fallback (`system`), so the work was the control's body. The `fieldset.appearance`/`legend`/radios are byte-for-byte the same elements; the pill is styling only — `--sunken` fieldset, `999px` labels, active label `--accent`/`--on-accent`, radios visually hidden but in tab flow (native arrow-key group behaviour intact), and a `:has(input:focus-visible)` outline on the label because the input itself is 1 px. The legend is absolutely positioned as a quiet eyebrow above the pill — browsers render a legend inside the fieldset's box, and without the escape the pill would carry a second row.
- Buttons: the four tiers landed in theme.css exactly as named (`.btn-primary` alias included; ghost covers `button.ghost`, `a.back`, `.back`; danger keeps a `--surface` fill). Micro-press and every transition live under one `@media (prefers-reduced-motion: no-preference)` block.
- Contrast pairs, computed before the test moved (Chaaya's maths): added to `textPairs` — stop/surface 8.45 light and 9.73 dark, stop/ground 7.43 and 10.56 (danger text), dim/sunken 5.69 and 11.70 (pill resting labels and legend on the sunken fill); added to `boundaryPairs` at 3:1 — stop/surface 8.45 and 9.73, accent/surface 10.48 and 10.96, accent/ground 9.21 and 11.89, accent/raised 9.98 and 9.89 (primary and active-segment borders). Nothing failed, so nothing was dropped. Break-proof on a `/tmp` copy of the tree: light `--stop` set to `#b98a8a` and the gate failed naming exactly stop/surface and stop/ground at 2.92 and 2.57 in both light blocks, plus the boundary call.
- History: the informational paragraph took `.info-card` (defined once in theme.css with the phase's exact properties); the switch kept its exact `<label class="switch"><input …/> Style history</label>` structure — `firstInputOf("Style history")` never notices — and the input itself became the track with a `::before` thumb, so the DOM is byte-identical to before. All transitions under the no-preference block.
- Account: the four canonical swaps landed (`product.md` needed nothing — three of them are screen-local sentences). `sessionLine` collapsed to owner-check-plus-guest-fallback, `home.svelte` imports it from `account/flow.ts`, and the dead `"You have a session."` branch is gone from both files. The code inputs keep their exact elements (`name="code"`, `name="deletion-code"`, `inputmode`, `maxlength`) stretched invisibly over a six-cell frame; the cells are `aria-hidden` mirrors of the bound state, so typing, paste and autofill stay native and the characters still land one per cell. `Who sees what` sits in `<details open class="disclosure">` with the `h2` inside the `summary` — `screens.test.ts`'s `withText("h2", "Who sees what")` finds it immediately, open by default. The three named destructive triggers carry `class="danger"`.
- Tests updated: one — `account.test.ts`'s deletion-stream expectation swaps the pinned sentence for `"Every stored part is confirmed and your sign-in is removed."`, the same event slot, so the test still proves the stream reaches the confirmed-account line. Every other pin held unchanged, including the icon pixel sizes, the `Bring the look` href, history's static-text pins, and the account shell-link pins.
- Surprised three times. ESLint's `svelte/require-each-key` refuses unkeyed each blocks, and the cell list cannot key by its own values (six empty strings early), so the keys are positional indices — the honest key for a slot list. The `/tmp` break-proof needed the repository root beside `web/`, not just the `web/` tree: `privacy.ts` imports `product.md?raw` across the root and Vite's import analysis fails the suite at load otherwise. And `opacity: 0` on the theme radios would mute a global focus outline, which is why the focus ring moved to the label via `:has`.
- One question left open for the orchestrator: the `"Delete the account"` dialog trigger keeps `class="primary"` — the task named three triggers and that was not one of them, but it is the most destructive control on the screen. One class swap if wanted.
- Visual sanity pass in a real browser at 375 px, light and dark: slim bar with the mark, segmented pill (Light active in `--accent`, System available, dark forces instantly), six-cell code frame with typed digits landing per cell and a focus ring around the frame, the switch sliding on an `--accent` track, the neutral info-card, and the three danger triggers distinct from the secondary `Email a fresh code`.
- Remediation (orchestrator decision on the open question above): the `"Delete the account"` dialog trigger swapped `class="primary"` → `class="danger"` so every destructive trigger on the Account screen styles consistently; stop/surface pairs were already gated in `textPairs`, and no test pins the trigger's class (`screens.test.ts` addresses screens by name and hash only).

---

### T5.12: Front door — Occasion cards, selectable fix chips, recall banner and judge samples ★
```yaml
requires:   T5.11
fixture-ok: yes
size:       M · mid
owns:       web/src/app/app.svelte, web/src/app/home.svelte, web/src/routes/occasion/, web/tests/a11y/
status:     in-review
```
Make the front door (`#/` and `#/occasion`) the Occasion picker itself: five editorial occasion cards (occasion name only on each card), in-place selectable fix chips with custom check badges when an occasion is chosen, the second-visit recall banner when style history is on, a one-tap "Try a sample look" bar for hackathon judges (`GET /api/samples` → `POST /api/samples/{id}/looks`), and a primary CTA leading straight into `#/capture`.

What to build:

1. **Occasion Cards & Selectable Fix Chips (`web/src/routes/occasion/occasion.svelte`, `web/src/routes/occasion/menu.ts`):**
   - Remove the duplicate `<legend>Occasion</legend>` visual clutter (keep accessible fieldset/legend structure, e.g. visually quiet eyebrow legend `Choose an occasion` and `Fixes you are open to` — note `helpers.ts` `openFixMenu()` looks for `withText("legend", "Fixes you are open to")` and `withText("label", "Family function")`, so keep those exact text nodes!).
   - Style each occasion `<label>` in the first fieldset as an **editorial Occasion Card** on `--raised` (`border: 1px solid var(--line)`, `border-radius: var(--radius-control)`), showing only the occasion name (`{occasion.name}`). When selected, apply `--accent-soft` background and a `3px solid var(--accent)` left border.
   - Style each fix `<label>` under `Fixes you are open to` as a **Selectable Fix Chip / Card** (`background: var(--surface)`, `border: 1px solid var(--line)` when unchecked; `background: var(--accent-soft)`, `border: 1px solid var(--accent)` when checked, with a custom visual check indicator).
   - When an occasion is selected and style history is enabled in the session (`readHistory()`), fetch `GET /api/history/recall?occasion={id}` and, if `line !== ""`, render the recall banner above the fix chips (using runtime expressions only — **never** put any word matching `memoryWords` into static `.svelte` markup!).
   - Beneath the `Save` button, add a primary navigation link/button `Bring the look` (`href="#/capture"`) so after picking an occasion and saving fixes, the user flows directly to Capture.

2. **Front Door Unification & Judge Samples (`web/src/app/app.svelte`, `web/src/app/home.svelte`):**
   - On the home route (`/`), render `<Home />` (slimmed lead + session status + `About` dialog trigger)together with `<Occasion />` directly on the front door so the 5 occasions are immediately interactive without an extra click!
   - Add a **Sample looks** section on the front door that loads `GET /api/samples` (`testdata/api/samples.json`) and renders one-tap sample buttons (`POST /api/samples/{id}/looks` → navigates to `#/look/render`).
   - Keep `verdictHash` (`#/look/verdict`), `renderHash` (`#/look/render`), `historyHash` (`#/history`), and the route branches in `app.svelte` (and keep the quiet secondary links `See the verdict`, `See the render`, `Style history` in a compact footer row on `/` so `verdict.test.ts`, `render.test.ts`, and `history.test.ts` source assertions on `app.svelte` continue to pass 100%).

**Done when:** `./tools/check.sh` passes; opening `#/` shows the 5 occasion cards immediately, reveals the selectable fix chips on tap, offers one-tap sample looks from `/api/samples`, and leads directly to `#/capture`.

Notes:

- The front door is one render: `<Home />` (session line, slimmed lead — the "for an occasion" phrase went, since the picker sits right below — and the About trigger), then `<Occasion standalone={false} />`, then the samples section, then the quiet footer row. The picker takes a `standalone` prop; only the standalone `#/occasion` route draws the Back button. The "Choose an occasion" button and its `openOccasion` are gone; `#/occasion` still resolves through the same `currentRoute` branch. The three quiet links became anchors carrying `verdictHash`/`renderHash`/`historyHash`, so every landed shell pin (`verdictHash`, `See the verdict`, `route === "/look/verdict"`, the render and history twins, `<History />`, `<Render />`, digit-free static text) holds unchanged.
- Contrast, computed before drawing: the new `accent/accent-soft` boundary pair joined `boundaryPairs` at 8.81 light and 8.00 dark — the checked chip border, the selected card's accent edge, and the checked tick's ring all sit on the soft accent fill. The pair the card recipe named — `line` on `raised` — was computed first and dropped: 3.88 light but 2.81 dark, under the non-text minimum, and `theme.css` is not this task's to retune. So the raised occasion cards carry the soft line (already in `softEdges` with exactly this excuse: a decorative edge whose own measured text names the card), and the recall banner is a sunken card (`text/sunken` and `line/sunken`, both measured) rather than T5.11's `.info-card`, which would have drawn the dropped pair again. Nothing else new is drawn: every other declaration uses pairs the table already measures.
- The occasion cards keep the exact accessible structure the gates pin: `fieldset`/`legend` with `Choose an occasion` and `Fixes you are open to` (the helpers' `withText("legend", …)` node byte-identical), labels whose `textContent` is only the occasion or the fix sentence, inputs visually hidden by the shipped `.appearance` pattern (absolute 1 px, `opacity: 0`) with the focus ring moved to the label via `:has(input:focus-visible)`, and the checked styles on `:has(input:checked)`. The chip's check indicator is a CSS tick on an `--accent` disc with `--on-accent` strokes — a measured text pair doing non-text duty. All chip/card transitions sit under one `prefers-reduced-motion: no-preference` block.
- Recall wiring: the screen reads `readHistory()` once per mount into `historyOn` (a failed switch read keeps the line away rather than blocking the menu). A dedicated effect keyed on `selected` plus `historyOn` resets the line and fetches `GET /api/history/recall?occasion={id}` through a new `readRecallLine` in `menu.ts`, which reuses `recallFromResponse` from `history/copy.ts` — the closed-switch body answers "", the on body answers its line. A `not_found` (an occasion the story has not met) or any failed read leaves the menu without the line; the banner is auxiliary and picking fixes carries on. The line renders only as `{line}` in a `role="status"` paragraph above the chips — no memory word sits in any static markup, and the sweep across every `.svelte` file stays clean.
- Samples: `GET /api/samples` loads with the front door, one-tap secondary buttons POST `/api/samples/{id}/looks` then set `#/look/render` (the hashchange listener carries the route; the render screen follows the recorded story look, so the returned ids stay unread). A reader validates the body before anything renders, and a failed start lands in the section's own `ErrorNotice`. Remediation (orchestrator ruling, `testdata/api/` granted): the remaining four samples each got a recorded POST answer (`sample-date-look.json`, `sample-interview-look.json`, `sample-festival-look.json`, `sample-funeral-look.json` — `sample-look.json` mirrored with only the path's id changed), so every one-tap button works in mock mode and the render screen still follows the recorded story look (the shared ids stay unread). Coverage for these is the recorded fixtures alone — `mock.test.ts` is not this task's to extend.
- Tests updated: `theme-contrast.test.ts` adds the computed `accent/accent-soft` boundary pair and documents the dropped `line/raised` one (intent unchanged — every drawn boundary measured or excused); `screens.test.ts` gained four mounted states, the front door with cards and samples open in both themes, and the occasion menu with the past-visit line in both themes — its `before` may now be async, awaited, so the switch write lands before the mount; `menu.test.ts` gained the two-world `readRecallLine` test (off world answers "", the pinned on body answers the line, an unmet occasion answers `not_found`). No landed pin moved. The a11y matrix now covers 13 state entries × 2 themes = 26 mounted tests.
- Browser pass at 375 px, light and dark: cards in a two-column grid, the selected card's accent edge, chip checks, the sunken banner, Save with the primary `Bring the look` anchor, the sample row, and the footer divider all read cleanly; a sample tap lands on the render screen. One in-page surprise along the way: an in-app browser pane stopped delivering trusted pointer events mid-pass (programmatic activation of the same label path kept working and persisting), so the interaction was proven through the DOM activation path itself — click the label, watch the check persist across re-renders — which is the same code path a finger takes.
- Remediation (orchestrator rulings; `theme.css` granted out of owns): `.info-card` left the raised recipe for the measured sunken one the recall banner already uses — the raised recipe drew `--line` over `--raised` (2.81:1 dark, under the non-text minimum), while `text/sunken` and `line/sunken` are both measured pairs, so no `boundaryPairs` entry moved and no contrast assertion regressed; the class's only user, `history.svelte`, keeps `class="info-card"` untouched and inherits the fix. The broader line/raised theme retune is declined: the soft edge is the design, and the sunken recipe is the pattern for raised-adjacent cards. The slimmed lead sentence in `home.svelte` is ratified as written.

---

### T5.13: Vanity mirror capture, collapsible product drawer and unified Look screen ★
```yaml
requires:   T5.12
fixture-ok: yes
size:       L · mid
owns:       web/src/routes/capture/, web/src/routes/look/, web/tests/a11y/
status:     not-started
```
Transform `#/capture` into an arched Vanity Mirror camera experience with button-styled file pickers, a collapsible product drawer, and a `Check the dress code` CTA (`POST /api/looks` → `#/look/verdict`). Transform `#/look/verdict` and `#/look/render` with a 4-step live pipeline stepper, an arched Before/After comparator with a visible split line, drag handle, and `Original` / `After the fix` chips, a Fix hero card, a 3-column verdict bar, and interactive fidelity popovers on the Shop sheet.

What to build:

1. **Arched Vanity Mirror Capture & Collapsible Product Drawer (`web/src/routes/capture/`):**
   - **Arched Mirror Viewfinder Frame:** Wrap the `<video>` preview in an arched mirror frame (`.mirror-frame`: 3:4 portrait box via padding spacer — **never** write the CSS property `aspect-ratio`, which trips the banned-word `ratio` regex in `shell.test.ts`! — `border-radius: 999px 999px var(--radius-panel) var(--radius-panel)`, `border: 2px solid var(--line)`, `background: var(--sunken)`, `max-width: min(100%, calc(68dvh * 3 / 4))`, centered). When the camera is not live, show a calm centered prompt inside the mirror (`The mirror opens here.`).
   - **Button-styled File Inputs:** Visually hide the native `<input type="file">` chrome (`position: absolute; width: 1px; height: 1px; opacity: 0; overflow: hidden`) and style `label.file` as a full-width secondary button (`background: var(--surface); border: 1px solid var(--line); border-radius: var(--radius-control); min-height: 2.75rem`). Keep all three `type="file"` inputs (`accept="image/*"`, `capture="user"`, and product photo) so `upload.test.ts` passes.
   - **Authored Camera Failure Copy:** In `capture.svelte`, never print raw browser `session.reason`; always show `"The camera did not open. Use the phone camera below."`.
   - **Collapsible Product Drawer:** Wrap the `<h2>A product</h2>` section inside a clean `<details class="product-drawer">` disclosure (`<summary>Considering a specific piece?</summary>`) so the selfie mirror is the uncluttered focus while keeping all product inputs and tests working.
   - **Start the Look CTA:** When `selfie` is registered (`The selfie is ready.`), show a primary CTA button (`Check the dress code`) that calls `POST /api/looks` (via a helper in `upload.ts`) and navigates to `#/look/verdict`.

2. **4-Step Live Pipeline Stepper & Verdict Flow (`web/src/routes/look/verdict/`):**
   - Build a **4-Step Live Stage Stepper** showing the four stage words (`Checking`, `Choosing a fix`, `Rendering`, `Checking the render`) as a connected vertical/horizontal progress card: completed stages show a check indicator, the active `{word}` stage pulses gently (`@media (prefers-reduced-motion: no-preference)`) in `--accent-soft` / `--accent`, and upcoming stages sit in `--dim`.
   - Wrap the `<h3>A fix</h3>` and `<p>{verdict.fix}</p>` + `<h3>Why</h3>` `<p>{verdict.why}</p>` in a **Fix Hero Card** (`background: var(--raised); border: 1px solid var(--line-soft); border-left: 3px solid var(--accent); border-radius: var(--radius-control); padding: 1rem`).
   - Add a full-width primary CTA link at the bottom of the finished verdict (`<a class="primary-link" href={renderHash}>See the render</a>`) so the user is never stranded on the verdict screen.
   - Preserve all existing `verdict.test.ts` pins (`!isTerminalStatus(stream.status)`, `{ code: "job_failed" }`, `stageWord(`, `pausedSentence`, `{verdict.fix}`, `{verdict.why}`, `{reason}`, no digits).

3. **Arched Before/After Comparator, Fix Hero Card & Fidelity Popovers (`web/src/routes/look/render/`):**
   - **Visible Split Line, Drag Handle & Chips (`swipe.svelte`):**
     - Give `.frame` the signature arched mirror silhouette (`border-radius: 999px 999px var(--radius-panel) var(--radius-panel)`), `border: 2px solid var(--line)`, and a responsive size cap (`max-width: min(100%, calc(72dvh * 3 / 4)); margin-inline: auto`) so it never exceeds a 768 px tablet viewport (keep `width: 100%` on `.frame` and `min-inline-size: 0` on `.swipe` so `render.test.ts` style pins pass!).
     - Render a `2px` vertical divider at `style:left="{clampedPosition(position)}%"` (`background: var(--surface); border-left: 1px solid var(--line); border-right: 1px solid var(--line)`), a centered `2.5rem` circular handle (`background: var(--surface); border: 1px solid var(--line); color: var(--text)`) with `<span aria-hidden="true">◂ ▸</span>`, and top corner badges (`Original` and `After the fix` on `--surface` with `1px solid var(--line)`).
     - Keep every `render.test.ts` pin on `swipe.svelte` (`type="range"`, `aria-label="Swipe between the original and the render"`, `bind:value={position}`, `src={before}`, `src={after}`, `Show the original`, `Show the render`, `onclick={showOriginal}`, `position = clampedPosition(originalPosition)`, `style:clip-path="inset(0 {clip}% 0 0)"`, `const clip = $derived(originalPosition - clampedPosition(position));`, and zero digits in `staticText(swipe)` — note: do **not** put any digit in markup text outside `{...}` expressions or `<style>`!).
   - **Fix Hero Card & Segmented Verdict Bar (`render.svelte`, `copy.ts`):**
     - In `render/copy.ts`, update `savedSentences.bought` from `"This look is marked bought."` to `"You bought this look."`.
     - Wrap `A fix` and `Why` in the Fix Hero Card (`--raised` + `3px solid var(--accent)` left border).
     - Style `.verdict` as a 3-column equal-width segmented action bar (`Keep` with `.primary` when active/default, `Dismiss`, `Bought it`).
   - **Interactive Fidelity Popovers (`sheet.svelte`):**
     - In `sheet.svelte`, add a Bits UI `Popover` (or `Dialog`/`Collapsible` explanation) so tapping/clicking a product's fidelity label (`{product.label}`) reveals a plain one-sentence explanation of what `exact product`, `approximate match`, or `visual simulation` means. Keep `{product.name}`, `{product.label}`, `{#if product.url}`, `{shopSentence}`, `Shop the look`, and the 375 px style rules (`width: min(40rem, 100%)`, `.name`, `.label`, `.product` `min-inline-size: 0` and `overflow-wrap: break-word`) intact.

**Done when:** `./tools/check.sh` passes; Capture has the arched mirror viewfinder, styled file buttons, collapsible product drawer, and `Check the dress code` flow; Verdict has the 4-step stepper and `See the render` CTA; Render has the arched Before/After comparator with visible split line and handle, Fix hero card, and fidelity explanations.

---

### T5.14: Visual verification and screenshot pass ★
```yaml
requires:   T5.13
fixture-ok: yes
size:       S · light
owns:       web/tests/a11y/
status:     not-started
```
Extend the accessibility and contrast gate matrix in `web/tests/a11y/` to cover the new interactive states (judge samples on home, collapsible drawers, fidelity explanation in the shop sheet), verify that every text pair (4.5:1) and boundary pair (3:1) holds with break-proofs in `/tmp`, and capture a fresh set of light, dark, and 768 px screenshots in `/tmp/ui-explore-after/` to prove the overhaul end-to-end.

**Done when:** `./tools/check.sh` passes; all screen states pass `a11yGate` and `contrastGate` in forced light and dark at 375 px; before/after screenshots confirm the visual overhaul across all 17 states and 768 px tablet width.

---

## Canonical Copy & String Specification

Every edited string and every newly authored UI string across P5B is pinned below. None of these strings contain a digit (`/\d/`) or any banned grading/memory word (`/\b(score|probability|percent|gender|attractiveness|ratio)\b/i`, `/\bmemory\b|\bremember(?:s|ing|ed)?\b|\brecall(?:s|ed|ing)?\b|jhumkas|second visit/i` in static `.svelte` markup).

### 1. Edited Source & Fixture Strings (`T5.10`)

| File | Before | After |
|---|---|---|
| `product.md`, `web/src/routes/account/privacy.ts` | `The call is text only.` | `That step is text only, with no photo sent.` |
| `product.md`, `web/src/routes/account/privacy.ts` | `**Cloudflare Workers AI** over REST: Clef, Clef-flash` | `**Cloudflare Workers AI** runs Clef to read the dress code and check the render` |
| `product.md`, `web/src/routes/account/privacy.ts` | `**No Google sign-in** for the event.` | `**No Google sign-in**, and no social login.` |
| `product.md`, `web/src/routes/account/privacy.ts` | `Account deletion fans out across stored files, rows and the Lambo session, survives a restart, and reports when every part has confirmed.` | `Account deletion removes your stored photos, looks, and style memory, continues if the server restarts, and reports when every part has confirmed.` |
| `testdata/occasions/*.json`, `docs/occasions.md`, `testdata/api/occasion.json` (`*-tie-hair`) | `Renders the hair tied back.` | `Ties the hair back.` |
| `testdata/occasions/interview.json`, `docs/occasions.md` (`interview-short-hair`) | `Renders the hair short.` | `Cuts the hair short and neat.` |
| `testdata/occasions/{date,family-function,interview}.json`, `docs/occasions.md`, `testdata/api/occasion.json`, `web/src/routes/occasion/menu.test.ts` (`*-short-beard`) | `Renders a short even beard.` | `Shapes a short, even beard.` |
| `testdata/occasions/funeral.json`, `docs/occasions.md` (`funeral-short-beard`) | `Renders a short close beard.` | `Shapes a short, close beard.` |
| `testdata/occasions/festival.json`, `docs/occasions.md` (`festival-full-beard`) | `Renders a full even beard.` | `Shapes a full, even beard.` |
| `testdata/occasions/{date,family-function,interview,festival}.json`, `docs/occasions.md`, `testdata/api/occasion.json` (`*-closed-smile`) | `Renders a closed-mouth smile.` | `Sets a calm, closed-mouth smile.` |
| `web/src/lib/api/errors.ts` (`chunk_mismatch`) | `A piece of the upload did not match. Dresscode will need it again.` | `A piece of the upload did not match. Start it again.` |
| `web/src/lib/api/errors.ts` (`lease_refused`) | `This session cannot start another fresh look.` | `This session cannot start another look.` |
| `web/src/lib/api/errors.ts` (`invalid_request`, `field === "choice"`) | `That sign-in choice is not one Dresscode honours.` | `That sign-in choice is not one Dresscode knows.` |

### 2. Edited Screen & Flow Strings (`T5.11`–`T5.13`)

| File | Before | After |
|---|---|---|
| `web/src/app/home.svelte`, `web/src/routes/account/flow.ts` (`sessionLine` fallback) | `You have a session.` | `You are here as a guest.` (shared helper, no dead fallback) |
| `web/src/routes/account/account.svelte` | `A new address keeps this device's looks because the guest is upgraded in place.` | `A new email address keeps the looks already on this device.` |
| `web/src/routes/account/account.svelte` | `This device is on the account. The guest's looks were not copied.` | `This device now uses your account. Earlier guest looks stayed separate.` |
| `web/src/routes/account/account.svelte` | `The guest was upgraded in place, so this device's looks stay.` | `You are signed in, and your looks on this device are kept.` |
| `web/src/routes/account/flow.ts`, `web/src/routes/account/account.test.ts` | `Every target is confirmed and the identity rows are gone.` | `Every stored part is confirmed and your sign-in is removed.` |
| `web/src/routes/capture/capture.svelte` (camera failure) | `{session.reason ?? "The camera did not open. Use the phone camera below."}` | `The camera did not open. Use the phone camera below.` |
| `web/src/routes/look/render/copy.ts` (`savedSentences.bought`) | `This look is marked bought.` | `You bought this look.` |

### 3. Newly Authored UI Strings (`T5.11`–`T5.13`)

| Screen / Component | Element | Exact String |
|---|---|---|
| Shell header (`web/src/app/shell.svelte`) | History nav link | `Style history` |
| Front door (`web/src/app/home.svelte` / `app.svelte`) | Judge sample section heading (`h3`) | `Sample looks` |
| Front door (`web/src/app/home.svelte` / `app.svelte`) | Judge sample section hint (`p`) | `Open a sample selfie to see a finished look right away.` |
| Occasion (`web/src/routes/occasion/occasion.svelte`) | Next-step CTA link beneath `Save` | `Bring the look` |
| Capture (`web/src/routes/capture/capture.svelte`) | Empty mirror viewfinder prompt | `The mirror opens here.` |
| Capture (`web/src/routes/capture/capture.svelte`) | Collapsible product drawer `<summary>` | `Considering a specific piece?` |
| Capture (`web/src/routes/capture/capture.svelte`) | Start-look primary button (when selfie is ready) | `Check the dress code` |
| Verdict (`web/src/routes/look/verdict/verdict.svelte`) | Primary CTA link on finished verdict | `See the render` |
| Render swipe (`web/src/routes/look/render/swipe.svelte`) | Left corner badge | `Original` |
| Render swipe (`web/src/routes/look/render/swipe.svelte`) | Right corner badge | `After the fix` |
| Shop sheet (`web/src/routes/look/render/sheet.svelte`, `copy.ts`) | Fidelity explanation: `exact product` | `This is the exact piece you brought or the occasion named, tried on directly.` |
| Shop sheet (`web/src/routes/look/render/sheet.svelte`, `copy.ts`) | Fidelity explanation: `approximate match` | `This piece is a close catalog match for the fix shown in the render.` |
| Shop sheet (`web/src/routes/look/render/sheet.svelte`, `copy.ts`) | Fidelity explanation: `visual simulation` | `The render shows the effect on your photo, paired with a shopping suggestion.` |

