```bash
cd backend
[ -f .env ] || cp .env.example .env
docker compose up --build
```

New accounts require email confirmation. Configure Oracle SMTP and a separate
OTP secret using the [email verification setup](docs/EMAIL_VERIFICATION.md).

The supported roles are `student` and `employer`. Employers can publish internships
as soon as they confirm their email and create a company profile. Admin pages,
API endpoints, and employer approval have been removed. Existing admin accounts
cannot sign in or renew sessions; their stored records are retained. Legacy
employer approval values are ignored. Existing private drafts stay private until
their owner publishes them.
