# P2: Clients

```yaml
id:       P2
size:     M
requires: [P1]
blocks:   P3
parallel: P4 accounts, P5
```

**Goal:** Real implementations of the P1 interfaces, each proved live once by its `b` sibling and otherwise tested on recordings.

---

### T2.1: The System One client ★
```yaml
requires:   T1.3, T0.6
fixture-ok: yes
size:       M · mid
owns:       internal/systemone/workersai.go, internal/systemone/openrouter.go (the Jev comparison)
status:     done
```
Workers AI REST (`/ai/run/@cf/cloudflare/clef` and `clef-flash`): images embedded as base64 under Clef's limits (4 MiB, 16 MP each, 8 MiB decoded in total), questions and state in, probabilities out. Retries through Keel `throttle` for errors a wait can fix. Every call metered through `cost`. The OpenRouter endpoint is the Jev comparison behind the same interface. Clef is the picker. The comparison is still required and is not this task's done-when.

**Done when:** tests against the T0.6 recordings pass; oversize images are refused before the call with a typed error; the meter records every call.

Notes:

- Added `WorkersAI` in `internal/systemone/workersai.go` and `OpenRouter` in `internal/systemone/openrouter.go`, both behind `SystemOne`. One Ask is one metered call: the quote is reserved, the POST is retried through Keel `throttle`, and the settle is the input tokens the reply reports priced at the model's rate (21,818 neurons per million for `clef`, 3,455 for `clef-flash`, $0.011 per 1,000 neurons). A refused attempt books nothing and frees the reserve. No `Once` settle: the same request asked again is a new paid call on the wire, and the books say so.
- Images are refused before any call and before any reserve, with a typed `LimitError`: 4 MiB per image, 16 megapixels per image, 8 MiB of images in all, and a 13 MiB body. The pixel size is read from the header alone — PNG and JPEG through the standard `image.DecodeConfig`, WebP through the three chunk forms the container spec fixes — and a file whose header holds no readable size is refused too, because Clef could not decode it either. The photo pipeline normalises EXIF orientation first, so header dimensions are the displayed ones. The 16 MP cap runs on Workers AI only; the OpenRouter endpoint keeps the byte caps until the comparison task records Jev's own.
- The throttle classes are split: a 429 draws the default three attempts, a 5xx or a transport timeout draws the two transient attempts, and the caller's own deadline is never read as a provider timeout. A Retry-After header on a 429 names the wait. Every other 4xx, a 200 whose envelope reports failure, and an incomplete answer come back at once and settle nothing.
- The tests replay all 41 top-level `testdata/clef` exchanges and the 4 `cost/` recordings through the real client over HTTP, matched by content key through the replay twin, with the harness stripping the account prefix the recordings leave out. Each reply decodes to the probabilities the recording holds, and each request goes to the model's own path, so the clef-flash checks pin the clef-flash endpoint. `stability/` is not replayed through the client: its ten repeats are one request, `Ambiguous` by design. The replay twin needed no change.
- No OpenRouter recording exists — T0.6 recorded Clef only — and no live call was made. The OpenRouter tests pin the documented decisions shape with an inline fixture and say so in place. The endpoint is `alpha/decisions` appended to the resolved `OPENROUTER_BASE_URL` (`https://openrouter.ai/api/v1` in docs/keys.md); if OpenRouter serves the decisions call only outside that prefix, the setting changes, not the code. Images ride in the state as `image_url` data-URL parts, the documented form; the pick comparison is text only, so no image crosses on the recorded path. A reply that reports its own `usage.cost` settles at that figure; the reserve uses the documented $0.042 per million input tokens.
- The key and the account id come from the caller's resolved references. Neither is in an error — a transport error is scrubbed and a provider note is trimmed to 200 characters — and a test pins the bearer on every recorded call. No new dependency.

Remediation (review):

