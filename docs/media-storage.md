# Media storage and cleanup

GOWA currently has no age-based retention setting or cleanup endpoint for received
media. Operators can remove old downloaded files manually without deleting the
WhatsApp session or chat databases.

## Storage locations

Paths are relative to GOWA's working directory: normally `src/` when running from
source, the directory where you start the binary, or `/app` inside Docker. The
repository's `docker-compose.yml` mounts `./storages` and `./statics` from the
project root into `/app/storages` and `/app/statics`.

| Path | Contents |
| --- | --- |
| `statics/media/` | Media downloaded for webhook payloads; REST/MCP downloads also use subdirectories by chat and message date. |
| `storages/<timestamp>-<uuid>[.<extension>]` | Images saved by the incoming-image handler when automatic media downloads are enabled. |
| `storages/whatsapp.db` | WhatsApp session and encryption state by default. Preserve it and any separate database configured with `DB_KEYS_URI`. |
| `storages/chatstorage.db` | Chat/message history and other application records. Deleting media files does not prune this database. |
| `storages/oauth.db` | MCP OAuth data by default. |
| `storages/scheduled/` | Attachments retained for scheduled sends. Let the scheduler manage these files. |
| `storages/ui/` | Cached web dashboard. |
| `storages/history-*.json` | History-sync dumps, separate from downloaded media. The commands below leave them intact. |

An incoming image can have copies in both `storages/` and `statics/media/`. Never
delete the whole `storages/` directory to reclaim media space. Keep database files,
their SQLite `-wal`/`-shm` sidecars, and scheduled-send attachments. If you use custom
database paths or store your own files in these directories, exclude them before
running cleanup.

## Preview and remove old media

Choose a retention period that allows webhook consumers and Chatwoot to finish
copying attachments. Deletion makes previously returned local file paths and
static URLs unavailable. It does not delete messages from WhatsApp or GOWA's chat
history. A later media download may succeed if the stored metadata and WhatsApp's
copy are still available; do not rely on that as a backup.

Pause GOWA and any other writers during cleanup, and back up files you still need.
Run these commands from the directory containing the deployment's `statics/` and
`storages/`. For the repository's Docker Compose bind mounts, this is the project
root on the host. For named volumes or custom mounts, use their actual mounted
directories. Restart GOWA after cleanup.

The examples use GNU find on a Linux host. First preview files whose
modification time is at least 31 full days ago (`-mtime +30` rounds down to whole
24-hour periods). This is the local file age, not the original message date.

```sh
find ./statics/media -type f -mtime +30 -print
find ./storages -maxdepth 1 -type f -mtime +30 \
  -name '[0-9]*-????????-????-????-????-????????????*' -print
```

The second command only matches top-level filenames with the generated
timestamp/UUID shape; it does not descend into `storages/scheduled/` or `storages/ui/`.
Neither command filters by extension, so extensionless downloads are included.
Neither follows symbolic links. Review every selected path before deletion.

After checking the preview, remove files with the same filters:

```sh
find ./statics/media -type f -mtime +30 -delete
find ./storages -maxdepth 1 -type f -mtime +30 \
  -name '[0-9]*-????????-????-????-????-????????????*' -delete
```

These commands leave directories in place and do not shrink the chat database.
There is currently no age-based chat-history pruning API. Deleting a session or
chat database is not a substitute for media retention.

## Reduce future downloads

Set `WHATSAPP_AUTO_DOWNLOAD_MEDIA=false` or start GOWA with
`rest --auto-download-media=false`, then restart the service. For Docker Compose,
recreate the container after changing its environment so the new setting takes
effect. This stops automatic incoming-media downloads, including the extra image
copy in `storages/`; it does not remove existing files or stop explicit REST/MCP
media downloads. Webhook consumers that require local media files must account for
this setting.

If you only want to skip status media, use `WHATSAPP_IGNORE_STATUS_MEDIA=true` or
`--ignore-status-media=true`. Chat/message records and history-sync dumps can still
grow independently of media downloads.
