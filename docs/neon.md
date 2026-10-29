# Store WhatsApp sessions in Neon

Use Neon as managed PostgreSQL storage for GOWA's WhatsApp sessions and encryption
keys. This guide starts a new GOWA deployment with Docker Compose and a dedicated
Neon database. You need a repository checkout, Docker with Compose, and a Neon account.

Changing `DB_URI` does not migrate existing SQLite sessions. Pair WhatsApp again
with the new deployment. Stop the previous deployment before switching, and keep
its storage backup. Do not run multiple deployments against the same WhatsApp
session or reuse live session data copied into another Neon branch.

## Create the Neon database

1. In the Neon console, create a project in a region near your GOWA server.
2. Create a fresh database dedicated to this deployment. Use a role that can create
   and alter tables in its schema, as well as read and write them.
3. Open **Connect** and select the branch, compute, database, and role.
4. Turn **Connection pooling** off and copy the direct connection string.
   The direct hostname has no `-pooler` suffix.

See [Neon's connection instructions](https://neon.com/docs/connect/connect-from-any-app).
GOWA initializes and upgrades its WhatsApp schema at startup. This guide uses a
direct connection. See [Neon's guidance on pooled and direct connections](https://neon.com/docs/connect/connection-pooling).

## Set GOWA's environment

From the repository root, create `src/.env` if it does not exist. If it already
exists, edit the matching entries without replacing the rest of the file. For a
new file, use this minimal configuration instead of copying the sample webhooks:

```dotenv
APP_PORT=3000
APP_HOST=0.0.0.0
APP_BASIC_AUTH='admin:replace-with-strong-password'
DB_URI='postgres://ROLE:PASSWORD@DIRECT_HOST/DATABASE?sslmode=verify-full'
DB_KEYS_URI=
WHATSAPP_WEBHOOK=
```

Replace the Basic Auth password and the database placeholders locally. Keep
credentials in `src/.env`, exclude the file from version control, and restrict
access with `chmod 600 src/.env`.

Adapt the connection string from Neon before setting `DB_URI`:

- Change the leading `postgresql://` to `postgres://`. GOWA's database selector
  recognizes the latter PostgreSQL URI scheme.
- Use `DB_URI`. GOWA does not read Neon's conventional `DATABASE_URL` variable.
- Replace `sslmode=require` with `sslmode=verify-full` to require TLS and verify
  the server certificate and hostname. The driver uses the system CA store by
  default. If you use a custom image, include trusted CA certificates. See
  [Neon's secure connection guide](https://neon.com/docs/connect/connect-securely).
- Remove `channel_binding=require` if Neon includes it, preserving the other query
  parameters and valid `?` and `&` separators. GOWA uses `lib/pq` v1.12.3, whose
  [authentication implementation](https://github.com/lib/pq/blob/v1.12.3/conn.go)
  supports SCRAM-SHA-256 without channel binding.
- Keep the copied password's URL encoding. Special characters in URI credentials
  must be percent-encoded. The single quotes in the example also prevent Compose
  from expanding dollar signs in the value.

Leave `DB_KEYS_URI` blank so durable session keys use the primary `DB_URI` database.
Do not use an in-memory key database for a deployment that must survive restarts.
Use the environment configuration in this guide and remove any existing
`--db-uri` or `--db-keys-uri` flags from your startup command.

Neon stores WhatsApp session and key data only. GOWA still uses local
`storages/chatstorage.db` for chat history and, when MCP OAuth is enabled,
`storages/oauth.db` for OAuth state. Downloaded media, uploads, and QR files remain
under `statics/`. Keep both storage directories persistent.

## Start GOWA with Docker Compose

Create `compose.neon.yml` in the repository root with the following content.
This builds the checked-out source and keeps local state in named volumes:

```yaml
services:
  gowa:
    build:
      context: .
      dockerfile: docker/golang.Dockerfile
    command: ["rest"]
    restart: unless-stopped
    env_file:
      - src/.env
    ports:
      - "127.0.0.1:3000:3000"
    volumes:
      - gowa_storages:/app/storages
      - gowa_statics:/app/statics

volumes:
  gowa_storages:
  gowa_statics:
```

Start the service and inspect its status and logs:

```sh
docker compose -f compose.neon.yml up -d --build
docker compose -f compose.neon.yml ps
docker compose -f compose.neon.yml logs --tail=100 gowa
```

Check for database initialization errors and confirm that the service stays
running. Keep raw logs private because startup output can include credentials.
The example exposes GOWA on the Docker host's loopback address.
For remote access, configure an authenticated HTTPS reverse proxy separately.

## Verify the database and restart behavior

1. Open `http://localhost:3000` on the Docker host and sign in with your Basic Auth
   credentials. Use the UI to add a device and pair WhatsApp.
2. In Neon's SQL Editor, select the same branch and database and run this query:

   ```sql
   SELECT table_name
   FROM information_schema.tables
   WHERE table_schema = 'public'
     AND table_name LIKE 'whatsmeow_%'
   ORDER BY table_name;
   ```

   Confirm that WhatsApp store tables exist. Inspect table names only. Session
   rows contain private key material and must not appear in screenshots or logs.
3. Run `docker compose -f compose.neon.yml restart gowa`. Confirm that the device
   reconnects without pairing again and that local chat and media state remains.

Keep the same Neon database and named volumes on subsequent starts. Do not use
`docker compose down -v` unless you intend to delete the local state.

## Troubleshoot startup and persistence

- If GOWA reports an unknown database type, check the `postgres://` prefix.
- If GOWA still creates `whatsapp.db`, check `DB_URI`, `env_file`, and command-line
  overrides. After an environment change, run the Compose `up` command again to
  recreate the container. A restart alone does not reload container environment.
- For permission or schema errors, check the selected Neon database and role.
  The role needs permission to create and alter tables, as well as read and write them.
- For TLS errors, keep TLS enabled and check the hostname, network access, and CA
  configuration. Remove the unsupported `channel_binding` parameter if present.
- If pairing is lost after restart, confirm that `DB_URI` still points to the same
  branch and database and that `DB_KEYS_URI` is blank. If chat history or media is
  missing, check that the same named volumes are mounted.

This guide was checked against GOWA's configuration and driver source. A live Neon
connection and WhatsApp pairing were not tested. Complete the verification steps
above in your environment.
