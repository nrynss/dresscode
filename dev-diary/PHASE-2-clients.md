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
status:     in-review
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
status:     not-started
```
One judge and one check request on a sample photo. Workers AI spend within the free allowance. **Done when:** the transcript is in the report and matches the recording's shape.

---

### T2.2: The YouCam client ★
```yaml
requires:   T1.3, T0.5
fixture-ok: yes
size:       L · mid
owns:       internal/youcam/client.go, internal/youcam/tasks.go
status:     not-started
```
v2 REST on `https://yce-api-01.makeupar.com`, the key as a Bearer token. Upload through the File API; create and poll through Keel `providertask` under an idempotency key, so a restart resumes the recorded task and never creates (and pays for) it twice; copy each result into `mediastore` at once (links expire). Units quoted, reserved and settled through `cost`.

**Done when:** tests on the T0.5 recordings cover success, every recorded error, a restart mid-poll that resumes without a second create, and a budget refusal that spends nothing.

### T2.2b: YouCam live
```yaml
requires:   T2.2
fixture-ok: no
size:       XS · light
owns:       internal/youcam/live_test.go
status:     not-started
```
One upload slot (free), then one render task on a sample photo. **Unit budget: 20.** **Done when:** the transcript and the units spent are in the report.

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
