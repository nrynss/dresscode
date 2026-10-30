# Lambo

The calls Dresscode's memory client makes, and the shapes the running server returned. Lambo 0.8.0, one HTTP serve, many sessions. Recordings are in [`testdata/lambo/`](../testdata/lambo/). The bearer token is redacted in every file. No key and no account id are in those files or in this one.

Dresscode keeps one session per account. The session id starts with `dc-u-` and is longer than that prefix. The probe used `dc-u-probe-a` and `dc-u-probe-b`, then erased both. An erased id never comes back, so those two ids must not be reused.

## Transport

Address a session at `POST /mcp/s/<session>` on the host in `LAMBO_URL`. Send:

- `Content-Type: application/json`
- `Accept: application/json, text/event-stream`
- `Authorization: Bearer <LAMBO_DRESSCODE_TOKEN>`
- `Mcp-Session-Id`, on every request after `initialize`

Header names in the recordings are lower case. That is the fixture, not a requirement.

A request with no token is refused before a session is created. Recorded in `01-initialize-no-token.json`:

- status `401`
- `content-type: text/plain; charset=utf-8`
- `www-authenticate: Bearer`
- body: `unauthorized: this endpoint requires 'Authorization: Bearer <token>'`

A response that carries a JSON-RPC message and a live session is `text/event-stream`, even `tools/list`. The body is two SSE events. The first is a priming event: empty `data`, and `retry` of `3000`. The second is one JSON-RPC message in `data`. Read the `data` lines and ignore an empty one. `02`, `04` and `05` were parsed before the event id and `retry` were kept, so those fields are absent there; the JSON-RPC message is the one the server returned. Later files keep them. On a new MCP session the priming id is `0` and the message event has no id (`07-initialize-b.json`). Each later response on that MCP session uses `0/n` for the priming event and `1/n` for the message, and `n` counts from zero (`09` is `0/0`, `10` is `0/1`).

`notifications/initialized` is not a request with an id. The server answers `202` and an empty body (`03`, `08`).

After a session has been erased, JSON-RPC comes back as `application/json`, not as SSE. See Erase.

## Open a session

`initialize` creates a session the credential is allowed to create. There is no separate create call.

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{
  "protocolVersion":"2025-06-18",
  "capabilities":{},
  "clientInfo":{"name":"dresscode","version":"0.0.0"}}}
```

Recorded result (`02`, `07`): `protocolVersion` `2025-06-18`, `capabilities` `{"tools":{}}`, `serverInfo` `{"name":"lambo","version":"0.8.0"}`. `instructions` names the session. That text is for a model. Do not depend on its wording.

The response header `Mcp-Session-Id` is the id to send back. Then:

```json
{"jsonrpc":"2.0","method":"notifications/initialized"}
```

## Calls Dresscode needs

Remember a look, wait until that write is applied, recall by occasion, erase the session. The server lists eight tools (`04-tools-list.json`): `lambo_derive`, `lambo_recall`, `lambo_stats`, `lambo_record_action`, `lambo_inspect`, `lambo_reserve`, `lambo_saints`, `lambo_stale`. The memory client needs the first three, plus the erase route below, which is not a tool. A kept or dismissed fix is a fact, so it is a concept on `lambo_derive`, not a `lambo_record_action`.

Every tool takes `agent_id` and rejects an argument it does not define. Use one stable id for the Dresscode process. The probe used `dresscode-probe`. Do not send `created_at`.

A tool call is `tools/call`:

```json
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"<tool>","arguments":{}}}
```

The result is `result.content[]` (text), `result.structuredContent`, and `result.isError`. A refused call sets `isError` or returns a JSON-RPC `error`. This probe's writes were accepted.

### Remember: `lambo_derive`

Required: `agent_id`, `concepts`. Each concept requires `content` and `concept_type`. `concept_type` is lower case: `entity`, `logic`, `constraint`, `resource`, `observation`. A look verdict is an `observation`. One concept is one short fact. Put the occasion in the text so a later query can find it.

The probe wrote two concepts per session, in one call:

```json
{"agent_id":"dresscode-probe","concepts":[
  {"content":"Family function kept the gold jhumkas.","concept_type":"observation"},
  {"content":"Family function dismissed the red lip.","concept_type":"observation"}]}
