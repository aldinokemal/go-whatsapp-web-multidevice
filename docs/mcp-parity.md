# MCP parity

House rule S5: whatever the web pages (the GUI) or the REST API can do, the
MCP endpoint at `/mcp` can do too, through the same code. This page is the
inventory: every capability, where it lives on each surface, and the few
actions that stay in the browser on purpose.

How the tools are built:

- **Same functions.** The GUI's actions live in `multiuser/web/actions.go`
  as methods on `web.Deps` that take a context and the acting user. The page
  handlers and the MCP tools (`multiuser/mcpauth/tools.go`) both call them,
  so validation, owner checks, refusals (the same sentences) and the audit
  log are shared. The WhatsApp tools call the same usecase methods as the
  REST handlers.
- **Owner-scoped.** Number tools go through the owner-scoped device usecase
  (`owners.ScopedDevices`): another person's number reads exactly like one
  that does not exist (`device not found`). WhatsApp tools resolve their
  device through the same owner-checked hook as upstream's tools.
- **Admin tools** (`wtf_admin_*`) are hidden from `tools/list` for anyone who
  is not a superadmin, and each one also checks `CheckSuperadmin` on the
  caller when it runs, so a member who calls one anyway is refused without
  learning anything. The actions behind them check it a third time.
- **Annotations.** Every WTF tool sets `readOnlyHint`, `destructiveHint` and
  `idempotentHint`; read-only tools are never destructive.
- **Isolation matrix.** `cmd/multiuser_phase2_isolation_test.go` pins the
  full tool list: a new tool fails `TestEveryMCPToolIsInTheIsolationMatrix`
  until it joins a group of the matrix and so its cross-user check.

## My WhatsApp numbers

| Capability | GUI | REST | MCP tool | Notes |
|---|---|---|---|---|
| List my numbers | `/` (My WhatsApp) | `GET /devices` | `wtf_my_devices` | read-only |
| Link status of each number | `/`, `GET /ui/devices/:id/status` | `GET /devices/:id/status` | `wtf_number_status` | connected, disconnected, signed_out, refused (with the reason), not_linked |
| Start linking a number | `POST /ui/devices` | `POST /devices` | `wtf_link_number` | reserves a slot within the person's limit and the server cap |
| Link with a QR code | `GET /ui/devices/:id/qr.png` | `GET /devices/:id/login` | `wtf_link_number` (no `phone`) | returns the QR as an MCP image (PNG) plus its text |
| Link with a pairing code | `POST /ui/devices/:id/pair-code` | `POST /devices/:id/login/code` | `wtf_link_number` (`phone`) | returns the 8-character code |
| Continue an unfinished link | `GET /ui/devices/:id/link` | `GET /devices/:id/login` | `wtf_link_number` (`device_id`) | |
| Reconnect | `POST /ui/devices/:id/reconnect` | `POST /devices/:id/reconnect` | `wtf_reconnect_number` | also `whatsapp_app action=reconnect` |
| Unlink (remove) | `POST /ui/devices/:id/unlink` | `DELETE /devices/:id` | `wtf_unlink_number` | destructive |
| Rename / clear the name | `POST /ui/devices/:id/label` | `PUT /devices/:id/label` | `wtf_rename_number` | `label: ""` clears it |
| Get the webhook | `GET /ui/devices/:id/webhook` | `GET /devices/:id/webhook` | `wtf_get_webhook` | the secret is write-only: only `has_secret` |
| Set the webhook | `POST /ui/devices/:id/webhook` | `PATCH /devices/:id/webhook` | `wtf_set_webhook` | same guard: public https only, TLS verified |
| Remove the webhook | `POST /ui/devices/:id/webhook` (`remove=1`) | `PATCH /devices/:id/webhook` | `wtf_remove_webhook` | destructive |
| Log a number out, keep the slot | — | `POST /devices/:id/logout` | `whatsapp_app action=logout` | |

## My account (read-only on MCP)

| Capability | GUI | REST | MCP tool | Notes |
|---|---|---|---|---|
| My profile | `/account`, menu | — | `wtf_my_account` | phone, status, admin or not, number limit and count |
| Retention window | `/` (note under the list) | — | `wtf_my_account` | `retention_hours` and the sentence the page shows |
| My browser sessions | `/account` | — | `wtf_my_sessions` | never a session id |
| My connected apps | `/account` | — | `wtf_my_connected_apps` | |
| My API keys | `/account` | — | `wtf_my_api_keys` | name and first characters only, never a key |
| My passkeys | `/account` | — | `wtf_my_passkeys` | never a credential |

## Admin (superadmins only)

| Capability | GUI | REST | MCP tool | Notes |
|---|---|---|---|---|
| List and search users, paged | `/admin/users` | — | `wtf_admin_list_users` | search digits, status filter, sort, 50 a page, each user's numbers |
| Add a user (sign-in link) | `POST /admin/users` | — | `wtf_admin_add_user` | returns the one-time link (15 minutes) |
| Get a sign-in link | `POST /admin/users/:id/signin-link` | — | `wtf_admin_signin_link` | active accounts only |
| Disable / enable (also unlink) | `POST /admin/users/:id/status` | — | `wtf_admin_set_user_status` | disabling ends sessions, keys and connected apps; `also_unlink` |
| Set the number limit | `POST /admin/users/:id/limit` | — | `wtf_admin_set_number_limit` | 0 to 20 |
| Change a user's number | `POST /admin/users/:id/phone` | — | `wtf_admin_change_number` | ends every credential and old sign-in link; never self or an admin |
| Unlink a user's number | `POST /admin/users/:id/devices/:device/unlink` | — | `wtf_admin_unlink_number` | destructive; only a number that user owns |
| Delete a disabled user | `POST /admin/users/:id/delete` | — | `wtf_admin_delete_user` | destructive; `confirm` must be their phone number; never self or the sign-in number's owner |
| Activity (audit log) | `/admin/activity` | — | `wtf_admin_activity` | paged, optional `user_id`; plain-word sentences, no IPs |
| Waitlist | `/admin/waitlist` | — | `wtf_admin_waitlist` | waiting or declined, paged, with counts and capacity |
| Let in / decline / reopen / remove | `POST /admin/waitlist/:digits/{admit,decline,reopen,remove}` | — | `wtf_admin_waitlist_decide` | admit sends the link from the sign-in number, or returns it |
| Settings | `/admin/settings` | — | `wtf_admin_settings` | sign-in number and its state, choices, capacity, memory, disk |
| Set the sign-in number | `POST /admin/settings/service-number` | — | `wtf_admin_set_signin_number` | one of the admin's own linked numbers |
| Capacity | `/admin/settings`, `/admin/waitlist` | — | `wtf_admin_capacity` | linked against the cap, users, waiting, near full |

