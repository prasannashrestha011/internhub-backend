# OCI deployment: one VM, 1 OCPU / 8 GB

The production stack runs the API, PostgreSQL 16, MinIO, and Caddy. Only Caddy
publishes ports 80 and 443. The database, API port, storage API port, and storage
console are not published. Docker volumes hold database records, documents, and
TLS certificates. This is a single VM with no failover.

## First deployment

The VPS does not hold the source code. GitHub Actions
(`.github/workflows/deploy.yml`) tests every push to `main`, builds the `api`,
`minio` and `caddy` images for x86_64 (`linux/amd64`), pushes them to GHCR, then
connects over SSH. It copies `docker-compose.production.yml` and the backup scripts
to `/opt/student-job-portal`, pulls the new images, and restarts the stack. The VPS
must be an x86_64 Ubuntu machine; these images do not run on ARM VMs.

On the VPS the directory looks like this. You create only `.env.production`:

```
/opt/student-job-portal/
├── .env.production              # you create this (secrets, never committed)
├── docker-compose.production.yml  # copied by CI
└── deploy/                      # copied by CI: backup.sh, restore.sh, systemd units
```

1. Install Docker Engine and the Compose plugin on the Ubuntu VPS. Enable Docker at
   boot. Create a deploy user that can run Docker and owns the app directory:

   ```bash
   sudo adduser --disabled-password deploy
   sudo usermod -aG docker deploy
   sudo mkdir -p /opt/student-job-portal
   sudo chown deploy:deploy /opt/student-job-portal
   ```

2. Set up SSH access for GitHub Actions. On your machine, generate a key pair used
   only for deploys, and install the public key for the deploy user on the VPS:

   ```bash
   ssh-keygen -t ed25519 -f portal-deploy -N '' -C github-actions-deploy
   ssh-copy-id -i portal-deploy.pub deploy@YOUR_VPS_IP
   ssh-keyscan -H YOUR_VPS_IP          # output goes into VPS_KNOWN_HOSTS
   ```

   In GitHub, open **Settings → Environments**, create an environment named
   `production`, and add these secrets:

   | Secret | Value |
   |---|---|
   | `VPS_HOST` | VPS IP address or hostname |
   | `VPS_USER` | `deploy` |
   | `VPS_SSH_KEY` | contents of the private key file `portal-deploy` |
   | `VPS_KNOWN_HOSTS` | output of `ssh-keyscan -H YOUR_VPS_IP` |

   If SSH listens on a port other than 22, add an environment variable `VPS_PORT`.
   Delete the local private key once it is saved in GitHub.
3. Create DNS records for `api.example.com` and `files.example.com` pointing to the
   VM. Allow inbound TCP 80/443 in the OCI network rules and host firewall. Keep SSH
   restricted to your administrator addresses. This configuration assumes Caddy is
   directly internet-facing; do not put another proxy/CDN in front without defining
   its trusted addresses. The Docker subnet `172.30.0.0/24` must be free on this host.
4. As the deploy user, create `/opt/student-job-portal/.env.production` with the
   contents of `.env.production.example` from this repository, then fill it in:

   ```bash
   cd /opt/student-job-portal
   nano .env.production     # paste .env.production.example, then fill values
   chmod 600 .env.production
   openssl rand -hex 32     # run once per secret
   ```

   Generate independent database, JWT, storage, and OTP secrets. Set the real DNS
   names, frontend HTTPS origin, Oracle SMTP credentials, and approved sender. Use
   single quotes around environment values containing `$` to prevent Compose
   interpolation. Do not commit the completed file.
5. For a frontend at `portal.example.com` and API at `api.example.com`, set
   `COOKIE_DOMAIN=example.com`. All subdomains receiving these cookies must be under
   your control. This lets the Next.js server read the authentication cookies.
   Configure the client with `NEXT_PUBLIC_API_URL=https://api.example.com/api/v1`
   and its server with `API_INTERNAL_URL=https://api.example.com/api/v1`. Unrelated
   frontend/API domains are not supported by the current SameSite=Lax session flow.
   A frontend reverse proxy serving the API on the frontend's own host can instead
   use host-only cookies (`COOKIE_DOMAIN=`).
6. Merge to `main` (or run the workflow manually from the **Actions** tab). The
   deploy job pulls the images and starts the stack. Then check:

   ```bash
   curl --fail https://api.example.com/ready
   ```

   The deploy logs the VPS in to GHCR with the job's temporary token and logs out
   afterwards, so private packages need no stored credentials. To pull by hand
   (for example during a restore), log in first with a GitHub personal access token
   that has `read:packages`:
   `echo TOKEN | docker login ghcr.io -u prasannashrestha011 --password-stdin`.

   Caddy obtains and renews HTTPS certificates. Allow time for the first certificate
   issuance. Confirm login, cookie refresh, an employer draft, CV upload/download,
   profile images, and an application from the actual frontend before opening access.
   `POST /api/v1/auth/refresh` accepts the HttpOnly refresh cookie and replaces both
   cookies. JSON token clients remain supported. The frontend refresh helper uses
   cookies; automatic renewal in Next.js server-rendered pages remains a separate
   frontend behavior (those pages currently redirect when an access token expires).

The API uses at most 10 open / 5 idle database connections. Container memory ceilings
are 1 GiB for the API, 3 GiB for PostgreSQL, 2 GiB for MinIO, and 256 MiB for Caddy.
The remaining memory is for the host and filesystem cache. These are starting
limits, not measured capacity. Images are built by GitHub Actions, so deploys do
not compile on the VPS.