```

The other session used "kept the silver studs" and "dismissed the braid". Recorded: `05-derive-a.json`, `09-derive-b.json`.

The call returns before the write is applied. `structuredContent` holds `receipt`, `receipt_state` (`pending`), `summary`, `agent_id` and `warnings`. `created` is absent until the outcome is known. The text names the same receipt.

### Wait: `lambo_stats`

Required: `agent_id`. Pass the receipt and `wait_ms` when the next recall must see the write. `wait_ms` on this probe was `30000`.

```json
{"agent_id":"dresscode-probe","receipt":"<receipt>","wait_ms":30000}
```

Recorded: `06-stats-wait-a.json`, `10-stats-wait-b.json`. `structuredContent.receipt.state` was `applied`. `detail` was `applied — derived 2 concept(s): 2 created (2 embedded), 0 matched existing`. `created` lists the full node ids, `created_count` is `2`, `embedded` is `2`, `matched` is empty. The same payload carries the session counts and `embedding_contract`: `{"kind":"bge_m3","model":"@cf/baai/bge-m3","dim":1024}` with `embedding_modalities` `["text"]`.

These two writes applied in `377` ms and `574` ms (`write_queue_apply_ms_p50` on each session's first applied write). That is this embedder and these short texts, not a limit. Ask with `wait_ms` rather than recalling on the ack.

### Recall: `lambo_recall`

The schema marks only `agent_id` required. This probe always sent a text `query`, plus `top_k` `10` and `max_tokens` `2000`.

```json
{"agent_id":"dresscode-probe","query":"family function","top_k":10,"max_tokens":2000}
```

Recorded: `11` through `14`, and `19`. `structuredContent` holds:

- `context`, the same block as `content[0].text`
- `hits[]`: `node_id` (hyphenated), `content`, `concept_type`, `score`, `is_canonical`, `blast_radius`
- `warnings`

`concept_type` comes back capitalised (`Observation`), not in the lower case sent to `lambo_derive`. The 8 hex characters in the context line (`id 1c8a743b`) are the prefix of `node_id`. Both of a session's concepts came back for `family function`, scores about `0.65`, `warnings` empty, `is_canonical` false, `blast_radius` null.

Recall returns the nearest concepts in that session. It does not filter to an occasion, and a weak query does not come back empty. Asking session A for "silver studs" returned A's own two concepts at about `0.59` and `0.58`, and none of B's text (`13`). Asking B for "gold jhumkas" returned only B, at about `0.53` (`14`). Dresscode has to keep accounts in different sessions, and it has to read the concept text. A score on its own is not an occasion match.

### Erase

Not an MCP tool. The credential that may erase a `dc-u-` session does it without stopping the serve:

```json
POST /admin/s/<session>/erase
{"confirm":"<session>"}
```

`confirm` repeats the id in the path. Recorded success (`15-erase-a.json`) is `200` and `application/json`:

```json
{"session":"dc-u-probe-a","already_absent":false,"removed":{
  "sessions":1,"interactions":1,"concepts":2,"vectors":2,"edges":3,
  "synonyms":0,"canonization_events":0,"reservations":0,"write_intents":1,
  "session_stats":1,"lease_refusals":0,"leases":1},"fence_token":2}
```

The same call again (`18-erase-a-again.json`) is also `200`, with `already_absent` true, every `removed` count `0`, and the same `fence_token`. That is the "gone" report. Safe to repeat.

A recall on the old `Mcp-Session-Id`, and a fresh `initialize`, then both return HTTP `200` with `application/json` and no hits (`16`, `17`):

```json
{"jsonrpc":"2.0","id":16,"error":{"code":-32003,"message":"lambo: this session's memory was erased by an operator. NOTHING WAS READ OR WRITTEN, and every later call to this session is refused: it does not come back. Do not retry; carry on with the work without memory."}}
```

Nothing was read. Treat `-32003` as deleted, not as an empty history and not as a retry. The id cannot be opened again.

Erasing A left B's concepts in place (`19-recall-b-after-a-erased.json`, the same two hits as before). B was erased after that check (`20-erase-b.json`, `already_absent` false, 2 concepts and 2 vectors removed) so the scratch text does not stay in the store.

## What was proved

On this one serve, in `dc-u-probe-a` and `dc-u-probe-b`:

- Each session stored a different look: a kept fix and a dismissed fix, both worded as family function. Recall of "family function" in A returned the gold jhumkas and the red lip, and not the silver studs or the braid. Recall in B returned the opposite pair.
- A query that used the other session's own words still did not return the other session's concepts.
- Erasing A removed its two concepts. The next recall and the next initialize returned `-32003` and no concept text. A second erase reported `already_absent: true`. B still recalled its own look, and was then erased.

## Recordings

| File | Call |
|---|---|
| `01-initialize-no-token.json` | `initialize` with no token, `401` |
| `02-initialize-a.json` | open `dc-u-probe-a` |
| `03-initialized-a.json` | `notifications/initialized`, `202` |
| `04-tools-list.json` | the eight tools and their schemas |
| `05-derive-a.json` | two concepts, receipt pending |
| `06-stats-wait-a.json` | receipt `applied`, two embeddings |
| `07-initialize-b.json` | open `dc-u-probe-b` |
| `08-initialized-b.json` | `notifications/initialized` for B |
| `09-derive-b.json` | B's two concepts |
| `10-stats-wait-b.json` | B's receipt `applied` |
| `11-recall-a-family-function.json` | A's kept and dismissed fixes |
| `12-recall-b-family-function.json` | B's kept and dismissed fixes |
| `13-recall-a-other-look.json` | A queried for B's words; only A's concepts |
| `14-recall-b-other-look.json` | B queried for A's words; only B's concepts |
| `15-erase-a.json` | first erase, `already_absent` false |
| `16-recall-a-after-erase.json` | recall, error `-32003`, nothing read |
| `17-initialize-a-after-erase.json` | initialize of an erased id, error `-32003` |
| `18-erase-a-again.json` | second erase, `already_absent` true |
| `19-recall-b-after-a-erased.json` | B unchanged |
| `20-erase-b.json` | B erased after the check |
