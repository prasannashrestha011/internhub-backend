# Backend Feature Development Skill

## Core Architecture

This backend follows a layered architecture:

```text
Model
  ↓
Repository
  ↓
Service
  ↓
Handler
  ↓
Routes
```

When adding a new backend feature, follow this structure unless the existing code clearly requires otherwise.

Typical project structure:

```text
internal/
├── models/
├── repositories/
├── services/
├── handlers/
├── routes/
├── responses/
└── ...
```

A feature should generally have:

```text
models/
    internship_application.go

repositories/
    internship_application_repository.go

services/
    internship_application_service.go

handlers/
    internship_application_handler.go

routes/
    ...
```

---

## 1. Model

Create the database model in:

```text
internal/models/
```

The model represents the database entity and its relationships.

Follow the existing GORM conventions:

* UUID primary keys where the project uses UUIDs.
* GORM tags for database configuration.
* JSON tags for API serialization.
* Explicit foreign keys and relationships.
* `BeforeCreate` hooks when required by the existing models.

Example pattern:

```go
type InternshipApplication struct {
    ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

    InternshipID uuid.UUID `gorm:"type:uuid;not null;index" json:"internship_id"`
    StudentID    uuid.UUID `gorm:"type:uuid;not null;index" json:"student_id"`

    // feature-specific fields...
}
```

Before creating a new model, inspect related existing models and follow their relationship conventions.

Do not put business logic in the model unless it is specifically database/model lifecycle behavior.

---

## 2. Repository

Create the repository in:

```text
internal/repositories/
```

The repository is responsible for **database access only**.

Typical responsibilities:

* Create
* Get by ID
* Update
* Delete
* List
* Search/filter
* Count
* Preload relationships
* Database-specific queries

Example:

```go
type InternshipApplicationRepository struct {
    db *gorm.DB
}

func NewInternshipApplicationRepository(db *gorm.DB) *InternshipApplicationRepository {
    return &InternshipApplicationRepository{db: db}
}
```

Methods should use the request context:

```go
func (r *InternshipApplicationRepository) Create(
    ctx context.Context,
    application *models.InternshipApplication,
) error {
    return r.db.WithContext(ctx).Create(application).Error
}
```

The repository should **not contain business rules**.

For example, do not put rules such as:

```text
Student cannot apply twice
Internship must be active
Deadline must not have passed
Student must be eligible
```

inside the repository.

The repository should query the database to provide the information required by the service.

---

## 3. Service

Create the service in:

```text
internal/services/
```

The service contains the **business logic and business validation**.

This is one of the most important conventions of this project.

The service is responsible for deciding whether an operation is allowed.

For example, an internship application service may validate:

```text
- internship exists
- student exists
- internship is active
- application deadline has not passed
- student has not already applied
- student is eligible
- required business conditions are satisfied
```

The repository should provide the database operations needed to perform these checks.

Example structure:

```go
var (
    ErrInvalidApplicationData = errors.New("invalid application data")
    ErrApplicationNotFound    = errors.New("application not found")
    ErrAlreadyApplied         = errors.New("student already applied")
)
```

Then:

```go
func (s *InternshipApplicationService) CreateApplication(
    ctx context.Context,
    application *models.InternshipApplication,
) error {
    if application == nil {
        return fmt.Errorf(
            "%w: application cannot be nil",
            ErrInvalidApplicationData,
        )
    }

    // Business validation happens here.

    // Check internship.
    // Check student.
    // Check duplicate application.
    // Check eligibility.
    // Check deadline.
    // Create application.

    return s.repo.Create(ctx, application)
}
```

### Business validation rule

**Business decisions belong in the service layer.**

Do not move business validation into handlers simply because the handler receives the request.

Bad:

```go
if internship.Status != "published" {
    // business decision in handler
}
```

Prefer:

```go
if err := h.service.CreateApplication(ctx, &application); err != nil {
    // handler only translates the service error into HTTP
}
```

The service should also define meaningful domain errors so handlers can map them to appropriate HTTP responses.

---

## 4. Handler

Create the handler in:

```text
internal/handlers/
```

The handler is responsible for HTTP concerns.

Typical responsibilities:

```text
- Read path parameters
- Read query parameters
- Bind JSON
- Get authenticated user information from Gin context
- Call the service
- Convert service errors into HTTP responses
- Return the API response
```

Example:

```go
func (h *InternshipApplicationHandler) CreateApplication(c *gin.Context) {
    var application models.InternshipApplication

    if err := c.ShouldBindJSON(&application); err != nil {
        responses.Error(
            c,
            http.StatusBadRequest,
            "invalid request payload: "+err.Error(),
        )
        return
    }

    userID, ok := authenticatedUserID(c)
    if !ok {
        responses.Error(
            c,
            http.StatusUnauthorized,
            "invalid user context",
        )
        return
    }

    application.StudentID = userID

    if err := h.service.CreateApplication(
        c.Request.Context(),
        &application,
    ); err != nil {
        writeInternshipApplicationError(c, err)
        return
    }

    responses.Success(
        c,
        http.StatusCreated,
        "application submitted successfully",
        application,
    )
}
```

