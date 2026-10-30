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
status:     not-started
```
Workers AI REST (`/ai/run/@cf/cloudflare/clef` and `clef-flash`): images embedded as base64 under Clef's limits (4 MiB, 16 MP each, 8 MiB decoded in total), questions and state in, probabilities out. Retries through Keel `throttle` for errors a wait can fix. Every call metered through `cost`. The OpenRouter endpoint is the Jev comparison behind the same interface. Clef is the picker. The comparison is still required and is not this task's done-when.

**Done when:** tests against the T0.6 recordings pass; oversize images are refused before the call with a typed error; the meter records every call.

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
status:     claimed:grok-4.7
```
MCP over streamable HTTP to the Dresscode Lambo server: one session per account, remember a look as text (the occasion, flagged criteria, fixes kept and dismissed, products, the look-in-words line), recall for an occasion, erase a session. Lambo's writes are applied in the background: a remember is confirmed through its receipt before the look reports "remembered". `record_action` does not de-duplicate, so a write is sent once (Keel `outbox` if it must survive a restart).

**Done when:** tests on the T0.7 recordings pass; erase is safe to repeat and reports gone; the client is a target Keel `erase` can call.

### T2.3b: Lambo live
```yaml
requires:   T2.3
fixture-ok: no
size:       XS · light
owns:       internal/memory/live_test.go
status:     not-started
```
Remember, recall and erase one look in a scratch session on the Dresscode server. **Done when:** the transcript is in the report.

---

### T2.4: The Resend sender
```yaml
requires:   T1.4
fixture-ok: yes
size:       S · light
owns:       internal/mail/
status:     in-review
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
status:     not-started
```
The user receives one sign-in code at their own address. **Done when:** the user confirms it arrived and how long it took.
