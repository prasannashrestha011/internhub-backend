#!/usr/bin/env bash
set -euo pipefail
umask 077
archive=$(realpath "${1:?Usage: restore.sh /path/to/portal-backup.tar (empty deployment only)}")
expected=$(cat "$archive.sha256")
actual=$(sha256sum "$archive" | cut -d ' ' -f 1)
if [[ "$actual" != "$expected" ]]; then
    printf 'Backup checksum does not match.\n' >&2
    exit 1
fi
cd "$(dirname "$0")/.."
dc() { docker compose --env-file "${PORTAL_ENV_FILE:-.env.production}" -f docker-compose.production.yml "$@"; }
dc config --quiet
work_dir=$(mktemp -d)
trap 'rm -rf -- "$work_dir"' EXIT
tar -C "$work_dir" -xf "$archive" db.dump minio.tar.gz

# Refuse existing deployments before stopping anything or changing stored data.
if [[ -n "$(dc ps -aq api)" ]]; then
    printf 'Restore requires a fresh deployment with no API container.\n' >&2
    exit 1
fi
dc up -d --wait postgres
tables=$(dc exec -T postgres sh -ec 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc "SELECT count(*) FROM information_schema.tables WHERE table_schema = '\''public'\''"')
if [[ "$tables" != "0" ]]; then
    printf 'Refusing to overwrite an existing database.\n' >&2
    exit 1
fi
if [[ -n "$(dc ps --status running -q minio)" ]]; then
    printf 'Refusing to restore into a running MinIO server.\n' >&2
    exit 1
fi
dc run --rm --no-deps -T --entrypoint sh minio -ec 'test -z "$(ls -A /minio_data)"'
dc exec -T postgres sh -ec 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --no-owner --no-privileges --exit-on-error' <"$work_dir/db.dump"
dc run --rm --no-deps -T --entrypoint tar minio -C /minio_data -xzf - <"$work_dir/minio.tar.gz"
dc up -d --wait --wait-timeout 120 minio api
printf 'Restored database and documents; verify the app before directing traffic here.\n'
