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
status:     claimed:grok-4.7
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
