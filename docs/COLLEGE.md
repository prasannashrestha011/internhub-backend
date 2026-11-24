> Historical design: college monitoring and admin endpoints are no longer supported.
> The current application supports only students and employers.

# College API and migration guide

All paths below start with `/api/v1`. Authentication uses the existing access-token
cookie or `Authorization: Bearer <token>`. The role is `college`, following the
existing lowercase `student`, `employer`, and `admin` roles.

## Migration and rollout

The API runs `database.AutoMigrate` at startup. It adds colleges, college membership,
internship progress, evaluations, and certificate metadata with foreign keys and
unique constraints. Universities are four fixed values returned by `GET /universities`;
there is no university table or CRUD API.

Existing students keep their free-text `college_name` and a nullable `college_id`.
Migration does not infer membership from a college name. New profiles require an
active college, and unassigned students must select one before applying again.
Once assigned, only an admin can change a student's college.

Existing application statuses remain intact. Migration backfills progress as follows:

| Existing application status | Internship progress |
| --- | --- |
| submitted, reviewing, shortlisted | applied |
| accepted | ongoing; start date uses accepted date or applied date |
| rejected, withdrawn | cancelled |

Repeating migration preserves completed internships and their evaluations/certificates.
Before deploying, back up the database and run the migration against a staging copy.
Create colleges and coordinators through the admin endpoints, then assign existing
student profiles. College access follows current student membership, including past
internship records; an admin reassignment moves that monitoring access.

Certificates use the existing private `MINIO_COMPANY_DOCUMENT_BUCKET`. Keep it
private and configure a MinIO endpoint reachable by browsers for signed URLs.
No new dependency or storage configuration variable is required.

## College setup

| Method | Path | Access / purpose |
| --- | --- | --- |
| GET | `/universities` | Public fixed `{value, label}` options |
| GET | `/colleges` | Public active colleges; `q`, `page`, `page_size` |
| GET, POST | `/admin/colleges` | Admin list (including inactive) / create |
| GET, PUT, DELETE | `/admin/colleges/:id` | Admin read / replace / delete unused college |
| GET, POST | `/admin/colleges/:id/users` | Admin list / create college coordinators |
| PUT | `/admin/users/:id/college` | Admin reassign an existing college user |
| PUT | `/admin/students/:id/college` | Admin assign a **student profile UUID**, not user UUID |
| GET | `/colleges/me` | Current college and university affiliation |

Create/update college payload:

```json
{
  "name": "ABC College",
  "university": "TRIBHUVAN_UNIVERSITY",
  "address": "Kathmandu",
  "contact_email": "internships@example.edu.np",
  "contact_phone": "01-5550000",
  "status": "active"
}
```

`name` and a supported `university` are required. `status` defaults to `active` and
accepts `active` or `inactive`. A college with members cannot be deleted; deactivate
it instead. Deactivation denies college monitoring access and new assignments/applications.

Create coordinator payload: `{"email":"coordinator@example.edu.np","full_name":"Coordinator","password":"Password123!"}`.
Passwords must be 8–72 bytes. The server sets the role and college. There is no public
college registration. Coordinators use `/auth/login`; `/auth/me` includes their
role, `college_id`, and college details.

Assignment payload: `{"college_id":"<college UUID>"}`. Student profile creation at
`POST /students/me/profile` also accepts `college_id`, with university derived from
the resolved college. Client-supplied nested college/document objects cannot change
those related records.

