# Account email confirmation

New student and employer accounts must confirm their email before signing in.
Employers can publish internships after confirming their email and saving a company profile.
There is no admin approval step.

## Oracle Email Delivery setup

1. Configure an email domain, SPF, DKIM, an approved sender, and SMTP credentials
   in Oracle Cloud. Use the SMTP endpoint for the approved sender's region.
2. Set these values in the backend environment (see `.env.example`):

   ```dotenv
   SMTP_HOST=smtp.email.your-region.oci.oraclecloud.com
   SMTP_PORT=587
   SMTP_USERNAME=your-oracle-smtp-username
   SMTP_PASSWORD=your-oracle-smtp-password
   SMTP_FROM=no-reply@your-domain.com
   EMAIL_OTP_SECRET=your-separate-random-secret
   ```

   Generate the OTP secret with `openssl rand -hex 32`. Keep credentials and the
   OTP secret on the backend. All API replicas need the same OTP secret.
   Port 587 requires STARTTLS; port 465 uses direct TLS. Certificate validation
   is mandatory and sends have an eight-second deadline.
3. Restart the backend. Production startup rejects missing or invalid mail
   configuration. Development may start without SMTP, but registration remains
   unavailable without an OTP secret; there is no confirmation bypass.
4. Send a signup email to an inbox you control, confirm its code, then sign in.
   Check Oracle delivery logs/suppressions if accepted mail does not arrive.

Oracle references: [Sending an email](https://docs.oracle.com/en-us/iaas/Content/Email/Reference/gettingstarted_topic-Begin_sending_email.htm),
[SMTP configuration](https://docs.oracle.com/en-us/iaas/Content/Email/Reference/gettingstarted_topic-Configure_the_SMTP_connection.htm).

## Behavior and API

- `POST /api/v1/auth/register/student` and `/register/employer` return HTTP 201
  with `user_id`, `email`, `verification_required`, `email_sent`, and
  `resend_after_seconds`. No session is created. If delivery fails after account
  creation, `email_sent` is false and the pending account can use resend.
- `POST /api/v1/auth/verify-email` accepts `{ "email": "...", "code": "000123" }`.
  The code is a string, exactly six digits. Success consumes it and permits login.
- `POST /api/v1/auth/resend-verification` accepts `{ "email": "..." }` and uses
  the same generic HTTP 202 response for missing, confirmed, pending, limited,
  and temporarily undeliverable accounts. Acceptance does not promise delivery.
- Codes expire after ten minutes. Each account can request five sends per hour,
  at least sixty seconds apart. Five incorrect codes cause a one-hour wait.
  Resending does not reset attempt limits. These limits persist in the database;
  the existing IP limiter also applies to all auth endpoints.
- A failed send invalidates its code and still counts toward send limits.
  Codes and SMTP errors containing message data are never logged. Only an HMAC
  of each code is stored. Rotating `EMAIL_OTP_SECRET` invalidates outstanding codes.
- Login and refresh return HTTP 403 with `code: "EMAIL_NOT_VERIFIED"` for
  unconfirmed accounts. The client sends them to `/verify-email`.
- Repeated signup returns HTTP 409 without changing existing credentials or role.

## Existing accounts and deployment

The first migration adds `email_verified_at` and grandfathers existing accounts
in the same transaction. This migration timestamp represents continued access,
not historical proof of email ownership. Later startups do not confirm pending
accounts. Development seed accounts are preconfirmed.

A unique index enforces email uniqueness ignoring case and surrounding whitespace.
Existing collisions stop the migration with an error; resolve them explicitly
before deployment. The migration never deletes or merges accounts. Back up the
database before rollout and restart all API replicas together so old code cannot
issue sessions to new pending accounts.

## Validation

Run `go test -race ./...`. To also exercise PostgreSQL row locks, set
`OTP_TEST_POSTGRES_DSN` to a **disposable test database** before running the tests.
The PostgreSQL test runs migrations and inserts accounts; never point it at an
application database. Unit tests use a fake mail sender and do not send email.
