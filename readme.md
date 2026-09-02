Act as a senior Go backend engineer and build a production-ready MVP backend for a **student-focused job portal**.

## Technology Stack

Use:

* Go
* REST API
* PostgreSQL
* GORM
* MinIO for CVs, profile images, company logos, and documents
* Docker and Docker Compose
* JWT authentication
* Role-based access control
* `golang-migrate` or GORM migrations
* Swagger/OpenAPI documentation
* Structured logging
* Environment-based configuration

Use Docker Compose to run:

* Backend API
* PostgreSQL
* MinIO

Do not run the Go API only on the host machine. The complete application should be runnable with:

```bash
docker compose up --build
```

## User Roles

Support three roles:

1. Student
2. Employer
3. Admin

Implement authorization middleware so users can access only the endpoints allowed for their role.

## Core Workflow

The main MVP workflow is:

```text
Student registers
→ completes profile
→ searches jobs
→ applies with a CV
→ employer reviews application
→ employer changes application status
→ student receives a notification
```

## Project Structure

Use a clean, modular structure such as:

```text
cmd/
  api/

internal/
  config/
  database/
  models/
  repositories/
  services/
  handlers/
  middleware/
  routes/
  validation/
  storage/
  responses/
  utils/

migrations/
docs/
Dockerfile
docker-compose.yml
.env.example
Makefile
README.md
```

Keep responsibilities separated:

* Handler: HTTP request and response
* Service: business logic
* Repository: database operations
* Model: GORM database structure
* DTO: request and response structures
* Middleware: authentication, authorization, logging, recovery
* Storage: MinIO integration

Do not put all logic inside handlers.

## Authentication

Implement:

* User registration
* Login
* Password hashing using bcrypt
* Access token
* Refresh token
* Logout or refresh-token revocation
* Current-user endpoint
* Role-based authorization

Suggested endpoints:

```text
POST /api/v1/auth/register/student
POST /api/v1/auth/register/employer
POST /api/v1/auth/login
POST /api/v1/auth/refresh
POST /api/v1/auth/logout
GET  /api/v1/auth/me
```

Use secure JWT signing from environment variables.

Do not store plain-text passwords or refresh tokens.

## Student Module

Students should be able to:

* Create and update their profile
* Add education records
* Add skills
* Add projects
* Add certifications
* Upload a profile image
* Upload multiple CVs
* Select a default CV
* View profile completion percentage
* Save jobs
* Apply for jobs
* Withdraw applications
* Track application statuses
* View notifications

Student profile fields should include:

```text
user_id
full_name
phone
location
bio
college_name
degree
faculty_or_major
current_semester
graduation_year
preferred_job_categories
preferred_locations
preferred_work_mode
availability
expected_salary
linkedin_url
github_url
portfolio_url
profile_image_key
is_searchable
profile_completion_percentage
```

Use related tables for:

* Student education
* Student skills
* Student projects
* Student certifications
* Student documents

## Employer and Company Module

Employers should be able to:

* Register an employer account
* Create a company profile
* Upload a company logo
* Request company verification
* Create job posts
* Update their own jobs
* Close jobs
* View applicants
* Shortlist applicants
* Reject applicants
* Schedule interviews
* Mark candidates as selected

Company fields should include:

```text
name
description
industry
website
email
phone
location
logo_key
verification_status
verified_at
```

A company verification status can be:

```text
pending
approved
rejected
```

Only approved employers should be able to publish jobs.

## Job Module

Support these opportunity types:

```text
internship
entry_level
part_time
freelance
volunteer
graduate_trainee
```

Job fields should include:

```text
company_id
created_by
title
slug
description
responsibilities
requirements
education_requirement
experience_requirement
opportunity_type
employment_type
work_mode
location
salary_min
salary_max
salary_currency
stipend_amount
is_paid
is_fresher_friendly
training_provided
number_of_openings
application_deadline
status
published_at
closed_at
```

Job status values:

```text
draft
pending_approval
published
rejected
closed
expired
```

Implement job skills and application questions using related tables.

Students should be able to search and filter jobs by:

* Keyword
* Opportunity type
* Category
* Location
* Work mode
* Paid or unpaid
* Salary range
* Required skill
* Fresher-friendly status
* Application deadline

Implement pagination, sorting, and filtering.

