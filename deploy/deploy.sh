#!/usr/bin/env bash
# GOWA production deploy script.
#
# Installed on the server as /opt/gowa-deploy/deploy.sh (owned by root) and run by
# GitHub Actions through:  ssh deploy@server <commit-sha>
# The SSH key is locked to this script (forced command), so the "deploy" user can
# run nothing else. See docs/deploy.md for the full setup.
#
# Usage:  deploy.sh [commit-sha]
#   Without a SHA it deploys the latest origin/main.
#   The SHA must be part of main's history (used for normal deploys and rollback).
#
# Steps: fetch -> checkout -> build image -> stop -> backup storages -> start
#        -> health check -> automatic rollback to the previous commit on failure.

set -Eeuo pipefail

# ---- Settings (override via environment if your paths differ) ----------------
SRC_DIR="${GOWA_SRC_DIR:-/opt/gowa}"
COMPOSE_FILE="${GOWA_COMPOSE_FILE:-/opt/gowa-deploy/compose.yml}"
BACKUP_DIR="${GOWA_BACKUP_DIR:-/var/backups/gowa}"
BACKUP_KEEP="${GOWA_BACKUP_KEEP:-7}"
HEALTH_URL="${GOWA_HEALTH_URL:-http://127.0.0.1:3000/}"
HEALTH_TRIES="${GOWA_HEALTH_TRIES:-30}"
BRANCH="main"
LOCK_FILE="${GOWA_LOCK_FILE:-/var/lock/gowa-deploy.lock}"
LOG_FILE="${GOWA_LOG_FILE:-/var/log/gowa-deploy.log}"

log() { printf '[%s] %s\n' "$(date '+%F %T')" "$*" | tee -a "$LOG_FILE" >&2; }
die() { log "ERROR: $*"; exit 1; }

compose() { docker compose -f "$COMPOSE_FILE" "$@"; }

# ---- Only one deploy at a time --------------------------------------------------
exec 9>"$LOCK_FILE"
flock -n 9 || die "another deploy is still running"

# ---- Validate input --------------------------------------------------------------
REQUESTED="${1:-}"
REQUESTED="${REQUESTED//[[:space:]]/}"
if [[ -n "$REQUESTED" && ! "$REQUESTED" =~ ^[0-9a-f]{7,40}$ ]]; then
  die "invalid commit sha: '$REQUESTED' (only hex commit hashes are accepted)"
fi

[[ -d "$SRC_DIR/.git" ]] || die "$SRC_DIR is not a git repository"
[[ -f "$COMPOSE_FILE" ]] || die "compose file not found: $COMPOSE_FILE"

cd "$SRC_DIR"

# Tracked files edited directly on the server would be lost by the checkout.
if ! git diff --quiet || ! git diff --cached --quiet; then
  git status --short | tee -a "$LOG_FILE" >&2
  die "local changes in $SRC_DIR; commit them to GitHub or discard them first"
fi

log "fetching origin/$BRANCH"
git fetch --quiet origin "$BRANCH"

TARGET="$(git rev-parse --verify "${REQUESTED:-origin/$BRANCH}^{commit}" 2>/dev/null)" \
  || die "commit not found: ${REQUESTED:-origin/$BRANCH}"
git merge-base --is-ancestor "$TARGET" "origin/$BRANCH" \
  || die "commit $TARGET is not part of $BRANCH history; refusing to deploy"

PREVIOUS="$(git rev-parse HEAD)"
log "current: ${PREVIOUS:0:12}  target: ${TARGET:0:12}"

checkout() { git checkout --quiet -B "$BRANCH" "$1"; }

health_check() {
  local i code
  for ((i = 1; i <= HEALTH_TRIES; i++)); do
    # Any HTTP answer below 500 (including 401 from basic auth) means the app is up.
    code="$(curl -s -o /dev/null -m 5 -w '%{http_code}' "$HEALTH_URL" || true)"
    if [[ "$code" =~ ^[1-4][0-9][0-9]$ ]]; then
      log "health check ok (HTTP $code)"
      return 0
    fi
    sleep 2
  done
  log "health check failed (last HTTP code: ${code:-none})"
  return 1
}

backup_storages() {
  [[ -d "$SRC_DIR/storages" ]] || { log "no storages directory, skipping backup"; return 0; }
  mkdir -p "$BACKUP_DIR"
  chmod 700 "$BACKUP_DIR"
  local file="$BACKUP_DIR/storages-$(date +%Y%m%d-%H%M%S)-${PREVIOUS:0:7}.tar.gz"
  tar -czf "$file" -C "$SRC_DIR" storages || { rm -f "$file"; return 1; }
  log "backup saved: $file ($(du -h "$file" | cut -f1))"
  # Keep only the newest $BACKUP_KEEP backups.
  ls -1t "$BACKUP_DIR"/storages-*.tar.gz 2>/dev/null | tail -n +"$((BACKUP_KEEP + 1))" | xargs -r rm -f
}

# ---- Deploy -----------------------------------------------------------------------
checkout "$TARGET"

# Build first while the old container keeps serving, so downtime stays short.
log "building image"
if ! compose build --pull; then
  checkout "$PREVIOUS"
  die "build failed; source restored to ${PREVIOUS:0:12}, running container untouched"
fi

rollback() {
  if [[ "$PREVIOUS" == "$TARGET" ]]; then
    log "no different previous commit to roll back to; restarting current version"
  else
    log "rolling back to ${PREVIOUS:0:12}"
    checkout "$PREVIOUS"
    compose build || log "rollback build failed"
  fi
  compose up -d --remove-orphans || true
  health_check
}

# Stop before backup so the SQLite files are consistent.
log "stopping container"
compose stop

if ! backup_storages; then
  log "backup failed (disk full?); deploy cancelled"
  rollback || die "backup failed AND restart failed; check 'docker compose -f $COMPOSE_FILE logs'"
  die "backup failed; previous version is running again"
fi

log "starting container"
compose up -d --remove-orphans || log "docker compose up returned an error"

if health_check; then
  docker image prune -f >/dev/null 2>&1 || true
  log "deploy OK: ${TARGET:0:12} ($(git log -1 --format=%s))"
  exit 0
fi

# ---- Automatic rollback -------------------------------------------------------------
compose logs --tail=80 2>&1 | tee -a "$LOG_FILE" >&2 || true
if rollback; then
  die "deploy of ${TARGET:0:12} failed; rolled back to ${PREVIOUS:0:12} (app is up)"
fi
die "deploy failed AND rollback failed; check 'docker compose -f $COMPOSE_FILE logs'"
