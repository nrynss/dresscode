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
status:     done
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
status:     done
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

Notes:

- Capture: the mirror is the padding-spacer portrait box (no `aspect-ratio` anywhere), `999px 999px` top radii, `2px` `--line` border, `--sunken` fill, the `max-width: min(100%, calc(68dvh * 3 / 4))` cap, centred; the idle prompt `The mirror opens here.` sits centred inside it whenever the session is neither live nor requesting, so denied, unavailable, failed and fresh all read the same calm frame. The three `type="file"` inputs kept their attributes byte-for-byte; `label.file` is the full-width secondary button and the native input hides with the phase's four properties, the focus ring moved to the label via `:has(input:focus-visible)` — the shipped appearance pattern. The camera failure is the authored sentence always; `session.reason` appears nowhere in the file (pinned). The product section sits inside `<details class="product-drawer">` with `<summary>Considering a specific piece?</summary>`, every input untouched inside.
- The start-look seam, named: `startLook` in `upload.ts` POSTs `/api/looks` with `{occasion, selfie_id, product_ids}` — the recorded request shape — and the screen navigates through `verdictHash`. The occasion comes from `storyOccasion` (`family-function`) because the picker keeps its choice in its own component state (not this task's file) and every look screen already follows the recorded story look, so the fresh look starts on the story occasion until the picker hands its choice across. Reported for a later task to close.
- Verdict: `stageOrder` joined `copy.ts` (the four words in order; `stageWord` still maps the stream's stage, so the landed pins hold). The stepper is an ordered list, `aria-label="Where the look is"`, `aria-current="step"` on the active word: done stages carry the shipped accent-disc-and-tick recipe, the active word sits in `--accent-soft`/`--accent` with a `box-shadow` pulse under `prefers-reduced-motion: no-preference`, upcoming words rest in `--dim`, and the connector between steps is a 1 px `--line-soft` border — decorative, excused like the other soft edges. The stage headline keeps `role="status"`, so a stage change still announces. The hero card wraps `A fix`/`Why` in the phase's exact recipe, and the finished verdict ends with `<a class="primary-link" href={renderHash}>See the render</a>`.
- Render: the swipe gained an inner `.window` (absolute inset, `border-radius: inherit`, `overflow: hidden`) — the pictures clip to the arch inside it, and the corner chips and the split line are never cut by the curve. The first browser pass showed the badges half-clipped when the frame itself clipped; the window was the fix. The divider rides `style:left="{clampedPosition(position)}%"` (a 2 px column that renders as two 1 px `--line` hairlines — under the global border box its 1 px left and right borders take the whole width, so the `--surface` core is 0 px — stopping 2 px short of the frame line), the `2.5rem` handle carries `◂ ▸` (aria-hidden), the badges read `Original` and `After the fix` on `--surface` with `1px solid var(--line)`, and the clip-path transition lives under the motion gate so only the tap glides. `copy.ts`: `savedSentences.bought` is `You bought this look.`; `fidelityExplanations` carries the three canonical sentences. The sheet's label is now a Bits UI `Popover` whose trigger wraps a `span.label` — the chip keeps its scoped class and every sheet pin, the bits-ui trigger resets the theme's button chrome through a `:global` class, and the content names itself `aria-label="What the label means"` because bits-ui gives the content a dialog role and an unnamed dialog is an axe serious rule. `render.svelte` got the same hero card and a three-equal-column `.verdict` grid whose stored verdict carries `.primary` — Keep until a choice is stored.
- Contrast: no new pair. Everything the three screens newly draw sits in pairs the tables already measure (accent/raised for the hero edges, accent-soft/accent for the active stage, dim/ground for upcoming words, line/sunken for the frames, text/surface and line/surface for chips and handle); the drawn-pair guards passing on the clean tree is the proof. What moved instead: T5.9's disclosed blind spot appeared for real — the hero cards and the stepper connector draw side-specific borders, and both guards skipped every one. The widening is two edits in `theme-contrast.test.ts`: `colourDeclaration` gained `border-[a-z-]+`, and the boundary guard's filter became `/^border(-[a-z-]+)?(-color)?$/`. Break-proof on a /tmp copy, never committed: `border-left: 1px solid var(--warn)` fails the boundary guard naming `warn` under the widened sweep and passes vacuously under the landed test file; `color: var(--faint)` on the new mirror prompt fails the drawn-text guard naming `faint`. The surprise the break-proof caught: the first attempt widened only the filter and guarded nothing, because the shared `declarations()` parser had never collected `border-left` at all — the tree would have stayed green and vacuous.
- Tests: `upload.test.ts` gains the `startLook` unit test (the recorded body shape on the wire; an answer without both ids is not a started look) and the screen pins for the mirror, the authored failure, the hiding properties, the drawer and the CTA; `verdict.test.ts` pins `stageOrder`, the stepper, the hero, the primary link and the pulse's motion gate; `render.test.ts` pins the bought sentence, the three explanations, the divider/handle/badges/arch/cap, the clip-path transition gate, the popover and the verdict segments. No landed pin moved: all 53 unit and 34 a11y tests pass with every pre-existing assertion untouched, and `openShopSheet`'s helpers still find the sheet by its old markers.
- Browser pass at 375 px, light and dark, on mock: the arch with its prompt, the button-styled pickers, the open drawer, the finished stepper with its four ticks, both hero cards, the primary CTA, the segmented bar (Keep carrying the accent fill), the sheet, and the fidelity explanation opening beside its label in both themes; a tap on `Show the original` rides the divider to the far edge. `/media` pictures still show as misses in dev — `<img>` loads bypass the mock's fetch interception (feedback.md); the server serves the real bytes.
- One more library surprise for feedback.md: a full-page screenshot from the in-app browser tiles the page in a grid; viewport screenshots only.
- Remediation (review findings): the stage headline's closing period is ratified — the line reads `{word}.` — and the edit joined the canonical table along with two strings review added there: `Opening the look.` (the capture screen's in-progress status paragraph) and `The look could not be started.` (`upload.ts`'s start-look failure, reaching the UI through `shownError`). The swipe divider's note was the only fault: under the global border box the 2 px column renders as two 1 px `--line` hairlines with a 0 px `--surface` core, the CSS is the phase's own recipe and plainly visible, and only the description was wrong (corrected above). The clip-path transition smoothing that drags slightly is ruled accepted as-is: the motion gate is met, and scoping the glide to taps only is possible later polish, not now.
- Remediation (orchestrator grant — `web/src/routes/occasion/menu.ts`, `web/src/routes/capture/upload.ts`, `web/src/routes/capture/upload.test.ts`): the occasion seam is closed. `writeOptIn` keeps the occasion the picker last saved this session (`keptOccasion` in `menu.ts`, a plain module variable beside `remembered` — nothing renders it reactively, so a runes `.svelte.ts` module would add nothing; `resetOccasionMenu` clears it, and a failed write keeps the earlier save). `startLook` sends that choice (`upload.ts`), falling back to the caller's occasion — the capture screen's `storyOccasion` — only when nothing is saved, so fresh-session judges who start straight away keep today's `family-function` behaviour. Pinned in `upload.test.ts`: `a saved picker occasion starts the look` saves Interview then starts the look and asserts the POST body carries `occasion: "interview"`; the no-save fallback pin (`starting the look posts the occasion, the selfie and the products`) is unchanged and green.
- Remediation (orchestrator grant — `web/vite.config.ts`): dev mode's `<img>` loads bypass the mock's fetch interception, so T5.10's virtual-module bytes never rendered in `pnpm dev`. The sample-media plugin gained a `configureServer` middleware answering `GET /media/{id}` from the same id-to-file map the virtual module embeds (`image/jpeg`, no duplicated mapping; unmapped paths pass through to the dev server as before). The test suite needs nothing — `mock.test.ts` already proves the bytes — so the proof is a curl against a scratch-port dev server on a `/tmp` copy of the tree: the mapped selfie id answered 200 `image/jpeg` with JPEG bytes (`ff d8`) at the same size as `testdata/samples/woman/V04-family-function.jpg`, the mapped product id likewise, an unmapped id and the app root behaved as before, and the copy was deleted afterwards.

---

### T5.14: Visual verification and screenshot pass ★
```yaml
requires:   T5.13
fixture-ok: yes
size:       S · light
owns:       web/tests/a11y/
status:     done:glm-5.3-flash
```
Extend the accessibility and contrast gate matrix in `web/tests/a11y/` to cover the new interactive states (judge samples on home, collapsible drawers, fidelity explanation in the shop sheet), verify that every text pair (4.5:1) and boundary pair (3:1) holds with break-proofs in `/tmp`, and capture a fresh set of light, dark, and 768 px screenshots in `/tmp/ui-explore-after/` to prove the overhaul end-to-end.

**Done when:** `./tools/check.sh` passes; all screen states pass `a11yGate` and `contrastGate` in forced light and dark at 375 px; before/after screenshots confirm the visual overhaul across all 17 states and 768 px tablet width.

Notes:

- The mounted matrix went from 13 to 15 states, so 30 gate runs across forced light and dark at 375 px (38 a11y tests total with the layout and contrast files). Two states are new: the capture screen with the product drawer opened (`openProductDrawer` waits on `details.product-drawer[open]`) and the render screen with a fidelity explanation opened in the shop sheet (`openShopSheet` then a tap on the `exact product` label, waiting on the popover's `aria-label="What the label means"`). Two states the overhaul added were already mounted — the front door with the occasion cards and the recall banner on the picker — and two more are carried by states that show them whole: the stepper stands visible (all four stages ticked) on the finished-verdict state, and the comparator badges render always on the render state.
- One honest-wait strengthening: the front-door state's marker was the samples section's hint paragraph, which is static markup — it proves nothing about `GET /api/samples`. The state now waits for a `.sample-row button`, so the gate judges the door with the judge samples actually loaded.
- The stepper mid-flight (one active stage, `aria-current="step"`) is not honestly gateable: the recorded job events arrive as one body, so the replay runs to `done` in milliseconds and any wait for an active stage is a race. Its colours sit in pairs the tables already measure (accent-soft/accent active, dim/ground upcoming), and `verdict.test.ts` pins the stepper's structure; the mounted gate sees the finished stepper.
- Contrast: no new pair. Both new states draw only measured pairs (drawer: `text`/`surface`, `line`/`surface`, `dim` labels; popover: `text`/`surface`, `line`/`surface`, a `line-soft` shadow). The drawn-pair guards already sweep every screen style block, and all four theme blocks pass the 4.5:1 text and 3:1 boundary tables unchanged — `tools/check.sh` needed no edit (nothing pins a test count).
- Break-proofs, on a `/tmp` copy of the tree with `node_modules` symlinked, never committed: stripping `aria-label="What the label means"` from the sheet's `Popover.Content` fails the new fidelity state in both themes (`the fidelity explanation: expected null not to be null`, 10 s wait naming the marker), and renaming the drawer's `<summary>` fails the new drawer state in both themes at the helper's own expect (`the product drawer summary: expected null not to be null`). The copy was deleted afterwards.
- Closing screenshots: `/tmp/ui-explore-after/` holds 47 shots — 20 light and 20 dark at 375×812 (one viewport each; the finished verdict and the front door each fit one viewport, so no second scrolled verdict shot), 7 wide at 768×1024, plus an `INDEX.md` that maps each file to the before set's numbering. The T5.13 dev media middleware served the real photographs, so the comparator, mirror and sheet show pictures. The before set in `/tmp/ui-explore/` was not touched.
- Surprises, all interaction-flavoured and none needing a fix: the app's own chips refuse Playwright's `.check()` (the styled tick span covers the input — the label click is the path a finger takes, matching T5.12's finding), and when the fidelity popover is open it covers the sheet's Close button for actionability, while `locator.press("Escape")` fails with "Active element is no longer the expected input target" because bits-ui keeps focus in the popover — a raw Escape keypress closes it cleanly. Also pleasant: none of the fixed-position states tiled at 375 this time, so the before set's 376 px workaround was never needed; and at 768 the comparator's dvh cap keeps the whole arch inside the viewport, closing the before set's tallest-frame finding.

---

### T5.15: Front door & Occasion picker declutter — compact fix pills, auto-save and collapsible sample drawer ★
```yaml
requires:   T5.14
fixture-ok: yes
size:       M · mid
owns:       web/src/app/shell.svelte, web/src/app/home.svelte, web/src/app/app.svelte, web/src/routes/occasion/, web/tests/a11y/
status:     not-started
```
Cut the wall of introductory text and duplicate button grids on the front door (`#/` and `#/occasion`): keep a single-line header, a 1-line lead, 5 occasion cards, compact 2–3 word Fix pill chips that auto-save on tap with one `Bring the look` CTA, a closed-by-default `Try a sample look` disclosure drawer, and `About` tucked into the quiet footer row.

What to build:

1. **Single-Row Header & Lean Lead (`web/src/app/shell.svelte`, `web/src/app/home.svelte`, `web/src/app/app.svelte`):**
   - In `shell.svelte`, keep the brand mark + `Dresscode` on the left and the nav links on the right on a single line at 375 px (`gap: 0.25rem 0.65rem; font-size: 0.95rem`).
   - In `home.svelte`, remove `<p class="session">{here}</p>` and `Opening a session.` from the home screen (session status already lives on `Account`). Replace the 2-sentence lead with one concise line: `<p class="lead">See the dress code before they do.</p>`.
   - Move the `About` `<Dialog.Root>` trigger out of the middle of the front door and into the `.quiet-row` footer at the bottom of `app.svelte` (styled as a quiet ghost trigger beside `See the verdict`, `See the render`, `Style history`).
   - Wrap the judge samples section in `app.svelte` inside a closed-by-default `<details class="samples disclosure">` with `<summary>Try a sample look</summary>` (remove the separate `<h3>Sample looks</h3>` and `<p class="hint">Open a sample selfie...</p>` paragraph; inside the open `<details>`, render the `.sample-row` buttons directly).

2. **Decluttered Occasion Picker, Compact Fix Pills & Auto-Save (`web/src/routes/occasion/occasion.svelte`, `web/src/routes/occasion/menu.ts`, `web/src/routes/occasion/menu.test.ts`):**
   - In `occasion.svelte`, remove `<h2>Occasion</h2>` and the 3-line instruction paragraph (`"Pick an occasion. Then tick the fixes you are open to..."`). Let `<fieldset class="cards"><legend>Choose an occasion</legend>` lead directly.
   - In `menu.ts`, add a pure helper `fixChipLabel(fix: OccasionFix): string` that maps each fix ID (or `fix.changes`) to a compact 2–3 word pill label (keeping `fix.changes` untouched on the wire and keeping zero `memoryWords` in `.svelte` files!):
     - `*-gold-jhumkas` → `"Gold jhumkas"`
     - `*-gold-chain` → `"Gold chain"`
     - `*-red-lip` → `"Red lip"`
     - `date-nude-lip` → `"Even nude lip"`
     - `interview-nude-lip`, `funeral-nude-lip` → `"Quiet nude lip"`
     - `*-festive-top` → `"Festive mandarin top"`
     - `date-open-collar` → `"Open-collar shirt"`
     - `interview-buttoned-shirt` → `"Buttoned shirt"`
     - `funeral-dark-shirt` → `"Dark buttoned shirt"`
     - `*-tie-hair` → `"Hair tied back"`
     - `interview-short-hair` → `"Short, neat hair"`
     - `*-short-beard` (except funeral) → `"Short, even beard"`
     - `funeral-short-beard` → `"Short, close beard"`
     - `festival-full-beard` → `"Full, even beard"`
     - `*-closed-smile` → `"Calm smile"`
     - fallback: `fix.changes`
   - Render `fieldset.menu` as a compact wrapping pill grid (`display: flex; flex-wrap: wrap; gap: 0.5rem`) where each `<label>` pill shows `<span class="tick" aria-hidden="true"></span> {fixChipLabel(fix)}`.
   - **Auto-save on tap:** When an occasion is selected, record it as the active occasion in `menu.ts`; when any fix pill is toggled, immediately call `writeOptIn(selected, chosen)` in the background (with error reporting via `ErrorNotice`). Remove the separate `Save` button and `"Saved for …"` confirmation paragraph, leaving a single primary CTA: `<a class="btn-primary bring" href="#/capture">Bring the look</a>`.
   - Update `web/tests/a11y/helpers.ts` and `screens.test.ts` so the home and occasion states match the decluttered DOM (e.g. open `details.samples` before waiting on `.sample-row button`, and wait on `withText("p", "See the dress code before they do.")` on home).

**Done when:** `./tools/check.sh` passes; the front door shows only the 1-line lead, the 5 occasion cards, compact 2–3 word auto-saving fix pills + 1 `Bring the look` CTA when an occasion is chosen, and the `Try a sample look` collapsible drawer below.

---

### T5.16: Vanity mirror Capture declutter — in-mirror camera CTA and compact fallbacks ★
```yaml
requires:   T5.15
fixture-ok: yes
size:       S · mid
owns:       web/src/routes/capture/, web/tests/a11y/
status:     not-started
```
Remove all instructional paragraphs and redundant headings from `#/capture`, move `Open the front camera` and `Take the photo` directly inside the arched vanity mirror viewfinder, and place the two file-input fallbacks side by side in one compact row.

What to build:

1. **In-Mirror Camera Controls & Zero Instructional Bloat (`web/src/routes/capture/capture.svelte`, `web/src/routes/capture/upload.test.ts`):**
   - Delete `<p class="lead">One selfie for the occasion, and any product you are considering.</p>`.
   - Delete the visible `<h2>Your selfie</h2>` heading above the mirror (use `aria-label="Your selfie"` on `<section>`).
   - Delete `<p class="hint">If that camera does not open, use the phone camera.</p>`.
   - Inside `.mirror-frame`:
     - When `session.phase !== "live" && session.phase !== "requesting"`, center `<p class="mirror-prompt">The mirror opens here.</p>` and the primary `<button type="button" class="primary" disabled={busy} onclick={() => void openLens("selfie")}>Open the front camera</button>` directly inside the arched mirror!
     - When `session.phase === "live"`, overlay `<button type="button" class="primary take-btn" disabled={busy} onclick={() => void take()}>Take the photo</button>` anchored at the bottom center of `.mirror-frame`.
   - When `selfie` is registered, show the primary `<button type="button" class="primary" disabled={busy || starting} onclick={() => void checkDressCode()}>Check the dress code</button>` right below the mirror (with a brief `<p class="hint">The selfie is ready.</p>`).
   - Place the two file-picker labels (`Choose a photo` and `Use the phone camera`) side by side in a 2-column `.row` below the mirror.
   - Inside `<details class="product-drawer"><summary>Considering a specific piece?</summary>`, remove the redundant `<h2>A product</h2>` heading (the summary already titles the section).
   - Update `screens.test.ts` capture state `ready` marker from `withText("h2", "Your selfie")` to `withText("button", "Open the front camera")`.

**Done when:** `./tools/check.sh` passes; `#/capture` leads immediately with the arched mirror containing `Open the front camera`, a 2-column `Choose a photo` / `Use the phone camera` row below, and the closed `Considering a specific piece?` drawer.

---

### T5.17: Unified Look screen — collapsible stepper, tappable comparator badges and collapsible dress-code notes ★
```yaml
requires:   T5.16
fixture-ok: yes
size:       M · mid
owns:       web/src/routes/look/, web/tests/a11y/
status:     not-started
```
Eliminate the duplicate text and two-page hop between `#/look/verdict` and `#/look/render`: collapse the 4-step pipeline stepper once the look finishes, remove the duplicated `Why` sentence that repeats the missed criterion word-for-word, tuck the missed criteria list into a collapsible `<details>` disclosure, make the `Original` and `After the fix` corner badges inside the arch tappable, and remove the two redundant buttons below the arch.

What to build:

1. **Tappable Corner Badges & No Extra Buttons Below the Arch (`web/src/routes/look/render/swipe.svelte`, `web/src/routes/look/render/render.test.ts`):**
   - Turn the top-left `Original` and top-right `After the fix` badges inside `.frame` into `<button type="button" class="badge original" onclick={showOriginal}>Original</button>` and `<button type="button" class="badge after" onclick={showRender}>After the fix</button>` (with `min-height: 2.75rem`, `background: var(--surface)`, `border: 1px solid var(--line)`, `color: var(--text)`, `z-index: 2` above the range input so taps reach the buttons while dragging the arch still drives the range input).
   - Remove the `<div class="actions">` row (`Show the original` / `Show the render` buttons) below the arch, and update `render.test.ts` assertions accordingly.

2. **Single Compact Fix Card & Collapsible Dress-Code Notes (`web/src/routes/look/render/render.svelte`, `web/src/routes/look/verdict/verdict.svelte`, `verdict.test.ts`, `render.test.ts`):**
   - **Delete the duplicate `<h3>Why</h3> <p>{...why}</p>`** from both `render.svelte` and `verdict.svelte` (it repeats `reasons[0]` word-for-word).
   - Inside `.hero`, show `<h3 class="hero-label">Suggested fix</h3>` (or `<h3>A fix</h3>`) and `<p class="hero-fix">{look.fix}</p>` — concise, single-sentence highlight.
   - Wrap the missed criteria list (`What the dress code still asks for` + `<ul>{#each reasons...}</ul>`) in a closed-by-default collapsible `<details class="disclosure reasons-drawer"><summary>What the dress code still asks for</summary><ul>...</ul></details>` so the screen stays focused on the photo and the fix unless the user expands the full criteria list.
   - On `verdict.svelte`, when `finished` is true, wrap the completed `<ol class="stages">` stepper inside a closed-by-default `<details class="disclosure stages-drawer"><summary>Checked against the dress code</summary><ol class="stages">...</ol></details>` (while `inProgress` is true, render `<ol class="stages">` directly outside `<details>` so the live progress is immediately visible).
   - Update `verdict.test.ts` and `render.test.ts` to match the decluttered markup.

**Done when:** `./tools/check.sh` passes; neither Verdict nor Render prints the same sentence twice; missed criteria and finished pipeline steps fold into clean `<details>` disclosures; `Original` and `After the fix` pills inside the arch replace the extra buttons below it.

---

### T5.18: History & Account progressive disclosure, product.md shade-match sync and screenshot pass ★
```yaml
requires:   T5.17
fixture-ok: yes
size:       M · mid
owns:       product.md, web/src/routes/account/, web/src/routes/history/, web/tests/a11y/
status:     not-started
```
Reduce cognitive load on `#/history` and `#account` using progressive disclosure (`<details>` sections closed by default for `How style history works`, `Who sees what`, and `Delete the account`), cut duplicate explanatory sentences in Account, align `product.md`'s analysis sentence with the shipped `faceShape` scope (closing the T3.3 product finding), and refresh the closing screenshot set in `/tmp/ui-explore-after/`.

What to build:

1. **Resolve the `product.md` Analysis Wording (`product.md`, `web/src/routes/account/privacy.ts`, `web/src/routes/account/account.test.ts`):**
   - In `product.md`:
     - Line 15: change `YouCam analysis runs only when the fix needs it: colour tones for a shade match, face shape and hair type for a hairstyle.` to `YouCam analysis runs only when the fix needs it: face shape for a hairstyle.`
     - Line 34: change `allowlisted attributes (face shape, hair type and length, colour tones)` to `allowlisted attributes (face shape)`
   - Verify `privacy.ts` and `account.test.ts` pass with the updated `product.md` sentence.

2. **History Progressive Disclosure (`web/src/routes/history/history.svelte`, `web/src/routes/history/history.test.ts`):**
   - Put `<h2 id="history-heading">Style history</h2>` and `<label class="switch">` in a single horizontal header bar (`.history-bar`: `display: flex; align-items: center; justify-content: space-between`), and visually hide the duplicate text inside `<label class="switch"><input ... /><span class="sr-only">Style history</span></label>` so `"Style history"` is not printed twice side-by-side while `firstInputOf("Style history")` still finds the label!
   - Wrap `<p class="info-card">{switchState.notice}</p>` inside a closed-by-default `<details class="disclosure"><summary>How style history works</summary><p class="info-card">{switchState.notice}</p></details>` so the 4-line explanation is tucked away until tapped.
   - Remove the redundant `<h3>Past looks</h3>` subheading above the occasion cards.

3. **Account Progressive Disclosure & Copy Deduplication (`web/src/routes/account/account.svelte`, `web/src/routes/account/account.test.ts`):**
   - Under `Sign in`:
     - Keep `<p>{accounts.keepsLooks}</p>` (`"Signing in keeps the guest's looks."`) and remove the duplicate `<p>A new email address keeps the looks already on this device.</p>` paragraph.
     - Move `<p>{accounts.noGoogle}</p>` inside the `Who sees what` disclosure block so the sign-in form has only 1 short sentence above the email input.
   - **`Who sees what` (`<details class="disclosure">`):**
     - Remove the `open` attribute so `Who sees what` is **collapsed by default** (hiding 9 paragraphs of pipeline architecture until tapped). Note: in SSR (`render(Account)` in `account.test.ts`), `<details>` still renders its children in the HTML string, so `account.test.ts` still verifies every sentence from `product.md`!
   - **Deletion (`<section aria-labelledby="deletion">`):**
     - Keep `<p>{accounts.stays}</p>` (`"Photos, renders and history stay until the user deletes them."`) and move `<p>{accounts.notAutomatic}</p>` inside the `Delete the account` disclosure so the Deletion section has only 1 sentence above the items.
     - Wrap the `Delete the account` subsection (`<h3>Delete the account</h3>`, `{accounts.deletion}`, fresh-code instructions, `Email a fresh code`, and fresh-code OTP input) inside a closed-by-default `<details class="disclosure"><summary>Delete the account</summary>...</details>` so account-erasure instructions only open when requested.

4. **Accessibility Matrix & Closing Screenshots (`web/tests/a11y/screens.test.ts`, `web/tests/a11y/helpers.ts`):**
   - Add an `open` step in `screens.test.ts` for the `Who sees what` disclosure state on Account, verify all mounted states pass `a11yGate` and `contrastGate` in forced light and dark at 375 px, and re-capture the screenshots in `/tmp/ui-explore-after/` (`light/`, `dark/`, `wide/`, `INDEX.md`).

**Done when:** `./tools/check.sh` passes; `Who sees what`, `Delete the account`, and `How style history works` are collapsed by default; duplicate paragraphs on Account and History are gone; `product.md` matches the shipped analysis scope; `/tmp/ui-explore-after/` holds the updated decluttered screenshots.

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
| `web/src/routes/look/verdict/verdict.svelte` (stage headline, `role="status"`) | `{word}` | `{word}.` |
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
| Capture (`web/src/routes/capture/capture.svelte`) | Status while the look starts (`role="status"` paragraph) | `Opening the look.` |
| Capture (`web/src/routes/capture/upload.ts`, reaches the UI through `shownError`) | Start-look failure sentence | `The look could not be started.` |
| Verdict (`web/src/routes/look/verdict/verdict.svelte`) | Primary CTA link on finished verdict | `See the render` |
| Render swipe (`web/src/routes/look/render/swipe.svelte`) | Left corner badge | `Original` |
| Render swipe (`web/src/routes/look/render/swipe.svelte`) | Right corner badge | `After the fix` |
| Shop sheet (`web/src/routes/look/render/sheet.svelte`, `copy.ts`) | Fidelity explanation: `exact product` | `This is the exact piece you brought or the occasion named, tried on directly.` |
| Shop sheet (`web/src/routes/look/render/sheet.svelte`, `copy.ts`) | Fidelity explanation: `approximate match` | `This piece is a close catalog match for the fix shown in the render.` |
| Shop sheet (`web/src/routes/look/render/sheet.svelte`, `copy.ts`) | Fidelity explanation: `visual simulation` | `The render shows the effect on your photo, paired with a shopping suggestion.` |

### 4. Progressive Disclosure & Declutter Strings (`T5.15`–`T5.18`)

| Screen / Component | Element | Exact String |
|---|---|---|
| Front door (`web/src/app/home.svelte`) | Single-line lead (`p.lead`) | `See the dress code before they do.` |
| Front door (`web/src/app/app.svelte`) | Collapsible sample looks `<summary>` | `Try a sample look` |
| Occasion (`web/src/routes/occasion/menu.ts` `fixChipLabel`) | Compact pill labels | `Gold jhumkas`, `Gold chain`, `Red lip`, `Even nude lip`, `Quiet nude lip`, `Festive mandarin top`, `Open-collar shirt`, `Buttoned shirt`, `Dark buttoned shirt`, `Hair tied back`, `Short, neat hair`, `Short, even beard`, `Short, close beard`, `Full, even beard`, `Calm smile` |
| Verdict (`web/src/routes/look/verdict/verdict.svelte`) | Finished stepper `<summary>` | `Checked against the dress code` |
| Verdict & Render (`verdict.svelte`, `render.svelte`) | Missed criteria `<summary>` | `What the dress code still asks for` |
| History (`web/src/routes/history/history.svelte`) | Privacy notice `<summary>` | `How style history works` |
| Account (`web/src/routes/account/account.svelte`) | Privacy disclosure `<summary>` (closed by default) | `Who sees what` |
| Account (`web/src/routes/account/account.svelte`) | Account erasure disclosure `<summary>` (closed by default) | `Delete the account` |
| `product.md`, `web/src/routes/account/privacy.ts` | Analysis scope sentence (`T5.18`) | `YouCam analysis runs only when the fix needs it: face shape for a hairstyle.` |


