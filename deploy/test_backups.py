"""Docker integration check. Build portal-review-api and portal-review-minio first.

Uses disposable Compose projects and real PostgreSQL/MinIO; only OCI upload is stubbed.
"""
import os
from pathlib import Path
import subprocess
import tempfile
import uuid

ROOT = Path(__file__).resolve().parents[1]


def run(*args, env=None, data=None, check=True):
    return subprocess.run(args, cwd=ROOT, env=env, input=data, text=True,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=check)


with tempfile.TemporaryDirectory(prefix="portal-backup-test-") as temporary:
    folder = Path(temporary)
    env_file = folder / "production.env"
    values = dict(line.split("=", 1) for line in (ROOT / ".env.production.example").read_text().splitlines()
                  if line and not line.startswith("#"))
    values.update(DB_PASSWORD="backup-test-only", JWT_ACCESS_SECRET="j" * 64,
                  MINIO_SECRET_KEY="m" * 64, EMAIL_OTP_SECRET="o" * 64,
                  SMTP_HOST="smtp.invalid", SMTP_USERNAME="test", SMTP_PASSWORD="test",
                  SMTP_FROM="test@example.com")
    env_file.write_text("\n".join(f"{key}={value}" for key, value in values.items()))
    upload = folder / "oci"
    upload.write_text("""#!/usr/bin/env python3
import os, pathlib, shutil, sys
if os.environ.get('FAIL_UPLOAD'): sys.exit(1)
args=sys.argv
source=pathlib.Path(args[args.index('--file')+1])
target=pathlib.Path(os.environ['TEST_REMOTE'])/source.name
target.parent.mkdir(parents=True, exist_ok=True)
shutil.copyfile(source, target)
""")
    upload.chmod(0o700)
    env = dict(os.environ, PATH=f"{folder}:{os.environ['PATH']}",
               PORTAL_ENV_FILE=str(env_file), BACKUP_DIR=str(folder / "backups"),
               BACKUP_NAMESPACE="test", BACKUP_BUCKET="test", TEST_REMOTE=str(folder / "remote"))
    prefix = "portal-backup-test-" + uuid.uuid4().hex[:8]
    projects = [prefix + "-source", prefix + "-restore"]

    def dc(*args, check=True, data=None):
        return run("docker", "compose", "--env-file", str(env_file), "-f",
                   "docker-compose.production.yml", *args, env=env, check=check, data=data)

    def sql(query):
        return dc("exec", "-T", "postgres", "psql", "-U", "portal", "-d",
                  "student_job_portal", "-Atc", query).stdout.strip()

    helper = folder / "objects.go"
    helper.write_text('''package main
import ("bytes"; "context"; "fmt"; "io"; "net/http"; "os"; "time";
"github.com/minio/minio-go/v7"; "github.com/minio/minio-go/v7/pkg/credentials")
func main() {
ctx:=context.Background()
opts:=&minio.Options{Region:"us-east-1", Creds:credentials.NewStaticV4("portal-storage",os.Getenv("TEST_STORAGE_SECRET"),"")}
c,err:=minio.New(os.Args[1]+":9000",opts); must(err)
if os.Args[2]=="put" {
must(c.MakeBucket(ctx,"backup-proof",minio.MakeBucketOptions{}))
_,err=c.PutObject(ctx,"backup-proof","cv.pdf",bytes.NewBufferString("backup document proof"),21,minio.PutObjectOptions{}); must(err)
}
opts.Secure=true
signer,err:=minio.New("files.example.com",opts); must(err)
u,err:=signer.PresignedGetObject(ctx,"backup-proof","cv.pdf",time.Minute,nil); must(err)
host:=u.Host; u.Scheme="http"; u.Host=os.Args[1]+":9000"
req,err:=http.NewRequest("GET",u.String(),nil); must(err); req.Host=host
res,err:=http.DefaultClient.Do(req); must(err); defer res.Body.Close()
body,err:=io.ReadAll(res.Body); must(err)
if res.StatusCode!=200 || string(body)!="backup document proof" { panic(fmt.Sprintf("signed download failed: %d %s",res.StatusCode,body)) }
}
func must(err error) { if err!=nil { panic(err) } }
''')
    env["TEST_STORAGE_SECRET"] = values["MINIO_SECRET_KEY"]

    binary = folder / "objects"
    run("go", "build", "-o", str(binary), str(helper), env=dict(env, CGO_ENABLED="0"))

    def objects(operation):
        run("docker", "run", "--rm", "--network", env["COMPOSE_PROJECT_NAME"] + "_portal",
            "--mount", f"type=bind,source={binary},target=/check,readonly",
            "--env", "TEST_STORAGE_SECRET=" + values["MINIO_SECRET_KEY"],
            "--entrypoint", "/check", "portal-review-minio", "minio", operation, env=env)

    # Point the production Compose file at the locally built review images.
    env.update(IMAGE_PREFIX=prefix, IMAGE_TAG="test")

    try:
        for service in ("api", "minio"):
            run("docker", "tag", f"portal-review-{service}", f"{prefix}/{service}:test")
        env["COMPOSE_PROJECT_NAME"] = projects[0]
        dc("up", "-d", "--no-build", "--wait", "--wait-timeout", "120", "postgres", "minio", "api")
        sql("CREATE TABLE deployment_backup_proof (value text); INSERT INTO deployment_backup_proof VALUES ('database proof')")
        objects("put")
        run("bash", "deploy/backup.sh", env=env)
        archive = next((folder / "remote").glob("*.tar"))
        assert archive.with_suffix(".tar.sha256").exists(), "remote checksum missing"
        failed_env = dict(env, FAIL_UPLOAD="1")
        assert run("bash", "deploy/backup.sh", env=failed_env, check=False).returncode != 0
        dc("exec", "-T", "api", "wget", "-q", "-O", "/dev/null", "http://localhost:8080/ready")
        assert len(list((folder / "backups").glob("*.tar"))) == 2, "failed upload was not retained"
        dc("down")
        env["COMPOSE_PROJECT_NAME"] = projects[1]
        run("bash", "deploy/restore.sh", str(archive), env=env)
        assert sql("SELECT value FROM deployment_backup_proof") == "database proof"
        objects("get")
        assert run("bash", "deploy/restore.sh", str(archive), env=env, check=False).returncode != 0, "existing deployment was overwritten"
        assert sql("SELECT value FROM deployment_backup_proof") == "database proof"
        print("PASS: backup, failed upload recovery, restore, signed document download, overwrite refusal")
    except subprocess.CalledProcessError as error:
        print(error.stdout, error.stderr)
        raise
    finally:
        for project in projects:
            env["COMPOSE_PROJECT_NAME"] = project
            dc("down", "--volumes", "--remove-orphans", check=False)
        for service in ("api", "minio"):
            run("docker", "image", "rm", f"{prefix}/{service}:test", check=False)
