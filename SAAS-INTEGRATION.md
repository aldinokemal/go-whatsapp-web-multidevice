# SaaS_Construction integration

This fork of `go-whatsapp-web-multidevice` adds a thin middleware layer so each container instance can serve exactly one SaaS organization. When the SaaS env vars are unset the fork behaves identically to upstream — the middleware is a no-op kill-switch.

Companion repo: `SaaS_Construction` (`feat/whatsapp-agent` branch). See `docs/superpowers/specs/2026-05-19-whatsapp-agent-design.md` for the full design.

## What the fork adds

```
src/internal/saas/
├── config.go                # env loader (lazy, once)
├── middleware_outbound.go   # HMAC-signs outbound webhooks; redirects URL to SaaS
├── middleware_inbound.go    # gates /send/* with X-Saas-Token
├── group_filter.go          # drops group-chat events before forwarding
└── health.go                # /healthz endpoint (paired_at, last_message)
```

Three upstream files received small additive edits — all gated on `saas.Enabled()`:

- `src/infrastructure/whatsapp/webhook.go` — outbound webhook signer + URL override.
- `src/infrastructure/whatsapp/webhook_forward.go` — group filter + `last_message` timestamp.
- `src/cmd/rest.go` — `/healthz` route + `/send/*` `X-Saas-Token` middleware.

## Required env vars

Add these to `src/.env` (alongside the existing `WHATSAPP_*` ones from `.env.example`):

| Var | Purpose |
| --- | --- |
| `ORG_ID` | UUID of the SaaS organization this bot serves. Sent as `X-Saas-Org-Id` on every outbound webhook. |
| `SAAS_WEBHOOK_URL` | E.g. `http://host.docker.internal:3000/api/v1/webhooks/whatsapp`. When set, OVERRIDES the upstream `WHATSAPP_WEBHOOK` list. |
| `SAAS_WEBHOOK_SECRET` | Shared secret. Must match `whatsapp_bot_instances.container_token` in the SaaS DB. Used to HMAC-sign outbound webhooks (`X-Bot-Token-Hmac`). |
| `SAAS_INBOUND_SECRET` | Shared secret the SaaS sends on every `/send/*` call (`X-Saas-Token`). Same value as `SAAS_WEBHOOK_SECRET` by default — they're directionally distinct knobs in case you want to rotate one without the other. |

Leave them empty to run a generic gowa container.

## Outbound webhook contract

When SaaS is enabled, every outbound POST to `SAAS_WEBHOOK_URL` carries:

```
Content-Type: application/json
X-Hub-Signature-256: sha256=<existing upstream HMAC, untouched>
X-Saas-Org-Id:      <ORG_ID env>
X-Bot-Timestamp:    <unix seconds>
X-Bot-Token-Hmac:   <hex(HMAC-SHA256(SAAS_WEBHOOK_SECRET, raw body bytes))>
```

The SaaS side verifies `X-Bot-Token-Hmac` against the decrypted `container_token`, with a ±5-minute drift window on `X-Bot-Timestamp` as replay defence. See `src/app/api/v1/webhooks/whatsapp/route.ts` in the SaaS repo.

Group chats (any payload where `chat_id` ends in `@g.us` or `chat_type === "group"`) are dropped at the bot before the HTTP attempt — no bandwidth, no SaaS-side rate-limit slot.

## Inbound `/send/*` contract

When SaaS is enabled, every call to `/send/message` (and any other `/send/*` route) must carry:

```
X-Saas-Token: <SAAS_INBOUND_SECRET>
```

Constant-time compared against the env var. Mismatch → 401 with `{ "ok": false, "error": "saas_token_invalid" }`.

The upstream basic-auth and device-scope middlewares still run after this check — they are not replaced.

## `/healthz`

Returns the SaaS-specific status payload:

```json
{
  "status": "ok",
  "saas_enabled": true,
  "org_id": "<uuid>",
  "paired_at": "2026-05-19T18:00:00Z",
  "last_message": "2026-05-19T18:01:42Z"
}
```

Distinct from the upstream `/health` endpoint (which just answers "is the WhatsApp client connected").

## Pairing flow (Phase A — manual, single-org dev)

1. On the SaaS side, register the bot row and grab the printed token:

   ```bash
   cd ../SaaS_Construction
   pnpm db:seed-whatsapp-bot acme http://host.docker.internal:3000
   # → prints SAAS_WEBHOOK_SECRET=<token> + SAAS_INBOUND_SECRET=<token>
   ```

2. Paste both into `whatsapp-bot/src/.env` along with `ORG_ID` and `SAAS_WEBHOOK_URL`.

3. Bring the bot up:

   ```bash
   docker compose up --build
   ```

4. Open the QR pairing page (the upstream UI), scan with the test WhatsApp number. Session keys persist in `./storages` so future restarts re-pair automatically.

5. Flip the SaaS bot row status to `active` manually (Phase A doesn't have an admin UI yet):

   ```sql
   UPDATE whatsapp_bot_instances
   SET status = 'active', paired_at = now()
   WHERE organization_id = '<acme-uuid>' AND deleted_at IS NULL;
   ```

6. Add Alberto's WhatsApp number to `notification_settings` so `resolveUser` recognises it. Use the SaaS-side encryption helpers (`encryptedText` + `hmacBytea`) — never insert plaintext directly.

7. Send "ayuda" from the test phone. You should see a reply within ~5–15s (cron-tick latency + LLM round-trip).

## Operational notes

- Phase A runs ONE container per org. Multi-org orchestration lands in Phase C.
- The `WHATSAPP_WEBHOOK` env from upstream is effectively replaced by `SAAS_WEBHOOK_URL` when SaaS is on — leaving it set is harmless but redundant.
- `host.docker.internal` on Linux requires the `extra_hosts: host-gateway` entry already added to `docker-compose.yml`.
- The pairing data lives in `./storages/whatsapp.db` (already volumed). Back up that file before any container recreate.
- The fork pins to upstream `main`. To bump upstream, rebase `feat/saas-middleware` on the new `main` and re-run the manual smoke (Task 22).
