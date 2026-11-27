#!/usr/bin/env bash
set -euo pipefail
umask 077
cd "$(dirname "$0")/.."
: "${BACKUP_NAMESPACE:?Set BACKUP_NAMESPACE}" "${BACKUP_BUCKET:?Set BACKUP_BUCKET}"
command -v oci >/dev/null
backup_dir=${BACKUP_DIR:-/var/backups/student-job-portal}
mkdir -p "$backup_dir"
exec 9>"$backup_dir/.lock"
flock -n 9
dc() { docker compose --env-file "${PORTAL_ENV_FILE:-.env.production}" -f docker-compose.production.yml "$@"; }
dc config --quiet
work_dir=$(mktemp -d "$backup_dir/.work.XXXXXX")
archive="$backup_dir/portal-$(date -u +%Y%m%dT%H%M%SZ).tar"
paused=false
cleanup() {
    result=$?
    trap - EXIT
    if $paused; then
        if ! dc start --wait --wait-timeout 120 minio api; then
            printf 'ERROR: restart failed; run docker compose start minio api immediately.\n' >&2
            result=1
        fi
    fi
    rm -rf -- "$work_dir"
    exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# shortcut: a quiesced snapshot pauses writes, use online/PITR backups when downtime is unacceptable.
paused=true
dc stop api minio
dc exec -T postgres sh -ec 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc --no-owner --no-privileges' >"$work_dir/db.dump"
dc exec -T postgres pg_restore --list <"$work_dir/db.dump" >/dev/null
dc run --rm --no-deps -T --entrypoint tar minio -C /minio_data -czf - . >"$work_dir/minio.tar.gz"
dc start --wait --wait-timeout 120 minio api
paused=false
tar -C "$work_dir" -cf "$archive" db.dump minio.tar.gz
sha256sum "$archive" | cut -d ' ' -f 1 >"$archive.sha256"
for file in "$archive" "$archive.sha256"; do
    oci os object put --auth instance_principal --namespace-name "$BACKUP_NAMESPACE" \
        --bucket-name "$BACKUP_BUCKET" --name "portal/$(basename "$file")" \
        --file "$file" --verify-checksum
done
printf 'Backup uploaded: %s\n' "$(basename "$archive")"
# Remove old local copies only after both remote uploads succeeded.
find "$backup_dir" -maxdepth 1 -type f \( -name 'portal-*.tar' -o -name 'portal-*.tar.sha256' \) -mtime +7 -delete
