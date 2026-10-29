# P4: Server

```yaml
id:       P4
size:     L
requires: [P1]
blocks:   P6
parallel: P3, P5
```

**Goal:** The `dresscode serve` binary: accounts, intake, the look routes, deletion, budgets and the samples judges open, all to the T1.5 contract.

Accounts (T4.1–T4.3) need only P1 and run beside the pipeline. The look routes (T4.4) wait for T3.6.

---

### T4.1: Guests and sessions ★
```yaml
requires:   T1.2, T1.5
fixture-ok: yes
size:       M · mid
owns:       internal/server/server.go, internal/server/session.go
status:     not-started
```
The HTTP server on Keel, serving the API and the built app. Keel `identity`: a first visit creates a guest user and a server-side session; the cookie carries only the signed session id; every request resolves it against the rows. `gate` in front of every route that spends.

**Done when:** a new visitor gets a guest; revoking a session ends it on the next request; spending routes refuse past the gate.

---

### T4.2: Sign-in by emailed code ★
```yaml
requires:   T4.1, T2.4
fixture-ok: yes
size:       M · mid
owns:       internal/server/signin.go
status:     not-started
```
Keel `identity` code sign-in with the Resend sender: start (email in, code sent, send limits refuse before sending), verify (code in, session upgraded). **Signing in keeps the guest's looks:** the guest's rows attach to the account; when the address already has an account, the guest's looks are added to it, never overwrite it. No Google sign-in.

**Done when:** tests cover a fresh sign-in, a returning account with guest looks merged, a wrong code, an expired code and the send limit.

---

### T4.3: Deletion ★
```yaml
requires:   T4.1, T2.3
fixture-ok: yes
size:       M · mid
owns:       internal/server/delete.go, internal/privacy/
status:     not-started
```
Delete a photo, a look, the style history, or the account. Account deletion needs a code entered in the last 10 minutes and runs as one Keel `erase` job over every target: mediastore blobs, every user-data table T1.2 listed, the identity rows, and the account's Lambo session. Safe to repeat; resumes after a restart; reports when every target confirmed.

**Done when:** a test deletes an account mid-way, restarts, and finishes with nothing left in any target; the T1.2 table list and the erase targets are checked against each other.

---

### T4.4: The look routes ★
```yaml
requires:   T3.6, T4.1
fixture-ok: yes
size:       M · mid
owns:       internal/server/looks.go
status:     not-started
```
Start a look (occasion, photo, products), its event stream, the verdict, renders, shop the look, keep / dismiss / bought, history on/off. Each paid start goes through the judge's `lease` and the budgets; a refusal is a typed error the app can show.

**Done when:** every look route in T1.5 answers with its recorded example's shape on replays.

---

### T4.5: Intake: upload, camera, links, share
```yaml
requires:   T4.1
fixture-ok: yes
size:       M · mid
owns:       internal/server/intake.go
status:     not-started
```
Resumable uploads through Keel `upload` into `mediastore`; every photo normalised by Keel `photo` (orientation applied, EXIF and GPS stripped, resized to YouCam's 1024 px and Clef's 4 MiB) and hashed for the cache. Product links through Keel `fetch` (SSRF-safe, `og:image`); a blocked retailer falls back to "upload a photo instead". The share-target intake for installed apps.

**Done when:** a photo with GPS comes out without it; a link to a private address is refused; a blocked fetch returns the fallback code.

---

### T4.6: Samples for judges ★
```yaml
requires:   T4.4, T3.9
fixture-ok: yes
size:       S · mid
owns:       internal/server/samples.go, testdata/samples/cached/
status:     not-started
```
The sample selfies with their analysis, judgements and renders already cached, so opening a sample spends nothing. A judge can still take a fresh selfie within the lease.

**Done when:** opening every sample spends zero units and zero Workers AI calls, shown by the meter.

---

### T4.7: Budgets and flags in operation
```yaml
requires:   T4.4
fixture-ok: yes
size:       S · light
owns:       internal/server/ops.go, cmd/dresscode/ (the ops commands)
status:     not-started
```
`dresscode budget` prints units and dollars spent and left; `dresscode flag` flips fresh uploads, renders and samples-only without a restart. The judging share of units is refused to everything but judge leases.

**Done when:** flipping "fresh uploads off" takes effect on the next request; the judging share holds under a test that tries to spend it.
