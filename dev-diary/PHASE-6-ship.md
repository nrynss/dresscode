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
status:     done
```
`dresscode` and the Dresscode Lambo server as systemd user units that restart on failure and start at boot; the built app served by `dresscode`; a public hostname through the existing Cloudflare Tunnel (HTTPS, which the camera and install need). Nightly backup of the SQLite file and media. A health check that fails the deploy if the public URL doesn't answer.

Bound every long-running process the deploy starts with `timeout` or a unit; never a bare background job.

**Done when:** the public URL serves the app on a phone; a reboot brings everything back; the backup ran once by hand.

Notes (T6.1 implementer):
- What exists: `deploy/` holds both unit files, the tunnel ingress config, `health-check`, `backup`, the backup service and timer, and a README with the install order, update steps and rollback. The machine-side install lives in `/home/nryn/dresscode-run/` (env mode 600, the derived `dresscode.toml`, `data/` copied from `testdata/{occasions,samples,youcam}` so the samples mount finds its sibling `youcam/results`, the built app in `web/dist/`, scripts in `bin/`), the binary in `~/.local/bin/dresscode`, units and timer in `~/.config/systemd/user/`, the tunnel config at `~/.cloudflared/config-dresscode.yml`.
- The deploy follows the machine's own Lambo pattern: user units, `EnvironmentFile`, `Restart=on-failure`, `WantedBy=default.target`, linger already on. The tunnel is a second, user-level cloudflared on the unused credential; the system tunnel was not touched, and nothing needed sudo.
- Port: 8090 was already held by an unrelated user process, so the service binds `127.0.0.1:8091` and the tunnel ingress points there. Health check hits both the public URL and the local port, page and samples list, and never dials a provider.
- Proofs: `https://dresscode.nryn.dev` answers 200 with the SPA HTML, and `GET /api/samples` through it lists the five samples (recorded data only); SIGKILL of the main process saw the unit restart (NRestarts 0→1) and the health check pass again; reboot-bring-back is verified by config (enabled units + `default.target` + `Linger=yes`), not by an actual reboot; `deploy/backup` ran once by hand and `PRAGMA integrity_check` passed on the copy; no env file in any backup.
- Surprises: the app build writes `web/build`, while `serve`'s default `-app` is `web/dist` — the unit passes `-app` explicitly, so the deploy is unaffected, but a bare `dresscode serve` from a checkout will not find the built app. Keel's mediastore keeps blobs as files under `data/media` (plus `upload-staging`), so the backup captures the store file and every sibling directory, not the DB alone.
- The phone clause is met, by the user's own eyes: the user opened the public URL on their phone and captured a selfie through it (their report to the orchestrator; capture worked, and the two dead ends they then hit are product defects chartered as T6.1b, not deploy defects). Reboot bring-back stands verified by config — enabled units, `WantedBy=default.target`, `Linger=yes`; an actual reboot was deliberately not performed on a live machine.
- Review round 1: two minor findings, both closed by the orchestrator under the docs exception at landing — the installed copy of the backup script carried a stale pre-correction comment (reinstalled from the commit, now byte-identical), and the phone clause was unrecorded (the line above). Everything else verified clean, including the reviewer's own gate run and its read-only zero-spend audit of the store.

---

### T6.2: README and the judges' table ★
```yaml
requires:   T6.1, T3.9
fixture-ok: yes
size:       S · mid
owns:       README.md
status:     done
```
First screen: what Dresscode does, the YouCam APIs it uses and why each, the public URL, and a **"For judges: start here"** table (the URL, the samples, the video, the evaluation numbers). Then setup from a fresh clone, keys by name, the architecture in one picture, Keel, Chaaya and Lambo named as dependencies, and the licence.

**Done when:** a fresh clone runs from the README alone with replay settings and no keys.