The handler should remain thin.

Do not put substantial business logic in the handler.

---

## 5. Error Mapping

Services should expose meaningful errors.

Example:

```go
var (
    ErrInvalidApplicationData = errors.New("invalid application data")
    ErrApplicationNotFound    = errors.New("application not found")
    ErrAlreadyApplied         = errors.New("student already applied")
    ErrInternshipClosed       = errors.New("internship is closed")
    ErrApplicationDeadline    = errors.New("application deadline has passed")
)
```

The handler translates these into HTTP responses:

```go
func writeInternshipApplicationError(
    c *gin.Context,
    err error,
) {
    switch {
    case errors.Is(err, services.ErrApplicationNotFound):
        responses.Error(
            c,
            http.StatusNotFound,
            "application not found",
        )

    case errors.Is(err, services.ErrAlreadyApplied):
        responses.Error(
            c,
            http.StatusConflict,
            "student has already applied",
        )

    case errors.Is(err, services.ErrInvalidApplicationData):
        responses.Error(
            c,
            http.StatusBadRequest,
            err.Error(),
        )

    default:
        responses.Error(
            c,
            http.StatusInternalServerError,
            "failed to process application",
        )
    }
}
```

Do not expose raw database errors to clients.

---

## 6. Routes

After the model, repository, service, and handler are implemented, register the feature in the existing route structure.

Follow the project's existing route grouping and middleware conventions.

For example:

```text
POST   /internships/:id/applications
GET    /internships/:id/applications
GET    /applications/:id
DELETE /applications/:id
```

Do not invent a new routing structure if the existing project already has an established pattern.

Use the appropriate authentication/role middleware based on the operation.

---

## 7. Dependency Construction

When adding a new feature, wire its dependencies through the existing application initialization pattern.

For example:

```text
DB
 ↓
Repository
 ↓
Service
 ↓
Handler
 ↓
Routes
```

A service that requires multiple repositories should receive them through its constructor.

Example:

```go
func NewInternshipApplicationService(
    repo *repositories.InternshipApplicationRepository,
    internshipRepo *repositories.InternshipRepository,
    studentRepo *repositories.StudentProfileRepository,
) *InternshipApplicationService {
    return &InternshipApplicationService{
        repo:           repo,
        internshipRepo: internshipRepo,
        studentRepo:    studentRepo,
    }
}
```

Do not instantiate repositories or database connections inside handlers or services.

---

## 8. Feature Development Workflow

When the user asks to add a new backend feature, follow this process:

### Step 1 — Understand the feature

Determine:

* What entity is being created or modified?
* What existing entities does it relate to?
* What operations are required?
* Who is allowed to perform each operation?
* What business rules apply?
* What API endpoints are needed?

Do not start coding before understanding these relationships.

### Step 2 — Inspect related existing code

Search the repository for related:

* Models
* Repositories
* Services
* Handlers
* Routes
* Error patterns
* Response patterns

For example, when adding `InternshipApplication`, inspect:

```text
Internship
StudentProfile
RecruiterProfile
```

and their existing implementations.

Reuse existing methods where appropriate.

### Step 3 — Implement the Model

Create the database representation and relationships.

### Step 4 — Implement the Repository

Add only the database operations required by the feature.

### Step 5 — Implement the Service

Add the business logic and validation.

This is where the feature's actual rules belong.

### Step 6 — Implement the Handler

Keep HTTP handling thin:

```text
request
 ↓
bind/parse
 ↓
authenticated user/context
 ↓
service call
 ↓
error mapping
 ↓
response
```

### Step 7 — Register Routes

Add the required endpoints using the existing route and middleware structure.

### Step 8 — Wire Dependencies

Connect repository → service → handler in the application's existing initialization/bootstrap code.

---

## 9. General Layer Responsibilities

Use this rule when deciding where code belongs:

| Layer      | Responsibility                                     |
| ---------- | -------------------------------------------------- |
| Model      | Database entity, relationships, GORM configuration |
| Repository | Database queries and persistence                   |
| Service    | Business logic and business validation             |
| Handler    | HTTP request/response handling                     |
| Routes     | Endpoint registration and middleware               |
| Response   | Consistent API response formatting                 |

If unsure where logic belongs, ask:

**"Is this a business decision?"**

If yes → **Service**

**"Is this a database operation?"**

If yes → **Repository**

**"Is this HTTP-specific?"**

If yes → **Handler**

**"Is this database/entity structure?"**

If yes → **Model**

---

## 10. Do Not Over-Engineer

