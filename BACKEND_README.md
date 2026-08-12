# Student Job Portal - Backend API

A production-ready MVP backend for a student-focused job portal built with Go, REST API, PostgreSQL, MinIO, JWT authentication, and role-based access control.

## 📋 Table of Contents

- [Overview](#overview)
- [Technology Stack](#technology-stack)
- [Project Status](#project-status)
- [Quick Start](#quick-start)
- [Development](#development)
- [Docker Deployment](#docker-deployment)
- [Project Structure](#project-structure)
- [API Endpoints](#api-endpoints)
- [Configuration](#configuration)
- [Architecture](#architecture)
- [Testing](#testing)
- [Security](#security)
- [Contributing](#contributing)

## Overview

This backend serves a comprehensive student job portal platform with three main user roles:

1. **Students**: Create profiles, upload CVs, search for jobs, apply for positions, and track applications
2. **Employers**: Create company profiles, post jobs, review applications, schedule interviews
3. **Admins**: Manage users, verify employers, approve jobs, handle reports

### Core Workflow

```
Student Registration
    ↓
Profile Completion
    ↓
Job Search & Filtering
    ↓
Application Submission
    ↓
Employer Review
    ↓
Application Status Change
    ↓
Notifications
```

## Technology Stack

| Component | Technology | Purpose |
|-----------|-----------|---------|
| **Language** | Go 1.24.0 | High-performance backend |
| **Web Framework** | Gin | REST API with middleware support |
| **Database** | PostgreSQL 16 | Relational data storage |
| **ORM** | GORM | Type-safe database operations |
| **Storage** | MinIO | Object storage for files |
| **Authentication** | JWT | Token-based authentication |
| **Containerization** | Docker | Application deployment |
| **Orchestration** | Docker Compose | Multi-service coordination |
| **API Documentation** | Swagger/OpenAPI | API documentation |

## Project Status

### ✅ Phase 1: Complete

- Project initialization
- Docker setup (Dockerfile, docker-compose.yml)
- PostgreSQL connection
- MinIO integration
- Configuration management
- Logging system
- Common response structures
- Makefile for development

**Status**: Production-ready foundation ✓

### 🔄 Phases 2-8: Upcoming

See [Implementation Roadmap](#implementation-roadmap) for details.

## Quick Start

### Prerequisites

- Docker and Docker Compose installed
- Git installed
- Go 1.24.0+ (for local development)

### Start the Application

```bash
# Clone the repository
cd /home/prasanna/Projects/student_job_portal/backend

# Copy environment variables
cp .env.example .env

# Start all services (PostgreSQL, MinIO, API)
docker-compose up --build

# Verify it's running
curl http://localhost:8080/health
```

### Stop the Application

```bash
docker-compose down
```

### View Logs

```bash
docker-compose logs -f api
docker-compose logs -f postgres
docker-compose logs -f minio
```

## Development

### Local Setup

#### Prerequisites for Local Development

- Go 1.24.0+
- PostgreSQL 16
- MinIO or S3-compatible storage
- `make` command-line tool

#### Running Locally

```bash
# Copy environment template
cp .env.example .env

# Edit .env for local development
# Update DB_HOST, MINIO_ENDPOINT if not using Docker for these services

# Start database and MinIO with Docker
make dev

# In another terminal, run the API
go run ./cmd/api/main.go

# Stop services
make dev-down
```

### Common Make Commands

```bash
# Development
make dev              # Start PostgreSQL and MinIO with Docker
make dev-down         # Stop local services

# Docker
make docker-up        # Start complete Docker stack
make docker-down      # Stop all services
make docker-rebuild   # Rebuild Docker images
make docker-logs      # View API logs

# Code Quality
make fmt              # Format code with go fmt
make lint             # Run linter (requires golangci-lint)
make test             # Run tests with coverage

# Build
make build            # Build binary to bin/api
make clean            # Remove build artifacts
```

### Building from Source

```bash
# Download dependencies
go mod download

# Build the application
make build

# Run the binary
./bin/api
```

## Docker Deployment

### Docker Compose Stack

The [docker-compose.yml](/home/prasanna/Projects/student_job_portal/backend/docker-compose.yml) orchestrates:

1. **PostgreSQL Service**
   - Port: 5432
   - Database: student_job_portal
   - Health check: Built-in pg_isready
   - Volume: postgres_data (persistent)

2. **MinIO Service**
   - API Port: 9000
   - Console Port: 9001
   - Access Key: minioadmin
   - Health check: MinIO health endpoint
   - Volume: minio_data (persistent)

3. **API Service**
   - Port: 8080
   - Depends on: PostgreSQL and MinIO
   - Health check: /health endpoint
   - Automatic restart policy

### Building Docker Image

```bash
# Build locally
docker build -t student-job-portal-api:latest .

# Run locally
docker run -p 8080:8080 \
  -e APP_PORT=8080 \
  -e DB_HOST=postgres \
  -e MINIO_ENDPOINT=minio:9000 \
  student-job-portal-api:latest
```

### Multi-Stage Build

The [Dockerfile](/home/prasanna/Projects/student_job_portal/backend/Dockerfile) uses multi-stage builds:

1. **Builder Stage**: Compiles Go code
2. **Runtime Stage**: Alpine Linux base with only the binary

Benefits:
- Smaller final image (~20-30MB)
- No build tools in production
- Improved security

## Project Structure

```
backend/
├── cmd/
│   └── api/
│       └── main.go                 # Application entry point
│
├── internal/
│   ├── config/
│   │   └── config.go              # Configuration management
│   ├── database/
│   │   └── connection.go          # Database connection & pooling
│   ├── logger/
│   │   └── logger.go              # Structured logging
│   ├── responses/
│   │   └── response.go            # Consistent JSON responses
│   ├── storage/
│   │   └── minio.go               # MinIO client & bucket management
│   ├── models/                     # (Phase 2+) GORM database models
│   ├── repositories/              # (Phase 2+) Data access layer
│   ├── services/                  # (Phase 2+) Business logic
│   ├── handlers/                  # (Phase 2+) HTTP request handlers
│   ├── middleware/                # (Phase 2+) HTTP middleware
│   ├── routes/                    # (Phase 2+) Route definitions
│   ├── validation/                # (Phase 2+) Request validation
│   └── utils/                     # (Phase 2+) Utility functions
│
├── migrations/                     # (Phase 2+) SQL migration files
├── docs/                          # Documentation
│   ├── PHASE1.md                 # Phase 1 implementation details
│   └── ...
│
├── Dockerfile                      # Multi-stage Docker build
├── docker-compose.yml             # Complete stack configuration
├── Makefile                       # Development commands
├── go.mod                         # Go module definition
├── go.sum                         # Dependency checksums
├── .env.example                   # Environment variables template
├── .gitignore                     # Git ignore rules
└── readme.md                      # Project specification (original)
```

## API Endpoints

### Phase 1 (Current)

#### Health Check
```
GET /health
GET /api/v1/health
```

Response:
```json
{
  "success": true,
  "message": "Health check passed",
  "data": {
    "status": "ok",
    "timestamp": "2024-08-05T18:10:38Z"
  }
}
```

### Phase 2-8 (Upcoming)

Detailed endpoint documentation will be added as each phase is completed. See [implementation roadmap](#implementation-roadmap) for planned endpoints.

## Configuration

### Environment Variables

All configuration is managed through environment variables. Copy [.env.example](/home/prasanna/Projects/student_job_portal/backend/.env.example) and customize:

```bash
cp .env.example .env
```

### Available Variables

```env
# Application
APP_ENV=development              # Environment: development, staging, production
APP_PORT=8080                    # API server port

# Database (PostgreSQL)
DB_HOST=postgres                 # PostgreSQL host
DB_PORT=5432                     # PostgreSQL port
DB_NAME=student_job_portal       # Database name
DB_USER=postgres                 # Database user
DB_PASSWORD=postgres             # Database password
DB_SSL_MODE=disable              # SSL mode: disable, require, verify-full

# JWT (CHANGE IN PRODUCTION!)
JWT_ACCESS_SECRET=your-secret    # Access token signing key
JWT_REFRESH_SECRET=your-secret   # Refresh token signing key
JWT_ACCESS_EXPIRY=15m            # Access token lifetime
JWT_REFRESH_EXPIRY=168h          # Refresh token lifetime

# MinIO (Object Storage)
MINIO_ENDPOINT=minio:9000        # MinIO API endpoint
MINIO_ACCESS_KEY=minioadmin      # MinIO access key
MINIO_SECRET_KEY=minioadmin      # MinIO secret key
MINIO_USE_SSL=false              # Use HTTPS for MinIO
MINIO_PROFILE_BUCKET=profile-images
MINIO_STUDENT_DOCUMENT_BUCKET=student-documents
MINIO_COMPANY_LOGO_BUCKET=company-logos
MINIO_COMPANY_DOCUMENT_BUCKET=company-documents
```

### Configuration Validation

The application validates required environment variables on startup. Missing or invalid configuration will cause the application to fail with descriptive error messages.

**Required Variables:**
- APP_ENV
- DB_HOST, DB_NAME, DB_USER, DB_PASSWORD
- JWT_ACCESS_SECRET, JWT_REFRESH_SECRET
- MINIO_ACCESS_KEY, MINIO_SECRET_KEY

## Architecture

### Layered Architecture

```
┌─────────────────────────────────┐
│   HTTP Request (Router)         │
├─────────────────────────────────┤
│   Middleware Layer              │
│   (Auth, Logging, Recovery)     │
├─────────────────────────────────┤
│   Handler Layer                 │
│   (Request validation/response) │
├─────────────────────────────────┤
│   Service Layer                 │
│   (Business logic)              │
├─────────────────────────────────┤
│   Repository Layer              │
│   (Database queries)            │
├─────────────────────────────────┤
│   Database Layer                │
│   (PostgreSQL + GORM)           │
└─────────────────────────────────┘
          ↕ (Parallel)
    ┌──────────────┐
    │ MinIO Storage│
    │ (File I/O)   │
    └──────────────┘
```

### Key Design Principles

1. **Separation of Concerns**: Clear boundaries between layers
2. **Dependency Injection**: Dependencies passed to functions
3. **Error Handling**: Comprehensive error handling at each layer
4. **Transaction Management**: Database transactions for critical operations
5. **Security First**: Security checks at middleware level
6. **Performance**: Connection pooling, query optimization, caching (Phase 5+)

## Testing

### Test Strategy

```
Unit Tests (Services, Validators)
  ↓
Integration Tests (Handlers + Repositories)
  ↓
End-to-End Tests (Full API workflows)
```

### Running Tests

```bash
# Run all tests with coverage
make test

# Run specific test file
go test -v ./internal/config

# Run with race detector
go test -race ./...

# Generate coverage report
go test -v -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Test Files Location

```
internal/
├── config/config_test.go
├── database/connection_test.go
├── logger/logger_test.go
├── handlers/*/handler_test.go
├── services/*/service_test.go
└── repositories/*/repository_test.go
```

### Testing with Docker

```bash
# Run tests in Docker container
docker-compose exec api go test ./...
```

## Security

### Implemented (Phase 1)

✅ Environment-based configuration (no hardcoded secrets)
✅ Non-root Docker user
✅ Connection pooling (prevents resource exhaustion)
✅ CORS configuration
✅ Panic recovery middleware

### To Be Implemented

⏳ Password hashing (bcrypt) - Phase 2
⏳ JWT authentication - Phase 2
⏳ Role-based access control (RBAC) - Phase 2
⏳ Rate limiting - Phase 2
⏳ Request validation - Phase 2
⏳ SQL injection prevention - Phase 3+
⏳ HTTPS support - Phase 8
⏳ Audit logging - Phase 7

### Security Best Practices

1. **Never commit .env files** with secrets
2. **Rotate JWT secrets** regularly
3. **Use strong passwords** for database and MinIO
4. **Enable HTTPS** in production
5. **Implement rate limiting** for authentication endpoints
6. **Use database transactions** for critical operations
7. **Validate all inputs** at handler level
8. **Audit important actions** (Phase 7)

## Implementation Roadmap

### Phase 2: User Authentication (🔄 Next)
- User model and database schema
- JWT token generation and validation
- User registration and login
- Refresh token management
- RBAC middleware
- Estimated: 2-3 weeks

### Phase 3: Student Profile
- Student profile model
- Education, skills, projects, certifications
- CV and profile image uploads
- Profile completion tracking
- Estimated: 2 weeks

### Phase 4: Companies & Employers
- Company profile
- Employer verification workflow
- Company document uploads
- Estimated: 1-2 weeks

### Phase 5: Jobs & Search
- Job posting model
- Job search and filtering
- Saved jobs functionality
- Pagination and sorting
- Estimated: 2-3 weeks

### Phase 6: Applications
- Application model
- Application status tracking
- Interview scheduling
- Status history logging
- Estimated: 2-3 weeks

### Phase 7: Advanced Features
- Notifications system
- Reporting and moderation
- Admin dashboard APIs
- Audit logging
- Estimated: 2-3 weeks

### Phase 8: Polish & Deployment
- Unit and integration tests
- Swagger documentation
- Database seeding
- README and deployment guides
- Security review and hardening
- Estimated: 1-2 weeks

**Total Timeline**: 14-21 weeks

## Contributing

### Code Style

```bash
# Format code
make fmt

# Run linter (requires golangci-lint)
make lint
```

### Git Workflow

1. Create a feature branch: `git checkout -b feature/feature-name`
2. Make your changes
3. Run tests: `make test`
4. Format code: `make fmt`
5. Commit with descriptive messages
6. Push and create a pull request

### Commit Message Format

```
type: description

Longer explanation if needed.

Related to #123
```

Types: feat, fix, docs, test, refactor, chore

## Troubleshooting

### Docker Issues

```bash
# Clean all containers and volumes
docker-compose down -v

# Rebuild and start
docker-compose up --build

# Check service status
docker-compose ps

# View logs
docker-compose logs -f [service-name]
```

### Database Connection Issues

1. Verify PostgreSQL is running: `docker-compose logs postgres`
2. Check DB_HOST in .env (should be `postgres` when using Docker)
3. Verify credentials match docker-compose.yml
4. Ensure database exists: Created automatically on first run

### MinIO Issues

1. Check MinIO logs: `docker-compose logs minio`
2. Access console: http://localhost:9001
3. Verify credentials in .env
4. Check bucket permissions

### API Won't Start

```bash
# Check for port conflicts
lsof -i :8080

# Check logs
docker-compose logs api

# Verify environment variables
docker-compose config | grep API
```

## Support & Documentation

- **Phase 1 Details**: [docs/PHASE1.md](/home/prasanna/Projects/student_job_portal/backend/docs/PHASE1.md)
- **Original Specification**: [readme.md](/home/prasanna/Projects/student_job_portal/backend/readme.md)
- **API Documentation**: (Coming in Phase 8 with Swagger)
- **Database Schema**: (Coming in Phase 2 with migrations)

## License

This project is proprietary and confidential.

## Contact

For questions or issues, please contact the development team.

---

**Last Updated**: August 5, 2024
**Status**: Phase 1 Complete ✓
**Next Phase**: Phase 2 (User Authentication)