- The Retry-After test now bounds the first wait from above as well as below: the hint names 2s and the throttle adds at most a second of spread on a hint, so the honest wait sits in [2s, 3s], while the default backoff waits at least 10s. A reader that dropped the hint no longer passes.
- `decodeClefResult` routes the envelope's `errors[].message` strings through the same scrub-and-trim the non-200 note uses (a shared `cleanNote`), so an `EnvelopeError`'s notes are key-free and capped at 200 characters like a `StatusError` note. Nothing else about the notes changed.
- The replay's charge-kind check now matches the kind whole against the model each request named; the old double prefix check was dead, because `workersai.clef` is also a prefix of the flash kind.
- The transient-fault and failed-envelope tests pin zero charges after a fully failed call (the non-retryable refusal test already carried that pin and the request count). The only behaviour change is the envelope notes' scrub and trim.

### T2.1b: System One live
```yaml
requires:   T2.1
fixture-ok: no
size:       XS · light
owns:       internal/systemone/live_test.go
status:     claimed:glm-5.3-flash
```
One judge and one check request on a sample photo. Workers AI spend within the free allowance. **Done when:** the transcript is in the report and matches the recording's shape.

---

### T2.2: The YouCam client ★
```yaml
requires:   T1.3, T0.5
fixture-ok: yes
size:       L · mid
owns:       internal/youcam/client.go, internal/youcam/tasks.go, internal/youcam/youcam.go (the analysis field only)
status:     done
```
v2 REST on `https://yce-api-01.makeupar.com`, the key as a Bearer token. Upload through the File API; create and poll through Keel `providertask` under an idempotency key, so a restart resumes the recorded task and never creates (and pays for) it twice; copy each result into `mediastore` at once (links expire). Units quoted, reserved and settled through `cost`.