Suggested public endpoints:

```text
GET /api/v1/jobs
GET /api/v1/jobs/:id
GET /api/v1/companies/:id
```

## Application Module

A student can apply only once to the same job.

An application should contain:

```text
job_id
student_id
student_document_id
cover_letter
status
applied_at
withdrawn_at
```

Application statuses:

```text
applied
under_review
shortlisted
interview_scheduled
selected
rejected
withdrawn
```

Store every status change in an application status history table containing:

```text
application_id
previous_status
new_status
changed_by
reason
created_at
```

Business rules:

* Students cannot apply to draft, closed, rejected, or expired jobs.
* Students cannot apply after the application deadline.
* Students can withdraw only active applications.
* Employers can manage only applications belonging to their own company’s jobs.
* Status transitions must be validated.
* Every application status change must create a history record.
* Historical applications must remain available even when a job is closed or soft-deleted.

Suggested endpoints:

```text
POST  /api/v1/jobs/:jobId/applications
GET   /api/v1/students/me/applications
GET   /api/v1/students/me/applications/:id
PATCH /api/v1/students/me/applications/:id/withdraw

GET   /api/v1/employers/jobs/:jobId/applications
GET   /api/v1/employers/applications/:id
PATCH /api/v1/employers/applications/:id/status
```

## Saved Jobs

Implement:

```text
POST   /api/v1/students/me/saved-jobs/:jobId
DELETE /api/v1/students/me/saved-jobs/:jobId
GET    /api/v1/students/me/saved-jobs
```

Prevent duplicate saved-job records.

## Notifications

Create in-app notifications for:

* Application submitted
* Application status changed
* Interview scheduled
* Employer verification approved or rejected
* Job approved or rejected
* Job deadline approaching

Notification fields:

```text
user_id
type
title
message
reference_type
reference_id
is_read
read_at
created_at
```

Provide endpoints to list notifications and mark them as read.

## Admin Module

Admins should be able to:

* View users
* Suspend or activate users
* Review employer verification requests
* Approve or reject employers
* Review pending jobs
* Approve or reject jobs
* Remove suspicious jobs
* View reports
* View basic dashboard statistics
* Review admin action history

All important admin actions should be stored in an audit log.

## Reporting Module

Students should be able to report:

* Fake jobs
* Jobs requesting money
* Misleading salary information
* Inappropriate content
* Suspicious employers

Report fields:

```text
reported_by
target_type
target_id
reason
description
status
reviewed_by
reviewed_at
resolution
```

Report status values:

```text
pending
reviewing
resolved
dismissed
```

## MinIO Integration

Use MinIO for:

* Student profile images
* Student CVs
* Certificates
* Company logos
* Company verification documents

Store only object keys and metadata in PostgreSQL.

Do not store file binaries in PostgreSQL.

Implement:

* File upload
* File validation
* File deletion
* Secure download
* Presigned URLs
* File-size limits
* MIME-type validation
* Unique object names
* Ownership checks

Suggested buckets:

```text
profile-images
student-documents
company-logos
company-documents
```

Do not expose MinIO administrative credentials to API clients.

## GORM Requirements

Use GORM with PostgreSQL.

Models should include:

* UUID primary keys
* CreatedAt
* UpdatedAt
* Soft deletion where appropriate
* Foreign-key constraints
* Unique indexes
* Composite indexes
* Check constraints where useful

Use database transactions for:

* Application creation
* Application status updates
* Employer verification
* Job approval
* File metadata creation with related profile updates

Avoid N+1 queries.

Use preloading only when necessary.

Create explicit repository methods instead of directly querying GORM throughout handlers.

## Suggested Main Entities

Implement these entities:

```text
users
refresh_tokens
student_profiles
student_educations
skills
student_skills
student_projects
student_certifications
student_documents

companies
employer_members
company_verification_requests
company_documents

job_categories
jobs
job_skills
job_questions
saved_jobs

applications
interviews

notifications
reports
admin_action_logs
```

## API Standards

Use a consistent JSON response format.

Success response:

```json
{
  "success": true,
  "message": "Job created successfully",
  "data": {}
}
```

Paginated response:

```json
{
  "success": true,
  "message": "Jobs retrieved successfully",
  "data": [],
  "pagination": {
    "page": 1,
    "page_size": 10,
    "total_size": 50,
    "total_pages": 5
  }
}
```

