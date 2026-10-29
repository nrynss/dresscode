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
status:     not-started
```
The app shell, Dresscode's own look on Chaaya's theme roles (light, dark, system), the installable manifest and icons, and Chaaya's API client with its Keel adapter so every `wire` error arrives typed. A mock mode serving `testdata/api/`.

**Done when:** the app installs on a phone from a local tunnel; every recorded error code renders a human message.

---

### T5.2: Occasion and opt-in menu ★
```yaml
requires:   T5.1
fixture-ok: yes
size:       S · mid
owns:       web/src/routes/occasion/
status:     not-started
```
Pick an occasion; tick the fixes you're open to (remembered per occasion). Nothing asks or infers gender.

**Done when:** the menu saves and reloads per occasion.

---

### T5.3: Bring the look ★
```yaml
requires:   T5.1
fixture-ok: yes
size:       M · mid
owns:       web/src/routes/capture/
status:     not-started
```
One selfie through Chaaya `capture` (front camera, frame grab, resize before upload), with `<input type="file" accept="image/*" capture="user">` as the fallback; products by photo, camera or link; Chaaya resumable upload with progress. Clear refusals: no face, too small, uploads paused.

**Done when:** a selfie from a phone's camera and one from the gallery both upload; a dropped connection resumes.

---

### T5.4: Progress and the verdict ★
```yaml
requires:   T5.3
fixture-ok: yes
size:       M · mid
owns:       web/src/routes/look/verdict/
status:     not-started
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
status:     not-started
```
Sign in by emailed code (looks kept); delete a photo, a look, history, or the account (fresh code, then progress until every part confirms). What YouCam and Cloudflare receive, said plainly.

**Done when:** account deletion shows progress to the end on mock data; the privacy text matches product.md.

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