- **Contract change (orchestrator's call, from T0.5):** the seam's `Status` has no field for analysis results. Add one that carries `faceShape` only; age, gender and ratios are never decoded into it.
- From T0.5 (docs/youcam.md): send makeup `skin_smooth` at 0; send hair `hair_color: src`; the necklace needs anchor and wearing points; sniff result bytes (PNGs come as `binary/octet-stream` under a `.jpg` name); re-polling a finished task returns a fresh result link for free; the balance is `GET /s2s/v1.0/client/credit`.

**Done when:** tests on the T0.5 recordings cover success, every recorded error, a restart mid-poll that resumes without a second create, and a budget refusal that spends nothing.

Notes:

- Added `Client` in `internal/youcam/client.go` over the recorded wire. One upload is the slot POST (no units) plus the PUT of the bytes to the signed URL with the Content-Type the slot named and no `Authorization`; create and poll go out under the Bearer; fetch copies the bytes and maps the recorded `403 AccessDenied` to the typed `ErrExpired`. `hair-transfer` builds at `/s2s/v2.1/`, every other task at v2.0. Redirects are refused, notes are scrubbed of the key and trimmed to 200 characters, and the reply body is bounded.
- The seam's `Status` gained `FaceShape` (the contract change the task names): the poll decodes `results.faceshape` into it and nothing else — `agegender`, `facialratio` and the other groups are never read off the wire, even when a reply fills them. The typed `face-attr-poll` recording still carries no faceshape (T0.5's gap), so the field is proven through the HTTP recording served over the wire: the client reads `Oval` from `http-face-attr-poll`.
- Retries are split like the System One client: a 429 draws the default three attempts, a 5xx or a transport timeout the two transient ones, a Retry-After header names its own wait (none was ever recorded; the reader is there for the documented limit), and every refusal, engine verdict and lapsed link comes back at once.
- Added `Runner` in `internal/youcam/tasks.go` over Keel `providertask`. The job's idempotency key is the providertask key: claim, create at most once, record the task id before polling, finish with the verdict. The uploads ride inside the create, so a resume never re-uploads; the store keeps no result URL, so every Run that reaches the result polls the finished task once more first — free — and fetches on the fresh link. That one fresh-link poll is also what recovers a lapsed link: one fetch answered `ErrExpired` is followed by one fresh poll and one fetch; a second expiry surfaces typed.
- The rate card is the documented table (`ai-smile` 1, `makeup-vto` 1, earring 1, necklace 1, `cloth-v4` 2, `hair-transfer` 2, `beard-style` 2, `face-attr-analysis` 10); a task off the card is refused before any quote, upload or reserve. One Run is one metered call: the card line is reserved before anything uploads and settled once per key through `cost.Once()` in a `youcam unit` denomination budget. A failed task settles nothing, because the provider charges then refunds (a net zero in docs/youcam.md's ledger). The result is copied into mediastore under an id derived from the key (32 lowercase hex, `id`-shaped), so a repeat of the same key is already kept; the content type is sniffed from the bytes, never from the header or the `.jpg` name.
- Tests replay all eleven recorded flows — smile, lip red, lip red with the default smoothing, earring, necklace, necklace with engine placement, cloth, both hair transfers, beard, face attributes — through the runner over the typed recordings and through the live client over an httptest harness that serves API legs by content key and the two signed legs from the typed fetch bytes, result links rewritten onto the test server. The three flows with no typed upload run create-first: their file ids exist only in their create bodies. The slot POST is the one leg not matched byte-exactly: the recorded body names the sample photo, this client derives its own name, so the harness builds the slot reply around the recorded file id.
- The four done-when classes: `TestRunnerCarriesEveryRecordedFlow` and `TestClientRunsEveryFlowOverTheWire` (success, one charge per flow, bytes kept match the recorded pictures); `TestEngineErrorsAreTypedAndSpendNothing` and `TestClientReplaysEveryRecordedRefusal` (all four engine verdicts typed with their details, plus wrong key, unknown file id, bad parameter, unknown task, unknown or expired task id, expired result link — each from its recording, each spending nothing where it ran); `TestARestartMidPollResumesWithoutASecondCreate` (a run cancelled on a scripted running poll, then the same key: one create total, one charge, the recorded bytes kept); `TestABudgetRefusalSpendsNothing` (a budget one unit short of the quote: no upload, no create, empty books, claim released, and a funded run of the same key succeeds).
- Surprised that Keel `providertask` deliberately keeps a claimed key with no task id after a create refusal — a re-run refuses `ErrTaskPending` instead of creating again, and `Store.Release` is the caller's deliberate recovery. Pinned that rather than adding release logic. Surprised that the throttle clamps the Retry-After hint to `Max`: a first test with a 2 ms cap silently swallowed a 1 s hint. No new YouCam behaviour surfaced — everything here is offline over T0.5's recordings, and its surprises are already in docs/youcam.md.
- Proposed, not taken: the package comment in `youcam.go` still says "This package does not [dial]", which the new `Client` makes half-wrong. That file is this task's for the analysis field only, so the line stands; the exact diff is in the report.

Remediation (review):

- `TestABudgetRefusalSpendsNothing` now carries the funded stanza its note described: after the refused run, a meter funded to the card line runs the same key again over the same ledger, and the test pins the one create, the kept bytes matching the recording, exactly one charge, and the succeeded claim. The note's last clause is now the test's own.
- A runner-level test runs a task name off the rate card and pins the ordering: the refusal comes back with no claim, no reserve, and no call of any kind, because `UnitsFor` runs before the claim and the estimate.
- The expired-link recovery test gained a sibling: a second fetch that expires again fails the run with the typed `ErrExpired` read through providertask's wrapping (`errors.Is`), after exactly two fetches — no third attempt, nothing kept, nothing settled.
- The Keel throttle clamp that swallowed the Retry-After hint is recorded in feedback.md under Keel.
- The package comment in `youcam.go` now carries the implementer's proposed line: the live client (client.go) dials the v2 API; the replay (replay.go) serves recordings and does not.

### T2.2b: YouCam live
```yaml
requires:   T2.2
fixture-ok: no
size:       XS · light
owns:       internal/youcam/live_test.go
status:     in-review
```
One upload slot (free), then one render task on a sample photo. **Unit budget: 20.** **Done when:** the transcript and the units spent are in the report.

Notes:

- Added `internal/youcam/live_test.go` behind `//go:build live`, `TestLiveYouCamRendersLipRed`. The gate does not build it. It reads `YOUCAM_API_KEY` from the environment and skips (not fails) when the name is absent; it does not open a `.env` file. The key never reaches the transcript: the client scrubs it out of its own errors and the test scrubs again before anything is logged, and a signed URL is logged without its query, because the query carries the grant (the recordings keep scheme, host and path only). The photo is the committed AI sample woman `V04-family-function.jpg` (896×1200, 278,400 bytes) — the same picture the T0.5 lip-red flow recorded, so the comparison is direct.
- The run, in order: balance 1,016 (event pool 976, trial pool 40); the charter's upload slot leg through the raw client — slot POST plus PUT, 1,748 ms, free, the balance read right after it unchanged at 1,016; then one lip-red `makeup-vto` render through the T2.2 `Runner` over Keel `providertask` under key `t22b-live-lip-red`, with the meter funded by exactly the rate-card line so nothing past one task can leave the process. The runner took its own slot inside the create (free), created once (366 ms), polled `running` five times (about 220 ms each), saw `success` with a result link, polled once more for the fresh link (the free re-poll the runner takes every time), fetched 1,431,541 bytes in 1,194 ms, and kept the blob into mediastore under the key-derived id `b2ecc2853b66e5acf7e7f8ec2a50422f`, content type sniffed `image/png` from the bytes — the recorded PNG-as-`binary/octet-stream`-under-a-`.jpg`-name behaviour, handled. Whole render leg 16.3 s including the balance reads and the keep.
- Units spent: 1 of the 20 budget. The books settled once (`youcam.makeup-vto`, ref `t22b-live-lip-red`, 1 unit, reservation freed) and the wire agrees: balance after 1,015 (event pool 975, trial pool untouched) — exactly before minus the card line. The debit hit the event pool, as the T0.5 ledger recorded. The claim finished `succeeded` with the created task id, so the resume machinery ran for real against the provider, not only in replay.
- Shape, field by field against the T0.5 recordings: no divergence. The slot reply carries the same `data.files[0]` fields (`content_type`, `file_name`, `file_id`, `requests[0]` with `method` PUT, `url`, the `Content-Length`/`Content-Type` headers) and the PUT took the two named headers with no `Authorization`; the create body is the recorded lip-red body byte-for-byte except the fresh `src_file_id`, and the reply is `{status:200, data:{task_id}}`; the poll walks `task_status` running → success and the success reply names `results.url`; the credit endpoint answers `{status, results[]}` with `type`/`amount`/`amount_dec`/`expiry` per pool, the two pool expiries decoding to the recorded dates. The task id again shares its source `file_id`'s head, as docs/youcam.md records. The recorded client needed no fix and got none.
- Two shape notes that are not divergences: the slot request's `file_name` is the client-derived `makeup-vto.jpg` where the recordings name the sample photo — the T2.2 note's known, intended shape, and the reply echoes back whatever name was sent; and the run made two slots (the charter's free slot leg plus the job's own upload inside the create) because a runner job owns its uploads so a resume never re-uploads. Both are free; the charter's "one upload slot" is spent as its free slot leg first, balance-proven, before the paid create.
- gofmt clean, `go vet -tags live ./internal/youcam/` silent, `./tools/check.sh` green without the tag, and a run without the key skips cleanly. The recorded-replay suite is untouched.

Remediation (review, round 2): the transcript scrub now cuts the query of every url it meets and redacts the path segment after the literal `ACCOUNT/` and `USER/` markers (the values there vary per upload, so only the markers are matched), in `briefLiveURL` and on every error path — a failed PUT or fetch is a `*url.Error` that embeds the signed url verbatim, grant query included, and the client's own scrub replaces the key alone. A guard test on fabricated secrets pins the redaction (signature, access-key id, account segment, user segment gone; the scheme, host, extension and the surrounding error text intact) and was run red by mutation: with the scrub reverted to the round-1 key-only form the guard failed on all of them, then the fix went back in and it passed. One process slip to record: a re-verify command ran the live test once more with the key still exported, one accidental paid render, 1,015 → 1,014 on the free balance read — the task's total spend is 2 of the 20 units, and the no-new-calls instruction was met everywhere but that one invocation.

---

### T2.3: The Lambo client ★
```yaml
requires:   T1.3, T0.7
fixture-ok: yes
size:       M · mid
owns:       internal/memory/lambo.go
status:     done
```
MCP over streamable HTTP to the Dresscode Lambo server: one session per account, remember a look as text (the occasion, flagged criteria, fixes kept and dismissed, products, the look-in-words line), recall for an occasion, erase a session. Lambo's writes are applied in the background: a remember is confirmed through its receipt before the look reports "remembered". `record_action` does not de-duplicate, so a write is sent once (Keel `outbox` if it must survive a restart).

**Done when:** tests on the T0.7 recordings pass; erase is safe to repeat and reports gone; the client is a target Keel `erase` can call.

Notes:

- Added `internal/memory.Client`. `New` does not dial. `Open` was already the recording reader. One MCP session per Dresscode session: `initialize`, then `notifications/initialized`, then the tool call. The session id from `Mcp-Session-Id` goes on every later call. The client name is `dresscode`, not the probe's `dresscode-t07`. The agent id defaults to `dresscode`. A test that replays the probe sets `dresscode-probe`.
- `Remember` sends one `lambo_derive` and does not send it again. It returns only after `lambo_stats` reports that receipt `applied`, with the created nodes and the embedded count. `ErrUnconfirmed` means the write was sent and not confirmed. A refused token and an erased session stay those errors, so a caller can tell them from a lost confirmation. `Recall` sends `top_k` 10 and `max_tokens` 2000. JSON-RPC `-32003` is `ErrErased`, the hit list is empty, and the call is not retried. The cached MCP session is dropped, so the next call is a fresh initialize, which the erased id also refuses.
- `Erase` is `POST /admin/s/<session>/erase` with `confirm` set to the session. A second call is the same request. `AlreadyAbsent` is the gone report, with every removed count at zero and the same fence token. `Target` is a Keel `erase.Target` named `memory`, the name history deletion and account deletion register. `Delete` returns `erase.ErrGone` when the session is already absent. An erasure counts that as confirmed. One erasure has one memory target, because the name is not the session id.
- Did not use Keel `outbox`. A Lambo write is not idempotent, and outbox delivery is at-least-once, so a replay would derive the look again. The remember call itself sends the write once and waits. Did not use Keel `fetch`. It refuses loopback, and both the Dresscode serve and the test replay are loopback. There is no MCP client in Keel. No new dependency.
- Tests replay `testdata/lambo` over HTTP, including the SSE priming event and the JSON body an erased session returns. The probe order is initialize, the initialized notification, derive, stats, recall, erase, the erased recall, the erased initialize, the second erase, the other session's recall, and its erase. `01` is the refusal with no token. `04` is `tools/list`, which this client does not call. No live serve. No token in an error. The package comment in `memory.go` still says the package does not dial. That was true of the replay twin. This file is the exception.

### T2.3b: Lambo live
```yaml
requires:   T2.3
fixture-ok: no
size:       XS · light
owns:       internal/memory/live_test.go
status:     done
```
Remember, recall and erase one look in a scratch session on the Dresscode server. **Done when:** the transcript is in the report.

Notes:

- Added `internal/memory/live_test.go` behind `//go:build live`. The gate does not build it. The test reads `LAMBO_URL` and `LAMBO_DRESSCODE_TOKEN` from the environment. A missing name fails with that name and no value. It does not open a `.env` file and does not log the token. It refuses ports 7700 and 7710 and dials only `127.0.0.1:7720`.
- Session `dc-u-live-t23b`. One look, two observations: kept the navy wrap, dismissed the extra belt. Remember came back `applied`, 2 created, 2 embedded, agent `dresscode`. Recall of "live check" returned those two and nothing from the probe. Erase reported `already_absent` false, 2 concepts, 2 vectors, 1 session, fence 2. The scratch text does not stay.
- One derive, then the stats wait, then one recall, then one erase. No second write. Did not reuse `dc-u-probe-a` or `dc-u-probe-b`. The receipt summary matches the recorded shape.

---

### T2.4: The Resend sender
```yaml
requires:   T1.4
fixture-ok: yes
size:       S · light
owns:       internal/mail/
status:     done
```
A Keel `identity.Sender` over Resend for sign-in codes only: plain text and a minimal HTML version, the from address from settings, Keel's send limits applied before sending. A recorder implementation for tests.

**Done when:** tests on a recorded Resend reply pass; the code never appears in a log.

Notes:

- Added `internal/mail`. `Client` implements Keel `identity.Sender` and posts one sign-in code to `POST /emails`. The body is plain text and a minimal HTML version of the same words. The from address is the resolved `RESEND_FROM`. The key is the resolved `RESEND_API_KEY`. Redirects are not followed, so the bearer token stays on that one request. A `User-Agent` is set because Resend's docs say a missing one can be refused.
- `Recorder` implements the same interface for tests. It keeps each accepted send and does not dial. Formatting a message or the recorder does not include the code.
- Keel's identity service applies its send ceilings before it calls `Send`. This client does not keep a second copy of those ceilings. A test mounts the code route on this client: five codes are posted, and the sixth is `send_limited` with no sixth request.
- The sign-in code is not in the subject, not in an error, and not in a log line. A provider id that contains the code is not logged. A transport error that contains the code or the key is replaced.
- The fixture `internal/mail/testdata/send.json` is Resend's documented success body, an id and nothing else. This task does not send a live message, so the fixture has no key and no address. The only address in the tests is `dresscode@nryn.dev`.

### T2.4b: Resend live
```yaml
requires:   T2.4
fixture-ok: no
size:       XS · user
owns:       internal/mail/live_test.go
status:     done
```
The user receives one sign-in code at their own address. **Done when:** the user confirms it arrived and how long it took.

Notes:

- Added `internal/mail/live_test.go` behind `//go:build live`, `TestLiveSigninCodeArrives`. It reads `RESEND_API_KEY`, `RESEND_FROM` and `RESEND_TO` from the environment and skips when any is missing, so `-tags live` without env does nothing. It builds the real `Client` from those values, no config load, and calls `Send` once. No retry: a failed send fails the test. The key is never printed, and a send error is reported only by its typed refusal fields (status and name) or a fixed line, never by its text.
- The provider id is read without changing the sender: the test passes a capturing `slog.Handler` as the client's `Log`, and the sender's own accepted-send record carries `provider_id`. The sender already refuses to log an id that contains the code.
- **The live run was refused.** One send attempted, `sent_at_utc` in the handoff log; Resend answered 403 with no error name, so no mail was sent and the user received nothing. Stopped there per the one-send rule. Unmet done-when: the user has not confirmed arrival. Likely outside the client: a 403 before the mail is accepted reads as account or domain verification, not message shape. Needs the user's Resend console check before the next attempt.
- gofmt clean, `go vet -tags live ./internal/mail/` silent, the gate passes without the tag, and a run without env skips cleanly.
- Landed after the console finding was worked around with the user's approval, by the account-owner quick path. A second single send, from `onboarding@resend.dev` to the user's own address at 2026-10-30T14:33:40Z, was accepted in 415 ms, and the user confirmed arrival within about a minute — in Gmail's spam, which is its own finding (feedback.md, Resend). Done-when met on that confirmation. The configured from-domain `nryn.dev` is still unverified: production-sender proof, and the demo, wait on the console fix.

### T2.5: The memory client survives session expiry
```yaml
requires:   T2.3
fixture-ok: yes
size:       S · light
owns:       internal/memory/, testdata/lambo/ (one recorded 404 exchange)
status:     done
```
Lambo expires idle sessions in minutes: a call on a dead `Mcp-Session-Id` gets HTTP `404` with the plain text `Not Found: Session not found`, not a JSON-RPC error. A real user's second visit idles longer than that, so the client must treat that 404 as "initialize the session again, then replay the original call once". A second consecutive 404 is a typed error, never a loop.

**Done when:** a replay test over a recorded 404 exchange shows one re-initialize and the replayed call succeeding; a repeat 404 fails typed; the gate stays offline.

*(Found by the orchestrator on 2026-10-30: `internal/memory/lambo.go` has no 404 handling; two orchestrator sessions expired mid-idle. See feedback.md under Lambo.)*

Notes:

- `Client` now reads a 404 on a call that carried an `Mcp-Session-Id` as an expired session: it drops the dead id, runs the same initialize and notifications/initialized open a first call runs, and replays the tool call once. A second 404 is `ErrSessionExpired` and there is no third attempt. The gate lock is held for the whole call, so one re-initialize per session is the most that can ever be in flight.
- `ErrSessionExpired` stays itself out of `Remember`, beside `ErrErased` and `ErrUnauthorized`: a derive refused at the session was never sent, which is not a lost confirmation, and `errors.Is` tells the caller. `Recall` returns it bare. Erase is the admin path and carries no session id, and initialize goes out without one, so a 404 on either stays a status error — only a call that named a session can mean expiry.
- Recorded `21-call-on-expired-session` in the T0.7 envelope: POST on the probe-a path with the probe-a session header, status 404, `"headers": {}` because the serve sent no content type, and the exact body `Not Found: Session not found` with no newline. The replay twin serves it raw. Twenty-one recordings now; the fake's unique-request count is 19, since the two erase-a exchanges still share one.
- The expiry tests replay a script: the replay twin gains a mode that serves named exchanges in order, so the re-initialize leg reuses `02`/`03` and the replayed leg reuses the recorded success `11`. Three tests: a recall survives expiry (one re-initialize, the replay succeeds on the fresh id); a second 404 fails typed with no third attempt; and a remember whose derive is refused twice reports `ErrSessionExpired`, not `ErrUnconfirmed` — the one recorded 404 reply carries for every tool call, because plain text has no call shape in it.
- Surprised: the fake's `Recall` on probe-a and "family function" is now a three-way `Ambiguous` (11, 16, 21) where it was a two-way; its test names only 11 and 16 and still passes, and the fake stays right to refuse choosing.
- Remediation of the review's two LOW findings. The 404 on the notifications/initialized leg now drops the session id: `open` treats `ErrSessionExpired` from `notify` as a dead id, drops the gate, and returns the refusal, so the next call starts from a fresh initialize. Chosen over a same-call open retry because it is symmetric with the initialize-failure branch right above it, keeps `open` loop-free by construction, and matches the `Erase` precedent that a dead id is learned on the next call, not chased within one. `TestANotifyExpiryDropsTheDeadSession` replays initialize ok, the 404 onto the notification, then the next call re-initializing and succeeding, reusing 02/21/03/11 in the script; it asserts the refused leg carried the first id and the recovered notification and recall carry what the second initialize returned. Verified it fails without the fix. The `ErrSessionExpired` comment now states both Remember legs: a derive refused twice was never sent, a stats wait refused twice had the write sent with a receipt that may be applied, and neither sends the write again.
