# P6: Ship

```yaml
id:       P6
size:     M
requires: [P3, P4, P5, P5B]
blocks:   submission
parallel: none
```

**Goal:** A public URL judges can use for free through judging, a README that leads them in, a 1–3 minute video on a phone, and the submission.

Anything outward is the user's call: making the repository public, publishing the video, submitting, upgrading a plan. The orchestrator asks each time.

---

### T6.1: Deploy on this machine ★
```yaml
requires:   T4.6, T5.6, T5.23
fixture-ok: no
size:       M · mid
owns:       deploy/
status:     claimed:zcode-t61
```
`dresscode` and the Dresscode Lambo server as systemd user units that restart on failure and start at boot; the built app served by `dresscode`; a public hostname through the existing Cloudflare Tunnel (HTTPS, which the camera and install need). Nightly backup of the SQLite file and media. A health check that fails the deploy if the public URL doesn't answer.

Bound every long-running process the deploy starts with `timeout` or a unit; never a bare background job.

**Done when:** the public URL serves the app on a phone; a reboot brings everything back; the backup ran once by hand.

---

### T6.2: README and the judges' table ★
```yaml
requires:   T6.1, T3.9
fixture-ok: yes
size:       S · mid
owns:       README.md
status:     not-started
```
First screen: what Dresscode does, the YouCam APIs it uses and why each, the public URL, and a **"For judges: start here"** table (the URL, the samples, the video, the evaluation numbers). Then setup from a fresh clone, keys by name, the architecture in one picture, Keel, Chaaya and Lambo named as dependencies, and the licence.

**Done when:** a fresh clone runs from the README alone with replay settings and no keys.

---

### T6.3: Video script
```yaml
requires:   T6.1
fixture-ok: yes
size:       S · frontier
owns:       docs/video-script.md
status:     not-started
```
Under three minutes, on a phone, the real hosted app. The second visit is the spine: first visit (two criteria missed, the jhumkas kept, the red lip dismissed), then the return and what memory changed. Name each YouCam API as it works. One evaluation number, said early. No brand logos, no third-party footage, no music without permission.

**Done when:** the script and shot list are committed and the user has read them.

---

### T6.4: Video ★
```yaml
requires:   T6.3
fixture-ok: no
size:       M · user
owns:       none
status:     not-started
```
The user records on a phone and uploads publicly to YouTube. The faces in the app are the AI sample people (T0.4), and the video says they are generated.

**Done when:** the video is public and under three minutes.

---

### T6.5: Submit ★
```yaml
requires:   T6.2, T6.4
fixture-ok: no
size:       S · user
owns:       docs/submission.md
status:     not-started
```
An agent drafts every field in `docs/submission.md`: the description (features, functionality, consumer and retail value), the YouCam APIs used, the URL, the repository (public with its licence, or private and shared with the organiser), the video, and any test notes judges need. The user reviews and submits, and claims the share-and-earn bonus units if wanted.

**Done when:** Devpost shows the submission as complete.

---

### T6.6: Judging posture
```yaml
requires:   T6.1
fixture-ok: no
size:       S · user
owns:       docs/judging.md
status:     not-started
```
Through the judging period (product.md gives both published windows; plan for the longer): Workers Paid on before judging starts; YouCam units checked against the judging share; the flags ready if units run low; the machine and tunnel up; the recorded video as the fallback if the connection drops. After judging: cancel Workers Paid if it isn't wanted.

**Done when:** `docs/judging.md` holds the checklist, and the user has upgraded Workers AI.

---

### T6.1b: The first real user could not reach a render
```yaml
id:         T6.1b
title:      The product-photo picker never loses a pick, and a suggestion-only look says what to do next
owns:       web/src/routes/capture/capture.svelte, web/src/routes/capture/upload.ts, web/src/routes/capture/upload.test.ts, web/src/routes/look/verdict/
requires:   T5.23
fixture-ok: no
size:       S · mid
status:     claimed:zcode-t61b
```
The deployed app met its first real user (the author's own phone, their own selfie, the funeral occasion) and the promise dead-ended twice. First: the judge ran live and correctly (suggestion-worded flags, no scores — the pipeline's behaviour was exactly per product), but with zero fixes ticked open the look ended suggestion-only, and the screen offered nothing — no reason, no next step, no way to proceed. The opt-in ticks exist on the occasion screen, but a look started with none ticked is a silent dead end. Second: on the product path, choosing a photo before the product's name and kind are typed swallows the pick — `onGallery` reads the FileList, resets the input (T5.22's reset is correct and stays), and only then gates on `productReady()`, so the refusal lands after the photo is already voided, and the message "Name the product and what it is." arrives too late to mean anything. The store confirms both: the look row has empty fix/runner-up/verdict, `opt_ins` and `products` have no rows for the attempt.

**Done when:**
- The product path never loses a chosen photo: the gate is answered before the pick is consumed (or the flow collects the pick first and asks for name and kind after), the refusal names what is missing while the user can still act on it, and once the gate passes the already-chosen photo completes the product without a re-pick.
- A suggestion-only verdict is not a dead end: when no fix was eligible because nothing is ticked open, the screen says so in the product's voice and offers the one-tap path to that occasion's fix menu; the ticks are reachable from the verdict.
- The opt-in semantics do not move: nothing is ticked by default, nothing is inferred, no pre-ticks to make the demo march.
- Tests pin both: the refused-pick path (pick with the fields empty behaves per the chosen design and never silently discards), and the suggestion-only screen renders the path forward. `./tools/check.sh` green.

Notes:

- Redeploy (rebuild the bundle, restart the unit per `deploy/README.md`'s own steps) follows landing; it is not this task's proof.
- Nothing about the user — no photo, no device detail, no account id — goes into any commit, test or diary line. The findings are described by behaviour only.