Notes (T6.2 implementer):
- What the README now carries, in order: the pitch; the judges' table (the URL with the free-sample and lease mechanics, the five cached stories with the AI-generated note product.md requires the README to say, the video as a row with no dates, the evaluation numbers all labelled); a YouCam API table naming all eight wired tasks with their set, reason and unit price (`2d-vto/earring`, `2d-vto/necklace`, `makeup-vto`, `cloth-v4`, `hair-transfer`, `beard-style`, `ai-smile`, `face-attr-analysis`) — proven against docs/youcam.md's measured unit table and the task names in `testdata/occasions/*.json` and `internal/pipeline/measure.go` (the only analysis actually wired is `face-attr-analysis`, face shape only); a line naming what is deliberately not YouCam (Clef on Workers AI judges, picks and checks the render); keyless setup from a fresh clone (the gate, the offline evaluation replay, the build, `version`); a keys-by-name table pointing at docs/keys.md, with `serve`'s refusal stated honestly; the architecture diagram, adapted from docs/architecture.md; Keel, Chaaya (`@nrynss/chaaya`) and Lambo named as published dependencies; Apache-2.0.
- Decisions: the biggest-issue section is quoted only through its two labels — the service's raw choice 28 of 37, gated on the look's own flags 32 of 37 — because T3.13 made the section carry both reads and a naked number would be wrong twice. The evaluation row says outright that these are evaluation numbers, not product output, so no judge reads a probability as something the app shows. The old README's "nothing below is implemented yet" block was deleted as false since T6.1 landed. Dependency links are only asserted where the tree proves them (Keel via go.mod, Chaaya via the npm name in web/package.json); Lambo is named without a URL because no published path is proven in this tree. `web/build` vs `web/dist` (T6.1's surprise) is stated so a stranger's build lands where `pnpm build` actually puts it.
- The fresh-clone proof was run as the stranger would: `git clone` of the branch into an empty directory (no `.env`, no `dresscode.toml`, no `data/`, no `node_modules`), then the README's own steps — `./tools/check.sh` green end to end, `go build ./cmd/dresscode`, `go run ./cmd/dresscode version` (`dresscode 0.0.0`), and `go test ./internal/eval/ -count=1` green (49.8s), the offline replay of the whole evaluation with no key and no network. A stranger does need one unproofed thing: the clone URL on GitHub, which cannot resolve until the repository goes public.
- Surprise worth the next task's attention: the first gate attempt in the clone FAILED — `internal/eval` hit go test's default 10-minute timeout under `-race`. The re-run with nothing else on the machine passed at 536.9s, twenty-three seconds under the limit, so the margin is real but thin: the offline replay costs about 50s plain and about 9 minutes under the race detector on this 12-core machine. My own concurrent foreground run caused the first failure, but the ceiling is the default, not my run — if the gate ever runs on a smaller machine or a loaded one, `internal/eval` is the package that flips it red. A `-timeout` on the eval test invocation, or splitting the double replay the test runs, is a one-line fix for whoever owns that package next.
- The `serve` clause of the charter is met as refusal, not as boot: `dresscode serve` deliberately refuses without keys (cmd/dresscode/serve.go refuses before anything opens), so the README says what a keyless fresh clone can run — the gate and the offline replay — and what a keyed one adds, rather than pretending a keyless serve exists.

---

### T6.3: Video script
```yaml
requires:   T6.1
fixture-ok: yes
size:       S · frontier
owns:       docs/video-script.md
status:     in-review
```
Under three minutes, on a phone, the real hosted app. The second visit is the spine: first visit (two criteria missed, the jhumkas kept, the red lip dismissed), then the return and what memory changed. Name each YouCam API as it works. One evaluation number, said early. No brand logos, no third-party footage, no music without permission.

**Done when:** the script and shot list are committed and the user has read them.

Notes (T6.3 implementer):

- What exists: `docs/video-script.md` — 15 shots, 168 seconds of cut, each shot naming the screen, the exact taps, the caption text and the voiceover line, then the recording checklist with the preconditions and the clean-screen rules.
- The decision that shaped the steps: the sample path carries no memory. `internal/server/samples.go` replays a recorded story (judge, pick, measure, render, check, shop) with "a sample carries no memory", and its idempotency rule answers a second open of the same sample with the same finished look — so no judge and no recording order can make the sample drawer show a second visit. The spine therefore runs on three real looks (earring keep, lip dismiss, second earring look) with style history on; the sample drawer is only the closing free-for-judges beat. Paid cost of the whole video: 3 YouCam units (earring 1, makeup 1, earring 1, per docs/youcam.md); judge and check calls are Workers AI, not units.
- The one number: 269 of 273 (98.5%) from docs/evaluation.md, said in shot 1 and never repeated. Every screen word in the script (tab names, "Fixes to try", "Check look", "Dress code notes", "A fix", "See the render", "Shop the look", the Keep/Dismiss/Bought-it bar, the recall banner's exact sentence) was read out of the route components and the pipeline, not invented.
- Surprise: the verdict's `remembered` field is not surfaced anywhere in the UI — the only visible confirmation of a verdict is the button filling as the stored one — so the checklist's truth-check for the memory write is "open the Occasion tab and confirm the banner" after the dismissal, before shooting the second visit.


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
