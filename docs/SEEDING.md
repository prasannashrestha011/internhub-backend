# Development database seeding

From the `backend` directory, run:

```bash
make db-seed
```

The command runs the current GORM migrations and then creates or refreshes an
idempotent development dataset. It refuses to run when `APP_ENV=production`.

## Seeded logins

All accounts use `Password123!` by default. Set `SEED_PASSWORD` to override the
password for every seeded account.

| Role | Email |
| --- | --- |
| Admin | `seed.admin@jobportal.test` |
| Student | `seed.student@jobportal.test` |
| Student | `seed.student2@jobportal.test` |
| Approved employer | `seed.employer@jobportal.test` |
| Pending employer | `seed.employer.pending@jobportal.test` |
| Rejected employer | `seed.employer.rejected@jobportal.test` |

## Dataset coverage

- Two 100% complete student profiles with education, skills, projects, and certifications.
- Three complete employer profiles with approved, pending, and rejected organization verifications.
- Six internships: three published, two private, and one closed.
- Six applications: submitted, reviewing, shortlisted, accepted, rejected, and withdrawn.

The seed intentionally omits uploaded document and logo records because their
database metadata would point to MinIO objects that do not exist.
