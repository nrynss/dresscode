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
status:     done (co-coder: qoder)
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
status:     claimed:glm-5.3-flash
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
status:     done:glm-5.3-flash
```
`dresscode budget` prints units and dollars spent and left; `dresscode flag` flips fresh uploads, renders and samples-only without a restart. The judging share of units is refused to everything but judge leases.

**Done when:** flipping "fresh uploads off" takes effect on the next request; the judging share holds under a test that tries to spend it.

Notes:

- The mechanism is the database file, and no seam was missing. The landed readers already resolve every switch per request out of the Keel `flag` store on the server's SQLite (intake per open/photo/product/link, the look start per start, `rendersPaused` once per attempt), and Keel sqlite runs WAL with a busy timeout. So `internal/server/ops.go` is the write side the commands call: `SetFlag`/`Flags` open a second handle on the server's database file, rewrite or read the one row, and close. There is no ops route and nothing in the server's memory to invalidate. The end-to-end test flips through exactly that path while the server's own handle is open: the next selfie open and the next `POST /api/photos` answer `403 uploads_paused`, a product open still proceeds (the flag refuses fresh selfies, not products), and flipping back reopens the intake. `samples_only` flips the same way and wins over `fresh_uploads_off` with both on. `renders_off` needs no new reader: T4.4's `rendersPaused` reads the row per attempt, and the flip lands in the same store the same way.
- The spend books are two Keel `cost/sqlitestore` namespaces on the same file, a convention this file names so the composition wiring opens the same ones: `spend_youcam` (denomination `youcam.credit`, period none, the settings' lifetime ceiling, keyed with owners `judging` = `judging_units` and `development` = the rest) and `spend_workers_ai` (USD nanodollars, period daily UTC, the settings' `dollars_per_day`). Keel rewrites the ceiling on every open, so the command opens with the settings file's numbers and the books always agree with the file; that is also why the command loads the full settings through `config.Load` (the strict parser cannot decode a slice of the file, so the key references resolve — none of the values print).
- The judging share holds at the budget: development's ceiling is the units minus the share, so a non-judge spend reaching for the share hits `cost.ErrOverBudget` — the typed refusal the render stage already wraps as `pipeline.Refused` (`budget_refused`) and the start refuses pre-lease through `budgetAdmits`. The test spends development to its ceiling, refuses one more unit on it, draws the judging share down to zero through a judge-lease meter, and refuses one more on that too. What the composition wiring owes this seam: build the YouCam meters over `Spend.Development` for every spend that is not a judge session and over `Spend.Judging` for the sessions that are, pass `Spend.UnitsLeft` as `LooksConfig.UnitsLeft`, and settle into `Spend.Ledger`/`Spend.Dollars`. That wiring is not this task's to take; the seam is exported and tested.
- `dresscode budget` prints four lines: the pool and each share as spent/left/ceiling in units, and the day's dollars (`$0.00` exact-cents; finer prices keep nine places with trailing zeros trimmed, so nothing rounds). `dresscode flag` with no arguments lists the three switches with position, change time and help; `dresscode flag <name> on|off` flips. Default paths are `data/dresscode.db` and `dresscode.toml` (`-db`/`-config` override). Both commands refuse a missing database file rather than creating one a server has never opened.
- Nothing dialled, no unit moved, no key read: the tests' spend runs through meters over the rig's own books, the settings fixture carries variable names only, and the injected environment answers stand-ins. Grants beyond `owns`: none — every touched path is `internal/server/ops.go`, its test, and `cmd/dresscode/`.
- Surprised (also in feedback.md): Keel `cost/sqlitestore.Open` rewrites the ceiling on every open, zero included, which makes "read the books" a write and killed the first sketch of opening empty and reading the ceiling back; Keel `config` cannot load part of a file, so the budget command resolves all ten key references to print two ceilings.

Review: CLEAN, no must-fix. Both **Done when** clauses proved at the real thing — the flip test drives live HTTP through `srv.Handler()` on a second handle over the server's own file (`internal/server/ops_test.go:61`), and the share test spends against real `cost/sqlitestore` books, not a fake (`:198`). Gate green at the tip, after the rebase, and in the main checkout. Two findings accepted as non-blockers, both open for the next hand on this file: (1) `OpenSpend` rewrites the live ceiling from whatever `-config` it is handed, so `dresscode budget` is a read that writes — the operator caveat ("run it with the settings file the server booted from") is not yet in the command's help; (2) `checkUnitsBudget` restates `config.YouCamBudget.check()` and `opsFlags()` restates `config.Flags.Declarations()`, both because the config versions are unexported or carry the file default rather than `Default:false`. Landed as `4ce325b`.

### T4.8: The server binary ★
```yaml
requires:   T4.4, T4.5, T4.6, T4.7
fixture-ok: yes
size:       M · mid
owns:       cmd/dresscode/, internal/config/, internal/server/server.go
status:     not-started
```
`dresscode serve` composes what P4 built and listens. Today nothing does: no production path calls `MountLooks`, `MountSamples`, the intake mounts or the deletion mount, and `cmd/dresscode` says so out loud ("not listening"). This task is the wiring, not new behaviour — every seam it joins is already exported and tested, and two of them already carry debt written down in this file.

What to build:

1. **The composition** (`cmd/dresscode/serve.go` or the equivalent): one database handle, the Keel stores on it, the runtimes, the mounts, in the order the constructors demand. `internal/server/server.go` gains the wiring it was built to host but has never hosted; keep its constructor honest — a server that cannot mount what it claims is a bug, not a warning.
2. **The spend wiring T4.7 named** (`PHASE-4-server.md`, T4.7 Notes): YouCam meters over `Spend.Development` for every spend that is not a judge session and over `Spend.Judging` for the sessions that are, `Spend.UnitsLeft` passed as `LooksConfig.UnitsLeft`, and the settled spend landing in `Spend.Ledger` / `Spend.Dollars`. Open the same two `cost/sqlitestore` namespaces T4.7's file names, on the same database file, with the settings' ceilings — the books and the settings file must agree, which is why `OpenSpend` rewrites the ceiling on open.
3. **The samples mount**, which T4.6 deferred rather than faked: `MountSamples` needs a samples directory, and that directory has the shape the recordings have — the second root is `<samplesDir>/../youcam/results`, so a production directory without that sibling fails the mount for the family render and the festival discard evidence. Decide the source (a `[paths]` setting or an argument) and name it in `internal/config`; the loader's refusal is already written to say which root failed.
4. **The shutdown posture**: what a signal does to an in-flight look, what a restart does to an unfinished row (T4.4's `kindStore` recovery is the answer, so the binary must not start a second runner over the same kinds), and what the SSE subscribers see when the process stops. The event demo must not strand a look mid-render.
5. **The operator caveats T4.7's review left**: `dresscode budget` is a read that writes, so its help says to run it with the settings file the server booted from.

**Done when:** a test boots the composition over a temp file and every route the binary claims answers — including one opened sample and one paid look on the same server; `dresscode flag fresh_uploads_off on` reaches the running server's next request without a restart; and the spend books settle in the same namespaces `dresscode budget` reads.

Notes:

- Chartered from the P4 reviews, not from a plan: `MountSamples` being called only by tests, the meters and `UnitsLeft` unwired, and the second-root layout requirement are all written down in T4.6's and T4.7's Notes. This is where that debt is paid.

---

### T4.9: Erasure reaches every row, and the lists stay bounded
```yaml
requires:   T4.4, T4.6
fixture-ok: yes
size:       S · light
owns:       internal/privacy/, internal/server/looks.go
status:     not-started
```
Two gaps T4.6's review found while it was not looking for them. Both are product promises, not polish: the promise is that a person's data goes away when they ask, and that a free route cannot grow the account without limit.

1. **A `jobs` row outlives every account deletion, for every kind.** `internal/privacy/privacy.go` erases sign-in, sessions, identities, users, looks, snapshots by cascade, media and analysis rows — and there is no `DELETE FROM jobs` anywhere in the repository. Keel's job store keeps its own table, so a born-finished sample row (which holds the recorded story plus minted ids and no user id) and a paid look's row both survive the erase. Find what identity Keel's `jobs` rows carry, and erase or prove-never-created every row an account's look touched. A row that survives because it never named the user is an answer, not an excuse: say which rows that is and why it holds nothing.
2. **`GET /api/looks` has no `LIMIT` anywhere in `looks.go`**, and the free sample route adds a `looks` row per open with no gate token, no lease seat, no budget reservation and no per-user cap. Bound the list, and bound the repeats: decide whether a sample open should be idempotent per `(user, sample id)` — T4.6 stopped reusing only what is safe to share, so a repeat open still adds a row and still re-writes its render's bytes.
3. **`deleteBlobIDs` has no reference count.** A look deletion's blob list is exactly `SELECT media_id FROM renders …` and every listed id is erased after an **owner** check only, so two looks naming one render blob let deleting one strand the other. T4.6 removed the case it created by refusing to reuse a blob into the render group — a rule about one route — and this is the rule about the store: erase a blob only when no surviving row still names it, whatever route shared it. Make `docs/api.md:503`'s promise of `files` to every look deletion true in that direction too, and check whether the deleted look's own `renders` row is swept at all (`deleteLooks` removes `looks` rows).

**Done when:** a test erases an account that opened samples and ran a paid look, then finds no `jobs`, media, snapshot, look or analysis row naming anything it can reach; the looks list answers a bounded page while an unbounded number of opens is refused or deduped by a stated rule; and deleting one of two looks that name the same blob leaves the other one's picture reading.

Notes:

- Chartered from a review of another task. The `DELETE FROM jobs` finding came from the T4.6 round-2 reviewer noticing that the deletion path never mentions the job table; the sample/paid blur it also reported belongs to this file's T4.8 marker decision, so the two tasks read each other.
- Item 3 is the durable half of a hazard T4.6's round-4 review measured through the real deletion route: `internal/privacy/privacy.go`'s `deleteBlobIDs` erases every listed blob whose **owner** matches, with no reference count, and a look deletion's list is exactly `SELECT media_id FROM renders …` (`internal/privacy/jobs.go:296-323`) — while `looks.go`'s `materialise` registers that row for **any** finished look on its first read, sample or paid. Two looks naming one render blob therefore let one deletion strand the other. T4.6 closes the case it created by refusing to reuse a blob into the render group, which is a rule about one route; reference counting is the rule about the store, and it is what makes sharing safe to have at all — a selfie already is shared across looks and survives only because no deletion path names it from `renders`. Also for this hand: the deleted look's own `renders` row appears not to be swept (`deleteLooks`, `privacy.go:314-343`), and `docs/api.md:503` promises `files` to every `POST /api/looks/{id}/deletion`, which the reference count has to make true rather than contradict.