The MinIO binary is pinned to the multi-architecture
[Coollabs release build](https://github.com/coollabsio/minio), then packaged on a
supported Alpine base with working health-check and backup tools. It replaces the
unavailable `minio/minio:latest` reference. The
[upstream community repository is archived](https://github.com/minio/minio), so this
pin does not provide ongoing MinIO security maintenance. Review this dependency
before a long-term public deployment; moving to a maintained storage service is a
separate migration. The public storage proxy accepts only GET/HEAD; buckets remain
private, and uploads/administrative calls stay inside Docker.

## Backups outside the VM

Install the [OCI CLI](https://docs.oracle.com/en-us/iaas/Content/API/SDKDocs/cliinstall.htm)
on the host, accessible to the systemd service (`/usr/local/bin/oci` works). Create a
private Object Storage bucket and authorize this VM through an instance-principal
dynamic group. Grant access to create and inspect objects in that backup bucket;
the CLI's checksum verification must succeed. Set `BACKUP_NAMESPACE` and
`BACKUP_BUCKET` in `.env.production`. Test the instance-principal access before
enabling the timer.

The first deploy copies the backup scripts and systemd units to
`/opt/student-job-portal/deploy`. Install the timer once from there:

```bash
cd /opt/student-job-portal
sudo cp deploy/portal-backup.service deploy/portal-backup.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl start portal-backup.service
sudo journalctl -u portal-backup.service --no-pager
sudo systemctl enable --now portal-backup.timer
```

The timer runs daily at 02:00 UTC (07:45 Nepal time). Each backup stops the API and
MinIO to take a matching database dump and object-volume archive, then restarts them
before uploading. **Expect an outage during the snapshot.** The script attempts to
restart services on failure, retains failed uploads locally, and removes local
copies older than seven days only after successful uploads. Inspect timer failures
and disk usage. Configure remote bucket retention/lifecycle separately; keep enough
free disk for the database dump, compressed objects, and final archive. A daily
backup can lose up to a day's changes. If pauses or that recovery window are
unacceptable, use online object backups and PostgreSQL point-in-time recovery.

Keep the production environment file in a separate secure recovery location. The
backup includes database records, password hashes, objects, and MinIO's internal
metadata. It does not include `.env.production` or Caddy's certificate volumes.
Treat the archive as sensitive, restrict access, and retain the bucket's storage
encryption.

## Restore drill / recovery

Restore into a fresh deployment with empty volumes and no API container. Use a
separate VM for drills: the production Compose file reserves a fixed proxy address.
Restore scripts refuse an existing public database schema, running MinIO, or a
nonempty storage volume. They do not delete existing volumes.

1. Install Docker, create `/opt/student-job-portal`, and put the recovered
   `.env.production`, `docker-compose.production.yml` and the `deploy/` scripts
   there (from this repository). Log in to GHCR (see step 6 of the first deployment)
   and pull the images:

   ```bash
   docker compose --env-file .env.production -f docker-compose.production.yml pull
   ```

2. Download the backup `.tar` and matching `.tar.sha256` from `portal/` in the OCI
   backup bucket. Use only trusted backups, and leave both files together.
3. Restore and start the HTTPS proxy:

   ```bash
   bash deploy/restore.sh /path/to/portal-YYYYMMDDTHHMMSSZ.tar
   docker compose --env-file .env.production -f docker-compose.production.yml up -d proxy
   ```

4. Confirm `/ready`, user login, recruiter draft ownership, application counts,
   and the contents of a previously uploaded CV/image. Record the recovery duration
   before switching DNS or directing traffic to the restored deployment. If a restore
   fails halfway, retain its volumes for diagnosis and retry in a new empty deployment.

For local integration tests only, `PORTAL_ENV_FILE` selects a separate environment
file and `COMPOSE_PROJECT_NAME` isolates the disposable deployment.

The repeatable Docker integration check creates disposable projects, stores a real
database record and S3 object, tests failed-upload recovery, restores both, verifies
a signed document download, and checks overwrite refusal. Only the OCI upload is
stubbed; it does not validate your cloud credentials or remote bucket.

```bash
docker build -t portal-review-api .
docker build -t portal-review-minio -f deploy/Dockerfile.minio .
python3 deploy/test_backups.py
```

## Updates and checks

Every push to `main` deploys automatically. Each image is tagged `latest` and
`sha-<7-char commit>`. To roll back, set `IMAGE_TAG=sha-xxxxxxx` in
`.env.production` on the VPS and run
`docker compose --env-file .env.production -f docker-compose.production.yml up -d`.
Later deploys keep using that pinned tag until you set it back to `latest`.

Back up first. Startup still runs the existing automatic migrations; rehearse schema
changes against a restored copy before deploying. Do not roll back to an older
binary without checking its schema compatibility. Never run `down -v` on production.

`/health` reports process liveness. `/ready` checks PostgreSQL and both storage
buckets with a two-second deadline; dependency outages return 503. Docker health
status alone does not restart an unhealthy but running process: monitor readiness,
container restarts, CPU, memory, disk space, and backup failures externally.

```bash
go test -race ./...
go vet ./...
# Use a disposable PostgreSQL database, never production:
OTP_TEST_POSTGRES_DSN='host=127.0.0.1 port=5432 user=test password=test dbname=portal_test sslmode=disable' \
  go test ./internal/services -run TestPostgres -count=1
docker stats
```

Measure capacity on the actual OCI VM with representative records. Increase mixed
traffic through 10, 25, and 50 active users, including searches, password checks,
application writes, and CV uploads. Measure response-time percentiles and errors
alongside CPU, connection waits, and disk latency. Those user counts are test stages,
not a supported-capacity promise. Search currently uses substring matching and a
count query per page; inspect slow queries before adding search indexes or caching.
