```bash
cd backend
[ -f .env ] || cp .env.example .env
docker compose up --build
```

OCI deployment: follow the [production setup and backup/restore guide](docs/DEPLOYMENT.md).
