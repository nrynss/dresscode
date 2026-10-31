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
status:     done
```
The HTTP server on Keel, serving the API and the built app. Keel `identity`: a first visit creates a guest user and a server-side session; the cookie carries only the signed session id; every request resolves it against the rows. `gate` in front of every route that spends.

**Done when:** a new visitor gets a guest; revoking a session ends it on the next request; spending routes refuse past the gate.

Notes:

- `internal/server` opens one SQLite file, migrates the store schema, then Keel identity, and serves one handler. `GET /api/session` returns `user_id` and `kind`. A first visit mints a guest and a session row. The cookie is `identity_session`, `HttpOnly`, `Secure`, `SameSite=Lax`, and the value is the session id plus its signature. The body does not carry the session id. A second visit with that cookie resolves the same user and does not mint another session. The same cookie still resolves after the process closes and opens again on the same file and the same key.
- A revoked row does not resolve. Keel's middleware treats that as no session and mints a new guest, so the next response is a different user with a new cookie. The revoked row stays revoked. Deleting the session row does the same. A bad signature does not resolve the user, and neither does a different signing key.
- `POST /api/looks` is the spending route. It sits behind Keel `gate`. A refusal is `429` `rate_limited` and the handler is not called. A refusal takes no token: after one interval the next request is admitted. With no look handler installed, an admission is `not_found` and no look row is written. `SetSpendHandler` with `RouteLook` installs the handler. The look route itself is T4.4. The default limit is 8 starts per client, then one every ten minutes, and 40 for the process, then one every minute. `Config.LookPerClient` and `Config.LookGlobal` override that. Those numbers bound requests, not credits. `POST /api/samples/{id}/looks` is not mounted and is not gated: after the look gate is exhausted it is still `not_found`, not `rate_limited`.
- The built app is `Config.AppDir`. `GET /` and an unknown non-API path serve `index.html` with `Cache-Control: no-store`. A real file is served as itself. An `/api/` miss is the `not_found` envelope, so sign-in and deletion are not mounted. An `/media/` miss is the plain `404 page not found` text with `Cache-Control: private, no-store`, so the app shell is not returned for a missing blob.
- The gate trusts forwarding headers only from loopback and private peers, which is what a tunnel on this box presents. A public peer cannot mint a bucket with `X-Forwarded-For`. The global bucket is the ceiling. No passcode.
- `cmd/dresscode` still does not listen. This task does not own it. `Open` takes the database path, the app directory, and the signing key bytes. No signing-key variable is named in `docs/keys.md`, so this task does not invent one. `identity.New` has no mail sender and no code key. T4.2 has to pass both when it mounts sign-in, and a guest-data check before a switch can strand looks. T4.4 calls `SetSpendHandler` before the process listens.
- Surprised: Keel mints a fresh guest on a revoked cookie instead of answering 401. That matches the contract, which creates a guest whenever the cookie does not resolve. A session fault inside Keel uses Keel's own sentence, not the fixture's `The request could not be finished.`

---

### T4.2: Sign-in by emailed code ★
```yaml
requires:   T4.1, T2.4
fixture-ok: yes
size:       M · mid
owns:       internal/server/signin.go
status:     done
```
Keel `identity` code sign-in with the Resend sender: start (email in, code sent, send limits refuse before sending), verify (code in, session upgraded). **Signing in keeps the guest's looks:** the guest's rows attach to the account; when the address already has an account, the guest's looks are added to it, never overwrite it. No Google sign-in.

**Done when:** tests cover a fresh sign-in, a returning account with guest looks merged, a wrong code, an expired code and the send limit.

Notes:

- `MountSignIn` serves `POST /api/session/code` and `POST /api/session/code/verify` with Keel's code handler. `Open` still leaves them unmounted, which is what the T4.1 miss test expects. The caller passes an `identity.Sender` and the code key. `mail.Client` is that sender for Resend. Tests pass `mail.Recorder` and do not dial.
- `Open` builds identity with no sender and no code key, and Keel v0.9.0 does not set either later. `MountSignIn` opens a second identity service on the same database, with the sender, the code key, and the look check. The session middleware stays the one `Open` built. `SigningKey` is the key `Open` used, because verify signs the rotated cookie and that middleware checks the signature.
- The look check is one row in `looks` for this user. A new address upgrades the guest in place, so the user id does not change and the look stays. An existing account, when this guest has a look, is `guest_data_conflict` and the code stays live. `choice` `merge` is `invalid_request`. `choice` `switch` moves the device onto the account. The guest's look stays on the guest. The account's look stays on the account. Nothing is copied. A guest with no looks joins the account on an empty choice, and no look row appears.
- The send ceilings are Keel's. Five codes to one address are accepted. The sixth is `send_limited` with `Retry-After` of the 24 hour window, and the recorder still has five messages.
- No Google route is mounted. A wrong code and a code past ten minutes on the injected clock are both `invalid_code`, and the guest is unchanged.
- The older sentence in this task said a returning account gains the guest's looks. `docs/api.md` wins. Keel v0.9.0 `VerifySignInCode` accepts only an empty choice or `switch`, and `switch` does not move rows. The returning-account test pins that.

---

### T4.3: Deletion ★
```yaml
requires:   T4.1, T2.3
fixture-ok: yes
size:       M · mid
owns:       internal/server/delete.go, internal/privacy/
status:     done
```
Delete a photo, a look, the style history, or the account. Account deletion needs a code entered in the last 10 minutes and runs as one Keel `erase` job over every target: mediastore blobs, every user-data table T1.2 listed, the identity rows, and the account's Lambo session. Safe to repeat; resumes after a restart; reports when every target confirmed.

**Done when:** a test deletes an account mid-way, restarts, and finishes with nothing left in any target; the T1.2 table list and the erase targets are checked against each other.

Notes:

- `internal/privacy` is the deletion engine. `Source` is the Keel erase source and the identity target source. The inner erasure registers `files`, `rows`, and `memory`. `files` removes mediastore blobs owned by the user. `memory` erases that account's one Lambo session (`dc-u-` plus the user id) and treats already-absent as confirmed. `rows` deletes every table `store.UserTables` names, then the sign-in codes, sessions, identities, and the user row, in that order. `RowTables` is `UserTables`. Identity table names are not in that list. Keel deletes the identity rows again after the inner erasure confirms, which is when `rows_done` becomes true. A second pass deletes nothing.
- Photo, look, and history deletion are Dresscode jobs of kind `deletion`, stage `erase`, with `current`, `total`, and `detail.targets` / `detail.confirmed`. The record also keeps the user, the operation, and the blob and look ids so a restart can finish. Photo deletion removes that blob and the looks that used it, including their render blobs and the analysis cache when no look still uses the hash. Look deletion removes that look and its render blobs and leaves the selfie. History deletion erases the session and deletes the switch row. Looks stay. A repeat, or a resource this user does not hold, is `ErrAbsent`.
- `Server.MountDeletion` mounts `POST /api/account/deletion` as Keel's `DeleteHandler`, plus the three Dresscode routes and `GET /api/jobs/{id}` and its event stream. The signing key has to be the one `Open` used. The code's ten minutes are Keel's. A caller who does not hold the address, including a guest, gets `not_found` from that handler. The response clears the session cookie. The deletion job id is the capability for that job's state and stream. The inner erase job is not readable. `Open` does not call `MountDeletion`: `server.go` is T4.1's, and its test expects these paths to miss until something mounts them. `cmd/dresscode` still does not listen.
- Tests use fakes for the blob store and for the Lambo target. Nothing dials.
- Surprised: Keel's account-deletion watch measures its minute on the injected clock and sleeps on the wall clock. A frozen clock does not trip it. The live subscriber to a job stream is not closed by the terminal frame. The handler stays up until the client leaves. A late subscriber still gets a retained terminal.

---

### T4.4: The look routes ★
```yaml
requires:   T3.6, T4.1
fixture-ok: yes
size:       M · mid
owns:       internal/server/looks.go
status:     in-review (co-coder: qoder)
```
Start a look (occasion, photo, products), its event stream, the verdict, renders, shop the look, keep / dismiss / bought, history on/off. Each paid start goes through the judge's `lease` and the budgets; a refusal is a typed error the app can show.

**Done when:** every look route in T1.5 answers with its recorded example's shape on replays.

Notes:

- The first implementer died mid-task. A second finished it on the same branch as co-coder. `MountLooks` registers `GET /api/looks`, `GET /api/looks/{id}`, `.../renders`, `.../shop`, `PUT .../verdict`, `GET`/`PUT /api/history` and `GET /api/history/recall`, and takes `POST /api/looks` through `SetSpendHandler(RouteLook)` so the spend gate wraps the start. `GET /api/jobs/{id}` and its stream are one pair of paths shared with deletion: `jobs.go` registers them once and dispatches by the job's kind, so either mount may come first and each runtime keeps its own visibility. `Open` still does not call `MountLooks`.
- The look id is the job's root id, so one id reads the outcome and drives the stream. A start answers 202 with `look_id` and `job_id` both set to it. The ids in the recorded examples are the fixture's own; the replay mints its own, and the tests rename rather than pin an id.
- Every route is compared against its T1.5 example: list, one look (settled and pending), renders, shop, the three verdicts, history on and off, recall (a hit and a first visit), job state (done, error, and mid-check), and all four event streams. The gate is green: `gofmt`, `go vet`, `go test -race ./...`, and the app's lint, type check and tests.
- Wiring P3 deferred to this task, all four in place: the config thresholds are projected onto the look's occasion (`thresholdsFor`, also on resume, because the judge refuses an override for a criterion the occasion does not name); T3.8's two call sites, `RecallFor` at the start and `Remember` at the verdict with `remembered = err == nil`, so a failed remember still stores the verdict; T3.7's decision (b), the server joins catalog media onto the shop frame, the shop route and the look body (`joinProducts`), so the pipeline never reads a blob; and `renders_paused`, a `Deps` hook read once per attempt, which refuses the render and not the look.
- `renders_paused` fails closed: a flag read that errors pauses renders. A refusal, budget or switch, leaves the suggestion standing, runs shop and reaches `done`. No unit is quoted for a refused render, no check is asked, and the hold is released.
- The judge's lease is taken after the budget admits the start, so a budget refusal never seats a judge. A start whose keep fails cancels its job and hands the seat back, because it delivered nothing. A second `job.Open` over the same store does not wait for any lease: it recovers at once, interrupts the unfinished attempt and starts its replacement. The killed-look test cancels the dead attempt after the restart so it stops writing; the paid-step keys are what keep that brief window of two attempts from spending twice. The resume asks Lambo nothing: recall rides in the snapshot.
- The snapshot lives in `look_snapshots`, in the store's migrations, keyed by the look with `ON DELETE CASCADE`, and listed in `store.UserTables()`, so account deletion and look deletion both take it. `Keep` runs in the same transaction as the look row. `judge_sessions` stays session-keyed in the `lookserver` namespace and holds no user rows.
- Grants beyond `owns`, each needed to land the wiring: `internal/store/store.go` and `internal/store/migrations/0007_look_snapshots.sql` (the table), `internal/store/store_test.go` and `internal/privacy/privacy_test.go` (their seeders and table lists learn the table), `internal/pipeline/run.go` (`RendersPaused`, the refusal's code and phrase, and `noFaceCode` on a render failure), `internal/server/looks_test.go` (new), `internal/server/server_test.go` (a mutex on the test clock: the upload sweeper reads it from its own goroutine, a race the longer parallel run exposed), `dresscode.toml.example` (T3.9's measured diff, as proposed), `internal/config/load_test.go` and `internal/config/settings.go` (the tests were anchored to the placeholder literals; the comment now cites the evaluation), `testdata/api/job-error.json` (the stage, below), and `docs/api.md` (the same correction in prose).
- Contract correction, with its evidence: `no_face_in_photo` does not end the job during `judge`. The judge is a Clef ask with no face detector, so the refusal comes from the provider step that ran. Each stage frame is published when that step *finishes*, so the state names the last step that finished: `measure` for the recorded smile fix, which names no analysis. The example and `docs/api.md` were corrected to `measure`. T3.6 had already flagged api.md's wording for this task. The only other reader of that example, `web/src/lib/api/errors.test.ts`, asserts the code and not the stage.
- For the user: the screen word lags one step, because a frame is published on completion. The phone reads "Choosing a fix" through the whole render, which is the longest step. Emitting `render` when it begins would fix the words, but P3's tests pin one render frame per attempt and `run.go`'s order. That is a contract decision, not this task's.
- Deviations from the examples, named in the test file's header: the look in words reads "jhumkas, gold" where the fixture composes "gold jhumkas" (T3.5's phrase-table tension, third instance, a data decision); the suggestion story's runner-up is the red lip rather than the festive top, whose cloth task composites a product photo, because one look may name one catalog product; the simulation's shop name copies the fix's changes wording, as no occasions field carries a display name for a fix (already flagged at T3.7's notes); the heartbeat comment cannot ride a replay, so frames are compared without comment lines; and the error example records the error frame alone, so the stages before the fault are pinned by name and the example is compared against the terminal frame.
- Surprised: Keel's `ServeTopic` registers the subscription before it writes the header, and a topic holding a retained terminal gives a subscriber with no cursor *only* that terminal. `Last-Event-ID: 0` is not a cursor either. So a stream test must subscribe while the job is held and release once the headers arrive; reading to EOF instead waits out the handler, which stays up after `done`. That was 30 s per stream test before the rig parked the judge ask and stopped at the terminal frame.
- Nothing dialled and no unit moved: YouCam is the typed replay of `testdata/youcam`, Clef is the stub, the budget and the lease are the rig's own. The earring story creates one task and settles one unit; the suggestion story creates two and settles two.

Review round (one pass, read-only reviewer; the co-coder fixed what it found):

- Must-fix, the shared job routes. `MountLooks` claimed the job pair only when deletion was absent, but `MountDeletion` registered the same two patterns unconditionally: looks-first panicked the mux on a duplicate pattern, deletion-first left every look job read and stream answering `not_found`, and the delegation written for the shared case was unreachable either way. Fixed in a new `internal/server/jobs.go`: the pair is registered once, by whichever mount asks first, and dispatched by the job's kind to the runtime that owns it. `delete.go` loses its two registrations and gains `deletionOf()` and a `srv` field; `looks.go` loses its delegation block. Neither mount order is special now, and a test mounts both in both orders and reads a look and a deletion through each.
- Must-fix, cross-kind recovery. Two runners share one `jobs` table, and Keel's `Open` recovery interrupts *every* unfinished row it is shown but resumes only its own kinds. Whichever mounted first therefore interrupted the other's in-flight jobs and left them interrupted with no replacement — an account deletion stranded mid-erase. Keel's `jobsql` namespace names only the migration ledger, not the table, so the stores cannot be separated, and `job.Runner` has no way to add a kind after `Open`. Fixed with `kindStore`, a `job.Store` wrapper that filters `Unfinished` to the runner's own kinds; every other read is by id or by root, which one kind owns. Both mounts wrap their store. A test seeds a running row of each kind and proves each restart interrupts its own and leaves the other running.
- Should-fix, all done: the stream writer now reports `io.ErrShortWrite` when a rewritten frame lands short instead of claiming the whole frame; a start whose keep fails cancels its job and hands the judge seat back (it delivered nothing, and the paid render must not run for a look no route can read); the four cancelled and interrupted examples are now compared, state and stream; `cut()` in the config tests really does take a sub-table with its parent, as its comment claimed; and the hand-rolled `fs.FS` for one migration string is gone, replaced by `go:embed` of `internal/server/migrations/0001_look_runtime.sql` (the same filename, so the ledger is unchanged). The `Picture` seam now checks the blob's owner: a picture that is not the user's falls through to the catalog seam, the only picture a look may read that is not their own.
- Two more fixture corrections, both to `testdata/api`: the cancelled and interrupted streams recorded a bare `judge` frame with none of the detail the judge publishes, which contradicts the contract's own table. They now carry the flags the replay's judge answers, so both streams are compared frame for frame instead of terminal-only. The state bodies were already exact.
- Grants for this round, beyond `owns`: `internal/server/jobs.go` (new), `internal/server/delete.go` (the shared registrations, `deletionOf`, the `srv` field, the wrapped store), `internal/server/migrations/0001_look_runtime.sql` (new, the embedded schema), `internal/server/looks_test.go`, `internal/config/load_test.go`, `testdata/api/job-events-cancelled.json`, `testdata/api/job-events-interrupted.json`.
- Left for the composition task, with the reason: `Server.Close` does not stop a job runner, because Keel's `job.Runner` has no `Close` and cancelling a look on shutdown would record `cancelled`, which is terminal and never resumes, where a restart's `interrupted` is. The process that listens should stop taking requests and let in-flight looks finish, or rely on the restart recovery. Also still open, and not this task's to decide: the screen word lags one step because a stage frame is published on completion; the words order, the suggestion runner-up and the simulation's display name are the data decisions named above.
- The gate is green again after the round: `gofmt`, `go vet`, `go test -race ./...`, and the app's lint, type check and tests.

---

### T4.5: Intake: upload, camera, links, share
```yaml
requires:   T4.1
fixture-ok: yes
size:       M · mid
owns:       internal/server/intake.go
status:     done
```
Resumable uploads through Keel `upload` into `mediastore`; every photo normalised by Keel `photo` (orientation applied, EXIF and GPS stripped, resized to YouCam's 1024 px and Clef's 4 MiB) and hashed for the cache. Product links through Keel `fetch` (SSRF-safe, `og:image`); a blocked retailer falls back to "upload a photo instead". The share-target intake for installed apps.

**Done when:** a photo with GPS comes out without it; a link to a private address is refused; a blocked fetch returns the fallback code.

Notes:

- `Open` mounts Keel upload at `/api/uploads`, `POST /api/photos`, `POST /api/products`, `POST /api/products/link`, and `GET /media/{id}`. The two calls in `server.go` (`mount` and `Close`) are the wiring. The mux is built there, and intake cannot register routes without them. Blobs sit in `media/` beside the database. Chunks stage in `upload-staging/`. `Close` stops the upload sweep.
- The open body is rewritten before Keel sees it. Keel stores the body's owner and visibility. Dresscode sets the owner to the session user, forces `private`, and defaults an omitted group to `selfie`. `group` must be `selfie` or `product`. A declared size may be omitted. `fresh_uploads_off` refuses a selfie open and `POST /api/photos` with `uploads_paused`. A product open still proceeds. `samples_only` refuses uploads, photos, products, and links, and it wins when both switches are on. The flags are Keel `flag` rows (`fresh_uploads_off`, `samples_only`), read on the request.
- `POST /api/photos` and `POST /api/products` normalise with Keel `photo`: long side 1024, output cap 4 MiB, JPEG. Orientation is applied first, so a wide image tagged orientation 6 comes out tall. The re-encode drops EXIF, including GPS. The response `sha256` is the normalised bytes and is the analysis cache key. No analysis row is written here. The same pixels with different GPS text hash the same. The raw blob is deleted on success and on a photo refusal, so a rejected upload does not keep the GPS. A photo whose longer side is under 64 px is `too_small`. There is no face detector on this path, so `no_face_in_photo` is left to the look job. The product id is the normalised media id. `photograph` stays empty. A photo product is `exact product`. A link product is `approximate match`. `origin` is `user`.
- A product link uses Keel `fetch` with the default address policy. `https://10.1.2.3`, `https://192.168.9.4`, and `https://127.0.0.1` answer `fetch_blocked_address` with `That link was refused.` and the body does not contain the address. `http` is `fetch_bad_scheme`. A page with no `og:image` (a block page included) is `upload_a_photo`. A non-2xx page stays `fetch_bad_status`, because Keel does not distinguish a block from any other failed status. The preview image is fetched again under the same policy and normalised the same way. `og:title` is the name. `product:category` is the category. No retailer host list was named, so none was invented.
- Share intake has no API path, as the contract says. `GET /share` is the app shell. The app reads the share on the device and calls the link path or the upload path. The camera uses that same upload. A private media miss stays the plain 404.
- One upload is capped at 64 MiB and one owner at 512 MiB across uploads this process is holding. `image/heic` is accepted into the store so the photo path can answer `heic`. `go.sum` gained the indirect `golang.org/x/image` and `golang.org/x/net` sums that `photo` and `fetch` import. Keel was already required.
- Surprised: Keel upload believes the JSON owner. The rewrite is the whole of "the server sets the owner". A revoked cookie still mints a new guest before these routes run, which is T4.1's behaviour, so intake never sees a missing session.

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
