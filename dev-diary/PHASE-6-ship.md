# P6: Ship

```yaml
id:       P6
size:     M
requires: [P3, P4, P5]
blocks:   submission
parallel: none
```

**Goal:** A public URL judges can use for free through judging, a README that leads them in, a 1–3 minute video on a phone, and the submission.

Anything outward is the user's call: making the repository public, publishing the video, submitting, upgrading a plan. The orchestrator asks each time.

---

### T6.1: Deploy on this machine ★
```yaml
requires:   T4.6, T5.6
fixture-ok: no
size:       M · mid
owns:       deploy/
status:     not-started
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