Error response:

```json
{
  "success": false,
  "message": "Validation failed",
  "errors": {
    "title": "Title is required"
  }
}
```

Use appropriate HTTP status codes.

## Validation and Security

Implement:

* Request DTO validation
* Email normalization
* Password strength validation
* SQL injection protection through parameterized GORM queries
* CORS configuration
* Rate limiting for authentication endpoints
* Panic recovery
* Request logging
* Secure headers
* File upload restrictions
* Ownership checks
* Role authorization
* Resource-level authorization
* Environment-secret validation
* Protection against duplicate submissions

Never trust role, user ID, company ID, or student ID values sent by clients. Read identity from the authenticated token.

## Docker Requirements

Create a multi-stage Dockerfile for the Go API.

Create a Docker Compose configuration containing:

```text
api
postgres
minio
minio-init
```

The MinIO initialization service should create the required buckets automatically.

Add health checks for:

* PostgreSQL
* MinIO
* API

The API should wait until PostgreSQL and MinIO are healthy.

Use named volumes for:

* PostgreSQL data
* MinIO data

Provide an `.env.example` containing:

```env
APP_ENV=development
APP_PORT=8080

DB_HOST=postgres
DB_PORT=5432
DB_NAME=student_job_portal
DB_USER=postgres
DB_PASSWORD=postgres
DB_SSL_MODE=disable

JWT_ACCESS_SECRET=
JWT_REFRESH_SECRET=
JWT_ACCESS_EXPIRY=15m
JWT_REFRESH_EXPIRY=168h

MINIO_ENDPOINT=minio:9000
MINIO_ACCESS_KEY=
MINIO_SECRET_KEY=
MINIO_USE_SSL=false

MINIO_PROFILE_BUCKET=profile-images
MINIO_STUDENT_DOCUMENT_BUCKET=student-documents
MINIO_COMPANY_LOGO_BUCKET=company-logos
MINIO_COMPANY_DOCUMENT_BUCKET=company-documents
```

Do not hardcode credentials.

## Database Initialization

Create migrations and seed data for:

* Admin user
* Default roles
* Job categories
* Common skills
* Application statuses
* Report reasons

The seeded admin password must come from environment variables.

## Testing

Write tests for:

* Registration and login
* JWT middleware
* Role authorization
* Job creation
* Job search filters
* Duplicate application prevention
* Application status transitions
* Employer ownership checks
* Student document ownership
* MinIO upload validation
* Admin approval workflow

Use mocks where appropriate and integration tests with PostgreSQL for critical repository behavior.

## Documentation

Generate:

* Complete README
* Local setup instructions
* Docker commands
* Environment variable documentation
* Migration commands
* Seed commands
* API endpoint list
* Example request and response payloads
* Swagger documentation
* Architecture explanation
* Database relationship explanation

## Implementation Order

Build the backend in these phases:

### Phase 1

* Project initialization
* Docker setup
* PostgreSQL connection
* MinIO connection
* Configuration
* Common response structure
* Logging and error handling

### Phase 2

* User model
* Authentication
* JWT
* Refresh tokens
* RBAC middleware

### Phase 3

* Student profile
* Education
* Skills
* Projects
* CV and profile-image uploads

### Phase 4

* Companies
* Employers
* Employer verification

### Phase 5

* Job creation
* Job approval
* Job search and filtering
* Saved jobs

### Phase 6

* Applications
* Application answers
* Status transitions
* Status history
* Interview scheduling

### Phase 7

* Notifications
* Reports
* Admin dashboard APIs
* Audit logs

### Phase 8

* Tests
* Swagger
* Seed data
* README
* Security review

For each phase:

1. Explain the design briefly.
2. Show the files being created.
3. Generate complete, compilable code.
4. Include migrations where required.
5. Include endpoint examples.
6. Include tests for important logic.
7. Verify imports, interfaces, and dependencies.
8. Do not leave placeholder code such as `TODO`.
9. Do not skip error handling.
10. Ensure the project still compiles before moving to the next phase.

Start by generating the project architecture, database entity relationships, Docker Compose file, Dockerfile, environment configuration, Go module dependencies, and Phase 1 implementation.
