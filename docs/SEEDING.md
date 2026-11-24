# Development database seeding

API startup does not create demo data. Run the command below only when you want
to populate the database with the development dataset.

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
| Student | `seed.student@jobportal.test` |
| Student | `seed.student2@jobportal.test` |
| Employer | `seed.employer@jobportal.test` |
| Employer | `seed.employer.pending@jobportal.test` |
| Employer | `seed.employer.rejected@jobportal.test` |

## Dataset coverage

- Two student profiles with college and degree details.
- Three employer profiles, ready to publish without approval.
- Six internships: three published, two private drafts, and one closed.
- Six applications covering submitted, accepted, rejected, and withdrawn states.

The older `pending` and `rejected` email names are retained for existing demo logins;
they do not restrict employer access. All seeded accounts have confirmed email addresses. The seed omits uploaded
CVs and logos because their metadata would point to MinIO objects that do not exist.
