# Keys

Names and where they live. No values in this file. `.env` is gitignored and holds the values on this machine.

| Name | What it is | Where it lives |
|---|---|---|
| `YOUCAM_API_KEY` | YouCam v2 Bearer token | Keyring `service=youcam`, `kind=api-key`, and `.env` |
| `YOUCAM_API_SECRET` | Stored beside the key. The API call does not send it | Keyring `service=youcam`, `kind=api-secret`, and `.env` |
| `WORKERS_AI_API_TOKEN` | Workers AI token for Clef, Clef-flash and `bge-m3` | Keyring `service=cloudflare`, `kind=workers-ai`, and `.env`. Not named `CLOUDFLARE_API_TOKEN` |
| `CLOUDFLARE_ACCOUNT_ID` | Account id for the Workers AI URL | `.env` |
| `RESEND_API_KEY` | Sign-in mail. Send-only. The key already used for `nryn.dev` | `.env` |
| `RESEND_FROM` | `dresscode@nryn.dev` | `.env` |
| `OPENROUTER_API_KEY` | Jev, kept for the comparison. Clef is the picker | `.env`, and the OMP credential store |
| `OPENROUTER_BASE_URL` | `https://openrouter.ai/api/v1` | `.env` |
| `LAMBO_URL` | Dresscode's own Lambo HTTP MCP | `.env`. This machine, `127.0.0.1:7720` |
| `LAMBO_DRESSCODE_TOKEN` | Bearer token for that server | `.env`, and the server's environment file outside this repo |

YouCam console balance: 1040 credits. They do not expire.

Lambo for Dresscode is a separate process from the dogfood writer. Its sqlite file is `~/lambo-dresscode/dresscode.db`. The embedder is Workers AI `@cf/baai/bge-m3`. Sessions for accounts use the prefix `dc-u-`. The dogfood server stays on ports 7700 and 7710.