Follow the architecture already present in the project.

Do not introduce:

* A new ORM
* A new HTTP framework
* A new repository abstraction
* A new service pattern
* A new validation framework
* A new response system
* Unnecessary interfaces
* Unnecessary packages

Use the existing patterns first.

The goal is for a new feature to look like it naturally belongs in the existing codebase.

## Golden Rule

For a new feature such as `InternshipApplication`:

```text
Model
  ↓
Repository
  ↓
Service
  ↓
Handler
  ↓
Routes
```

**Repository handles data access.
Service handles business rules.
Handler handles HTTP.
Routes expose the functionality.**

When the user asks for a feature, implement it consistently across all required layers rather than placing the entire feature inside a single file or layer.


## Pagination for List Endpoints

Any endpoint that returns a potentially large list of records should use the project's existing pagination pattern.

Do not return an unbounded list from the database unless the dataset is intentionally small or there is a specific reason not to paginate.

### Repository

List repository methods should generally accept:

```go
page, pageSize int
```

and return:

```go
([]models.Entity, int64, error)
```

where the `int64` is the total number of matching records.

Example:

```go
func (r *InternshipApplicationRepository) ListByStudent(
    ctx context.Context,
    studentID uuid.UUID,
    page,
    pageSize int,
) ([]models.InternshipApplication, int64, error) {
    var applications []models.InternshipApplication
    var total int64

    query := r.db.WithContext(ctx).
        Model(&models.InternshipApplication{}).
        Where("student_id = ?", studentID)

    if err := query.Count(&total).Error; err != nil {
        return nil, 0, err
    }

    off, limit := getPagination(page, pageSize)

    err := query.
        Order("created_at DESC").
        Offset(off).
        Limit(limit).
        Find(&applications).Error

    return applications, total, err
}
```

Use the existing `getPagination()` helper rather than implementing pagination logic differently in each repository.

The existing behavior is:

```text
page <= 0          → page 1
pageSize <= 0      → default 10
pageSize > 100     → default 10
```

Do not create a second pagination helper unless there is a genuine project-wide requirement.

---

### Service

The service should pass pagination parameters to the repository and perform any business validation required for the operation.

Example:

```go
func (s *InternshipApplicationService) ListByStudent(
    ctx context.Context,
    studentID uuid.UUID,
    page,
    pageSize int,
) ([]models.InternshipApplication, int64, error) {
    if studentID == uuid.Nil {
        return nil, 0, fmt.Errorf(
            "%w: invalid student ID",
            ErrInvalidApplicationData,
        )
    }

    return s.repo.ListByStudent(
        ctx,
        studentID,
        page,
        pageSize,
    )
}
```

---

### Handler

Read pagination parameters using the existing pagination parser:

```go
page, pageSize := parsePagination(c)
```

Then pass them to the service.

Example:

```go
func (h *InternshipApplicationHandler) ListMyApplications(c *gin.Context) {
    userID, ok := authenticatedUserID(c)
    if !ok {
        responses.Error(
            c,
            http.StatusUnauthorized,
            "invalid user context",
        )
        return
    }

    page, pageSize := parsePagination(c)

    applications, total, err := h.service.ListByStudent(
        c.Request.Context(),
        userID,
        page,
        pageSize,
    )

    if err != nil {
        responses.Error(
            c,
            http.StatusInternalServerError,
            "failed to fetch applications",
        )
        return
    }

    responses.SuccessWithPagination(
        c,
        http.StatusOK,
        "applications",
        applications,
        responses.CalculatePagination(
            int64(page),
            int64(pageSize),
            total,
        ),
    )
}
```

---

### Search/List Endpoints

For endpoints that support filtering and searching, pagination should be applied **after the filters** and the total count should represent the number of records matching those filters.

Typical repository pattern:

```go
query := r.db.WithContext(ctx).
    Model(&models.Entity{})

// filters...

if err := query.Count(&total).Error; err != nil {
    return nil, 0, err
}

off, limit := getPagination(page, pageSize)

err := query.
    Order("created_at DESC").
    Offset(off).
    Limit(limit).
    Find(&entities).Error
```

The order is important:

```text
Build query
    ↓
Apply filters
    ↓
Count filtered results
    ↓
Apply offset + limit
    ↓
Fetch current page
```

Do not count all records before applying filters.

---

### Pagination Rule

Whenever adding a new list/search endpoint, ask:

```text
Does this endpoint return multiple database records?
```

If yes, use pagination unless the endpoint has a clear reason not to.

Follow the existing project convention:

```text
Handler
    ↓
parsePagination(c)
    ↓
Service
    ↓
Repository
    ↓
Count()
    ↓
Offset()
    ↓
Limit()
    ↓
return data + total
    ↓
SuccessWithPagination()
```

The frontend should then receive both the current page of data and the pagination metadata through the project's existing response format.
