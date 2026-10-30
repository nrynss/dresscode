# Feedback log

Friction and wins with the services and libraries Dresscode uses, recorded when they were hit. The submission's feedback, and issues for Keel, Chaaya and Lambo, are drafted from this file.

Each entry gives what happened, the evidence, what it cost, and what would fix it. Never paste a key.

## YouCam API

- **A stored key with a stray space is rejected as unrecognised.** The key read back from the keyring with one extra whitespace character got `401 InvalidApiKey` ("This API key is not recognized") from both the India and global servers; trimmed, it worked. *Fix:* trim keys when loading them, and say so in `docs/keys.md`.
- **v2 has no balance endpoint.** `/s2s/v2.0/client/credit` is `404`; the v1 path needs the older token flow. Units are visible only in the console. *Fix:* meter units ourselves through Keel `cost`, starting from the console balance T0.2 records.

## Cloudflare Workers AI

- **An account-owned token fails the user-token check.** A token made from the Workers AI template answers `Invalid API Token` at `/user/tokens/verify` but `active` at `/accounts/{id}/tokens/verify`, and works for inference. *Fix:* verify at the account path.

## Lambo

- **Idle sessions expire in minutes.** Two orchestrator MCP sessions (`dc-u-orch-01`, `dc-u-orch-02`) died with `404 Session not found` after roughly 10–20 minutes of idling between calls; the next `tools/call` on the old `Mcp-Session-Id` was an HTTP `404` plain-text, not a JSON-RPC error. A later `initialize` with the same path works. *Cost:* two landing records were delayed, none lost — each was re-sent on a fresh session. *Fix for the product:* a real user's second visit can idle far longer than that, so Dresscode's memory client (T2.3) must treat `404 Session not found` as "initialize again, then replay the call", not as a failure. Worth checking whether T2.3's client already does; if not, that is a gap to close before T3.8.

## Resend

- **A refused send carries no error name.** The T2.4b live send (one POST to `/emails`, 2026-10-30T13:38:42Z) answered `403` in 0.37 s with an empty `name`, where T2.4's recorded errors and Resend's docs carry a snake_case name such as `validation_error`. An unnamed 403 reads as account or domain gating (from-domain not verified, or the key lacking send permission), not message shape — but the response says none of that. *Cost:* one refused call, nothing; no mail, no retry. *Fix:* the console must say what the API will not; and the sender's typed `Refusal` should surface the name verbatim when Resend gives one, so an empty name is itself the tell.
