# Manual Deployment Guide

This guide shows how to put the backend online by hand. You build the
images on your computer, push them to GitHub's image storage, then pull and run
them on the VPS.

It covers the **backend stack**: the API, the database, file storage and Caddy.
The Next.js frontend is not part of `docker-compose.production.yml`, so it is
not covered here.

> In this guide, `api.example.com`, `files.example.com` and `YOUR_VPS_IP` are
> placeholders. Replace them with your real domains and server IP.

---

## Table of contents

1. [The big picture](#1-the-big-picture)
2. [Words you need to know](#2-words-you-need-to-know)
3. [How to read the commands in this guide](#3-how-to-read-the-commands-in-this-guide)
4. [One-time setup on your computer](#4-one-time-setup-on-your-computer)
5. [One-time setup on the VPS](#5-one-time-setup-on-the-vps)
6. [Caddy: HTTPS and the front door](#6-caddy-https-and-the-front-door)
7. [Every deployment, step by step](#7-every-deployment-step-by-step)
8. [Checking that everything works](#8-checking-that-everything-works)
9. [Rolling back to an older version](#9-rolling-back-to-an-older-version)
10. [Common problems](#10-common-problems)
11. [Safety rules](#11-safety-rules)
12. [Cheat sheet](#12-cheat-sheet)

---

## 1. The big picture

```
 YOUR COMPUTER                    GITHUB (ghcr.io)                 VPS
 ─────────────                    ────────────────                 ───
 1. docker build  ──────────►  2. docker push  ──────────►  3. docker pull
    (make images)                 (store images)                (download images)
                                                                   │
                                                                   ▼
                                                             4. docker compose up
                                                                (run the app)

 Inside the VPS:

   Internet ──► Caddy (ports 80/443, HTTPS)
                  ├── api.example.com   ──► api    (Go backend, port 8080)
                  └── files.example.com ──► minio  (file storage, port 9000)

                api ──► postgres (database, port 5432)
                api ──► minio
```

Only Caddy can be reached from the internet. The API, database and file storage
can only talk to each other inside Docker.

### The 4 services (from `docker-compose.production.yml`)

| Service    | What it is                                  | Where the image comes from                                   |
|------------|---------------------------------------------|--------------------------------------------------------------|
| `postgres` | The database (PostgreSQL 16)                | Docker Hub (official `postgres:16-alpine`); you don't build it |
| `minio`    | Stores uploaded files (CVs, profile images) | You build it from `deploy/Dockerfile.minio`                  |
| `api`      | The Go backend                              | You build it from `Dockerfile`                               |
| `proxy`    | Caddy: HTTPS and request forwarding         | You build it from `deploy/Dockerfile.caddy`                  |

So you build and push **3 images**: `api`, `minio`, `caddy`.

---

## 2. Words you need to know

| Word | Simple meaning |
|------|----------------|
| **VPS** | Virtual Private Server. A computer you rent in a data center that is always on and connected to the internet. |
| **SSH** | Secure Shell. A way to open a terminal on the VPS from your computer. Everything you type runs on the VPS. |
| **SSH key** | A pair of files used instead of a password. The **private key** stays on your computer (secret). The **public key** goes on the VPS. |
| **Docker** | A tool that packs an app and everything it needs into one box, so it runs the same way on every machine. |
| **Image** | The packed box. A read-only template of the app. Like a recipe or an installer. |
| **Container** | A running copy of an image. Like a program started from the installer. |
| **Dockerfile** | A text file with step-by-step instructions to build an image. |
| **Registry** | An online store for images. You push images to it and pull images from it. |
| **GHCR** | GitHub Container Registry (`ghcr.io`). GitHub's registry. Your images live here. |
| **Tag** | A label on an image version, written after `:`. Example: `api:latest` or `api:sha-598bbcc`. |
| **Docker Compose** | A tool that starts several containers together, using one YAML file that describes them all. |
| **Volume** | A storage folder managed by Docker. Data in a volume survives when a container is deleted or replaced. Your database and uploaded files live in volumes. |
| **Port** | A numbered door on a computer. Websites use 80 (HTTP) and 443 (HTTPS). SSH uses 22. |
| **DNS** | The internet's phone book. It turns a name like `api.example.com` into an IP address. |
| **A record** | A DNS entry that says "this name points to this IPv4 address". |
| **HTTPS / TLS** | Encrypted web traffic (the padlock in the browser). It needs a **certificate**. |
| **Certificate** | A file that proves your server really owns the domain. Caddy gets one for free from **Let's Encrypt**. |
| **Reverse proxy** | A server that receives all requests first and forwards each one to the right app. Caddy does this. |
| **Environment variable** | A named setting given to an app from outside, like `DB_PASSWORD=...`. |
| **`.env.production`** | A file on the VPS that holds all the secret settings. Never commit it to git. |
| **Personal Access Token (PAT)** | A long password-like string from GitHub that lets Docker log in to GHCR. |
| **`sudo`** | "Run this as administrator". Linux will ask for your password. |
| **Health check** | A small test Docker runs over and over to see if a container is working. |

---

## 3. How to read the commands in this guide

- Each command is shown in a code block, followed by a table that explains it.
- Lines starting with `#` are comments. They are notes for you and do nothing.
- A `\` at the end of a line means "the command continues on the next line".
- Words in CAPITALS, like `YOUR_VPS_IP`, are things you must replace.
- The heading above each block says where to run it:
  **💻 your computer** or **🖥️ VPS**.

---

## 4. One-time setup on your computer

### 4.1 Install Docker

Install Docker Desktop (Windows/Mac) or Docker Engine (Linux) from
<https://docs.docker.com/get-docker/>. Check it works:

**💻 your computer**
```bash
docker --version
docker compose version
```

| Part | Meaning |
|------|---------|
| `docker --version` | Prints the installed Docker version. If you see a version, Docker is installed. |
| `docker compose version` | Prints the Compose version. Compose comes as a plugin inside Docker. |

### 4.2 Create a GitHub token for pushing images

1. GitHub → your profile picture → **Settings** → **Developer settings** →
   **Personal access tokens** → **Tokens (classic)** → **Generate new token (classic)**.
2. Give it a name like `ghcr-push`.
3. Tick **`write:packages`** (this also ticks `read:packages`).
4. Click **Generate** and copy the token. GitHub shows it only once.

> Use a **classic** token. GHCR does not fully support the newer fine-grained
> tokens.

### 4.3 Log Docker in to GHCR

**💻 your computer**
```bash
docker login ghcr.io -u prasannashrestha011
```

| Part | Meaning |
|------|---------|
| `docker login` | Logs Docker in to a registry so you can push and pull private images. |
| `ghcr.io` | The registry address (GitHub Container Registry). |
| `-u prasannashrestha011` | `-u` = username. Your GitHub username. |

It will ask for a **Password**. Paste the token (not your GitHub password).
Nothing appears while you paste; that is normal. Press Enter.
You should see `Login Succeeded`. You only need to do this once per computer.

### 4.4 Create an SSH key (if you don't have one)

**💻 your computer**
```bash
ssh-keygen -t ed25519 -C "your-email@example.com"
```

| Part | Meaning |
|------|---------|
| `ssh-keygen` | Creates a new SSH key pair. |
| `-t ed25519` | `-t` = type. `ed25519` is a modern, short and secure key type. |
| `-C "..."` | `-C` = comment. A label stored in the key so you can recognise it later. |

Press Enter to accept the default location (`~/.ssh/id_ed25519`). Add a
passphrase if you want extra protection. This creates:

- `~/.ssh/id_ed25519` → **private key**. Never share it.
- `~/.ssh/id_ed25519.pub` → **public key**. Safe to copy to servers.

(`~` means your home folder.)

---

## 5. One-time setup on the VPS

The VPS must be an **x86_64 (also called amd64) Ubuntu** machine. The images
are built for that type of processor and will not run on ARM servers.

### 5.1 Log in to the VPS for the first time

Your VPS provider gives you an IP address and a first user (often `root` or
`ubuntu`).

**💻 your computer**
```bash
ssh ubuntu@YOUR_VPS_IP
```

| Part | Meaning |
|------|---------|
| `ssh` | Opens a remote terminal on another computer. |
| `ubuntu@YOUR_VPS_IP` | `user@address`: log in as user `ubuntu` on that server. |

The first time, it asks *"Are you sure you want to continue connecting?"*.
Type `yes`. This saves the server's fingerprint so SSH can warn you if
someone pretends to be your server later. To leave the VPS, type `exit`.

### 5.2 Install Docker on the VPS

**🖥️ VPS**
```bash
curl -fsSL https://get.docker.com | sudo sh
```

| Part | Meaning |
|------|---------|
| `curl` | Downloads something from a URL. |
| `-f` | **f**ail: stop with an error if the server returns an error, instead of saving the error page. |
| `-s` | **s**ilent: hide the progress bar. |
| `-S` | **S**how errors even when silent. |
| `-L` | follow **L**inks: if the URL redirects, follow it. |
| `https://get.docker.com` | Docker's official install script. |
| `\|` | "pipe": send the output of the left command as input to the right command. |
| `sudo sh` | Run the downloaded script with the shell (`sh`) as administrator. |

Then make Docker start automatically when the VPS reboots:

**🖥️ VPS**
```bash
sudo systemctl enable --now docker
```

| Part | Meaning |
|------|---------|
| `systemctl` | The tool that manages background services on Ubuntu. |
| `enable` | Start this service automatically at every boot. |
| `--now` | Also start it right now. |
| `docker` | The service name. |

### 5.3 Create a `deploy` user and the app folder

Running everything as `root` is risky. Make a normal user just for the app.

**🖥️ VPS**
```bash
sudo adduser --disabled-password deploy
sudo usermod -aG docker deploy
sudo mkdir -p /opt/student-job-portal
sudo chown deploy:deploy /opt/student-job-portal
```

| Command / part | Meaning |
|----------------|---------|
| `adduser deploy` | Creates a new user named `deploy`. |
| `--disabled-password` | The user has no password, so it can only log in with an SSH key. Safer. |
| `usermod` | Changes an existing user. |
| `-a` | **a**ppend: add to a group without removing the user from other groups. Always use it with `-G`. |
| `-G docker` | The group to add. Members of `docker` can run Docker commands without `sudo`. |
| `mkdir -p /opt/student-job-portal` | Make the folder. `-p` also creates parent folders and doesn't complain if it already exists. |
| `chown deploy:deploy <folder>` | **ch**ange **own**er: make user `deploy` (and group `deploy`) own the folder. |

> Being in the `docker` group is almost the same as being `root`. Only give it
> to users you trust.

### 5.4 Let yourself log in as `deploy` with your SSH key

Copy your public key into the `deploy` user's allowed keys:

**🖥️ VPS**
```bash
sudo mkdir -p /home/deploy/.ssh
sudo nano /home/deploy/.ssh/authorized_keys
```

Paste the content of your **public** key (`~/.ssh/id_ed25519.pub` on your
computer, one line starting with `ssh-ed25519`). Save with `Ctrl+O`, Enter,
then exit with `Ctrl+X`.

**🖥️ VPS**
```bash
sudo chown -R deploy:deploy /home/deploy/.ssh
sudo chmod 700 /home/deploy/.ssh
sudo chmod 600 /home/deploy/.ssh/authorized_keys
```

| Part | Meaning |
|------|---------|
| `nano` | A simple text editor in the terminal. |
| `authorized_keys` | The list of public keys allowed to log in as this user. |
| `chown -R` | Change owner; `-R` = **R**ecursive, also for everything inside the folder. |
| `chmod 700` | Change permissions: only the owner can read, write and open the folder. |
| `chmod 600` | Only the owner can read and write the file. SSH refuses keys if these files are too open. |

Now, from your computer, test it:

**💻 your computer**
```bash
ssh deploy@YOUR_VPS_IP
```

From now on, always log in as `deploy` for deployments.

**Optional shortcut.** Add this to `~/.ssh/config` on your computer:

```
Host portal
    HostName YOUR_VPS_IP
    User deploy
    IdentityFile ~/.ssh/id_ed25519
```

| Line | Meaning |
|------|---------|
| `Host portal` | A nickname. Now `ssh portal` is enough. |
| `HostName` | The real IP or hostname. |
| `User` | The user to log in as. |
| `IdentityFile` | Which private key to use. |

### 5.5 Open the firewall (ports 22, 80, 443)

Two firewalls may block traffic:

1. **Your cloud provider's firewall** (web dashboard). On Oracle Cloud it's
   *VCN → Security List → Ingress Rules*. Allow TCP **80** and **443** from
   `0.0.0.0/0` (everyone). Keep port **22** (SSH) open only to your own IP if
   possible.
2. **The firewall on the VPS itself.**

If your VPS uses `ufw` (common on Ubuntu):

**🖥️ VPS**
```bash
sudo ufw allow OpenSSH
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw enable
sudo ufw status
```

| Part | Meaning |
|------|---------|
| `ufw` | **U**ncomplicated **F**ire**w**all, Ubuntu's simple firewall tool. |
| `allow OpenSSH` | Keep port 22 open. **Do this first**, or you will lock yourself out. |
| `allow 80/tcp` | Open port 80 (HTTP). Caddy needs it to get certificates and redirect to HTTPS. |
| `allow 443/tcp` | Open port 443 (HTTPS). |
| `enable` | Turn the firewall on. |
| `status` | Show the current rules. |

> **Oracle Cloud Ubuntu images** use `iptables` rules instead of `ufw`, and
> block everything except SSH by default. Run this instead:
>
> ```bash
> sudo iptables -I INPUT 6 -m state --state NEW -p tcp --dport 80 -j ACCEPT
> sudo iptables -I INPUT 6 -m state --state NEW -p tcp --dport 443 -j ACCEPT
> sudo netfilter-persistent save
> ```
>
> | Part | Meaning |
> |------|---------|
> | `iptables` | Linux's low-level firewall tool. |
> | `-I INPUT 6` | **I**nsert a rule into the `INPUT` (incoming traffic) list at position 6, before the "reject everything" rule. |
> | `-m state --state NEW` | Match new incoming connections. |
> | `-p tcp` | Protocol is TCP. |
> | `--dport 80` | **D**estination **port** 80. |
> | `-j ACCEPT` | **J**ump to `ACCEPT`: allow it. |
> | `netfilter-persistent save` | Save the rules so they survive a reboot. |

### 5.6 Point your domains to the VPS (DNS)

In your domain provider's dashboard (Cloudflare, Namecheap, etc.), add two
**A records**:

| Type | Name    | Value         |
|------|---------|---------------|
| A    | `api`   | `YOUR_VPS_IP` |
| A    | `files` | `YOUR_VPS_IP` |

- `api.example.com` → the backend API.
- `files.example.com` → downloads of uploaded files.

If you use Cloudflare, set these to **DNS only** (grey cloud), not proxied.
Caddy must talk to the internet directly to get certificates.

Check that DNS works (it can take a few minutes to a few hours):

**💻 your computer**
```bash
nslookup api.example.com
```

| Part | Meaning |
|------|---------|
| `nslookup` | Asks DNS "what IP does this name point to?". It should print `YOUR_VPS_IP`. |

### 5.7 Copy the Compose file to the VPS

The VPS doesn't need the source code. It only needs
`docker-compose.production.yml` and `.env.production`.

**💻 your computer** (inside the `backend` folder)
```bash
scp docker-compose.production.yml deploy@YOUR_VPS_IP:/opt/student-job-portal/
```

| Part | Meaning |
|------|---------|
| `scp` | **S**ecure **c**o**p**y: copies files over SSH. |
| `docker-compose.production.yml` | The local file to send. |
| `deploy@YOUR_VPS_IP:` | The user and server to send it to. The `:` separates server from path. |
| `/opt/student-job-portal/` | The folder on the VPS where the file goes. |

Run this again **whenever you change `docker-compose.production.yml`**.

Optional: also copy the backup scripts (see the "Backups" section of
[DEPLOYMENT.md](DEPLOYMENT.md)):

```bash
ssh deploy@YOUR_VPS_IP "mkdir -p /opt/student-job-portal/deploy"
scp deploy/backup.sh deploy/restore.sh deploy/portal-backup.service deploy/portal-backup.timer \
    deploy@YOUR_VPS_IP:/opt/student-job-portal/deploy/
```

| Part | Meaning |
|------|---------|
| `ssh user@ip "command"` | Runs one command on the VPS, then disconnects. |

### 5.8 Create the secrets file `.env.production`

**🖥️ VPS** (as `deploy`)
```bash
cd /opt/student-job-portal
nano .env.production
```

Paste the contents of `backend/.env.production.example` from the repo, then
fill in every value. Make a random secret for each password/secret field:

```bash
openssl rand -hex 32
```

| Part | Meaning |
|------|---------|
| `cd` | **c**hange **d**irectory: move into that folder. |
| `openssl rand` | Generate random bytes. |
| `-hex` | Print them as hexadecimal text (0-9, a-f), safe to paste anywhere. |
| `32` | 32 random bytes, which gives a 64-character secret. |

Run it once **for each** secret (`DB_PASSWORD`, `JWT_ACCESS_SECRET`,
`MINIO_SECRET_KEY`, `EMAIL_OTP_SECRET`). Never reuse one secret for two fields.

The important values:

| Variable | What to put |
|----------|-------------|
| `IMAGE_TAG` | `latest` (or `sha-xxxxxxx` to pin a version, see [Rolling back](#9-rolling-back-to-an-older-version)). |
| `API_DOMAIN` | `api.example.com` |
| `STORAGE_DOMAIN` | `files.example.com` |
| `ACME_EMAIL` | Your email. Let's Encrypt sends certificate warnings here. |
| `CORS_ALLOWED_ORIGINS` | Your frontend URL, e.g. `https://portal.example.com`. Only this site may call the API from a browser. |
| `COOKIE_DOMAIN` | `example.com` when frontend and API are on subdomains of the same domain. |
| `DB_PASSWORD`, `JWT_ACCESS_SECRET`, `MINIO_SECRET_KEY`, `EMAIL_OTP_SECRET` | Output of `openssl rand -hex 32`. |
| `SMTP_*` | Your email-sending provider's settings. |

> If a value contains a `$`, wrap it in single quotes: `SMTP_PASSWORD='ab$cd'`.
> Otherwise Compose thinks `$cd` is a variable.

Lock the file so only `deploy` can read it:

```bash
chmod 600 .env.production
```

Keep a copy of this file somewhere safe (a password manager). If the VPS dies,
you need it to recover.

### 5.9 Log the VPS in to GHCR

Your images are private, so the VPS must log in to pull them. Create **another**
classic token with only **`read:packages`** (see 4.2). It's safer than reusing
your push token: if the VPS is hacked, the attacker can only download images,
not replace them.

**🖥️ VPS** (as `deploy`)
```bash
docker login ghcr.io -u prasannashrestha011
```

Paste the read-only token as the password. Done once.

### 5.10 A shortcut for the long Compose command

Every Compose command on the VPS needs the same two options. Save them as a
short alias called `dc`:

**🖥️ VPS** (as `deploy`)
```bash
echo "alias dc='docker compose --env-file .env.production -f docker-compose.production.yml'" >> ~/.bashrc
source ~/.bashrc
```

| Part | Meaning |
|------|---------|
| `alias dc='...'` | Makes `dc` a short name for the long command. |
| `echo "..." >> ~/.bashrc` | Append that line to `~/.bashrc`, a file that runs every time you open a terminal. `>>` appends; a single `>` would erase the file. |
| `source ~/.bashrc` | Load the file now, so the alias works without logging out. |
| `--env-file .env.production` | Read variables from this file. |
| `-f docker-compose.production.yml` | `-f` = **f**ile. Use this Compose file instead of the default `docker-compose.yml`. |

The alias only works when you are inside `/opt/student-job-portal`, because the
file names are relative to the current folder.

---

## 6. Caddy: HTTPS and the front door

### What Caddy does

Caddy is a web server used here as a **reverse proxy**:

1. It's the only container open to the internet (ports 80 and 443).
2. It looks at which domain was requested and forwards the request:
   - `api.example.com` → `api` container
   - `files.example.com` → `minio` container
3. It **gets and renews HTTPS certificates automatically** from Let's Encrypt.
   You don't need certbot or any manual certificate work.

### The Caddyfile, line by line

File: `backend/deploy/Caddyfile`

```
{
	email {$ACME_EMAIL}
}
```
The first `{ }` block holds global settings. `email` is the address given to
Let's Encrypt. `{$ACME_EMAIL}` means "read the environment variable
`ACME_EMAIL`" (it comes from `.env.production`).

```
{$API_DOMAIN} {
	request_body {
		max_size 12MB
	}
	reverse_proxy api:8080 {
		header_up -X-Real-IP
	}
}
```
| Line | Meaning |
|------|---------|
| `{$API_DOMAIN} {` | Rules for requests to your API domain, e.g. `api.example.com`. Writing a domain name here is what turns on automatic HTTPS. |
| `request_body { max_size 12MB }` | Reject uploads bigger than 12 MB. |
| `reverse_proxy api:8080` | Forward the request to the container named `api` on port 8080. Docker's internal DNS turns `api` into the container's address. |
| `header_up -X-Real-IP` | Remove the `X-Real-IP` header from incoming requests, so visitors can't fake their IP address. |

```
{$STORAGE_DOMAIN} {
	@download method GET HEAD
	handle @download {
		reverse_proxy minio:9000
	}
	respond "Method not allowed" 405
}
```
| Line | Meaning |
|------|---------|
| `{$STORAGE_DOMAIN} {` | Rules for the files domain, e.g. `files.example.com`. |
| `@download method GET HEAD` | Define a "matcher" named `@download` for read-only requests (GET = download, HEAD = check if it exists). |
| `handle @download { reverse_proxy minio:9000 }` | Only those read-only requests go to MinIO. |
| `respond "Method not allowed" 405` | Everything else (uploads, deletes) is refused. Uploads go through the API, never directly from the internet. |

### Why Caddy has a fixed IP (`172.30.0.2`)

In `docker-compose.production.yml`, Caddy always gets the address `172.30.0.2`
on the private Docker network, and the API has `TRUSTED_PROXIES: "172.30.0.2"`.
This tells the API: "trust the visitor IP that Caddy reports, but only from
Caddy". That gives correct IPs for logs and rate limits. The network
`172.30.0.0/24` must not be used by anything else on the VPS.

### Where the Caddyfile lives

The Caddyfile is **baked into the `caddy` image** (`deploy/Dockerfile.caddy`):

```dockerfile
FROM caddy:2-alpine
COPY deploy/Caddyfile /etc/caddy/Caddyfile
```

| Line | Meaning |
|------|---------|
| `FROM caddy:2-alpine` | Start from the official Caddy v2 image (Alpine = a very small Linux). |
| `COPY deploy/Caddyfile /etc/caddy/Caddyfile` | Put our Caddyfile where Caddy looks for it. |

So **if you change the Caddyfile, you must rebuild and push the `caddy` image**.
Editing a file on the VPS does nothing.

### Certificates are stored in volumes

`caddy_data` and `caddy_config` volumes keep the certificates. Don't delete
them. If you do, Caddy must request new ones, and Let's Encrypt limits how many
you can get per week.

---

## 7. Every deployment, step by step

This is what you do each time you want to release new code.

### Step 1: Commit your code

**💻 your computer**
```bash
cd backend
git status
git add .
git commit -m "Describe your change"
git push
```

| Command | Meaning |
|---------|---------|
| `cd backend` | Go into the backend folder (all build commands run from here). |
| `git status` | Show changed files. Check there's nothing you don't want to ship. |
| `git add .` | Stage all changes in this folder (`.` = current folder). |
| `git commit -m "..."` | Save a snapshot with a message (`-m` = **m**essage). |
| `git push` | Upload the commit to GitHub. |

Commit first because the image tag uses the commit ID. That way every image
matches an exact version of the code.

### Step 2: Set two helper variables

**💻 your computer** (inside `backend`)
```bash
PREFIX=ghcr.io/prasannashrestha011/internship-portal-api
TAG=sha-$(git rev-parse --short=7 HEAD)
echo $TAG
```

| Part | Meaning |
|------|---------|
| `PREFIX=...` | Saves the image path in a shell variable named `PREFIX`, so you don't retype it. No spaces around `=`. |
| `$( ... )` | Run the command inside and put its output here. |
| `git rev-parse HEAD` | Print the ID of the latest commit. |
| `--short=7` | Only the first 7 characters, e.g. `598bbcc`. |
| `TAG=sha-...` | So `TAG` becomes something like `sha-598bbcc`. |
| `echo $TAG` | Print it to check. `$TAG` means "the value of `TAG`". |

These variables only last until you close the terminal.

### Step 3: Build the 3 images

**💻 your computer** (inside `backend`)
```bash
docker build --platform linux/amd64 -f Dockerfile \
  -t $PREFIX/api:latest -t $PREFIX/api:$TAG .

docker build --platform linux/amd64 -f deploy/Dockerfile.minio \
  -t $PREFIX/minio:latest -t $PREFIX/minio:$TAG .

docker build --platform linux/amd64 -f deploy/Dockerfile.caddy \
  -t $PREFIX/caddy:latest -t $PREFIX/caddy:$TAG .
```

| Part | Meaning |
|------|---------|
| `docker build` | Build an image by following a Dockerfile. |
| `--platform linux/amd64` | Build for x86_64 Linux, the same processor type as the VPS. Needed if your computer is an Apple Silicon Mac (ARM); harmless otherwise. |
| `-f Dockerfile` | `-f` = **f**ile: which Dockerfile to use. |
| `-t $PREFIX/api:latest` | `-t` = **t**ag: the name for the image. `latest` = "the newest version". |
| `-t $PREFIX/api:$TAG` | A second name with the commit ID, e.g. `api:sha-598bbcc`. Lets you roll back later. |
| `.` | The **build context**: the folder Docker can copy files from. `.` = current folder (`backend`). Files listed in `.dockerignore` are left out. |

**Why build all three every time?** The `minio` and `caddy` images rarely
change and rebuild in seconds thanks to Docker's cache. Building all three
means every version tag (`sha-...`) exists for every service, so rolling back
with `IMAGE_TAG` always works.

**What happens inside the API build** (from `Dockerfile`), in short:
1. A Go image downloads dependencies and compiles the app into one file.
2. A tiny Alpine image receives only that file. The final image is small and
   has no compiler or source code.
3. The app runs as a normal user (`appuser`), not root.

### Step 4: Push the images to GHCR

**💻 your computer**
```bash
docker push --all-tags $PREFIX/api
docker push --all-tags $PREFIX/minio
docker push --all-tags $PREFIX/caddy
```

| Part | Meaning |
|------|---------|
| `docker push` | Upload an image to the registry. |
| `--all-tags` | Upload every tag of this image (`latest` and `sha-...`) in one go. |
| `$PREFIX/api` | Which image to upload. |

After this, you can see the images on GitHub under your profile →
**Packages**.

### Step 5: Log in to the VPS

**💻 your computer**
```bash
ssh deploy@YOUR_VPS_IP
cd /opt/student-job-portal
```

(or `ssh portal` if you set up the shortcut in 5.4.)

If you changed `docker-compose.production.yml`, copy it again first (see 5.7).

### Step 6: Pull the new images

**🖥️ VPS**
```bash
dc pull
```

Without the alias, this is:
```bash
docker compose --env-file .env.production -f docker-compose.production.yml pull
```

| Part | Meaning |
|------|---------|
| `docker compose` | Run Docker Compose. |
| `--env-file .env.production` | Load settings and secrets from this file. |
| `-f docker-compose.production.yml` | Use the production Compose file. |
| `pull` | Download the newest version of every image in the file, using the tag in `IMAGE_TAG`. Running containers keep running for now. |

### Step 7: Restart with the new images

**🖥️ VPS**
```bash
dc up -d --remove-orphans --wait --wait-timeout 180
```

| Part | Meaning |
|------|---------|
| `up` | Create and start all services. If a service's image changed, Compose replaces its container. Unchanged ones keep running. |
| `-d` | **d**etached: run in the background and give you your terminal back. Without it, logs fill your screen and `Ctrl+C` stops the app. |
| `--remove-orphans` | Delete containers for services that are no longer in the Compose file. |
| `--wait` | Don't return until every service is running and its health check passes. |
| `--wait-timeout 180` | Give up waiting after 180 seconds (3 minutes) and report an error. |

Your data is safe: the database and files live in **volumes**, which are kept
when containers are replaced.

The API runs database migrations automatically when it starts.

### Step 8: Clean up old images

**🖥️ VPS**
```bash
docker image prune -f
```

| Part | Meaning |
|------|---------|
| `docker image prune` | Delete old images that no container uses anymore. Frees disk space. |
| `-f` | **f**orce: don't ask "Are you sure?". |

This only deletes images, never volumes. Your data is safe.

### Step 9: Check it

See the next section.

---

## 8. Checking that everything works

**🖥️ VPS**
```bash
dc ps
```

| Part | Meaning |
|------|---------|
| `ps` | List the containers of this project and their status. |

All 4 should show `Up`. `postgres`, `minio` and `api` should also say
`(healthy)`. `starting` means wait a moment. `unhealthy` or `Restarting`
means something is wrong. Read the logs.

**🖥️ VPS**
```bash
dc logs -f --tail 100 api
```

| Part | Meaning |
|------|---------|
| `logs` | Show what a container printed. |
| `-f` | **f**ollow: keep showing new lines live. Press `Ctrl+C` to stop watching (the app keeps running). |
| `--tail 100` | Start from the last 100 lines instead of the whole history. |
| `api` | Which service. Use `proxy` for Caddy, `postgres`, or `minio`. Leave it out to see all. |

**💻 your computer** (or anywhere)
```bash
curl --fail https://api.example.com/ready
```

| Part | Meaning |
|------|---------|
| `curl` | Make a web request from the terminal. |
| `--fail` | Exit with an error if the server answers with an error code (like 503), so problems are obvious. |
| `/ready` | Checks that the API can reach the database and file storage. |
| `/health` | (alternative) Only checks the API process itself is alive. |

Then open the real frontend and test: log in, upload a CV, view a profile
image, apply to an internship.

**Other useful commands**

| Command | What it does |
|---------|--------------|
| `docker stats` | Live CPU and memory use of each container. `Ctrl+C` to exit. |
| `df -h` | Free disk space (`-h` = **h**uman readable, e.g. `20G`). |
| `dc restart api` | Restart just the API container. |
| `dc config --quiet` | Check the Compose file and `.env.production` for mistakes. Prints nothing if OK. |
| `cat .env.production \| grep IMAGE_TAG` | Show which version is set. `grep` keeps only lines containing the word. |

---

## 9. Rolling back to an older version

Every push created a `sha-xxxxxxx` tag. To go back to an older version:

1. Find the tag: GitHub → **Packages** → `api` → versions, or run
   `git log --oneline` on your computer and take the first 7 characters of a
   commit.
2. On the VPS, edit the file:

   **🖥️ VPS**
   ```bash
   cd /opt/student-job-portal
   nano .env.production
   ```
   Change `IMAGE_TAG=latest` to `IMAGE_TAG=sha-xxxxxxx`.
3. Apply it:
   ```bash
   dc pull
   dc up -d --wait
   ```

The VPS stays on that version until you set `IMAGE_TAG=latest` again.

> **Careful with database changes.** The API changes the database structure
> automatically on startup. An old version may not understand a newer
> database. Make a backup before deploying changes to the database models.

---

## 10. Common problems

| Problem | Likely cause and fix |
|---------|----------------------|
| `denied` or `unauthorized` on push | Not logged in, or token lacks `write:packages`. Run `docker login ghcr.io` again (4.3). |
| `denied` or `unauthorized` on `dc pull` | VPS not logged in, or token expired. Redo 5.9. |
| `exec format error` in logs | Image built for the wrong processor (e.g. on a Mac M1/M2 without `--platform linux/amd64`). Rebuild with that flag. |
| `required variable X is missing a value` / `Set X` | A value is empty in `.env.production`. Fill it in. |
| `permission denied ... docker.sock` | The user isn't in the `docker` group, or hasn't logged out and back in since being added. Log out and in again. |
| Site not reachable at all | DNS not pointing to the VPS (`nslookup`), or ports 80/443 blocked (cloud firewall + VPS firewall, 5.5). |
| Browser shows a certificate error | Caddy couldn't get a certificate. Check `dc logs proxy`. Usually DNS is wrong, port 80 is blocked, or Cloudflare proxy (orange cloud) is on. |
| `Pool overlaps with other one on this host` | Another Docker network already uses `172.30.0.0/24`. Remove it or change the subnet (and `TRUSTED_PROXIES` and Caddy's IP) in the Compose file. |
| API `unhealthy` | Read `dc logs api`. Often a wrong DB password or SMTP setting in `.env.production`. |
| Changed the Caddyfile but nothing changed | The Caddyfile is inside the image. Rebuild and push the `caddy` image, then pull and `up -d`. |
| Browser shows CORS error | `CORS_ALLOWED_ORIGINS` doesn't exactly match the frontend URL (including `https://`, no trailing `/`). |

---

## 11. Safety rules

- **Never run `dc down -v`** on production. `-v` deletes the volumes, meaning the
  **whole database and all uploaded files**. Plain `dc down` (without `-v`) only
  stops and removes containers; data stays.
- Never commit `.env.production` or paste tokens into chat, issues or code.
- Keep a copy of `.env.production` in a password manager.
- Back up before deploying database changes (see [DEPLOYMENT.md](DEPLOYMENT.md),
  "Backups outside the VM").
- Use a **read-only** token on the VPS and a **write** token only on your own computer.
- Keep SSH (port 22) restricted to your own IP in the cloud firewall if you can.

---

## 12. Cheat sheet

**💻 Your computer: build and push** (inside `backend`)
```bash
git push
PREFIX=ghcr.io/prasannashrestha011/internship-portal-api
TAG=sha-$(git rev-parse --short=7 HEAD)

docker build --platform linux/amd64 -f Dockerfile               -t $PREFIX/api:latest   -t $PREFIX/api:$TAG .
docker build --platform linux/amd64 -f deploy/Dockerfile.minio  -t $PREFIX/minio:latest -t $PREFIX/minio:$TAG .
docker build --platform linux/amd64 -f deploy/Dockerfile.caddy  -t $PREFIX/caddy:latest -t $PREFIX/caddy:$TAG .

docker push --all-tags $PREFIX/api
docker push --all-tags $PREFIX/minio
docker push --all-tags $PREFIX/caddy

# only if the compose file changed:
scp docker-compose.production.yml deploy@YOUR_VPS_IP:/opt/student-job-portal/
```

**🖥️ VPS: pull and restart**
```bash
ssh deploy@YOUR_VPS_IP
cd /opt/student-job-portal
dc pull
dc up -d --remove-orphans --wait --wait-timeout 180
docker image prune -f
dc ps
curl --fail https://api.example.com/ready
```

> **Next step (later): CI/CD.** Right now deployment is fully manual, as this
> guide describes. CI/CD (Continuous Integration / Continuous Deployment) means
> a service like GitHub Actions runs the build, push and VPS steps for you on
> every push. A draft workflow exists at `backend/.github/workflows/deploy.yml`,
> but it is **not committed, not configured and not in use**. Learn and set it up
> later. Until then, ignore the GitHub Actions parts of
> [DEPLOYMENT.md](DEPLOYMENT.md).