`POST /admin/users/:id/delete` arrives with the admin Delete account branch;
`wtf_admin_delete_user` runs the same guards (`web.Deps.AdminDeleteUser`) and
the same `account.Deleter`.

## WhatsApp (upstream REST)

| Capability | GUI | REST | MCP tool | Notes |
|---|---|---|---|---|
| Send text, media, location, contact, poll, link, forward | — | `POST /send/*` | `whatsapp_send` | also scheduling |
| Scheduled sends | — | `/send/schedules*` | `whatsapp_schedule` | |
| Message actions (react, edit, revoke, delete, read, played, star, download) | — | `/message/:id/*` | `whatsapp_message` | forward is `whatsapp_send type=forward` |
| Chats, messages, contacts, archive | — | `GET /chats`, `/chat/:jid/messages`, `/chat/:jid/archive`, `/user/my/contacts` | `whatsapp_chat` | |
| Pin a chat, disappearing timer, request older history | — | `/chat/:jid/{pin,disappearing,history}` | `wtf_chat_settings` | |
| Groups (create, join, leave, info, participants, admins, invite link, name, topic, settings, join requests) | — | `/group*` | `whatsapp_group` | participants export is the `participants` list |
| Group info from an invite link | — | `GET /group/info-from-link` | `wtf_group_preview` | read-only |
| Connection: status, QR, pairing code, logout, reconnect | — | `/app/*` | `whatsapp_app` | |
| Contact info, avatar, is on WhatsApp, business profile | — | `GET /user/{info,avatar,check,business-profile}` | `wtf_contact_info` | read-only |
| My privacy, groups, channels | — | `GET /user/my/{privacy,groups,newsletters}` | `wtf_my_whatsapp` | read-only |
| Change my display name | — | `POST /user/pushname` | `wtf_set_push_name` | |
| Presence and typing | — | `POST /send/presence`, `POST /send/chat-presence` | `wtf_presence` | |
| Reject a call | — | `POST /call/reject` | `wtf_reject_call` | |
| Channels: posts, media, unfollow | — | `/newsletter/*` | `wtf_newsletter` | unfollow is destructive |

## Deliberately not on MCP

These stay in the browser with a person present. An AI agent holding a token
must not be able to mint or extend credentials, or destroy the account it acts
for. Read-only views of each are tools (above).

| Capability | GUI | REST | Why not on MCP |
|---|---|---|---|
| Make an API key | `POST /account/api-keys` | — | Minting a credential: an agent could create a key that outlives the token it was given, and keep access after the person disconnects it. |
| Revoke an API key | `POST /account/api-keys/:id/revoke` | — | Credential management stays with the person; an agent acting for them must not cut off their other tools. |
| Add a passkey | `/auth/passkey/register/*` | — | A passkey is a way in; it needs the person's authenticator, which an agent does not have, and must never be added for them. |
| Remove a passkey | `POST /account/passkeys/:cred/delete` | — | Could lock the person out of their own account. |
| End a session, sign out everywhere | `POST /account/sessions/:handle/signout`, `/account/signout-everywhere` | — | Ending the person's sign-ins is a credential decision for the person. |
| Disconnect a connected app | `POST /account/connections/:family/revoke` | — | An agent must not revoke or rearrange the connections that give it (or another app) access. |
| Delete my own account | `POST /account/delete` | — | Self-destruct: the agent would destroy the account it acts for. An admin deleting another, disabled account is `wtf_admin_delete_user`. |
| Sign in, verify, sign out | `/signin*`, `/signout`, `/auth/passkey/login/*` | — | These are how a person gets a session; an MCP caller is already signed in by its token. |

Other REST routes not offered as tools, and why:

| Capability | REST | Why not on MCP |
|---|---|---|
| Change my avatar, set a group photo | `POST /user/avatar`, `POST /group/photo` | They take an uploaded file (multipart); MCP tool arguments carry no file. Use REST. |
| WhatsApp passkey login | `/app/passkey*` | A browser WebAuthn ceremony with the phone; not something an agent can complete. |
| Chatwoot settings | `/devices/:id/chatwoot/config`, `/chatwoot/configs` | Only when the operator enables Chatwoot; it stores a third-party API token, which is a credential. |
| Server version, the app's device list | `GET /app/info`, `GET /app/devices` | The version is in the MCP `initialize` answer (`serverInfo.version`); the numbers are `wtf_my_devices`. |
| Media files | `GET /files/...` | Download links the tools already return (`file_url`); fetched over HTTPS with a session or API key. |
| Live events | `/ws` | Not served in multi-user mode; webhooks (`wtf_set_webhook`) carry events. |