## Monitoring and access

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/colleges/me/dashboard` | College's summary metrics |
| GET | `/colleges/me/internships` | College's paginated internship records |
| GET | `/colleges/me/internships/:id` | College's record and outcomes |
| GET | `/colleges/me/internships/:id/certificate` | Authorized signed certificate URL |
| GET | `/internship-records` | Paginated records for any authenticated role |
| GET | `/internship-records/:id` | Record and outcomes for any authenticated role |
| GET | `/internship-records/:id/certificate` | Authorized signed certificate URL |

Here `:id` is the **application UUID**, which identifies one student at one internship.
Students see their own records; employers see applications to their own postings;
colleges see only their current students; admins see all records. College membership
is reloaded on every request. Shared university affiliation grants no access.
An out-of-scope record returns `404`; inactive/unassigned college accounts receive `403`.

Record fields include `id`, `student_id`, `student_name`, `college` (with university),
`program`, `faculty_or_major`, `semester`, `graduation_year`, `internship_id`,
`company_id` (employer user UUID), `company_name`, `position`, `application_status`,
`status` (internship progress), `start_date`, `end_date`, `applied_at`, `evaluation_status`,
`certificate_status`, `evaluation`, and `certificate`. Missing dates/outcomes are null.
Monitoring responses exclude employer notes, CVs, and private storage keys.

All record lists accept these filters together, before pagination:

| Query | Values / matching |
| --- | --- |
| `student_name` | Case-insensitive substring |
| `program` | Case-insensitive degree or faculty/major substring |
| `semester` | Case-insensitive exact current semester |
| `graduation_year` | Existing student graduation year |
| `company` | Case-insensitive company-name substring |
| `student_id` | Student profile UUID; still restricted to caller's scope |
| `status` | applied, ongoing, completed, cancelled |
| `evaluation_status` | not_due, pending, submitted |
| `certificate_status` | not_due, pending, issued |
| `page`, `page_size` | Existing pagination; defaults 1 and 10, size up to 100 |

Responses use the existing `{success, message, data}` envelope. Lists also include
`pagination: {page, page_size, total_size, total_pages}` with a filtered total.

Dashboard keys are `total_students_in_internships`, `ongoing_internships`,
`completed_internships`, `pending_evaluations`, `evaluations_received`,
`pending_certificates`, and `certificates_received`. Students are counted distinctly
across all internship activity; other metrics count application records.
Missing evaluations/certificates are `pending` only after completion, otherwise `not_due`.

## Company completion workflow

1. Student applies using existing `POST /internships/:id/apply`.
2. Employer accepts using `PUT /recruiters/applications/:id/status` with
   `{"status":"accepted"}`. Progress becomes `ongoing`, with the current start date.
3. Employer updates dates/completes using `PUT /recruiters/applications/:id/progress`:

   ```json
   {"status":"completed","start_date":"2026-05-01T00:00:00Z","end_date":"2026-08-01T00:00:00Z"}
   ```

   Status accepts `ongoing`, `completed`, or `cancelled`. Omitted dates retain their
   values; a missing final end date defaults to now. End must not precede start,
   and completion cannot end in the future. Completed/cancelled records are final.
   Admins can use `PUT /admin/internship-records/:id/progress` with the same payload.
4. Employer submits `POST /recruiters/applications/:id/evaluation`:

   ```json
   {"scores":{"technical":90,"communication":85},"remarks":"Delivered reliable work."}
   ```

   Requires completion, 1–20 named integer scores from 0 to 100, and nonblank remarks
   up to 10,000 bytes. There is one immutable evaluation per application, with
   `submitted_by`, `submitted_at`, and status `submitted`.
5. Employer uploads `POST /recruiters/applications/:id/certificate`, multipart field
   `file`, containing a PDF up to 10 MiB. Requires completion and an evaluation.
   One certificate per application stores issuer, filename, size, issue time, and
   status `issued`. The PDF content type is detected on the server.
6. Student/company/college read the record to see outcomes. Fetch its certificate
   endpoint for `{url, expires_in:300}`. Add `?download=true` for attachment download;
   default is inline viewing. URLs expire after five minutes; a URL already issued
   remains usable until expiry even if membership changes.

Invalid input returns `400`, unauthorized roles `403`, inaccessible/missing records
`404`, and invalid lifecycle transitions or duplicate outcomes `409`. College users
cannot update progress, evaluations, or certificates.

## Verification and development data

Run from `backend`:

```bash
go test -race ./...
go vet ./...
go build ./...
```

`internal/routes/college_test.go` exercises real routes, JWT checks, repositories,
database constraints, pagination, same-university college isolation, the outcome
workflow, and repeat migration. Only the external object store uses a test transport.
It defaults to in-memory SQLite. To verify PostgreSQL, point the test at an **empty,
disposable UTF-8 database** (the test writes fixtures and does not clean its tables):

```bash
COLLEGE_TEST_DATABASE_URL='host=localhost port=5432 user=postgres dbname=college_test sslmode=disable' \
  go test ./internal/routes -run TestCollegeInternshipWorkflow -count=1
```

`make db-seed` supplies two colleges in the same university and two coordinators;
see [seed accounts and data](SEEDING.md). Frontend implementation follows this API contract.
