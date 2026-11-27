# Instructions for AI

Review the Git diffs below and prepare commands to stage and commit the changes.

- Group related changes into focused commits; keep unrelated changes in separate commits.
- Write concise, professional commit subjects in the imperative mood (maximum 72 characters).
- Respond with exactly one fenced `bash` code block containing all commands in execution order, with no prose outside it. Do not split commands across responses or code blocks.
- For each group, use `git add -- <paths>` followed by `git commit --only -m '<subject>' -- <same paths>` so other staged changes stay out of that commit.
- Use explicit, safely quoted file paths and commit messages. Avoid broad staging commands and exclude this generated `git-diff.md` report.
- Chain all commands with `&&` so execution stops if any command fails.
- Only generate the commands; do not execute them.

# Git diff

# internal/database/migrations.go

diff --git a/internal/database/migrations.go b/internal/database/migrations.go
index ee1de46..b70cd68 100644
--- a/internal/database/migrations.go
+++ b/internal/database/migrations.go
@@ -7,7 +7,8 @@ import (
 
 // AutoMigrate keeps the API and standalone database commands on the same schema.
 func AutoMigrate(db *gorm.DB) error {
-	return db.AutoMigrate(
+	if err := db.AutoMigrate(
+		&models.College{},
 		&models.User{},
 		&models.RefreshToken{},
 		&models.StudentProfile{},
@@ -18,8 +19,21 @@ func AutoMigrate(db *gorm.DB) error {
 		&models.StudentDocument{},
 		&models.Internship{},
 		&models.InternshipApplication{},
+		&models.InternshipEvaluation{},
+		&models.InternshipCertificate{},
 		&models.Interview{},
 		&models.RecruiterProfile{},
 		&models.OrganizationVerification{},
-	)
+	); err != nil {
+		return err
+	}
+	// Preserve existing records without guessing college affiliation from free text.
+	if err := db.Model(&models.InternshipApplication{}).
+		Where("status = ? AND progress_status = ?", models.ApplicationStatusAccepted, "applied").
+		Updates(map[string]interface{}{"progress_status": "ongoing", "start_date": gorm.Expr("COALESCE(accepted_at, applied_at)")}).Error; err != nil {
+		return err
+	}
+	return db.Model(&models.InternshipApplication{}).
+		Where("status IN ? AND progress_status = ?", []models.InternshipApplicationStatus{models.ApplicationStatusRejected, models.ApplicationStatusWithdrawn}, "applied").
+		Update("progress_status", "cancelled").Error
 }
diff --git a/internal/database/migrations.go b/internal/database/migrations.go
index b70cd68..b5a34b6 100644
--- a/internal/database/migrations.go
+++ b/internal/database/migrations.go
@@ -1,39 +1,36 @@
 package database
 
 import (
+	"fmt"
+	"time"
+
 	"github.com/prasanna/student-job-portal/backend/internal/models"
 	"gorm.io/gorm"
 )
 
-// AutoMigrate keeps the API and standalone database commands on the same schema.
 func AutoMigrate(db *gorm.DB) error {
-	if err := db.AutoMigrate(
-		&models.College{},
-		&models.User{},
-		&models.RefreshToken{},
-		&models.StudentProfile{},
-		&models.StudentEducation{},
-		&models.StudentSkill{},
-		&models.StudentProject{},
-		&models.StudentCertification{},
-		&models.StudentDocument{},
-		&models.Internship{},
-		&models.InternshipApplication{},
-		&models.InternshipEvaluation{},
-		&models.InternshipCertificate{},
-		&models.Interview{},
-		&models.RecruiterProfile{},
-		&models.OrganizationVerification{},
-	); err != nil {
-		return err
-	}
-	// Preserve existing records without guessing college affiliation from free text.
-	if err := db.Model(&models.InternshipApplication{}).
-		Where("status = ? AND progress_status = ?", models.ApplicationStatusAccepted, "applied").
-		Updates(map[string]interface{}{"progress_status": "ongoing", "start_date": gorm.Expr("COALESCE(accepted_at, applied_at)")}).Error; err != nil {
-		return err
-	}
-	return db.Model(&models.InternshipApplication{}).
-		Where("status IN ? AND progress_status = ?", []models.InternshipApplicationStatus{models.ApplicationStatusRejected, models.ApplicationStatusWithdrawn}, "applied").
-		Update("progress_status", "cancelled").Error
+	// Keep the column and legacy backfill atomic so a restart cannot verify new accounts.
+	return db.Transaction(func(tx *gorm.DB) error {
+		hadVerification := tx.Migrator().HasColumn(&models.User{}, "EmailVerifiedAt")
+		if err := tx.AutoMigrate(&models.User{}); err != nil {
+			return err
+		}
+		if !hadVerification {
+			if err := tx.Unscoped().Model(&models.User{}).Where("email_verified_at IS NULL").Update("email_verified_at", time.Now().UTC()).Error; err != nil {
+				return err
+			}
+		}
+		// Retired tables and columns are retained so existing records are not destroyed.
+		if err := tx.AutoMigrate(&models.EmailVerification{}, &models.RefreshToken{}, &models.StudentProfile{}, &models.StudentDocument{}, &models.RecruiterProfile{}, &models.Internship{}, &models.InternshipApplication{}); err != nil {
+			return err
+		}
+		if err := tx.Model(&models.InternshipApplication{}).Where("status IN ?", []string{"reviewing", "shortlisted"}).Update("status", models.ApplicationStatusSubmitted).Error; err != nil {
+			return err
+		}
+		// SQLite table recreation can drop expression indexes, so recreate this last.
+		if err := tx.Exec("CREATE UNIQUE INDEX IF NOT EXISTS users_email_normalized ON users (LOWER(TRIM(email)))").Error; err != nil {
+			return fmt.Errorf("email uniqueness migration: resolve duplicate emails ignoring case and whitespace: %w", err)
+		}
+		return nil
+	})
 }

# internal/handlers/college_handler.go

diff --git a/internal/handlers/college_handler.go b/internal/handlers/college_handler.go
new file mode 100644
index 0000000..c9b1645
--- /dev/null
+++ b/internal/handlers/college_handler.go
@@ -0,0 +1,171 @@
+package handlers
+
+import (
+	"errors"
+	"net/http"
+
+	"github.com/gin-gonic/gin"
+	"github.com/google/uuid"
+	"github.com/prasanna/student-job-portal/backend/internal/models"
+	"github.com/prasanna/student-job-portal/backend/internal/responses"
+	"github.com/prasanna/student-job-portal/backend/internal/services"
+)
+
+type CollegeHandler struct{ service *services.CollegeService }
+
+func NewCollegeHandler(service *services.CollegeService) *CollegeHandler {
+	return &CollegeHandler{service: service}
+}
+
+func (h *CollegeHandler) Universities(c *gin.Context) {
+	responses.Success(c, http.StatusOK, "supported universities", models.Universities())
+}
+
+func (h *CollegeHandler) List(activeOnly bool) gin.HandlerFunc {
+	return func(c *gin.Context) {
+		page, pageSize := parsePagination(c)
+		items, total, err := h.service.List(c.Request.Context(), c.Query("q"), activeOnly, page, pageSize)
+		if err != nil {
+			writeCollegeError(c, err)
+			return
+		}
+		responses.SuccessWithPagination(c, http.StatusOK, "colleges", items, responses.CalculatePagination(int64(page), int64(pageSize), total))
+	}
+}
+
+func (h *CollegeHandler) Get(c *gin.Context) {
+	id, ok := recordPathID(c)
+	if !ok {
+		return
+	}
+	college, err := h.service.Get(c.Request.Context(), id)
+	if err != nil {
+		writeCollegeError(c, err)
+		return
+	}
+	responses.Success(c, http.StatusOK, "college", college)
+}
+
+func (h *CollegeHandler) Me(c *gin.Context) {
+	uid, _ := getAuthenticatedUserID(c)
+	college, err := h.service.Resolve(c.Request.Context(), uid)
+	if err != nil {
+		writeCollegeError(c, err)
+		return
+	}
+	responses.Success(c, http.StatusOK, "college", college)
+}
+
+func (h *CollegeHandler) Save(c *gin.Context) {
+	id := uuid.Nil
+	if c.Param("id") != "" {
+		var ok bool
+		id, ok = recordPathID(c)
+		if !ok {
+			return
+		}
+	}
+	var input models.College
+	if err := c.ShouldBindJSON(&input); err != nil {
+		responses.Error(c, http.StatusBadRequest, "invalid college payload")
+		return
+	}
+	if err := h.service.Save(c.Request.Context(), id, &input); err != nil {
+		writeCollegeError(c, err)
+		return
+	}
+	status := http.StatusOK
+	if id == uuid.Nil {
+		status = http.StatusCreated
+	}
+	responses.Success(c, status, "college saved", input)
+}
+
+func (h *CollegeHandler) Delete(c *gin.Context) {
+	id, ok := recordPathID(c)
+	if !ok {
+		return
+	}
+	if err := h.service.Delete(c.Request.Context(), id); err != nil {
+		writeCollegeError(c, err)
+		return
+	}
+	responses.Success(c, http.StatusOK, "college deleted", nil)
+}
+
+func (h *CollegeHandler) CreateUser(c *gin.Context) {
+	id, ok := recordPathID(c)
+	if !ok {
+		return
+	}
+	var input registerRequest
+	if err := c.ShouldBindJSON(&input); err != nil {
+		responses.Error(c, http.StatusBadRequest, "invalid college user payload")
+		return
+	}
+	user, err := h.service.CreateUser(c.Request.Context(), id, input.Email, input.FullName, input.Password)
+	if err != nil {
+		writeCollegeError(c, err)
+		return
+	}
+	responses.Success(c, http.StatusCreated, "college user created", user)
+}
+
+func (h *CollegeHandler) ListUsers(c *gin.Context) {
+	id, ok := recordPathID(c)
+	if !ok {
+		return
+	}
+	page, pageSize := parsePagination(c)
+	users, total, err := h.service.ListUsers(c.Request.Context(), id, page, pageSize)
+	if err != nil {
+		writeCollegeError(c, err)
+		return
+	}
+	responses.SuccessWithPagination(c, http.StatusOK, "college users", users, responses.CalculatePagination(int64(page), int64(pageSize), total))
+}
+
+func (h *CollegeHandler) Assign(student bool) gin.HandlerFunc {
+	return func(c *gin.Context) {
+		id, ok := recordPathID(c)
+		if !ok {
+			return
+		}
+		var input struct {
+			CollegeID uuid.UUID `json:"college_id"`
+		}
+		if err := c.ShouldBindJSON(&input); err != nil {
+			responses.Error(c, http.StatusBadRequest, "invalid college assignment")
+			return
+		}
+		if err := h.service.Assign(c.Request.Context(), id, input.CollegeID, student); err != nil {
+			writeCollegeError(c, err)
+			return
+		}
+		responses.Success(c, http.StatusOK, "college assigned", nil)
+	}
+}
+
+func recordPathID(c *gin.Context) (uuid.UUID, bool) {
+	id, err := uuid.Parse(c.Param("id"))
+	if err != nil || id == uuid.Nil {
+		responses.Error(c, http.StatusBadRequest, "invalid id")
+		return uuid.Nil, false
+	}
+	return id, true
+}
+
+func writeCollegeError(c *gin.Context, err error) {
+	switch {
+	case errors.Is(err, services.ErrCollegeNotFound), errors.Is(err, services.ErrRecordNotFound), errors.Is(err, services.ErrStudentProfileNotFound):
+		responses.Error(c, http.StatusNotFound, "record not found")
+	case errors.Is(err, services.ErrCollegeAccess):
+		responses.Error(c, http.StatusForbidden, "access denied")
+	case errors.Is(err, services.ErrInvalidCollegeData), errors.Is(err, services.ErrInvalidRecordData):
+		responses.Error(c, http.StatusBadRequest, err.Error())
+	case errors.Is(err, services.ErrCollegeConflict), errors.Is(err, services.ErrRecordConflict):
+		responses.Error(c, http.StatusConflict, err.Error())
+	default:
+		responses.Error(c, http.StatusInternalServerError, "failed to process request")
+	}
+}
diff --git a/internal/handlers/college_handler.go b/internal/handlers/college_handler.go
deleted file mode 100644
index c9b1645..0000000
--- a/internal/handlers/college_handler.go
+++ /dev/null
@@ -1,171 +0,0 @@
-package handlers
-
-import (
-	"errors"
-	"net/http"
-
-	"github.com/gin-gonic/gin"
-	"github.com/google/uuid"
-	"github.com/prasanna/student-job-portal/backend/internal/models"
-	"github.com/prasanna/student-job-portal/backend/internal/responses"
-	"github.com/prasanna/student-job-portal/backend/internal/services"
-)
-
-type CollegeHandler struct{ service *services.CollegeService }
-
-func NewCollegeHandler(service *services.CollegeService) *CollegeHandler {
-	return &CollegeHandler{service: service}
-}
-
-func (h *CollegeHandler) Universities(c *gin.Context) {
-	responses.Success(c, http.StatusOK, "supported universities", models.Universities())
-}
-
-func (h *CollegeHandler) List(activeOnly bool) gin.HandlerFunc {
-	return func(c *gin.Context) {
-		page, pageSize := parsePagination(c)
-		items, total, err := h.service.List(c.Request.Context(), c.Query("q"), activeOnly, page, pageSize)
-		if err != nil {
-			writeCollegeError(c, err)
-			return
-		}
-		responses.SuccessWithPagination(c, http.StatusOK, "colleges", items, responses.CalculatePagination(int64(page), int64(pageSize), total))
-	}
-}
-
-func (h *CollegeHandler) Get(c *gin.Context) {
-	id, ok := recordPathID(c)
-	if !ok {
-		return
-	}
-	college, err := h.service.Get(c.Request.Context(), id)
-	if err != nil {
-		writeCollegeError(c, err)
-		return
-	}
-	responses.Success(c, http.StatusOK, "college", college)
-}
-
-func (h *CollegeHandler) Me(c *gin.Context) {
-	uid, _ := getAuthenticatedUserID(c)
-	college, err := h.service.Resolve(c.Request.Context(), uid)
-	if err != nil {
-		writeCollegeError(c, err)
-		return
-	}
-	responses.Success(c, http.StatusOK, "college", college)
-}
-
-func (h *CollegeHandler) Save(c *gin.Context) {
-	id := uuid.Nil
-	if c.Param("id") != "" {
-		var ok bool
-		id, ok = recordPathID(c)
-		if !ok {
-			return
-		}
-	}
-	var input models.College
-	if err := c.ShouldBindJSON(&input); err != nil {
-		responses.Error(c, http.StatusBadRequest, "invalid college payload")
-		return
-	}
-	if err := h.service.Save(c.Request.Context(), id, &input); err != nil {
-		writeCollegeError(c, err)
-		return
-	}
-	status := http.StatusOK
-	if id == uuid.Nil {
-		status = http.StatusCreated
-	}
-	responses.Success(c, status, "college saved", input)
-}
-
-func (h *CollegeHandler) Delete(c *gin.Context) {
-	id, ok := recordPathID(c)
-	if !ok {
-		return
-	}
-	if err := h.service.Delete(c.Request.Context(), id); err != nil {
-		writeCollegeError(c, err)
-		return
-	}
-	responses.Success(c, http.StatusOK, "college deleted", nil)
-}
-
-func (h *CollegeHandler) CreateUser(c *gin.Context) {
-	id, ok := recordPathID(c)
-	if !ok {
-		return
-	}
-	var input registerRequest
-	if err := c.ShouldBindJSON(&input); err != nil {
-		responses.Error(c, http.StatusBadRequest, "invalid college user payload")
-		return
-	}
-	user, err := h.service.CreateUser(c.Request.Context(), id, input.Email, input.FullName, input.Password)
-	if err != nil {
-		writeCollegeError(c, err)
-		return
-	}
-	responses.Success(c, http.StatusCreated, "college user created", user)
-}
-
-func (h *CollegeHandler) ListUsers(c *gin.Context) {
-	id, ok := recordPathID(c)
-	if !ok {
-		return
-	}
-	page, pageSize := parsePagination(c)
-	users, total, err := h.service.ListUsers(c.Request.Context(), id, page, pageSize)
-	if err != nil {
-		writeCollegeError(c, err)
-		return
-	}
-	responses.SuccessWithPagination(c, http.StatusOK, "college users", users, responses.CalculatePagination(int64(page), int64(pageSize), total))
-}
-
-func (h *CollegeHandler) Assign(student bool) gin.HandlerFunc {
-	return func(c *gin.Context) {
-		id, ok := recordPathID(c)
-		if !ok {
-			return
-		}
-		var input struct {
-			CollegeID uuid.UUID `json:"college_id"`
-		}
-		if err := c.ShouldBindJSON(&input); err != nil {
-			responses.Error(c, http.StatusBadRequest, "invalid college assignment")
-			return
-		}
-		if err := h.service.Assign(c.Request.Context(), id, input.CollegeID, student); err != nil {
-			writeCollegeError(c, err)
-			return
-		}
-		responses.Success(c, http.StatusOK, "college assigned", nil)
-	}
-}
-
-func recordPathID(c *gin.Context) (uuid.UUID, bool) {
-	id, err := uuid.Parse(c.Param("id"))
-	if err != nil || id == uuid.Nil {
-		responses.Error(c, http.StatusBadRequest, "invalid id")
-		return uuid.Nil, false
-	}
-	return id, true
-}
-
-func writeCollegeError(c *gin.Context, err error) {
-	switch {
-	case errors.Is(err, services.ErrCollegeNotFound), errors.Is(err, services.ErrRecordNotFound), errors.Is(err, services.ErrStudentProfileNotFound):
-		responses.Error(c, http.StatusNotFound, "record not found")
-	case errors.Is(err, services.ErrCollegeAccess):
-		responses.Error(c, http.StatusForbidden, "access denied")
-	case errors.Is(err, services.ErrInvalidCollegeData), errors.Is(err, services.ErrInvalidRecordData):
-		responses.Error(c, http.StatusBadRequest, err.Error())
-	case errors.Is(err, services.ErrCollegeConflict), errors.Is(err, services.ErrRecordConflict):
-		responses.Error(c, http.StatusConflict, err.Error())
-	default:
-		responses.Error(c, http.StatusInternalServerError, "failed to process request")
-	}
-}

# internal/handlers/internship_record_handler.go

diff --git a/internal/handlers/internship_record_handler.go b/internal/handlers/internship_record_handler.go
new file mode 100644
index 0000000..960ae3e
--- /dev/null
+++ b/internal/handlers/internship_record_handler.go
@@ -0,0 +1,149 @@
+package handlers
+
+import (
+	"net/http"
+	"strconv"
+
+	"github.com/gin-gonic/gin"
+	"github.com/google/uuid"
+	"github.com/prasanna/student-job-portal/backend/internal/repositories"
+	"github.com/prasanna/student-job-portal/backend/internal/responses"
+	"github.com/prasanna/student-job-portal/backend/internal/services"
+)
+
+type InternshipRecordHandler struct {
+	service *services.InternshipRecordService
+}
+
+func NewInternshipRecordHandler(service *services.InternshipRecordService) *InternshipRecordHandler {
+	return &InternshipRecordHandler{service: service}
+}
+
+func (h *InternshipRecordHandler) List(c *gin.Context) {
+	uid, _ := getAuthenticatedUserID(c)
+	page, pageSize := parsePagination(c)
+	filter := repositories.InternshipRecordFilter{StudentName: c.Query("student_name"), Program: c.Query("program"), Semester: c.Query("semester"),
+		Company: c.Query("company"), Status: c.Query("status"), EvaluationStatus: c.Query("evaluation_status"), CertificateStatus: c.Query("certificate_status"), Page: page, PageSize: pageSize}
+	if year := c.Query("graduation_year"); year != "" {
+		var err error
+		filter.AcademicYear, err = strconv.Atoi(year)
+		if err != nil {
+			responses.Error(c, http.StatusBadRequest, "invalid graduation_year")
+			return
+		}
+	}
+	if value := c.Query("student_id"); value != "" {
+		id, err := uuid.Parse(value)
+		if err != nil {
+			responses.Error(c, http.StatusBadRequest, "invalid student_id")
+			return
+		}
+		filter.StudentID = &id
+	}
+	records, total, err := h.service.List(c.Request.Context(), uid, filter)
+	if err != nil {
+		writeCollegeError(c, err)
+		return
+	}
+	responses.SuccessWithPagination(c, http.StatusOK, "internship records", records, responses.CalculatePagination(int64(page), int64(pageSize), total))
+}
+
+func (h *InternshipRecordHandler) Dashboard(c *gin.Context) {
+	uid, _ := getAuthenticatedUserID(c)
+	metrics, err := h.service.Dashboard(c.Request.Context(), uid)
+	if err != nil {
+		writeCollegeError(c, err)
+		return
+	}
+	responses.Success(c, http.StatusOK, "college dashboard", metrics)
+}
+
+func (h *InternshipRecordHandler) Get(c *gin.Context) {
+	id, ok := recordPathID(c)
+	if !ok {
+		return
+	}
+	uid, _ := getAuthenticatedUserID(c)
+	record, err := h.service.Get(c.Request.Context(), uid, id)
+	if err != nil {
+		writeCollegeError(c, err)
+		return
+	}
+	responses.Success(c, http.StatusOK, "internship record", record)
+}
+
+func (h *InternshipRecordHandler) UpdateProgress(c *gin.Context) {
+	id, ok := recordPathID(c)
+	if !ok {
+		return
+	}
+	uid, _ := getAuthenticatedUserID(c)
+	var input services.InternshipProgressInput
+	if err := c.ShouldBindJSON(&input); err != nil {
+		responses.Error(c, http.StatusBadRequest, "invalid progress payload")
+		return
+	}
+	record, err := h.service.UpdateProgress(c.Request.Context(), uid, id, input)
+	if err != nil {
+		writeCollegeError(c, err)
+		return
+	}
+	responses.Success(c, http.StatusOK, "internship progress updated", record)
+}
+
+func (h *InternshipRecordHandler) SubmitEvaluation(c *gin.Context) {
+	id, ok := recordPathID(c)
+	if !ok {
+		return
+	}
+	uid, _ := getAuthenticatedUserID(c)
+	var input services.InternshipEvaluationInput
+	if err := c.ShouldBindJSON(&input); err != nil {
+		responses.Error(c, http.StatusBadRequest, "invalid evaluation payload")
+		return
+	}
+	evaluation, err := h.service.SubmitEvaluation(c.Request.Context(), uid, id, input)
+	if err != nil {
+		writeCollegeError(c, err)
+		return
+	}
+	responses.Success(c, http.StatusCreated, "evaluation submitted", evaluation)
+}
+
+func (h *InternshipRecordHandler) IssueCertificate(c *gin.Context) {
+	id, ok := recordPathID(c)
+	if !ok {
+		return
+	}
+	uid, _ := getAuthenticatedUserID(c)
+	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, services.MaxCertificateSize+(1<<20))
+	file, err := c.FormFile("file")
+	if c.Request.MultipartForm != nil {
+		defer c.Request.MultipartForm.RemoveAll()
+	}
+	if err != nil {
+		responses.Error(c, http.StatusBadRequest, "PDF file of at most 10 MiB is required")
+		return
+	}
+	certificate, err := h.service.IssueCertificate(c.Request.Context(), uid, id, file)
+	if err != nil {
+		writeCollegeError(c, err)
+		return
+	}
+	responses.Success(c, http.StatusCreated, "certificate issued", certificate)
+}
+
+func (h *InternshipRecordHandler) CertificateURL(c *gin.Context) {
+	id, ok := recordPathID(c)
+	if !ok {
+		return
+	}
+	uid, _ := getAuthenticatedUserID(c)
+	link, err := h.service.CertificateURL(c.Request.Context(), uid, id, c.Query("download") == "true")
+	if err != nil {
+		writeCollegeError(c, err)
+		return
+	}
+	c.Header("Cache-Control", "no-store")
+	responses.Success(c, http.StatusOK, "certificate access", gin.H{"url": link, "expires_in": 300})
+}
diff --git a/internal/handlers/internship_record_handler.go b/internal/handlers/internship_record_handler.go
deleted file mode 100644
index 960ae3e..0000000
--- a/internal/handlers/internship_record_handler.go
+++ /dev/null
@@ -1,149 +0,0 @@
-package handlers
-
-import (
-	"net/http"
-	"strconv"
-
-	"github.com/gin-gonic/gin"
-	"github.com/google/uuid"
-	"github.com/prasanna/student-job-portal/backend/internal/repositories"
-	"github.com/prasanna/student-job-portal/backend/internal/responses"
-	"github.com/prasanna/student-job-portal/backend/internal/services"
-)
-
-type InternshipRecordHandler struct {
-	service *services.InternshipRecordService
-}
-
-func NewInternshipRecordHandler(service *services.InternshipRecordService) *InternshipRecordHandler {
-	return &InternshipRecordHandler{service: service}
-}
-
-func (h *InternshipRecordHandler) List(c *gin.Context) {
-	uid, _ := getAuthenticatedUserID(c)
-	page, pageSize := parsePagination(c)
-	filter := repositories.InternshipRecordFilter{StudentName: c.Query("student_name"), Program: c.Query("program"), Semester: c.Query("semester"),
-		Company: c.Query("company"), Status: c.Query("status"), EvaluationStatus: c.Query("evaluation_status"), CertificateStatus: c.Query("certificate_status"), Page: page, PageSize: pageSize}
-	if year := c.Query("graduation_year"); year != "" {
-		var err error
-		filter.AcademicYear, err = strconv.Atoi(year)
-		if err != nil {
-			responses.Error(c, http.StatusBadRequest, "invalid graduation_year")
-			return
-		}
-	}
-	if value := c.Query("student_id"); value != "" {
-		id, err := uuid.Parse(value)
-		if err != nil {
-			responses.Error(c, http.StatusBadRequest, "invalid student_id")
-			return
-		}
-		filter.StudentID = &id
-	}
-	records, total, err := h.service.List(c.Request.Context(), uid, filter)
-	if err != nil {
-		writeCollegeError(c, err)
-		return
-	}
-	responses.SuccessWithPagination(c, http.StatusOK, "internship records", records, responses.CalculatePagination(int64(page), int64(pageSize), total))
-}
-
-func (h *InternshipRecordHandler) Dashboard(c *gin.Context) {
-	uid, _ := getAuthenticatedUserID(c)
-	metrics, err := h.service.Dashboard(c.Request.Context(), uid)
-	if err != nil {
-		writeCollegeError(c, err)
-		return
-	}
-	responses.Success(c, http.StatusOK, "college dashboard", metrics)
-}
-
-func (h *InternshipRecordHandler) Get(c *gin.Context) {
-	id, ok := recordPathID(c)
-	if !ok {
-		return
-	}
-	uid, _ := getAuthenticatedUserID(c)
-	record, err := h.service.Get(c.Request.Context(), uid, id)
-	if err != nil {
-		writeCollegeError(c, err)
-		return
-	}
-	responses.Success(c, http.StatusOK, "internship record", record)
-}
-
-func (h *InternshipRecordHandler) UpdateProgress(c *gin.Context) {
-	id, ok := recordPathID(c)
-	if !ok {
-		return
-	}
-	uid, _ := getAuthenticatedUserID(c)
-	var input services.InternshipProgressInput
-	if err := c.ShouldBindJSON(&input); err != nil {
-		responses.Error(c, http.StatusBadRequest, "invalid progress payload")
-		return
-	}
-	record, err := h.service.UpdateProgress(c.Request.Context(), uid, id, input)
-	if err != nil {
-		writeCollegeError(c, err)
-		return
-	}
-	responses.Success(c, http.StatusOK, "internship progress updated", record)
-}
-
-func (h *InternshipRecordHandler) SubmitEvaluation(c *gin.Context) {
-	id, ok := recordPathID(c)
-	if !ok {
-		return
-	}
-	uid, _ := getAuthenticatedUserID(c)
-	var input services.InternshipEvaluationInput
-	if err := c.ShouldBindJSON(&input); err != nil {
-		responses.Error(c, http.StatusBadRequest, "invalid evaluation payload")
-		return
-	}
-	evaluation, err := h.service.SubmitEvaluation(c.Request.Context(), uid, id, input)
-	if err != nil {
-		writeCollegeError(c, err)
-		return
-	}
-	responses.Success(c, http.StatusCreated, "evaluation submitted", evaluation)
-}
-
-func (h *InternshipRecordHandler) IssueCertificate(c *gin.Context) {
-	id, ok := recordPathID(c)
-	if !ok {
-		return
-	}
-	uid, _ := getAuthenticatedUserID(c)
-	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, services.MaxCertificateSize+(1<<20))
-	file, err := c.FormFile("file")
-	if c.Request.MultipartForm != nil {
-		defer c.Request.MultipartForm.RemoveAll()
-	}
-	if err != nil {
-		responses.Error(c, http.StatusBadRequest, "PDF file of at most 10 MiB is required")
-		return
-	}
-	certificate, err := h.service.IssueCertificate(c.Request.Context(), uid, id, file)
-	if err != nil {
-		writeCollegeError(c, err)
-		return
-	}
-	responses.Success(c, http.StatusCreated, "certificate issued", certificate)
-}
-
-func (h *InternshipRecordHandler) CertificateURL(c *gin.Context) {
-	id, ok := recordPathID(c)
-	if !ok {
-		return
-	}
-	uid, _ := getAuthenticatedUserID(c)
-	link, err := h.service.CertificateURL(c.Request.Context(), uid, id, c.Query("download") == "true")
-	if err != nil {
-		writeCollegeError(c, err)
-		return
-	}
-	c.Header("Cache-Control", "no-store")
-	responses.Success(c, http.StatusOK, "certificate access", gin.H{"url": link, "expires_in": 300})
-}

# internal/handlers/student_handler.go

diff --git a/internal/handlers/student_handler.go b/internal/handlers/student_handler.go
index ae5f614..1a722a9 100644
--- a/internal/handlers/student_handler.go
+++ b/internal/handlers/student_handler.go
@@ -85,7 +85,7 @@ func (h *StudentHandler) UpsertProfile(c *gin.Context) {
 	}
 	payload.UserID = userID
 	payload.UpdatedAt = time.Now()
-	if err := h.Svc.CreateOrUpdateProfile(&payload); err != nil {
+	if err := h.Svc.CreateOrUpdateProfile(c.Request.Context(), &payload); err != nil {
 		writeStudentError(c, err, "failed to save profile")
 		return
 	}
diff --git a/internal/handlers/student_handler.go b/internal/handlers/student_handler.go
index 1a722a9..b17b2e8 100644
--- a/internal/handlers/student_handler.go
+++ b/internal/handlers/student_handler.go
@@ -3,223 +3,139 @@ package handlers
 import (
 	"errors"
 	"net/http"
-	"time"
 
 	"github.com/gin-gonic/gin"
-	"github.com/google/uuid"
-
-	"github.com/prasanna/student-job-portal/backend/internal/config"
-	"github.com/prasanna/student-job-portal/backend/internal/logger"
 	"github.com/prasanna/student-job-portal/backend/internal/models"
-	"github.com/prasanna/student-job-portal/backend/internal/repositories"
 	"github.com/prasanna/student-job-portal/backend/internal/responses"
 	"github.com/prasanna/student-job-portal/backend/internal/services"
 )
 
-type StudentHandler struct {
-	Svc  *services.StudentService
-	Repo *repositories.StudentRepository
-	Cfg  *config.Config
-	Log  *logger.Logger
-}
+type StudentHandler struct{ Svc *services.StudentService }
 
-func NewStudentHandler(svc *services.StudentService, repo *repositories.StudentRepository, cfg *config.Config, l *logger.Logger) *StudentHandler {
-	return &StudentHandler{Svc: svc, Repo: repo, Cfg: cfg, Log: l}
+func NewStudentHandler(svc *services.StudentService) *StudentHandler {
+	return &StudentHandler{Svc: svc}
 }
 
-// GetProfile returns the current student's profile (or 404)
 func (h *StudentHandler) GetProfile(c *gin.Context) {
-	uid, ok := c.Get("user_id")
+	userID, ok := authenticatedUserID(c)
 	if !ok {
-		responses.Error(c, http.StatusUnauthorized, "missing user in context")
+		responses.Error(c, http.StatusUnauthorized, "invalid user context")
 		return
 	}
-	userIDStr, _ := uid.(string)
-	userID, err := uuid.Parse(userIDStr)
-	if err != nil {
-		responses.Error(c, http.StatusUnauthorized, "invalid user id")
-		return
-	}
-	p, err := h.Svc.GetProfile(userID)
+	profile, err := h.Svc.GetProfile(userID)
 	if err != nil {
 		writeStudentError(c, err, "failed to fetch profile")
 		return
 	}
-	responses.Success(c, http.StatusOK, "profile fetched", p)
-}
-
-// GetApplicationStats returns application metrics for the current student's dashboard.
-func (h *StudentHandler) GetApplicationStats(c *gin.Context) {
-	profileID, ok := getProfileID(c)
-	if !ok {
-		responses.Error(c, http.StatusUnauthorized, "invalid student profile context")
-		return
-	}
-
-	stats, err := h.Svc.GetApplicationStats(c.Request.Context(), profileID)
-	if err != nil {
-		writeStudentError(c, err, "failed to fetch application stats")
-		return
-	}
-
-	responses.Success(c, http.StatusOK, "student application stats fetched", stats)
+	responses.Success(c, http.StatusOK, "profile fetched", profile)
 }
 
-// UpsertProfile creates or updates the student's profile
 func (h *StudentHandler) UpsertProfile(c *gin.Context) {
-	uid, ok := c.Get("user_id")
+	userID, ok := authenticatedUserID(c)
 	if !ok {
-		responses.Error(c, http.StatusUnauthorized, "missing user in context")
+		responses.Error(c, http.StatusUnauthorized, "invalid user context")
 		return
 	}
-	userIDStr, _ := uid.(string)
-	userID, err := uuid.Parse(userIDStr)
-	if err != nil {
-		responses.Error(c, http.StatusUnauthorized, "invalid user id")
-		return
-	}
-	var payload models.StudentProfile
-	if err := c.ShouldBindJSON(&payload); err != nil {
-		responses.Error(c, http.StatusBadRequest, "invalid payload")
+	var profile models.StudentProfile
+	if err := c.ShouldBindJSON(&profile); err != nil {
+		responses.Error(c, http.StatusBadRequest, "invalid profile")
 		return
 	}
-	payload.UserID = userID
-	payload.UpdatedAt = time.Now()
-	if err := h.Svc.CreateOrUpdateProfile(c.Request.Context(), &payload); err != nil {
+	profile.UserID = userID
+	if err := h.Svc.CreateOrUpdateProfile(c.Request.Context(), &profile); err != nil {
 		writeStudentError(c, err, "failed to save profile")
 		return
 	}
-	responses.Success(c, http.StatusOK, "profile saved", payload)
+	responses.Success(c, http.StatusOK, "profile saved", profile)
 }
 
-// UploadDocument handles multipart file upload for student documents
-func (h *StudentHandler) UploadDocument(c *gin.Context) {
-	uid, ok := c.Get("user_id")
+func (h *StudentHandler) GetApplicationStats(c *gin.Context) {
+	profileID, ok := getProfileID(c)
 	if !ok {
-		responses.Error(c, http.StatusUnauthorized, "missing user in context")
+		responses.Error(c, http.StatusUnauthorized, "invalid student profile context")
 		return
 	}
-	userIDStr, _ := uid.(string)
-	userID, err := uuid.Parse(userIDStr)
+	stats, err := h.Svc.GetApplicationStats(c.Request.Context(), profileID)
 	if err != nil {
-		responses.Error(c, http.StatusUnauthorized, "invalid user id")
+		writeStudentError(c, err, "failed to fetch application stats")
 		return
 	}
-	// ensure profile exists
-	p, err := h.Svc.GetProfile(userID)
-	if err != nil {
-		writeStudentError(c, err, "failed to fetch profile")
+	responses.Success(c, http.StatusOK, "application stats", stats)
+}
+
+func (h *StudentHandler) UploadProfilePicture(c *gin.Context) {
+	userID, ok := authenticatedUserID(c)
+	if !ok {
+		responses.Error(c, http.StatusUnauthorized, "invalid user context")
 		return
 	}
-
+	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, (5<<20)+(1<<20))
 	file, err := c.FormFile("file")
 	if err != nil {
-		responses.Error(c, http.StatusBadRequest, "missing file")
+		responses.Error(c, http.StatusBadRequest, services.ErrInvalidProfilePicture.Error())
 		return
 	}
-	// Upload to minio via service then persist metadata
-	objectKey, size, mimeType, err := h.Svc.UploadDocument(userID, p.ID, file)
+	profile, err := h.Svc.UploadProfilePicture(c.Request.Context(), userID, file)
 	if err != nil {
-		writeStudentError(c, err, "failed to upload document")
-		return
-	}
-	doc := &models.StudentDocument{
-		ProfileID: p.ID,
-		UserID:    userID,
-		ObjectKey: objectKey,
-		FileName:  file.Filename,
-		MimeType:  mimeType,
-		Size:      size,
-		IsDefault: false,
-	}
-	if err := h.Svc.AddDocument(doc); err != nil {
-		writeStudentError(c, err, "failed to save document metadata")
+		writeStudentError(c, err, "failed to upload profile picture")
 		return
 	}
-	responses.Success(c, http.StatusCreated, "uploaded", doc)
+	responses.Success(c, http.StatusOK, "profile picture saved", profile)
 }
 
-// ListDocuments returns documents for the student's profile
-func (h *StudentHandler) ListDocuments(c *gin.Context) {
-	uid, ok := c.Get("user_id")
+func (h *StudentHandler) GetResume(c *gin.Context) {
+	userID, ok := authenticatedUserID(c)
 	if !ok {
-		responses.Error(c, http.StatusUnauthorized, "missing user in context")
+		responses.Error(c, http.StatusUnauthorized, "invalid user context")
 		return
 	}
-	userIDStr, _ := uid.(string)
-	userID, err := uuid.Parse(userIDStr)
+	resume, err := h.Svc.GetResume(c.Request.Context(), userID)
 	if err != nil {
-		responses.Error(c, http.StatusUnauthorized, "invalid user id")
+		writeStudentError(c, err, "failed to fetch CV")
 		return
 	}
-	docs, err := h.Svc.ListDocuments(userID)
-	if err != nil {
-		writeStudentError(c, err, "failed to list documents")
-		return
-	}
-	responses.Success(c, http.StatusOK, "documents", docs)
+	responses.Success(c, http.StatusOK, "CV", resume)
 }
 
-// SetDefaultDocument sets a document as default
-func (h *StudentHandler) SetDefaultDocument(c *gin.Context) {
-	docIDStr := c.Param("id")
-	docID, err := uuid.Parse(docIDStr)
-	if err != nil {
-		responses.Error(c, http.StatusBadRequest, "invalid document id")
-		return
-	}
-	uid, ok := c.Get("user_id")
+func (h *StudentHandler) UploadResume(c *gin.Context) {
+	userID, ok := authenticatedUserID(c)
 	if !ok {
-		responses.Error(c, http.StatusUnauthorized, "missing user in context")
+		responses.Error(c, http.StatusUnauthorized, "invalid user context")
 		return
 	}
-	userIDStr, _ := uid.(string)
-	userID, err := uuid.Parse(userIDStr)
+	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, (10<<20)+(1<<20))
+	file, err := c.FormFile("file")
 	if err != nil {
-		responses.Error(c, http.StatusUnauthorized, "invalid user id")
+		responses.Error(c, http.StatusBadRequest, "upload a PDF CV of at most 10 MiB")
 		return
 	}
-	if err := h.Svc.SetDefaultDocument(userID, docID); err != nil {
-		writeStudentError(c, err, "failed to set default document")
+	resume, err := h.Svc.UploadResume(c.Request.Context(), userID, file)
+	if err != nil {
+		writeStudentError(c, err, "failed to save CV")
 		return
 	}
-	responses.Success(c, http.StatusOK, "default set", nil)
+	responses.Success(c, http.StatusOK, "CV saved", resume)
 }
 
-// DeleteDocument deletes a student's document
-func (h *StudentHandler) DeleteDocument(c *gin.Context) {
-	docIDStr := c.Param("id")
-	docID, err := uuid.Parse(docIDStr)
-	if err != nil {
-		responses.Error(c, http.StatusBadRequest, "invalid document id")
-		return
-	}
-	uid, ok := c.Get("user_id")
+func (h *StudentHandler) DeleteResume(c *gin.Context) {
+	userID, ok := authenticatedUserID(c)
 	if !ok {
-		responses.Error(c, http.StatusUnauthorized, "missing user in context")
-		return
-	}
-	userIDStr, _ := uid.(string)
-	userID, err := uuid.Parse(userIDStr)
-	if err != nil {
-		responses.Error(c, http.StatusUnauthorized, "invalid user id")
+		responses.Error(c, http.StatusUnauthorized, "invalid user context")
 		return
 	}
-	if err := h.Svc.DeleteDocumentByUserID(userID, docID); err != nil {
-		writeStudentError(c, err, "failed to delete document")
+	if err := h.Svc.DeleteResume(c.Request.Context(), userID); err != nil {
+		writeStudentError(c, err, "failed to delete CV")
 		return
 	}
-	responses.Success(c, http.StatusOK, "deleted", nil)
+	responses.Success(c, http.StatusOK, "CV deleted", nil)
 }
 
 func writeStudentError(c *gin.Context, err error, fallback string) {
 	switch {
-	case errors.Is(err, services.ErrInvalidStudentData):
+	case errors.Is(err, services.ErrInvalidStudentData), errors.Is(err, services.ErrInvalidProfilePicture):
 		responses.Error(c, http.StatusBadRequest, err.Error())
 	case errors.Is(err, services.ErrStudentProfileNotFound):
 		responses.Error(c, http.StatusNotFound, "profile not found")
-	case errors.Is(err, services.ErrStudentDocumentNotFound):
-		responses.Error(c, http.StatusNotFound, "document not found")
 	default:
 		responses.Error(c, http.StatusInternalServerError, fallback)
 	}

# internal/middleware/jwt_middleware.go

diff --git a/internal/middleware/jwt_middleware.go b/internal/middleware/jwt_middleware.go
index 91ee844..ac02a63 100644
--- a/internal/middleware/jwt_middleware.go
+++ b/internal/middleware/jwt_middleware.go
@@ -31,7 +31,6 @@ func JWTAuthMiddleware(cfg *config.Config, l *logger.Logger) gin.HandlerFunc {
 				}
 			}
 		}
-		log.Println("JWTAuthMiddleware: tokenStr:", tokenStr)
 
 		// 3. Reject if no token was found in either cookie or header
 		if tokenStr == "" {

# internal/models/college.go

diff --git a/internal/models/college.go b/internal/models/college.go
new file mode 100644
index 0000000..8818ce8
--- /dev/null
+++ b/internal/models/college.go
@@ -0,0 +1,58 @@
+package models
+
+import (
+	"time"
+
+	"github.com/google/uuid"
+	"gorm.io/gorm"
+)
+
+type University string
+
+const (
+	TribhuvanUniversity  University = "TRIBHUVAN_UNIVERSITY"
+	PurbanchalUniversity University = "PURBANCHAL_UNIVERSITY"
+	PokharaUniversity    University = "POKHARA_UNIVERSITY"
+	KathmanduUniversity  University = "KATHMANDU_UNIVERSITY"
+)
+
+type UniversityOption struct {
+	Value University `json:"value"`
+	Label string     `json:"label"`
+}
+
+func Universities() []UniversityOption {
+	return []UniversityOption{
+		{TribhuvanUniversity, "Tribhuvan University (TU)"},
+		{PurbanchalUniversity, "Purbanchal University"},
+		{PokharaUniversity, "Pokhara University"},
+		{KathmanduUniversity, "Kathmandu University (KU)"},
+	}
+}
+
+func (u University) IsValid() bool {
+	switch u {
+	case TribhuvanUniversity, PurbanchalUniversity, PokharaUniversity, KathmanduUniversity:
+		return true
+	}
+	return false
+}
+
+type College struct {
+	ID           uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
+	Name         string     `gorm:"size:255;not null" json:"name"`
+	University   University `gorm:"type:text;not null;check:university IN ('TRIBHUVAN_UNIVERSITY','PURBANCHAL_UNIVERSITY','POKHARA_UNIVERSITY','KATHMANDU_UNIVERSITY')" json:"university"`
+	Address      string     `gorm:"size:500" json:"address"`
+	ContactEmail string     `gorm:"size:255" json:"contact_email"`
+	ContactPhone string     `gorm:"size:50" json:"contact_phone"`
+	Status       string     `gorm:"size:20;not null;default:active;check:status IN ('active','inactive')" json:"status"`
+	CreatedAt    time.Time  `json:"created_at"`
+	UpdatedAt    time.Time  `json:"updated_at"`
+}
+
+func (c *College) BeforeCreate(tx *gorm.DB) error {
+	if c.ID == uuid.Nil {
+		c.ID = uuid.New()
+	}
+	return nil
+}
diff --git a/internal/models/college.go b/internal/models/college.go
deleted file mode 100644
index 8818ce8..0000000
--- a/internal/models/college.go
+++ /dev/null
@@ -1,58 +0,0 @@
-package models
-
-import (
-	"time"
-
-	"github.com/google/uuid"
-	"gorm.io/gorm"
-)
-
-type University string
-
-const (
-	TribhuvanUniversity  University = "TRIBHUVAN_UNIVERSITY"
-	PurbanchalUniversity University = "PURBANCHAL_UNIVERSITY"
-	PokharaUniversity    University = "POKHARA_UNIVERSITY"
-	KathmanduUniversity  University = "KATHMANDU_UNIVERSITY"
-)
-
-type UniversityOption struct {
-	Value University `json:"value"`
-	Label string     `json:"label"`
-}
-
-func Universities() []UniversityOption {
-	return []UniversityOption{
-		{TribhuvanUniversity, "Tribhuvan University (TU)"},
-		{PurbanchalUniversity, "Purbanchal University"},
-		{PokharaUniversity, "Pokhara University"},
-		{KathmanduUniversity, "Kathmandu University (KU)"},
-	}
-}
-
-func (u University) IsValid() bool {
-	switch u {
-	case TribhuvanUniversity, PurbanchalUniversity, PokharaUniversity, KathmanduUniversity:
-		return true
-	}
-	return false
-}
-
-type College struct {
-	ID           uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
-	Name         string     `gorm:"size:255;not null" json:"name"`
-	University   University `gorm:"type:text;not null;check:university IN ('TRIBHUVAN_UNIVERSITY','PURBANCHAL_UNIVERSITY','POKHARA_UNIVERSITY','KATHMANDU_UNIVERSITY')" json:"university"`
-	Address      string     `gorm:"size:500" json:"address"`
-	ContactEmail string     `gorm:"size:255" json:"contact_email"`
-	ContactPhone string     `gorm:"size:50" json:"contact_phone"`
-	Status       string     `gorm:"size:20;not null;default:active;check:status IN ('active','inactive')" json:"status"`
-	CreatedAt    time.Time  `json:"created_at"`
-	UpdatedAt    time.Time  `json:"updated_at"`
-}
-
-func (c *College) BeforeCreate(tx *gorm.DB) error {
-	if c.ID == uuid.Nil {
-		c.ID = uuid.New()
-	}
-	return nil
-}

# internal/models/internship_application.go

diff --git a/internal/models/internship_application.go b/internal/models/internship_application.go
index fc6905c..bb4bd18 100644
--- a/internal/models/internship_application.go
+++ b/internal/models/internship_application.go
@@ -26,7 +26,12 @@ type InternshipApplication struct {
 
 	Status InternshipApplicationStatus `gorm:"size:30;not null;default:submitted;index" json:"status"`
 
-	EmployerNote string `gorm:"type:text" json:"employer_note,omitempty"`
+	EmployerNote   string                 `gorm:"type:text" json:"employer_note,omitempty"`
+	ProgressStatus string                 `gorm:"size:20;not null;default:applied;index;check:progress_status IN ('applied','ongoing','completed','cancelled')" json:"progress_status"`
+	StartDate      *time.Time             `json:"start_date,omitempty"`
+	EndDate        *time.Time             `json:"end_date,omitempty"`
+	Evaluation     *InternshipEvaluation  `gorm:"foreignKey:ApplicationID" json:"evaluation,omitempty"`
+	Certificate    *InternshipCertificate `gorm:"foreignKey:ApplicationID" json:"certificate,omitempty"`
 
 	AppliedAt     time.Time  `gorm:"not null" json:"applied_at"`
 	ReviewedAt    *time.Time `json:"reviewed_at,omitempty"`
diff --git a/internal/models/internship_application.go b/internal/models/internship_application.go
index bb4bd18..c95b971 100644
--- a/internal/models/internship_application.go
+++ b/internal/models/internship_application.go
@@ -10,12 +10,10 @@ import (
 type InternshipApplicationStatus string
 
 const (
-	ApplicationStatusSubmitted   InternshipApplicationStatus = "submitted"
-	ApplicationStatusReviewing   InternshipApplicationStatus = "reviewing"
-	ApplicationStatusShortlisted InternshipApplicationStatus = "shortlisted"
-	ApplicationStatusRejected    InternshipApplicationStatus = "rejected"
-	ApplicationStatusAccepted    InternshipApplicationStatus = "accepted"
-	ApplicationStatusWithdrawn   InternshipApplicationStatus = "withdrawn"
+	ApplicationStatusSubmitted InternshipApplicationStatus = "submitted"
+	ApplicationStatusRejected  InternshipApplicationStatus = "rejected"
+	ApplicationStatusAccepted  InternshipApplicationStatus = "accepted"
+	ApplicationStatusWithdrawn InternshipApplicationStatus = "withdrawn"
 )
 
 type InternshipApplication struct {
@@ -26,19 +24,9 @@ type InternshipApplication struct {
 
 	Status InternshipApplicationStatus `gorm:"size:30;not null;default:submitted;index" json:"status"`
 
-	EmployerNote   string                 `gorm:"type:text" json:"employer_note,omitempty"`
-	ProgressStatus string                 `gorm:"size:20;not null;default:applied;index;check:progress_status IN ('applied','ongoing','completed','cancelled')" json:"progress_status"`
-	StartDate      *time.Time             `json:"start_date,omitempty"`
-	EndDate        *time.Time             `json:"end_date,omitempty"`
-	Evaluation     *InternshipEvaluation  `gorm:"foreignKey:ApplicationID" json:"evaluation,omitempty"`
-	Certificate    *InternshipCertificate `gorm:"foreignKey:ApplicationID" json:"certificate,omitempty"`
-
-	AppliedAt     time.Time  `gorm:"not null" json:"applied_at"`
-	ReviewedAt    *time.Time `json:"reviewed_at,omitempty"`
-	ShortlistedAt *time.Time `json:"shortlisted_at,omitempty"`
-	AcceptedAt    *time.Time `json:"accepted_at,omitempty"`
-	RejectedAt    *time.Time `json:"rejected_at,omitempty"`
-	WithdrawnAt   *time.Time `json:"withdrawn_at,omitempty"`
+	EmployerNote string `gorm:"type:text" json:"employer_note,omitempty"`
+
+	AppliedAt time.Time `gorm:"not null" json:"applied_at"`
 
 	Internship *Internship     `gorm:"foreignKey:InternshipID;references:ID" json:"internship,omitempty"`
 	Student    *StudentProfile `gorm:"foreignKey:StudentID;references:ID" json:"student,omitempty"`

# internal/models/internship_record.go

diff --git a/internal/models/internship_record.go b/internal/models/internship_record.go
new file mode 100644
index 0000000..ea8e525
--- /dev/null
+++ b/internal/models/internship_record.go
@@ -0,0 +1,44 @@
+package models
+
+import (
+	"time"
+
+	"github.com/google/uuid"
+	"gorm.io/gorm"
+)
+
+// Outcomes belong to an application (one student at one internship).
+type InternshipEvaluation struct {
+	ID            uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
+	ApplicationID uuid.UUID      `gorm:"type:uuid;not null;uniqueIndex" json:"application_id"`
+	SubmittedBy   uuid.UUID      `gorm:"type:uuid;not null" json:"submitted_by"`
+	Scores        map[string]int `gorm:"serializer:json;type:text;not null" json:"scores"`
+	Remarks       string         `gorm:"type:text;not null" json:"remarks"`
+	Status        string         `gorm:"size:20;not null;default:submitted;check:status = 'submitted'" json:"status"`
+	SubmittedAt   time.Time      `gorm:"not null" json:"submitted_at"`
+}
+
+func (e *InternshipEvaluation) BeforeCreate(tx *gorm.DB) error {
+	if e.ID == uuid.Nil {
+		e.ID = uuid.New()
+	}
+	return nil
+}
+
+type InternshipCertificate struct {
+	ID            uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
+	ApplicationID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex" json:"application_id"`
+	IssuedBy      uuid.UUID `gorm:"type:uuid;not null" json:"issued_by"`
+	ObjectKey     string    `gorm:"size:512;not null" json:"-"`
+	FileName      string    `gorm:"size:255;not null" json:"file_name"`
+	Size          int64     `gorm:"not null" json:"size"`
+	Status        string    `gorm:"size:20;not null;default:issued;check:status = 'issued'" json:"status"`
+	IssuedAt      time.Time `gorm:"not null" json:"issued_at"`
+}
+
+func (c *InternshipCertificate) BeforeCreate(tx *gorm.DB) error {
+	if c.ID == uuid.Nil {
+		c.ID = uuid.New()
+	}
+	return nil
+}
diff --git a/internal/models/internship_record.go b/internal/models/internship_record.go
deleted file mode 100644
index ea8e525..0000000
--- a/internal/models/internship_record.go
+++ /dev/null
@@ -1,44 +0,0 @@
-package models
-
-import (
-	"time"
-
-	"github.com/google/uuid"
-	"gorm.io/gorm"
-)
-
-// Outcomes belong to an application (one student at one internship).
-type InternshipEvaluation struct {
-	ID            uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
-	ApplicationID uuid.UUID      `gorm:"type:uuid;not null;uniqueIndex" json:"application_id"`
-	SubmittedBy   uuid.UUID      `gorm:"type:uuid;not null" json:"submitted_by"`
-	Scores        map[string]int `gorm:"serializer:json;type:text;not null" json:"scores"`
-	Remarks       string         `gorm:"type:text;not null" json:"remarks"`
-	Status        string         `gorm:"size:20;not null;default:submitted;check:status = 'submitted'" json:"status"`
-	SubmittedAt   time.Time      `gorm:"not null" json:"submitted_at"`
-}
-
-func (e *InternshipEvaluation) BeforeCreate(tx *gorm.DB) error {
-	if e.ID == uuid.Nil {
-		e.ID = uuid.New()
-	}
-	return nil
-}
-
-type InternshipCertificate struct {
-	ID            uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
-	ApplicationID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex" json:"application_id"`
-	IssuedBy      uuid.UUID `gorm:"type:uuid;not null" json:"issued_by"`
-	ObjectKey     string    `gorm:"size:512;not null" json:"-"`
-	FileName      string    `gorm:"size:255;not null" json:"file_name"`
-	Size          int64     `gorm:"not null" json:"size"`
-	Status        string    `gorm:"size:20;not null;default:issued;check:status = 'issued'" json:"status"`
-	IssuedAt      time.Time `gorm:"not null" json:"issued_at"`
-}
-
-func (c *InternshipCertificate) BeforeCreate(tx *gorm.DB) error {
-	if c.ID == uuid.Nil {
-		c.ID = uuid.New()
-	}
-	return nil
-}

# internal/models/student_profile.go

diff --git a/internal/models/student_profile.go b/internal/models/student_profile.go
index ff2779a..68c35e7 100644
--- a/internal/models/student_profile.go
+++ b/internal/models/student_profile.go
@@ -9,13 +9,16 @@ import (
 
 // StudentProfile holds extended profile information for student users
 type StudentProfile struct {
-	ID                       uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
-	UserID                   uuid.UUID      `gorm:"type:uuid;uniqueIndex;not null"`
-	FullName                 string         `gorm:"size:255" json:"full_name"`
-	Phone                    string         `gorm:"size:50" json:"phone"`
-	Location                 string         `gorm:"size:255" json:"location"`
-	Bio                      string         `gorm:"type:text" json:"bio"`
-	CollegeName              string         `gorm:"size:255" json:"college_name"`
+	ID          uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
+	UserID      uuid.UUID `gorm:"type:uuid;uniqueIndex;not null"`
+	FullName    string    `gorm:"size:255" json:"full_name"`
+	Phone       string    `gorm:"size:50" json:"phone"`
+	Location    string    `gorm:"size:255" json:"location"`
+	Bio         string    `gorm:"type:text" json:"bio"`
+	CollegeName string    `gorm:"size:255" json:"college_name"`
+	// Nullable for legacy profiles; new profiles require a college in the service.
+	CollegeID                *uuid.UUID     `gorm:"type:uuid;index" json:"college_id"`
+	College                  *College       `gorm:"foreignKey:CollegeID;constraint:OnDelete:RESTRICT" json:"college,omitempty"`
 	Degree                   string         `gorm:"size:255" json:"degree"`
 	FacultyOrMajor           string         `gorm:"size:255" json:"faculty_or_major"`
 	CurrentSemester          string         `gorm:"size:64" json:"current_semester"`
diff --git a/internal/models/student_profile.go b/internal/models/student_profile.go
index 68c35e7..0e7d8da 100644
--- a/internal/models/student_profile.go
+++ b/internal/models/student_profile.go
@@ -9,36 +9,25 @@ import (
 
 // StudentProfile holds extended profile information for student users
 type StudentProfile struct {
-	ID          uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
-	UserID      uuid.UUID `gorm:"type:uuid;uniqueIndex;not null"`
-	FullName    string    `gorm:"size:255" json:"full_name"`
-	Phone       string    `gorm:"size:50" json:"phone"`
-	Location    string    `gorm:"size:255" json:"location"`
-	Bio         string    `gorm:"type:text" json:"bio"`
-	CollegeName string    `gorm:"size:255" json:"college_name"`
-	// Nullable for legacy profiles; new profiles require a college in the service.
-	CollegeID                *uuid.UUID     `gorm:"type:uuid;index" json:"college_id"`
-	College                  *College       `gorm:"foreignKey:CollegeID;constraint:OnDelete:RESTRICT" json:"college,omitempty"`
-	Degree                   string         `gorm:"size:255" json:"degree"`
-	FacultyOrMajor           string         `gorm:"size:255" json:"faculty_or_major"`
-	CurrentSemester          string         `gorm:"size:64" json:"current_semester"`
-	GraduationYear           int            `json:"graduation_year"`
-	PreferredJobCategories   string         `gorm:"type:text" json:"preferred_job_categories"`
-	PreferredLocations       string         `gorm:"type:text" json:"preferred_locations"`
-	PreferredWorkMode        string         `gorm:"size:64" json:"preferred_work_mode"`
-	Availability             string         `gorm:"size:64" json:"availability"`
-	ExpectedSalary           string         `gorm:"size:64" json:"expected_salary"`
-	LinkedinURL              string         `gorm:"size:255" json:"linkedin_url"`
-	GithubURL                string         `gorm:"size:255" json:"github_url"`
-	PortfolioURL             string         `gorm:"size:255" json:"portfolio_url"`
-	ProfileImageKey          string         `gorm:"size:512" json:"profile_image_key"`
-	IsSearchable             bool           `gorm:"default:true" json:"is_searchable"`
-	ProfileCompletionPercent int            `gorm:"default:0" json:"profile_completion_percentage"`
-	CreatedAt                time.Time      `json:"created_at"`
-	UpdatedAt                time.Time      `json:"updated_at"`
-	DeletedAt                gorm.DeletedAt `gorm:"index" json:"-"`
-
-	Documents []StudentDocument `gorm:"foreignKey:ProfileID;references:ID" json:"documents,omitempty"`
+	ID                uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
+	UserID            uuid.UUID      `gorm:"type:uuid;uniqueIndex;not null" json:"user_id"`
+	ProfilePictureKey string         `gorm:"type:text" json:"-"`
+	ProfilePicture    string         `gorm:"-" json:"profile_picture,omitempty"`
+	FullName          string         `gorm:"size:255" json:"full_name"`
+	Phone             string         `gorm:"size:50" json:"phone"`
+	Location          string         `gorm:"size:255" json:"location"`
+	Bio               string         `gorm:"type:text" json:"bio"`
+	CollegeName       string         `gorm:"size:255" json:"college_name"`
+	Degree            string         `gorm:"size:255" json:"degree"`
+	FacultyOrMajor    string         `gorm:"size:255" json:"faculty_or_major"`
+	CurrentSemester   string         `gorm:"size:64" json:"current_semester"`
+	GraduationYear    int            `json:"graduation_year"`
+	LinkedinURL       string         `gorm:"size:255" json:"linkedin_url"`
+	GithubURL         string         `gorm:"size:255" json:"github_url"`
+	PortfolioURL      string         `gorm:"size:255" json:"portfolio_url"`
+	CreatedAt         time.Time      `json:"created_at"`
+	UpdatedAt         time.Time      `json:"updated_at"`
+	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`
 }
 
 func (s *StudentProfile) BeforeCreate(tx *gorm.DB) (err error) {

# internal/models/user.go

diff --git a/internal/models/user.go b/internal/models/user.go
index cc11022..d019450 100644
--- a/internal/models/user.go
+++ b/internal/models/user.go
@@ -13,6 +13,7 @@ const (
 	RoleStudent  Role = "student"
 	RoleEmployer Role = "employer"
 	RoleAdmin    Role = "admin"
+	RoleCollege  Role = "college"
 )
 
 type User struct {
@@ -21,6 +22,8 @@ type User struct {
 	PasswordHash string         `gorm:"not null" json:"-"`
 	FullName     string         `gorm:"size:255" json:"full_name"`
 	Role         Role           `gorm:"type:text;not null;default:'student'" json:"role"`
+	CollegeID    *uuid.UUID     `gorm:"type:uuid;index;check:role <> 'college' OR college_id IS NOT NULL" json:"college_id,omitempty"`
+	College      *College       `gorm:"foreignKey:CollegeID;constraint:OnDelete:RESTRICT" json:"college,omitempty"`
 	CreatedAt    time.Time      `json:"created_at"`
 	UpdatedAt    time.Time      `json:"updated_at"`
 	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
diff --git a/internal/models/user.go b/internal/models/user.go
index d019450..907e2c8 100644
--- a/internal/models/user.go
+++ b/internal/models/user.go
@@ -12,21 +12,22 @@ type Role string
 const (
 	RoleStudent  Role = "student"
 	RoleEmployer Role = "employer"
-	RoleAdmin    Role = "admin"
-	RoleCollege  Role = "college"
 )
 
 type User struct {
-	ID           uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
-	Email        string         `gorm:"uniqueIndex;not null" json:"email"`
-	PasswordHash string         `gorm:"not null" json:"-"`
-	FullName     string         `gorm:"size:255" json:"full_name"`
-	Role         Role           `gorm:"type:text;not null;default:'student'" json:"role"`
-	CollegeID    *uuid.UUID     `gorm:"type:uuid;index;check:role <> 'college' OR college_id IS NOT NULL" json:"college_id,omitempty"`
-	College      *College       `gorm:"foreignKey:CollegeID;constraint:OnDelete:RESTRICT" json:"college,omitempty"`
-	CreatedAt    time.Time      `json:"created_at"`
-	UpdatedAt    time.Time      `json:"updated_at"`
-	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
+	ID              uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
+	Email           string         `gorm:"uniqueIndex;not null" json:"email"`
+	PasswordHash    string         `gorm:"not null" json:"-"`
+	FullName        string         `gorm:"size:255" json:"full_name"`
+	Role            Role           `gorm:"type:text;not null;default:'student'" json:"role"`
+	EmailVerifiedAt *time.Time     `json:"email_verified_at"`
+	CreatedAt       time.Time      `json:"created_at"`
+	UpdatedAt       time.Time      `json:"updated_at"`
+	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
+}
+
+func (u *User) CanSignIn() bool {
+	return (u.Role == RoleStudent || u.Role == RoleEmployer) && u.EmailVerifiedAt != nil
 }
 
 // BeforeCreate sets UUID if not provided (for sqlite/local dev)

# internal/repositories/college_repository.go

diff --git a/internal/repositories/college_repository.go b/internal/repositories/college_repository.go
new file mode 100644
index 0000000..67e384b
--- /dev/null
+++ b/internal/repositories/college_repository.go
@@ -0,0 +1,91 @@
+package repositories
+
+import (
+	"context"
+	"strings"
+
+	"github.com/google/uuid"
+	"github.com/prasanna/student-job-portal/backend/internal/models"
+	"gorm.io/gorm"
+	"gorm.io/gorm/clause"
+)
+
+type CollegeRepository struct{ db *gorm.DB }
+
+func NewCollegeRepository(db *gorm.DB) *CollegeRepository { return &CollegeRepository{db: db} }
+
+func (r *CollegeRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.College, error) {
+	var college models.College
+	err := r.db.WithContext(ctx).First(&college, "id = ?", id).Error
+	return &college, err
+}
+
+func (r *CollegeRepository) Create(ctx context.Context, college *models.College) error {
+	return r.db.WithContext(ctx).Create(college).Error
+}
+
+func (r *CollegeRepository) Update(ctx context.Context, college *models.College) error {
+	return r.db.WithContext(ctx).Model(college).Select("name", "university", "address", "contact_email", "contact_phone", "status").Updates(college).Error
+}
+
+func (r *CollegeRepository) Delete(ctx context.Context, id uuid.UUID) error {
+	return r.db.WithContext(ctx).Delete(&models.College{}, "id = ?", id).Error
+}
+
+func (r *CollegeRepository) HasMembers(ctx context.Context, id uuid.UUID) (bool, error) {
+	var count int64
+	for _, model := range []interface{}{&models.User{}, &models.StudentProfile{}} {
+		if err := r.db.WithContext(ctx).Unscoped().Model(model).Where("college_id = ?", id).Count(&count).Error; err != nil {
+			return false, err
+		}
+		if count > 0 {
+			return true, nil
+		}
+	}
+	return false, nil
+}
+
+func (r *CollegeRepository) List(ctx context.Context, query string, activeOnly bool, page, pageSize int) ([]models.College, int64, error) {
+	out := []models.College{}
+	var total int64
+	q := r.db.WithContext(ctx).Model(&models.College{})
+	if activeOnly {
+		q = q.Where("status = ?", "active")
+	}
+	if query = strings.TrimSpace(query); query != "" {
+		q = q.Where("LOWER(name) LIKE ?", "%"+strings.ToLower(query)+"%")
+	}
+	if err := q.Count(&total).Error; err != nil {
+		return nil, 0, err
+	}
+	offset, limit := getPagination(page, pageSize)
+	err := q.Order("name ASC, id ASC").Offset(offset).Limit(limit).Find(&out).Error
+	return out, total, err
+}
+
+func (r *CollegeRepository) ListUsers(ctx context.Context, id uuid.UUID, page, pageSize int) ([]models.User, int64, error) {
+	out := []models.User{}
+	var total int64
+	q := r.db.WithContext(ctx).Model(&models.User{}).Where("college_id = ? AND role = ?", id, models.RoleCollege)
+	if err := q.Count(&total).Error; err != nil {
+		return nil, 0, err
+	}
+	offset, limit := getPagination(page, pageSize)
+	err := q.Select("id", "email", "full_name", "role", "college_id", "created_at").Order("created_at DESC, id ASC").Offset(offset).Limit(limit).Find(&out).Error
+	return out, total, err
+}
+
+func (r *CollegeRepository) AssignUser(ctx context.Context, userID, collegeID uuid.UUID) error {
+	return r.db.WithContext(ctx).Model(&models.User{}).Where("id = ? AND role = ?", userID, models.RoleCollege).Update("college_id", collegeID).Error
+}
+
+func (r *CollegeRepository) AssignStudent(ctx context.Context, studentID uuid.UUID, college *models.College) error {
+	return r.db.WithContext(ctx).Model(&models.StudentProfile{}).Where("id = ?", studentID).
+		Updates(map[string]interface{}{"college_id": college.ID, "college_name": college.Name}).Error
+}
+
+func (r *CollegeRepository) CreateUser(ctx context.Context, user *models.User) (bool, error) {
+	result := r.db.WithContext(ctx).Omit(clause.Associations).
+		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "email"}}, DoNothing: true}).Create(user)
+	return result.RowsAffected == 1, result.Error
+}
diff --git a/internal/repositories/college_repository.go b/internal/repositories/college_repository.go
deleted file mode 100644
index 67e384b..0000000
--- a/internal/repositories/college_repository.go
+++ /dev/null
@@ -1,91 +0,0 @@
-package repositories
-
-import (
-	"context"
-	"strings"
-
-	"github.com/google/uuid"
-	"github.com/prasanna/student-job-portal/backend/internal/models"
-	"gorm.io/gorm"
-	"gorm.io/gorm/clause"
-)
-
-type CollegeRepository struct{ db *gorm.DB }
-
-func NewCollegeRepository(db *gorm.DB) *CollegeRepository { return &CollegeRepository{db: db} }
-
-func (r *CollegeRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.College, error) {
-	var college models.College
-	err := r.db.WithContext(ctx).First(&college, "id = ?", id).Error
-	return &college, err
-}
-
-func (r *CollegeRepository) Create(ctx context.Context, college *models.College) error {
-	return r.db.WithContext(ctx).Create(college).Error
-}
-
-func (r *CollegeRepository) Update(ctx context.Context, college *models.College) error {
-	return r.db.WithContext(ctx).Model(college).Select("name", "university", "address", "contact_email", "contact_phone", "status").Updates(college).Error
-}
-
-func (r *CollegeRepository) Delete(ctx context.Context, id uuid.UUID) error {
-	return r.db.WithContext(ctx).Delete(&models.College{}, "id = ?", id).Error
-}
-
-func (r *CollegeRepository) HasMembers(ctx context.Context, id uuid.UUID) (bool, error) {
-	var count int64
-	for _, model := range []interface{}{&models.User{}, &models.StudentProfile{}} {
-		if err := r.db.WithContext(ctx).Unscoped().Model(model).Where("college_id = ?", id).Count(&count).Error; err != nil {
-			return false, err
-		}
-		if count > 0 {
-			return true, nil
-		}
-	}
-	return false, nil
-}
-
-func (r *CollegeRepository) List(ctx context.Context, query string, activeOnly bool, page, pageSize int) ([]models.College, int64, error) {
-	out := []models.College{}
-	var total int64
-	q := r.db.WithContext(ctx).Model(&models.College{})
-	if activeOnly {
-		q = q.Where("status = ?", "active")
-	}
-	if query = strings.TrimSpace(query); query != "" {
-		q = q.Where("LOWER(name) LIKE ?", "%"+strings.ToLower(query)+"%")
-	}
-	if err := q.Count(&total).Error; err != nil {
-		return nil, 0, err
-	}
-	offset, limit := getPagination(page, pageSize)
-	err := q.Order("name ASC, id ASC").Offset(offset).Limit(limit).Find(&out).Error
-	return out, total, err
-}
-
-func (r *CollegeRepository) ListUsers(ctx context.Context, id uuid.UUID, page, pageSize int) ([]models.User, int64, error) {
-	out := []models.User{}
-	var total int64
-	q := r.db.WithContext(ctx).Model(&models.User{}).Where("college_id = ? AND role = ?", id, models.RoleCollege)
-	if err := q.Count(&total).Error; err != nil {
-		return nil, 0, err
-	}
-	offset, limit := getPagination(page, pageSize)
-	err := q.Select("id", "email", "full_name", "role", "college_id", "created_at").Order("created_at DESC, id ASC").Offset(offset).Limit(limit).Find(&out).Error
-	return out, total, err
-}
-
-func (r *CollegeRepository) AssignUser(ctx context.Context, userID, collegeID uuid.UUID) error {
-	return r.db.WithContext(ctx).Model(&models.User{}).Where("id = ? AND role = ?", userID, models.RoleCollege).Update("college_id", collegeID).Error
-}
-
-func (r *CollegeRepository) AssignStudent(ctx context.Context, studentID uuid.UUID, college *models.College) error {
-	return r.db.WithContext(ctx).Model(&models.StudentProfile{}).Where("id = ?", studentID).
-		Updates(map[string]interface{}{"college_id": college.ID, "college_name": college.Name}).Error
-}
-
-func (r *CollegeRepository) CreateUser(ctx context.Context, user *models.User) (bool, error) {
-	result := r.db.WithContext(ctx).Omit(clause.Associations).
-		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "email"}}, DoNothing: true}).Create(user)
-	return result.RowsAffected == 1, result.Error
-}

# internal/repositories/internship_application_repository.go

diff --git a/internal/repositories/internship_application_repository.go b/internal/repositories/internship_application_repository.go
index d22d6d1..1752e3c 100644
--- a/internal/repositories/internship_application_repository.go
+++ b/internal/repositories/internship_application_repository.go
@@ -3,6 +3,7 @@ package repositories
 import (
 	"context"
 	"strings"
+	"time"
 
 	"github.com/google/uuid"
 	"gorm.io/gorm"
@@ -167,6 +168,11 @@ func (r *InternshipApplicationRepository) ListForEmployer(
 	return out, total, err
 }
 
-func (r *InternshipApplicationRepository) Update(ctx context.Context, a *models.InternshipApplication) error {
-	return r.db.WithContext(ctx).Save(a).Error
+// UpdateApplicationStatus compares the previous status to prevent concurrent reviews/withdrawals from undoing acceptance.
+func (r *InternshipApplicationRepository) UpdateApplicationStatus(ctx context.Context, app *models.InternshipApplication, previous models.InternshipApplicationStatus) (bool, error) {
+	result := r.db.WithContext(ctx).Model(&models.InternshipApplication{}).Where("id = ? AND status = ?", app.ID, previous).
+		Updates(map[string]interface{}{"status": app.Status, "employer_note": app.EmployerNote, "reviewed_at": app.ReviewedAt,
+			"shortlisted_at": app.ShortlistedAt, "accepted_at": app.AcceptedAt, "rejected_at": app.RejectedAt, "withdrawn_at": app.WithdrawnAt,
+			"progress_status": app.ProgressStatus, "start_date": app.StartDate, "updated_at": time.Now()})
+	return result.RowsAffected == 1, result.Error
 }
diff --git a/internal/repositories/internship_application_repository.go b/internal/repositories/internship_application_repository.go
index 1752e3c..b25f58e 100644
--- a/internal/repositories/internship_application_repository.go
+++ b/internal/repositories/internship_application_repository.go
@@ -48,7 +48,6 @@ func (r *InternshipApplicationRepository) GetByID(ctx context.Context, id uuid.U
 	if err := r.db.WithContext(ctx).
 		Preload("Internship").
 		Preload("Student").
-		Preload("Student.Documents", "is_default = ?", true).
 		First(&out, "id = ?", id).Error; err != nil {
 		return nil, err
 	}
@@ -114,7 +113,6 @@ func (r *InternshipApplicationRepository) ListByInternship(ctx context.Context,
 
 	off, limit := getPagination(page, pageSize)
 	err := query.Preload("Student").
-		Preload("Student.Documents", "is_default = ?", true).
 		Order("applied_at DESC").
 		Offset(off).
 		Limit(limit).
@@ -171,8 +169,6 @@ func (r *InternshipApplicationRepository) ListForEmployer(
 // UpdateApplicationStatus compares the previous status to prevent concurrent reviews/withdrawals from undoing acceptance.
 func (r *InternshipApplicationRepository) UpdateApplicationStatus(ctx context.Context, app *models.InternshipApplication, previous models.InternshipApplicationStatus) (bool, error) {
 	result := r.db.WithContext(ctx).Model(&models.InternshipApplication{}).Where("id = ? AND status = ?", app.ID, previous).
-		Updates(map[string]interface{}{"status": app.Status, "employer_note": app.EmployerNote, "reviewed_at": app.ReviewedAt,
-			"shortlisted_at": app.ShortlistedAt, "accepted_at": app.AcceptedAt, "rejected_at": app.RejectedAt, "withdrawn_at": app.WithdrawnAt,
-			"progress_status": app.ProgressStatus, "start_date": app.StartDate, "updated_at": time.Now()})
+		Updates(map[string]interface{}{"status": app.Status, "employer_note": app.EmployerNote, "updated_at": time.Now()})
 	return result.RowsAffected == 1, result.Error
 }

# internal/repositories/internship_record_repository.go

diff --git a/internal/repositories/internship_record_repository.go b/internal/repositories/internship_record_repository.go
new file mode 100644
index 0000000..6fc9be0
--- /dev/null
+++ b/internal/repositories/internship_record_repository.go
@@ -0,0 +1,165 @@
+package repositories
+
+import (
+	"context"
+	"strings"
+
+	"github.com/google/uuid"
+	"github.com/prasanna/student-job-portal/backend/internal/models"
+	"gorm.io/gorm"
+	"gorm.io/gorm/clause"
+)
+
+type InternshipRecordRepository struct{ db *gorm.DB }
+
+func NewInternshipRecordRepository(db *gorm.DB) *InternshipRecordRepository {
+	return &InternshipRecordRepository{db: db}
+}
+
+// Scope is supplied by the service after resolving the authenticated user.
+type InternshipRecordScope struct {
+	CollegeID     *uuid.UUID
+	StudentUserID *uuid.UUID
+	EmployerID    *uuid.UUID
+}
+
+type InternshipRecordFilter struct {
+	StudentName       string
+	Program           string
+	Semester          string
+	AcademicYear      int
+	Company           string
+	Status            string
+	EvaluationStatus  string
+	CertificateStatus string
+	StudentID         *uuid.UUID
+	Page              int
+	PageSize          int
+}
+
+type CollegeDashboard struct {
+	TotalStudents        int64 `json:"total_students_in_internships"`
+	OngoingInternships   int64 `json:"ongoing_internships"`
+	CompletedInternships int64 `json:"completed_internships"`
+	PendingEvaluations   int64 `json:"pending_evaluations"`
+	EvaluationsReceived  int64 `json:"evaluations_received"`
+	PendingCertificates  int64 `json:"pending_certificates"`
+	CertificatesReceived int64 `json:"certificates_received"`
+}
+
+func (r *InternshipRecordRepository) query(ctx context.Context, scope InternshipRecordScope, filter InternshipRecordFilter) *gorm.DB {
+	q := r.db.WithContext(ctx).Model(&models.InternshipApplication{}).
+		Joins("JOIN student_profiles ON student_profiles.id = internship_applications.student_id AND student_profiles.deleted_at IS NULL").
+		Joins("JOIN internships ON internships.id = internship_applications.internship_id").
+		Joins("JOIN recruiter_profiles ON recruiter_profiles.user_id = internships.issued_by").
+		Joins("LEFT JOIN internship_evaluations ON internship_evaluations.application_id = internship_applications.id").
+		Joins("LEFT JOIN internship_certificates ON internship_certificates.application_id = internship_applications.id")
+	if scope.CollegeID != nil {
+		q = q.Where("student_profiles.college_id = ?", *scope.CollegeID)
+	}
+	if scope.StudentUserID != nil {
+		q = q.Where("student_profiles.user_id = ?", *scope.StudentUserID)
+	}
+	if scope.EmployerID != nil {
+		q = q.Where("internships.issued_by = ?", *scope.EmployerID)
+	}
+	if filter.StudentID != nil {
+		q = q.Where("student_profiles.id = ?", *filter.StudentID)
+	}
+	if text := strings.TrimSpace(filter.StudentName); text != "" {
+		q = q.Where("LOWER(student_profiles.full_name) LIKE ?", "%"+strings.ToLower(text)+"%")
+	}
+	if text := strings.TrimSpace(filter.Program); text != "" {
+		like := "%" + strings.ToLower(text) + "%"
+		q = q.Where("(LOWER(student_profiles.degree) LIKE ? OR LOWER(student_profiles.faculty_or_major) LIKE ?)", like, like)
+	}
+	if filter.Semester != "" {
+		q = q.Where("LOWER(student_profiles.current_semester) = ?", strings.ToLower(strings.TrimSpace(filter.Semester)))
+	}
+	if filter.AcademicYear != 0 {
+		q = q.Where("student_profiles.graduation_year = ?", filter.AcademicYear)
+	}
+	if text := strings.TrimSpace(filter.Company); text != "" {
+		q = q.Where("LOWER(recruiter_profiles.organization_name) LIKE ?", "%"+strings.ToLower(text)+"%")
+	}
+	if filter.Status != "" {
+		q = q.Where("internship_applications.progress_status = ?", filter.Status)
+	}
+	switch filter.EvaluationStatus {
+	case "pending":
+		q = q.Where("internship_applications.progress_status = ? AND internship_evaluations.id IS NULL", "completed")
+	case "submitted":
+		q = q.Where("internship_evaluations.id IS NOT NULL")
+	case "not_due":
+		q = q.Where("internship_applications.progress_status <> ? AND internship_evaluations.id IS NULL", "completed")
+	}
+	switch filter.CertificateStatus {
+	case "pending":
+		q = q.Where("internship_applications.progress_status = ? AND internship_certificates.id IS NULL", "completed")
+	case "issued":
+		q = q.Where("internship_certificates.id IS NOT NULL")
+	case "not_due":
+		q = q.Where("internship_applications.progress_status <> ? AND internship_certificates.id IS NULL", "completed")
+	}
+	return q
+}
+
+func recordPreloads(q *gorm.DB) *gorm.DB {
+	// Explicit fields keep private notes, CVs, and user account details out of monitoring responses.
+	return q.Select("internship_applications.*").
+		Preload("Student", func(db *gorm.DB) *gorm.DB {
+			return db.Select("id", "user_id", "full_name", "college_id", "degree", "faculty_or_major", "current_semester", "graduation_year")
+		}).
+		Preload("Student.College").
+		Preload("Internship", func(db *gorm.DB) *gorm.DB { return db.Select("id", "issued_by", "title") }).
+		Preload("Internship.Issuer", func(db *gorm.DB) *gorm.DB { return db.Select("id", "user_id", "organization_name") }).
+		Preload("Evaluation").Preload("Certificate")
+}
+
+func (r *InternshipRecordRepository) List(ctx context.Context, scope InternshipRecordScope, filter InternshipRecordFilter) ([]models.InternshipApplication, int64, error) {
+	out := []models.InternshipApplication{}
+	var total int64
+	q := r.query(ctx, scope, filter)
+	if err := q.Count(&total).Error; err != nil {
+		return nil, 0, err
+	}
+	offset, limit := getPagination(filter.Page, filter.PageSize)
+	err := recordPreloads(q).Order("internship_applications.applied_at DESC, internship_applications.id ASC").Offset(offset).Limit(limit).Find(&out).Error
+	return out, total, err
+}
+
+func (r *InternshipRecordRepository) Get(ctx context.Context, scope InternshipRecordScope, id uuid.UUID) (*models.InternshipApplication, error) {
+	var out models.InternshipApplication
+	err := recordPreloads(r.query(ctx, scope, InternshipRecordFilter{})).Where("internship_applications.id = ?", id).First(&out).Error
+	return &out, err
+}
+
+func (r *InternshipRecordRepository) Dashboard(ctx context.Context, collegeID uuid.UUID) (*CollegeDashboard, error) {
+	var out CollegeDashboard
+	err := r.query(ctx, InternshipRecordScope{CollegeID: &collegeID}, InternshipRecordFilter{}).
+		Select(`COUNT(DISTINCT student_profiles.id) AS total_students,
+		COALESCE(SUM(CASE WHEN progress_status = 'ongoing' THEN 1 ELSE 0 END), 0) AS ongoing_internships,
+		COALESCE(SUM(CASE WHEN progress_status = 'completed' THEN 1 ELSE 0 END), 0) AS completed_internships,
+		COALESCE(SUM(CASE WHEN progress_status = 'completed' AND internship_evaluations.id IS NULL THEN 1 ELSE 0 END), 0) AS pending_evaluations,
+		COUNT(internship_evaluations.id) AS evaluations_received,
+		COALESCE(SUM(CASE WHEN progress_status = 'completed' AND internship_certificates.id IS NULL THEN 1 ELSE 0 END), 0) AS pending_certificates,
+		COUNT(internship_certificates.id) AS certificates_received`).Scan(&out).Error
+	return &out, err
+}
+
+func (r *InternshipRecordRepository) UpdateProgress(ctx context.Context, app *models.InternshipApplication, previous string) (bool, error) {
+	result := r.db.WithContext(ctx).Model(&models.InternshipApplication{}).
+		Where("id = ? AND progress_status = ? AND status = ?", app.ID, previous, models.ApplicationStatusAccepted).
+		Updates(map[string]interface{}{"progress_status": app.ProgressStatus, "start_date": app.StartDate, "end_date": app.EndDate})
+	return result.RowsAffected == 1, result.Error
+}
+
+func (r *InternshipRecordRepository) CreateEvaluation(ctx context.Context, evaluation *models.InternshipEvaluation) (bool, error) {
+	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "application_id"}}, DoNothing: true}).Create(evaluation)
+	return result.RowsAffected == 1, result.Error
+}
+
+func (r *InternshipRecordRepository) CreateCertificate(ctx context.Context, certificate *models.InternshipCertificate) (bool, error) {
+	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "application_id"}}, DoNothing: true}).Create(certificate)
+	return result.RowsAffected == 1, result.Error
+}
diff --git a/internal/repositories/internship_record_repository.go b/internal/repositories/internship_record_repository.go
deleted file mode 100644
index 6fc9be0..0000000
--- a/internal/repositories/internship_record_repository.go
+++ /dev/null
@@ -1,165 +0,0 @@
-package repositories
-
-import (
-	"context"
-	"strings"
-
-	"github.com/google/uuid"
-	"github.com/prasanna/student-job-portal/backend/internal/models"
-	"gorm.io/gorm"
-	"gorm.io/gorm/clause"
-)
-
-type InternshipRecordRepository struct{ db *gorm.DB }
-
-func NewInternshipRecordRepository(db *gorm.DB) *InternshipRecordRepository {
-	return &InternshipRecordRepository{db: db}
-}
-
-// Scope is supplied by the service after resolving the authenticated user.
-type InternshipRecordScope struct {
-	CollegeID     *uuid.UUID
-	StudentUserID *uuid.UUID
-	EmployerID    *uuid.UUID
-}
-
-type InternshipRecordFilter struct {
-	StudentName       string
-	Program           string
-	Semester          string
-	AcademicYear      int
-	Company           string
-	Status            string
-	EvaluationStatus  string
-	CertificateStatus string
-	StudentID         *uuid.UUID
-	Page              int
-	PageSize          int
-}
-
-type CollegeDashboard struct {
-	TotalStudents        int64 `json:"total_students_in_internships"`
-	OngoingInternships   int64 `json:"ongoing_internships"`
-	CompletedInternships int64 `json:"completed_internships"`
-	PendingEvaluations   int64 `json:"pending_evaluations"`
-	EvaluationsReceived  int64 `json:"evaluations_received"`
-	PendingCertificates  int64 `json:"pending_certificates"`
-	CertificatesReceived int64 `json:"certificates_received"`
-}
-
-func (r *InternshipRecordRepository) query(ctx context.Context, scope InternshipRecordScope, filter InternshipRecordFilter) *gorm.DB {
-	q := r.db.WithContext(ctx).Model(&models.InternshipApplication{}).
-		Joins("JOIN student_profiles ON student_profiles.id = internship_applications.student_id AND student_profiles.deleted_at IS NULL").
-		Joins("JOIN internships ON internships.id = internship_applications.internship_id").
-		Joins("JOIN recruiter_profiles ON recruiter_profiles.user_id = internships.issued_by").
-		Joins("LEFT JOIN internship_evaluations ON internship_evaluations.application_id = internship_applications.id").
-		Joins("LEFT JOIN internship_certificates ON internship_certificates.application_id = internship_applications.id")
-	if scope.CollegeID != nil {
-		q = q.Where("student_profiles.college_id = ?", *scope.CollegeID)
-	}
-	if scope.StudentUserID != nil {
-		q = q.Where("student_profiles.user_id = ?", *scope.StudentUserID)
-	}
-	if scope.EmployerID != nil {
-		q = q.Where("internships.issued_by = ?", *scope.EmployerID)
-	}
-	if filter.StudentID != nil {
-		q = q.Where("student_profiles.id = ?", *filter.StudentID)
-	}
-	if text := strings.TrimSpace(filter.StudentName); text != "" {
-		q = q.Where("LOWER(student_profiles.full_name) LIKE ?", "%"+strings.ToLower(text)+"%")
-	}
-	if text := strings.TrimSpace(filter.Program); text != "" {
-		like := "%" + strings.ToLower(text) + "%"
-		q = q.Where("(LOWER(student_profiles.degree) LIKE ? OR LOWER(student_profiles.faculty_or_major) LIKE ?)", like, like)
-	}
-	if filter.Semester != "" {
-		q = q.Where("LOWER(student_profiles.current_semester) = ?", strings.ToLower(strings.TrimSpace(filter.Semester)))
-	}
-	if filter.AcademicYear != 0 {
-		q = q.Where("student_profiles.graduation_year = ?", filter.AcademicYear)
-	}
-	if text := strings.TrimSpace(filter.Company); text != "" {
-		q = q.Where("LOWER(recruiter_profiles.organization_name) LIKE ?", "%"+strings.ToLower(text)+"%")
-	}
-	if filter.Status != "" {
-		q = q.Where("internship_applications.progress_status = ?", filter.Status)
-	}
-	switch filter.EvaluationStatus {
-	case "pending":
-		q = q.Where("internship_applications.progress_status = ? AND internship_evaluations.id IS NULL", "completed")
-	case "submitted":
-		q = q.Where("internship_evaluations.id IS NOT NULL")
-	case "not_due":
-		q = q.Where("internship_applications.progress_status <> ? AND internship_evaluations.id IS NULL", "completed")
-	}
-	switch filter.CertificateStatus {
-	case "pending":
-		q = q.Where("internship_applications.progress_status = ? AND internship_certificates.id IS NULL", "completed")
-	case "issued":
-		q = q.Where("internship_certificates.id IS NOT NULL")
-	case "not_due":
-		q = q.Where("internship_applications.progress_status <> ? AND internship_certificates.id IS NULL", "completed")
-	}
-	return q
-}
-
-func recordPreloads(q *gorm.DB) *gorm.DB {
-	// Explicit fields keep private notes, CVs, and user account details out of monitoring responses.
-	return q.Select("internship_applications.*").
-		Preload("Student", func(db *gorm.DB) *gorm.DB {
-			return db.Select("id", "user_id", "full_name", "college_id", "degree", "faculty_or_major", "current_semester", "graduation_year")
-		}).
-		Preload("Student.College").
-		Preload("Internship", func(db *gorm.DB) *gorm.DB { return db.Select("id", "issued_by", "title") }).
-		Preload("Internship.Issuer", func(db *gorm.DB) *gorm.DB { return db.Select("id", "user_id", "organization_name") }).
-		Preload("Evaluation").Preload("Certificate")
-}
-
-func (r *InternshipRecordRepository) List(ctx context.Context, scope InternshipRecordScope, filter InternshipRecordFilter) ([]models.InternshipApplication, int64, error) {
-	out := []models.InternshipApplication{}
-	var total int64
-	q := r.query(ctx, scope, filter)
-	if err := q.Count(&total).Error; err != nil {
-		return nil, 0, err
-	}
-	offset, limit := getPagination(filter.Page, filter.PageSize)
-	err := recordPreloads(q).Order("internship_applications.applied_at DESC, internship_applications.id ASC").Offset(offset).Limit(limit).Find(&out).Error
-	return out, total, err
-}
-
-func (r *InternshipRecordRepository) Get(ctx context.Context, scope InternshipRecordScope, id uuid.UUID) (*models.InternshipApplication, error) {
-	var out models.InternshipApplication
-	err := recordPreloads(r.query(ctx, scope, InternshipRecordFilter{})).Where("internship_applications.id = ?", id).First(&out).Error
-	return &out, err
-}
-
-func (r *InternshipRecordRepository) Dashboard(ctx context.Context, collegeID uuid.UUID) (*CollegeDashboard, error) {
-	var out CollegeDashboard
-	err := r.query(ctx, InternshipRecordScope{CollegeID: &collegeID}, InternshipRecordFilter{}).
-		Select(`COUNT(DISTINCT student_profiles.id) AS total_students,
-		COALESCE(SUM(CASE WHEN progress_status = 'ongoing' THEN 1 ELSE 0 END), 0) AS ongoing_internships,
-		COALESCE(SUM(CASE WHEN progress_status = 'completed' THEN 1 ELSE 0 END), 0) AS completed_internships,
-		COALESCE(SUM(CASE WHEN progress_status = 'completed' AND internship_evaluations.id IS NULL THEN 1 ELSE 0 END), 0) AS pending_evaluations,
-		COUNT(internship_evaluations.id) AS evaluations_received,
-		COALESCE(SUM(CASE WHEN progress_status = 'completed' AND internship_certificates.id IS NULL THEN 1 ELSE 0 END), 0) AS pending_certificates,
-		COUNT(internship_certificates.id) AS certificates_received`).Scan(&out).Error
-	return &out, err
-}
-
-func (r *InternshipRecordRepository) UpdateProgress(ctx context.Context, app *models.InternshipApplication, previous string) (bool, error) {
-	result := r.db.WithContext(ctx).Model(&models.InternshipApplication{}).
-		Where("id = ? AND progress_status = ? AND status = ?", app.ID, previous, models.ApplicationStatusAccepted).
-		Updates(map[string]interface{}{"progress_status": app.ProgressStatus, "start_date": app.StartDate, "end_date": app.EndDate})
-	return result.RowsAffected == 1, result.Error
-}
-
-func (r *InternshipRecordRepository) CreateEvaluation(ctx context.Context, evaluation *models.InternshipEvaluation) (bool, error) {
-	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "application_id"}}, DoNothing: true}).Create(evaluation)
-	return result.RowsAffected == 1, result.Error
-}
-
-func (r *InternshipRecordRepository) CreateCertificate(ctx context.Context, certificate *models.InternshipCertificate) (bool, error) {
-	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "application_id"}}, DoNothing: true}).Create(certificate)
-	return result.RowsAffected == 1, result.Error
-}

# internal/repositories/student_repository.go

diff --git a/internal/repositories/student_repository.go b/internal/repositories/student_repository.go
index 4cd57c5..48538ed 100644
--- a/internal/repositories/student_repository.go
+++ b/internal/repositories/student_repository.go
@@ -2,9 +2,11 @@ package repositories
 
 import (
 	"context"
+	"errors"
 
 	"github.com/google/uuid"
 	"gorm.io/gorm"
+	"gorm.io/gorm/clause"
 
 	"github.com/prasanna/student-job-portal/backend/internal/models"
 )
@@ -46,8 +48,12 @@ func (r *StudentRepository) ResolveProfileID(
 	return profile.ID, nil
 }
 func (r *StudentRepository) GetByUserID(userID uuid.UUID) (*models.StudentProfile, error) {
+	return r.GetByUserIDContext(context.Background(), userID)
+}
+
+func (r *StudentRepository) GetByUserIDContext(ctx context.Context, userID uuid.UUID) (*models.StudentProfile, error) {
 	var p models.StudentProfile
-	if err := r.DB.Where("user_id = ?", userID).First(&p).Error; err != nil {
+	if err := r.DB.WithContext(ctx).Preload("College").Where("user_id = ?", userID).First(&p).Error; err != nil {
 		return nil, err
 	}
 	return &p, nil
@@ -92,15 +98,31 @@ func (r *StudentRepository) GetApplicationStats(
 	return &stats, nil
 }
 
-func (r *StudentRepository) CreateOrUpdateProfile(p *models.StudentProfile) error {
+func (r *StudentRepository) CreateOrUpdateProfile(ctx context.Context, p *models.StudentProfile) error {
 	// Upsert by user_id
+	db := r.DB.WithContext(ctx)
 	var existing models.StudentProfile
-	if err := r.DB.Where("user_id = ?", p.UserID).First(&existing).Error; err == nil {
+	err := db.Where("user_id = ?", p.UserID).First(&existing).Error
+	if err == nil {
 		p.ID = existing.ID
 		p.CreatedAt = existing.CreatedAt
-		return r.DB.Save(p).Error
+		result := db.Model(p).Where("college_id = ? OR college_id IS NULL", p.CollegeID).
+			Omit(clause.Associations).Select("*").Updates(p)
+		if result.Error == nil && result.RowsAffected == 0 {
+			return gorm.ErrRecordNotFound
+		}
+		return result.Error
+	}
+	if !errors.Is(err, gorm.ErrRecordNotFound) {
+		return err
 	}
-	return r.DB.Create(p).Error
+	return db.Omit(clause.Associations).Create(p).Error
+}
+
+func (r *StudentRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.StudentProfile, error) {
+	var student models.StudentProfile
+	err := r.DB.WithContext(ctx).Preload("College").First(&student, "id = ?", id).Error
+	return &student, err
 }
 
 func (r *StudentRepository) AddEducation(e *models.StudentEducation) error {
diff --git a/internal/repositories/student_repository.go b/internal/repositories/student_repository.go
index 48538ed..7df5840 100644
--- a/internal/repositories/student_repository.go
+++ b/internal/repositories/student_repository.go
@@ -15,19 +15,12 @@ type StudentRepository struct {
 	DB *gorm.DB
 }
 
-// StudentApplicationStats is the dashboard-facing summary of a student's
-// internship applications. Active applications are all non-final applications
-// (submitted, reviewing, or shortlisted), while pending applications are those
-// that have only been submitted.
 type StudentApplicationStats struct {
-	TotalApplications       int64 `json:"total_applications"`
-	ActiveApplications      int64 `json:"active_applications"`
-	ApprovedApplications    int64 `json:"approved_applications"`
-	RejectedApplications    int64 `json:"rejected_applications"`
-	PendingApplications     int64 `json:"pending_applications"`
-	UnderReviewApplications int64 `json:"under_review_applications"`
-	ShortlistedApplications int64 `json:"shortlisted_applications"`
-	WithdrawnApplications   int64 `json:"withdrawn_applications"`
+	TotalApplications     int64 `json:"total_applications"`
+	PendingApplications   int64 `json:"pending_applications"`
+	ApprovedApplications  int64 `json:"approved_applications"`
+	RejectedApplications  int64 `json:"rejected_applications"`
+	WithdrawnApplications int64 `json:"withdrawn_applications"`
 }
 
 func NewStudentRepository(db *gorm.DB) *StudentRepository {
@@ -53,7 +46,7 @@ func (r *StudentRepository) GetByUserID(userID uuid.UUID) (*models.StudentProfil
 
 func (r *StudentRepository) GetByUserIDContext(ctx context.Context, userID uuid.UUID) (*models.StudentProfile, error) {
 	var p models.StudentProfile
-	if err := r.DB.WithContext(ctx).Preload("College").Where("user_id = ?", userID).First(&p).Error; err != nil {
+	if err := r.DB.WithContext(ctx).Where("user_id = ?", userID).First(&p).Error; err != nil {
 		return nil, err
 	}
 	return &p, nil
@@ -65,32 +58,12 @@ func (r *StudentRepository) GetApplicationStats(
 ) (*StudentApplicationStats, error) {
 	var stats StudentApplicationStats
 
-	err := r.DB.
-		WithContext(ctx).
-		Model(&models.InternshipApplication{}).
-		Where("student_id = ?", profileID).
-		Select(`
-			COUNT(*) AS total_applications,
-			COALESCE(SUM(CASE WHEN status IN (?, ?, ?) THEN 1 ELSE 0 END), 0) AS active_applications,
-			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS approved_applications,
-			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS rejected_applications,
-			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS pending_applications,
-			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS under_review_applications,
-			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS shortlisted_applications,
-			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS withdrawn_applications
-		`,
-			models.ApplicationStatusSubmitted,
-			models.ApplicationStatusReviewing,
-			models.ApplicationStatusShortlisted,
-			models.ApplicationStatusAccepted,
-			models.ApplicationStatusRejected,
-			models.ApplicationStatusSubmitted,
-			models.ApplicationStatusReviewing,
-			models.ApplicationStatusShortlisted,
-			models.ApplicationStatusWithdrawn,
-		).
-		Scan(&stats).
-		Error
+	err := r.DB.WithContext(ctx).Model(&models.InternshipApplication{}).Where("student_id = ?", profileID).
+		Select(`COUNT(*) AS total_applications,
+ COALESCE(SUM(CASE WHEN status = 'submitted' THEN 1 ELSE 0 END), 0) AS pending_applications,
+ COALESCE(SUM(CASE WHEN status = 'accepted' THEN 1 ELSE 0 END), 0) AS approved_applications,
+ COALESCE(SUM(CASE WHEN status = 'rejected' THEN 1 ELSE 0 END), 0) AS rejected_applications,
+ COALESCE(SUM(CASE WHEN status = 'withdrawn' THEN 1 ELSE 0 END), 0) AS withdrawn_applications`).Scan(&stats).Error
 	if err != nil {
 		return nil, err
 	}
@@ -106,8 +79,9 @@ func (r *StudentRepository) CreateOrUpdateProfile(ctx context.Context, p *models
 	if err == nil {
 		p.ID = existing.ID
 		p.CreatedAt = existing.CreatedAt
-		result := db.Model(p).Where("college_id = ? OR college_id IS NULL", p.CollegeID).
-			Omit(clause.Associations).Select("*").Updates(p)
+		p.ProfilePictureKey = existing.ProfilePictureKey
+		result := db.Model(p).
+			Omit(clause.Associations, "ProfilePictureKey").Select("*").Updates(p)
 		if result.Error == nil && result.RowsAffected == 0 {
 			return gorm.ErrRecordNotFound
 		}
@@ -119,88 +93,62 @@ func (r *StudentRepository) CreateOrUpdateProfile(ctx context.Context, p *models
 	return db.Omit(clause.Associations).Create(p).Error
 }
 
+func (r *StudentRepository) UpdateProfilePicture(ctx context.Context, userID uuid.UUID, objectKey string) (string, error) {
+	var oldKey string
+	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
+		var profile models.StudentProfile
+		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&profile).Error; err != nil {
+			return err
+		}
+		oldKey = profile.ProfilePictureKey
+		return tx.Model(&profile).Update("profile_picture_key", objectKey).Error
+	})
+	return oldKey, err
+}
+
 func (r *StudentRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.StudentProfile, error) {
 	var student models.StudentProfile
-	err := r.DB.WithContext(ctx).Preload("College").First(&student, "id = ?", id).Error
+	err := r.DB.WithContext(ctx).First(&student, "id = ?", id).Error
 	return &student, err
 }
 
-func (r *StudentRepository) AddEducation(e *models.StudentEducation) error {
-	return r.DB.Create(e).Error
-}
-
-func (r *StudentRepository) ListEducations(profileID uuid.UUID) ([]models.StudentEducation, error) {
-	var out []models.StudentEducation
-	if err := r.DB.Where("profile_id = ?", profileID).Find(&out).Error; err != nil {
-		return nil, err
-	}
-	return out, nil
-}
-
-func (r *StudentRepository) AddSkill(s *models.StudentSkill) error {
-	return r.DB.Create(s).Error
-}
-
-func (r *StudentRepository) ListSkills(profileID uuid.UUID) ([]models.StudentSkill, error) {
-	var out []models.StudentSkill
-	if err := r.DB.Where("profile_id = ?", profileID).Find(&out).Error; err != nil {
-		return nil, err
-	}
-	return out, nil
-}
-
-func (r *StudentRepository) AddProject(pj *models.StudentProject) error {
-	return r.DB.Create(pj).Error
-}
-
-func (r *StudentRepository) ListProjects(profileID uuid.UUID) ([]models.StudentProject, error) {
-	var out []models.StudentProject
-	if err := r.DB.Where("profile_id = ?", profileID).Find(&out).Error; err != nil {
-		return nil, err
-	}
-	return out, nil
-}
-
-func (r *StudentRepository) AddCertification(c *models.StudentCertification) error {
-	return r.DB.Create(c).Error
+// GetResume selects the latest CV; legacy document rows stay in the database.
+func (r *StudentRepository) GetResume(ctx context.Context, profileID uuid.UUID) (*models.StudentDocument, error) {
+	var resume models.StudentDocument
+	err := r.DB.WithContext(ctx).Where("profile_id = ?", profileID).Order("created_at DESC, id DESC").First(&resume).Error
+	return &resume, err
 }
 
-func (r *StudentRepository) ListCertifications(profileID uuid.UUID) ([]models.StudentCertification, error) {
-	var out []models.StudentCertification
-	if err := r.DB.Where("profile_id = ?", profileID).Find(&out).Error; err != nil {
-		return nil, err
-	}
-	return out, nil
-}
-
-func (r *StudentRepository) AddDocument(d *models.StudentDocument) error {
-	return r.DB.Create(d).Error
-}
-
-func (r *StudentRepository) ListDocuments(profileID uuid.UUID) ([]models.StudentDocument, error) {
-	var out []models.StudentDocument
-	if err := r.DB.Where("profile_id = ?", profileID).Order("is_default desc, created_at desc").Find(&out).Error; err != nil {
-		return nil, err
-	}
-	return out, nil
-}
-
-func (r *StudentRepository) SetDefaultDocument(profileID, docID uuid.UUID) error {
-	// clear existing defaults
-	if err := r.DB.Model(&models.StudentDocument{}).Where("profile_id = ?", profileID).Update("is_default", false).Error; err != nil {
-		return err
-	}
-	return r.DB.Model(&models.StudentDocument{}).Where("id = ? AND profile_id = ?", docID, profileID).Update("is_default", true).Error
-}
-
-func (r *StudentRepository) DeleteDocumentByID(docID uuid.UUID) error {
-	return r.DB.Where("id = ?", docID).Delete(&models.StudentDocument{}).Error
+func (r *StudentRepository) SaveResume(ctx context.Context, resume *models.StudentDocument) (string, error) {
+	var oldKey string
+	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
+		var profile models.StudentProfile
+		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&profile, "id = ? AND user_id = ?", resume.ProfileID, resume.UserID).Error; err != nil {
+			return err
+		}
+		existing, err := NewStudentRepository(tx).GetResume(ctx, profile.ID)
+		if err == nil {
+			resume.ID, resume.CreatedAt, oldKey = existing.ID, existing.CreatedAt, existing.ObjectKey
+		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
+			return err
+		}
+		return tx.Save(resume).Error
+	})
+	return oldKey, err
 }
 
-func (r *StudentRepository) GetDocumentByID(docID uuid.UUID) (*models.StudentDocument, error) {
-	var d models.StudentDocument
-	if err := r.DB.First(&d, "id = ?", docID).Error; err != nil {
-		return nil, err
-	}
-	return &d, nil
+func (r *StudentRepository) DeleteResume(ctx context.Context, profileID, userID uuid.UUID) ([]models.StudentDocument, error) {
+	var documents []models.StudentDocument
+	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
+		var profile models.StudentProfile
+		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&profile, "id = ? AND user_id = ?", profileID, userID).Error; err != nil {
+			return err
+		}
+		if err := tx.Where("profile_id = ?", profileID).Find(&documents).Error; err != nil {
+			return err
+		}
+		// Remove legacy CVs too so an older file cannot become the current CV.
+		return tx.Where("profile_id = ?", profileID).Delete(&models.StudentDocument{}).Error
+	})
+	return documents, err
 }

# internal/repositories/user_repository.go

diff --git a/internal/repositories/user_repository.go b/internal/repositories/user_repository.go
index 22b098a..36246c6 100644
--- a/internal/repositories/user_repository.go
+++ b/internal/repositories/user_repository.go
@@ -1,6 +1,7 @@
 package repositories
 
 import (
+	"context"
 	"crypto/sha256"
 	"encoding/hex"
 	"time"
@@ -33,7 +34,7 @@ func (r *UserRepository) List(page, pageSize int) ([]models.User, int64, error)
 
 	offset, limit := getPagination(page, pageSize)
 	err := r.DB.Model(&models.User{}).
-		Select("id, email, full_name, role, created_at").
+		Select("id, email, full_name, role, college_id, created_at").
 		Order("created_at DESC").
 		Offset(offset).
 		Limit(limit).
@@ -51,8 +52,12 @@ func (r *UserRepository) GetByEmail(email string) (*models.User, error) {
 }
 
 func (r *UserRepository) GetByID(id uuid.UUID) (*models.User, error) {
+	return r.GetByIDContext(context.Background(), id)
+}
+
+func (r *UserRepository) GetByIDContext(ctx context.Context, id uuid.UUID) (*models.User, error) {
 	var u models.User
-	if err := r.DB.First(&u, "id = ?", id).Error; err != nil {
+	if err := r.DB.WithContext(ctx).Preload("College").First(&u, "id = ?", id).Error; err != nil {
 		return nil, err
 	}
 	return &u, nil
diff --git a/internal/repositories/user_repository.go b/internal/repositories/user_repository.go
index 36246c6..eea0aa9 100644
--- a/internal/repositories/user_repository.go
+++ b/internal/repositories/user_repository.go
@@ -4,6 +4,7 @@ import (
 	"context"
 	"crypto/sha256"
 	"encoding/hex"
+	"strings"
 	"time"
 
 	"github.com/google/uuid"
@@ -24,28 +25,9 @@ func (r *UserRepository) Create(user *models.User) error {
 	return r.DB.Create(user).Error
 }
 
-func (r *UserRepository) List(page, pageSize int) ([]models.User, int64, error) {
-	var users []models.User
-	var total int64
-
-	if err := r.DB.Model(&models.User{}).Count(&total).Error; err != nil {
-		return nil, 0, err
-	}
-
-	offset, limit := getPagination(page, pageSize)
-	err := r.DB.Model(&models.User{}).
-		Select("id, email, full_name, role, college_id, created_at").
-		Order("created_at DESC").
-		Offset(offset).
-		Limit(limit).
-		Find(&users).Error
-
-	return users, total, err
-}
-
 func (r *UserRepository) GetByEmail(email string) (*models.User, error) {
 	var u models.User
-	if err := r.DB.Where("email = ?", email).First(&u).Error; err != nil {
+	if err := r.DB.Where("LOWER(TRIM(email)) = ?", strings.ToLower(strings.TrimSpace(email))).First(&u).Error; err != nil {
 		return nil, err
 	}
 	return &u, nil
@@ -57,7 +39,7 @@ func (r *UserRepository) GetByID(id uuid.UUID) (*models.User, error) {
 
 func (r *UserRepository) GetByIDContext(ctx context.Context, id uuid.UUID) (*models.User, error) {
 	var u models.User
-	if err := r.DB.WithContext(ctx).Preload("College").First(&u, "id = ?", id).Error; err != nil {
+	if err := r.DB.WithContext(ctx).First(&u, "id = ?", id).Error; err != nil {
 		return nil, err
 	}
 	return &u, nil

# internal/routes/college_test.go

diff --git a/internal/routes/college_test.go b/internal/routes/college_test.go
new file mode 100644
index 0000000..e1e9804
--- /dev/null
+++ b/internal/routes/college_test.go
@@ -0,0 +1,410 @@
+package routes_test
+
+import (
+	"bytes"
+	"encoding/json"
+	"fmt"
+	"io"
+	"mime/multipart"
+	"net/http"
+	"net/http/httptest"
+	"net/url"
+	"os"
+	"strings"
+	"testing"
+	"time"
+
+	"github.com/gin-gonic/gin"
+	"github.com/google/uuid"
+	"github.com/minio/minio-go/v7"
+	"github.com/minio/minio-go/v7/pkg/credentials"
+	"github.com/prasanna/student-job-portal/backend/internal/config"
+	"github.com/prasanna/student-job-portal/backend/internal/database"
+	"github.com/prasanna/student-job-portal/backend/internal/enums"
+	"github.com/prasanna/student-job-portal/backend/internal/logger"
+	"github.com/prasanna/student-job-portal/backend/internal/models"
+	"github.com/prasanna/student-job-portal/backend/internal/repositories"
+	"github.com/prasanna/student-job-portal/backend/internal/responses"
+	"github.com/prasanna/student-job-portal/backend/internal/routes"
+	"github.com/prasanna/student-job-portal/backend/internal/services"
+	"gorm.io/driver/postgres"
+	"gorm.io/driver/sqlite"
+	"gorm.io/gorm"
+	gormlogger "gorm.io/gorm/logger"
+)
+
+type certificateTransport func(*http.Request) (*http.Response, error)
+
+func (f certificateTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
+
+// Exercise real routes, JWT middleware, services, repositories and FK constraints.
+// Only the external object store is replaced with an in-memory HTTP transport.
+func TestCollegeInternshipWorkflow(t *testing.T) {
+	gin.SetMode(gin.TestMode)
+	var dialector gorm.Dialector = sqlite.Open("file:" + uuid.NewString() + "?mode=memory&cache=shared&_foreign_keys=on")
+	if dsn := os.Getenv("COLLEGE_TEST_DATABASE_URL"); dsn != "" {
+		dialector = postgres.Open(dsn)
+	}
+	db, err := gorm.Open(dialector, &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
+	if err != nil {
+		t.Fatal(err)
+	}
+	sqlDB, err := db.DB()
+	if err != nil {
+		t.Fatal(err)
+	}
+	t.Cleanup(func() { sqlDB.Close() })
+	sqlDB.SetMaxOpenConns(1)
+	if err := database.AutoMigrate(db); err != nil {
+		t.Fatal(err)
+	}
+	cfg := &config.Config{}
+	cfg.JWT.AccessSecret, cfg.JWT.AccessExpiry = "college-integration-test-secret", time.Hour
+	cfg.JWT.RefreshExpiry = time.Hour
+	cfg.MinIO.CompanyDocBucket = "company-documents"
+	log := logger.New("error")
+	puts := 0
+	storage, err := minio.New("storage.example.test", &minio.Options{Secure: true, Region: "us-east-1", Creds: credentials.NewStaticV4("test-key", "test-secret", ""),
+		Transport: certificateTransport(func(req *http.Request) (*http.Response, error) {
+			if req.Method != http.MethodPut {
+				return nil, fmt.Errorf("unexpected storage request: %s %s", req.Method, req.URL)
+			}
+			puts++
+			if _, err := io.Copy(io.Discard, req.Body); err != nil {
+				return nil, err
+			}
+			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Etag": {"test-etag"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
+		})})
+	if err != nil {
+		t.Fatal(err)
+	}
+	router := gin.New()
+	routes.Register(router, db, cfg, log, storage)
+	auth := services.NewAuthService(repositories.NewUserRepository(db), cfg, log)
+
+	create := func(record interface{}) {
+		t.Helper()
+		if err := db.Create(record).Error; err != nil {
+			t.Fatal(err)
+		}
+	}
+	user := func(role models.Role) models.User {
+		t.Helper()
+		u := models.User{ID: uuid.New(), Email: uuid.NewString() + "@example.test", Role: role, PasswordHash: "unused", FullName: string(role)}
+		create(&u)
+		return u
+	}
+	token := func(u models.User) string {
+		t.Helper()
+		signed, err := auth.GenerateAccessToken(&u)
+		if err != nil {
+			t.Fatal(err)
+		}
+		return signed
+	}
+	admin, employer, stranger := user(models.RoleAdmin), user(models.RoleEmployer), user(models.RoleEmployer)
+	adminToken, employerToken, strangerToken := token(admin), token(employer), token(stranger)
+	create(&models.RecruiterProfile{UserID: employer.ID, OrganizationName: "Company X", VerificationStatus: "approved"})
+	studentA, studentB, legacyUser := user(models.RoleStudent), user(models.RoleStudent), user(models.RoleStudent)
+	studentAToken, studentBToken := token(studentA), token(studentB)
+
+	request := func(method, path, authToken string, payload interface{}, expected int) json.RawMessage {
+		t.Helper()
+		var body io.Reader
+		contentType := "application/json"
+		if pdf, ok := payload.([]byte); ok {
+			var b bytes.Buffer
+			w := multipart.NewWriter(&b)
+			part, err := w.CreateFormFile("file", "certificate.pdf")
+			if err != nil {
+				t.Fatal(err)
+			}
+			if _, err := part.Write(pdf); err != nil {
+				t.Fatal(err)
+			}
+			if err := w.Close(); err != nil {
+				t.Fatal(err)
+			}
+			contentType, body = w.FormDataContentType(), &b
+		} else if payload != nil {
+			b, err := json.Marshal(payload)
+			if err != nil {
+				t.Fatal(err)
+			}
+			body = bytes.NewReader(b)
+		}
+		req := httptest.NewRequest(method, "/api/v1"+path, body)
+		req.Header.Set("Content-Type", contentType)
+		if authToken != "" {
+			req.Header.Set("Authorization", "Bearer "+authToken)
+		}
+		w := httptest.NewRecorder()
+		router.ServeHTTP(w, req)
+		if w.Code != expected {
+			t.Fatalf("%s %s: status %d, want %d; %s", method, path, w.Code, expected, w.Body.String())
+		}
+		return json.RawMessage(w.Body.Bytes())
+	}
+	decode := func(raw json.RawMessage, value interface{}) {
+		t.Helper()
+		var envelope struct {
+			Data json.RawMessage `json:"data"`
+		}
+		if err := json.Unmarshal(raw, &envelope); err != nil {
+			t.Fatal(err)
+		}
+		if err := json.Unmarshal(envelope.Data, value); err != nil {
+			t.Fatalf("decode %s: %v", raw, err)
+		}
+	}
+	listCount := func(path, authToken string, total, size int) []services.InternshipRecord {
+		t.Helper()
+		raw := request("GET", path, authToken, nil, 200)
+		var envelope struct {
+			Data       []services.InternshipRecord `json:"data"`
+			Pagination responses.Pagination        `json:"pagination"`
+		}
+		if err := json.Unmarshal(raw, &envelope); err != nil {
+			t.Fatal(err)
+		}
+		if envelope.Pagination.TotalItems != int64(total) || len(envelope.Data) != size {
+			t.Fatalf("unexpected list: %s", raw)
+		}
+		return envelope.Data
+	}
+
+	request("GET", "/colleges/me/dashboard", "", nil, 401)
+	request("POST", "/admin/colleges", studentAToken, nil, 403)
+	request("POST", "/auth/register/college", "", nil, 404)
+	request("POST", "/admin/colleges", adminToken, gin.H{"name": "Bad", "university": "UNSUPPORTED"}, 400)
+	var universities []interface{}
+	decode(request("GET", "/universities", "", nil, 200), &universities)
+	if len(universities) != 4 {
+		t.Fatal("expected four fixed universities")
+	}
+	var collegeA, collegeB models.College
+	decode(request("POST", "/admin/colleges", adminToken, gin.H{"name": "College A", "university": models.TribhuvanUniversity}, 201), &collegeA)
+	decode(request("POST", "/admin/colleges", adminToken, gin.H{"name": "College B", "university": models.TribhuvanUniversity}, 201), &collegeB)
+	var coordinatorA, coordinatorB models.User
+	decode(request("POST", "/admin/colleges/"+collegeA.ID.String()+"/users", adminToken, gin.H{"email": "college-a@example.test", "full_name": "Coordinator A", "password": "Password123!"}, 201), &coordinatorA)
+	decode(request("POST", "/admin/colleges/"+collegeB.ID.String()+"/users", adminToken, gin.H{"email": "college-b@example.test", "full_name": "Coordinator B", "password": "Password123!"}, 201), &coordinatorB)
+	collegeAToken, collegeBToken := token(coordinatorA), token(coordinatorB)
+	request("POST", "/admin/colleges/"+collegeA.ID.String()+"/users", adminToken, gin.H{"email": "college-a@example.test", "full_name": "Duplicate", "password": "Password123!"}, 409)
+	login := request("POST", "/auth/login", "", gin.H{"email": "college-a@example.test", "password": "Password123!"}, 200)
+	if bytes.Contains(login, []byte("password")) {
+		t.Fatal("password leaked")
+	}
+	var me models.User
+	decode(request("GET", "/auth/me", collegeAToken, nil, 200), &me)
+	if me.Role != models.RoleCollege || me.CollegeID == nil || *me.CollegeID != collegeA.ID {
+		t.Fatal("college authentication metadata missing")
+	}
+
+	request("POST", "/students/me/profile", studentAToken, gin.H{"full_name": "Anita"}, 400)
+	var profileA, profileB models.StudentProfile
+	decode(request("POST", "/students/me/profile", studentAToken, gin.H{"full_name": "Anita", "college_id": collegeA.ID, "degree": "BSc CSIT", "current_semester": "7", "graduation_year": 2027,
+		"college": gin.H{"id": collegeB.ID, "name": "Tampered"}, "documents": []gin.H{{"object_key": "forged"}}}, 200), &profileA)
+	decode(request("POST", "/students/me/profile", studentBToken, gin.H{"full_name": "Bina", "college_id": collegeB.ID, "degree": "BIT", "current_semester": "6"}, 200), &profileB)
+	if profileA.College == nil || profileA.College.Name != "College A" {
+		t.Fatal("college association was not resolved by the server")
+	}
+	request("POST", "/students/me/profile", studentAToken, gin.H{"full_name": "Anita", "college_id": collegeB.ID}, 400)
+	request("DELETE", "/admin/colleges/"+collegeA.ID.String(), adminToken, nil, 409)
+	var storedCollege models.College
+	if err := db.First(&storedCollege, "id = ?", collegeB.ID).Error; err != nil || storedCollege.Name != "College B" {
+		t.Fatal("nested college input modified another college")
+	}
+
+	internship := models.Internship{IssuedBy: employer.ID, Title: "Backend Intern", Description: "Build useful APIs with Go", WorkMode: "remote", Status: enums.InternshipStatusPublished, IsActive: true}
+	if err := db.Omit("Issuer").Create(&internship).Error; err != nil {
+		t.Fatal(err)
+	}
+	legacyProfile := models.StudentProfile{UserID: legacyUser.ID, FullName: "Legacy student", CollegeName: "College A"}
+	create(&legacyProfile)
+	request("POST", "/internships/"+internship.ID.String()+"/apply", token(legacyUser), nil, 400)
+	var appA, appB models.InternshipApplication
+	decode(request("POST", "/internships/"+internship.ID.String()+"/apply", studentAToken, nil, 201), &appA)
+	decode(request("POST", "/internships/"+internship.ID.String()+"/apply", studentBToken, nil, 201), &appB)
+	request("POST", "/internships/"+internship.ID.String()+"/apply", studentAToken, nil, 409)
+	base := "/recruiters/applications/" + appA.ID.String()
+	read := "/internship-records/" + appA.ID.String()
+	request("PUT", base+"/status", strangerToken, gin.H{"status": "accepted"}, 403)
+	request("PUT", base+"/status", employerToken, gin.H{"status": "accepted", "employer_note": "private hiring discussion"}, 200)
+	var record services.InternshipRecord
+	raw := request("GET", read, collegeAToken, nil, 200)
+	decode(raw, &record)
+	if record.Status != "ongoing" || record.StartDate == nil || record.CompanyName != "Company X" || record.College.ID != collegeA.ID {
+		t.Fatalf("unexpected record: %s", raw)
+	}
+	if bytes.Contains(raw, []byte("private hiring discussion")) || bytes.Contains(raw, []byte("documents")) {
+		t.Fatal("private application data leaked to college")
+	}
+	request("GET", read, collegeBToken, nil, 404)
+	request("GET", read, studentBToken, nil, 404)
+	request("GET", read, strangerToken, nil, 404)
+	listCount("/colleges/me/internships", collegeAToken, 1, 1)
+	listCount("/colleges/me/internships?student_id="+profileB.ID.String(), collegeAToken, 0, 0)
+	listCount("/internship-records", adminToken, 2, 2)
+	listCount("/internship-records?student_name=ANITA&program=csit&semester=7&company=company&graduation_year=2027&status=ongoing", collegeAToken, 1, 1)
+	listCount("/internship-records?program=BIT", collegeAToken, 0, 0)
+	request("GET", "/internship-records?status=unknown", collegeAToken, nil, 400)
+	request("GET", "/internship-records?evaluation_status=unknown", collegeAToken, nil, 400)
+	request("GET", "/internship-records?certificate_status=unknown", collegeAToken, nil, 400)
+	request("GET", "/internship-records?student_id=bad", collegeAToken, nil, 400)
+
+	evaluation := gin.H{"scores": gin.H{"technical": 90, "communication": 85}, "remarks": "Delivered reliable work."}
+	request("POST", base+"/evaluation", collegeAToken, evaluation, 403)
+	request("POST", base+"/evaluation", strangerToken, evaluation, 404)
+	request("POST", base+"/evaluation", employerToken, evaluation, 409)
+	request("PUT", base+"/progress", employerToken, gin.H{"status": "completed", "end_date": time.Now().Add(-time.Hour)}, 400)
+	request("PUT", base+"/progress", employerToken, gin.H{"status": "completed", "end_date": time.Now().Add(time.Hour)}, 400)
+	request("PUT", base+"/progress", employerToken, gin.H{"status": "completed", "start_date": time.Now().AddDate(0, -3, 0), "end_date": time.Now().Add(-time.Hour)}, 200)
+	request("PUT", base+"/progress", employerToken, gin.H{"status": "ongoing"}, 409)
+	listCount("/internship-records?evaluation_status=pending&certificate_status=pending", collegeAToken, 1, 1)
+	var dashboard repositories.CollegeDashboard
+	decode(request("GET", "/colleges/me/dashboard", collegeAToken, nil, 200), &dashboard)
+	if dashboard.TotalStudents != 1 || dashboard.CompletedInternships != 1 || dashboard.PendingEvaluations != 1 || dashboard.PendingCertificates != 1 {
+		t.Fatalf("bad dashboard: %+v", dashboard)
+	}
+	pdf := []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\n%%EOF")
+	request("POST", base+"/certificate", employerToken, pdf, 409)
+	request("POST", base+"/evaluation", employerToken, gin.H{"scores": gin.H{"technical": 101}, "remarks": "Bad score"}, 400)
+	request("POST", base+"/evaluation", employerToken, evaluation, 201)
+	request("POST", base+"/evaluation", employerToken, evaluation, 409)
+	request("POST", base+"/certificate", collegeAToken, pdf, 403)
+	request("POST", base+"/certificate", strangerToken, pdf, 404)
+	request("POST", base+"/certificate", employerToken, []byte("not a PDF"), 400)
+	request("POST", base+"/certificate", employerToken, pdf, 201)
+	request("POST", base+"/certificate", employerToken, pdf, 409)
+	if puts != 1 {
+		t.Fatalf("expected one PDF upload, got %d", puts)
+	}
+	for _, accessToken := range []string{collegeAToken, studentAToken, employerToken, adminToken} {
+		raw := request("GET", read, accessToken, nil, 200)
+		decode(raw, &record)
+		if record.EvaluationStatus != "submitted" || record.CertificateStatus != "issued" || record.Evaluation.Scores["technical"] != 90 {
+			t.Fatalf("outcomes unavailable: %s", raw)
+		}
+		if bytes.Contains(raw, []byte("object_key")) {
+			t.Fatal("private storage key exposed")
+		}
+		var link struct {
+			URL       string `json:"url"`
+			ExpiresIn int    `json:"expires_in"`
+		}
+		decode(request("GET", read+"/certificate?download=true", accessToken, nil, 200), &link)
+		parsed, err := url.Parse(link.URL)
+		if err != nil || link.ExpiresIn != 300 || !strings.HasPrefix(parsed.Query().Get("response-content-disposition"), "attachment;") {
+			t.Fatalf("bad download: %+v", link)
+		}
+	}
+	request("GET", read+"/certificate", collegeBToken, nil, 404)
+	request("GET", read+"/certificate", studentBToken, nil, 404)
+	request("GET", read+"/certificate", strangerToken, nil, 404)
+	listCount("/internship-records?evaluation_status=submitted&certificate_status=issued", collegeAToken, 1, 1)
+	decode(request("GET", "/colleges/me/dashboard", collegeAToken, nil, 200), &dashboard)
+	if dashboard.EvaluationsReceived != 1 || dashboard.CertificatesReceived != 1 || dashboard.PendingEvaluations != 0 || dashboard.PendingCertificates != 0 {
+		t.Fatalf("bad completed metrics: %+v", dashboard)
+	}
+	decode(request("GET", "/colleges/me/dashboard", collegeBToken, nil, 200), &dashboard)
+	if dashboard.TotalStudents != 1 || dashboard.CompletedInternships != 0 || dashboard.EvaluationsReceived != 0 || dashboard.CertificatesReceived != 0 {
+		t.Fatalf("cross-college metrics leak: %+v", dashboard)
+	}
+
+	// A second application for the same student must not inflate the student count.
+	secondInternship := internship
+	secondInternship.ID = uuid.New()
+	if err := db.Omit("Issuer").Create(&secondInternship).Error; err != nil {
+		t.Fatal(err)
+	}
+	request("POST", "/internships/"+secondInternship.ID.String()+"/apply", studentAToken, nil, 201)
+	listCount("/colleges/me/internships?page_size=1", collegeAToken, 2, 1)
+	listCount("/colleges/me/internships?page_size=1&page=2", collegeAToken, 2, 1)
+	listCount("/colleges/me/internships?page_size=1&page=3", collegeAToken, 2, 0)
+	decode(request("GET", "/colleges/me/dashboard", collegeAToken, nil, 200), &dashboard)
+	if dashboard.TotalStudents != 1 {
+		t.Fatal("student count was inflated by multiple applications")
+	}
+
+	// Account reassignment is effective even with the old access token.
+	request("PUT", "/admin/users/"+coordinatorA.ID.String()+"/college", adminToken, gin.H{"college_id": collegeB.ID}, 200)
+	request("GET", read, collegeAToken, nil, 404)
+	request("GET", "/internship-records/"+appB.ID.String(), collegeAToken, nil, 200)
+	request("PUT", "/admin/colleges/"+collegeB.ID.String(), adminToken, gin.H{"name": collegeB.Name, "university": collegeB.University, "status": "inactive"}, 200)
+	request("GET", "/colleges/me/dashboard", collegeBToken, nil, 403)
+	request("GET", "/internship-records", collegeBToken, nil, 403)
+	request("GET", "/internship-records/"+appB.ID.String()+"/certificate", collegeBToken, nil, 403)
+	request("POST", "/internships/"+secondInternship.ID.String()+"/apply", studentBToken, nil, 400)
+	var publicColleges []models.College
+	decode(request("GET", "/colleges", "", nil, 200), &publicColleges)
+	if len(publicColleges) != 1 || publicColleges[0].ID != collegeA.ID {
+		t.Fatal("inactive college appeared in selection list")
+	}
+	request("PUT", "/admin/students/"+legacyProfile.ID.String()+"/college", adminToken, gin.H{"college_id": collegeA.ID}, 200)
+	request("POST", "/internships/"+internship.ID.String()+"/apply", token(legacyUser), nil, 201)
+	request("PUT", "/admin/users/"+coordinatorA.ID.String()+"/college", adminToken, gin.H{"college_id": collegeA.ID}, 200)
+
+	// Existing accepted/rejected/withdrawn rows receive progress defaults, while
+	// a free-text college name never grants a college access to historical data.
+	historicalUser := user(models.RoleStudent)
+	historicalStudent := models.StudentProfile{UserID: historicalUser.ID, FullName: "Historical student", CollegeName: collegeA.Name}
+	create(&historicalStudent)
+	var historicalApps []models.InternshipApplication
+	for _, status := range []models.InternshipApplicationStatus{models.ApplicationStatusAccepted, models.ApplicationStatusRejected, models.ApplicationStatusWithdrawn} {
+		posting := internship
+		posting.ID = uuid.New()
+		if err := db.Omit("Issuer").Create(&posting).Error; err != nil {
+			t.Fatal(err)
+		}
+		app := models.InternshipApplication{InternshipID: posting.ID, StudentID: historicalStudent.ID, Status: status, AppliedAt: time.Now().AddDate(0, -1, 0)}
+		create(&app)
+		historicalApps = append(historicalApps, app)
+	}
+
+	// Restart migrations preserve outcomes and require no guessed legacy affiliation.
+	// The installed SQLite driver rebuilds existing tables during AutoMigrate.
+	// Suspend FK enforcement only for that rebuild, then verify every FK below.
+	if db.Dialector.Name() == "sqlite" {
+		if err := db.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
+			t.Fatal(err)
+		}
+	}
+	if err := database.AutoMigrate(db); err != nil {
+		t.Fatalf("repeat migration: %v", err)
+	}
+	if db.Dialector.Name() == "sqlite" {
+		if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
+			t.Fatal(err)
+		}
+		rows, err := db.Raw("PRAGMA foreign_key_check").Rows()
+		if err != nil {
+			t.Fatal(err)
+		}
+		defer rows.Close()
+		if rows.Next() {
+			t.Fatal("foreign key violation after migration")
+		}
+		if err := rows.Err(); err != nil {
+			t.Fatal(err)
+		}
+		rows.Close()
+	}
+	decode(request("GET", read, studentAToken, nil, 200), &record)
+	if record.Status != "completed" || record.Certificate == nil {
+		t.Fatal("migration reset completed internship")
+	}
+	for _, app := range historicalApps {
+		decode(request("GET", "/internship-records/"+app.ID.String(), adminToken, nil, 200), &record)
+		if record.College != nil {
+			t.Fatal("migration guessed a historical student's college")
+		}
+		if app.Status == models.ApplicationStatusAccepted {
+			if record.Status != "ongoing" || record.StartDate == nil || !record.StartDate.Equal(record.AppliedAt) {
+				t.Fatal("migration did not backfill accepted application progress")
+			}
+		} else if record.Status != "cancelled" {
+			t.Fatal("migration did not backfill cancelled application progress")
+		}
+		request("GET", "/internship-records/"+app.ID.String(), collegeAToken, nil, 404)
+	}
+}
diff --git a/internal/routes/college_test.go b/internal/routes/college_test.go
deleted file mode 100644
index e1e9804..0000000
--- a/internal/routes/college_test.go
+++ /dev/null
@@ -1,410 +0,0 @@
-package routes_test
-
-import (
-	"bytes"
-	"encoding/json"
-	"fmt"
-	"io"
-	"mime/multipart"
-	"net/http"
-	"net/http/httptest"
-	"net/url"
-	"os"
-	"strings"
-	"testing"
-	"time"
-
-	"github.com/gin-gonic/gin"
-	"github.com/google/uuid"
-	"github.com/minio/minio-go/v7"
-	"github.com/minio/minio-go/v7/pkg/credentials"
-	"github.com/prasanna/student-job-portal/backend/internal/config"
-	"github.com/prasanna/student-job-portal/backend/internal/database"
-	"github.com/prasanna/student-job-portal/backend/internal/enums"
-	"github.com/prasanna/student-job-portal/backend/internal/logger"
-	"github.com/prasanna/student-job-portal/backend/internal/models"
-	"github.com/prasanna/student-job-portal/backend/internal/repositories"
-	"github.com/prasanna/student-job-portal/backend/internal/responses"
-	"github.com/prasanna/student-job-portal/backend/internal/routes"
-	"github.com/prasanna/student-job-portal/backend/internal/services"
-	"gorm.io/driver/postgres"
-	"gorm.io/driver/sqlite"
-	"gorm.io/gorm"
-	gormlogger "gorm.io/gorm/logger"
-)
-
-type certificateTransport func(*http.Request) (*http.Response, error)
-
-func (f certificateTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
-
-// Exercise real routes, JWT middleware, services, repositories and FK constraints.
-// Only the external object store is replaced with an in-memory HTTP transport.
-func TestCollegeInternshipWorkflow(t *testing.T) {
-	gin.SetMode(gin.TestMode)
-	var dialector gorm.Dialector = sqlite.Open("file:" + uuid.NewString() + "?mode=memory&cache=shared&_foreign_keys=on")
-	if dsn := os.Getenv("COLLEGE_TEST_DATABASE_URL"); dsn != "" {
-		dialector = postgres.Open(dsn)
-	}
-	db, err := gorm.Open(dialector, &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
-	if err != nil {
-		t.Fatal(err)
-	}
-	sqlDB, err := db.DB()
-	if err != nil {
-		t.Fatal(err)
-	}
-	t.Cleanup(func() { sqlDB.Close() })
-	sqlDB.SetMaxOpenConns(1)
-	if err := database.AutoMigrate(db); err != nil {
-		t.Fatal(err)
-	}
-	cfg := &config.Config{}
-	cfg.JWT.AccessSecret, cfg.JWT.AccessExpiry = "college-integration-test-secret", time.Hour
-	cfg.JWT.RefreshExpiry = time.Hour
-	cfg.MinIO.CompanyDocBucket = "company-documents"
-	log := logger.New("error")
-	puts := 0
-	storage, err := minio.New("storage.example.test", &minio.Options{Secure: true, Region: "us-east-1", Creds: credentials.NewStaticV4("test-key", "test-secret", ""),
-		Transport: certificateTransport(func(req *http.Request) (*http.Response, error) {
-			if req.Method != http.MethodPut {
-				return nil, fmt.Errorf("unexpected storage request: %s %s", req.Method, req.URL)
-			}
-			puts++
-			if _, err := io.Copy(io.Discard, req.Body); err != nil {
-				return nil, err
-			}
-			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Etag": {"test-etag"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
-		})})
-	if err != nil {
-		t.Fatal(err)
-	}
-	router := gin.New()
-	routes.Register(router, db, cfg, log, storage)
-	auth := services.NewAuthService(repositories.NewUserRepository(db), cfg, log)
-
-	create := func(record interface{}) {
-		t.Helper()
-		if err := db.Create(record).Error; err != nil {
-			t.Fatal(err)
-		}
-	}
-	user := func(role models.Role) models.User {
-		t.Helper()
-		u := models.User{ID: uuid.New(), Email: uuid.NewString() + "@example.test", Role: role, PasswordHash: "unused", FullName: string(role)}
-		create(&u)
-		return u
-	}
-	token := func(u models.User) string {
-		t.Helper()
-		signed, err := auth.GenerateAccessToken(&u)
-		if err != nil {
-			t.Fatal(err)
-		}
-		return signed
-	}
-	admin, employer, stranger := user(models.RoleAdmin), user(models.RoleEmployer), user(models.RoleEmployer)
-	adminToken, employerToken, strangerToken := token(admin), token(employer), token(stranger)
-	create(&models.RecruiterProfile{UserID: employer.ID, OrganizationName: "Company X", VerificationStatus: "approved"})
-	studentA, studentB, legacyUser := user(models.RoleStudent), user(models.RoleStudent), user(models.RoleStudent)
-	studentAToken, studentBToken := token(studentA), token(studentB)
-
-	request := func(method, path, authToken string, payload interface{}, expected int) json.RawMessage {
-		t.Helper()
-		var body io.Reader
-		contentType := "application/json"
-		if pdf, ok := payload.([]byte); ok {
-			var b bytes.Buffer
-			w := multipart.NewWriter(&b)
-			part, err := w.CreateFormFile("file", "certificate.pdf")
-			if err != nil {
-				t.Fatal(err)
-			}
-			if _, err := part.Write(pdf); err != nil {
-				t.Fatal(err)
-			}
-			if err := w.Close(); err != nil {
-				t.Fatal(err)
-			}
-			contentType, body = w.FormDataContentType(), &b
-		} else if payload != nil {
-			b, err := json.Marshal(payload)
-			if err != nil {
-				t.Fatal(err)
-			}
-			body = bytes.NewReader(b)
-		}
-		req := httptest.NewRequest(method, "/api/v1"+path, body)
-		req.Header.Set("Content-Type", contentType)
-		if authToken != "" {
-			req.Header.Set("Authorization", "Bearer "+authToken)
-		}
-		w := httptest.NewRecorder()
-		router.ServeHTTP(w, req)
-		if w.Code != expected {
-			t.Fatalf("%s %s: status %d, want %d; %s", method, path, w.Code, expected, w.Body.String())
-		}
-		return json.RawMessage(w.Body.Bytes())
-	}
-	decode := func(raw json.RawMessage, value interface{}) {
-		t.Helper()
-		var envelope struct {
-			Data json.RawMessage `json:"data"`
-		}
-		if err := json.Unmarshal(raw, &envelope); err != nil {
-			t.Fatal(err)
-		}
-		if err := json.Unmarshal(envelope.Data, value); err != nil {
-			t.Fatalf("decode %s: %v", raw, err)
-		}
-	}
-	listCount := func(path, authToken string, total, size int) []services.InternshipRecord {
-		t.Helper()
-		raw := request("GET", path, authToken, nil, 200)
-		var envelope struct {
-			Data       []services.InternshipRecord `json:"data"`
-			Pagination responses.Pagination        `json:"pagination"`
-		}
-		if err := json.Unmarshal(raw, &envelope); err != nil {
-			t.Fatal(err)
-		}
-		if envelope.Pagination.TotalItems != int64(total) || len(envelope.Data) != size {
-			t.Fatalf("unexpected list: %s", raw)
-		}
-		return envelope.Data
-	}
-
-	request("GET", "/colleges/me/dashboard", "", nil, 401)
-	request("POST", "/admin/colleges", studentAToken, nil, 403)
-	request("POST", "/auth/register/college", "", nil, 404)
-	request("POST", "/admin/colleges", adminToken, gin.H{"name": "Bad", "university": "UNSUPPORTED"}, 400)
-	var universities []interface{}
-	decode(request("GET", "/universities", "", nil, 200), &universities)
-	if len(universities) != 4 {
-		t.Fatal("expected four fixed universities")
-	}
-	var collegeA, collegeB models.College
-	decode(request("POST", "/admin/colleges", adminToken, gin.H{"name": "College A", "university": models.TribhuvanUniversity}, 201), &collegeA)
-	decode(request("POST", "/admin/colleges", adminToken, gin.H{"name": "College B", "university": models.TribhuvanUniversity}, 201), &collegeB)
-	var coordinatorA, coordinatorB models.User
-	decode(request("POST", "/admin/colleges/"+collegeA.ID.String()+"/users", adminToken, gin.H{"email": "college-a@example.test", "full_name": "Coordinator A", "password": "Password123!"}, 201), &coordinatorA)
-	decode(request("POST", "/admin/colleges/"+collegeB.ID.String()+"/users", adminToken, gin.H{"email": "college-b@example.test", "full_name": "Coordinator B", "password": "Password123!"}, 201), &coordinatorB)
-	collegeAToken, collegeBToken := token(coordinatorA), token(coordinatorB)
-	request("POST", "/admin/colleges/"+collegeA.ID.String()+"/users", adminToken, gin.H{"email": "college-a@example.test", "full_name": "Duplicate", "password": "Password123!"}, 409)
-	login := request("POST", "/auth/login", "", gin.H{"email": "college-a@example.test", "password": "Password123!"}, 200)
-	if bytes.Contains(login, []byte("password")) {
-		t.Fatal("password leaked")
-	}
-	var me models.User
-	decode(request("GET", "/auth/me", collegeAToken, nil, 200), &me)
-	if me.Role != models.RoleCollege || me.CollegeID == nil || *me.CollegeID != collegeA.ID {
-		t.Fatal("college authentication metadata missing")
-	}
-
-	request("POST", "/students/me/profile", studentAToken, gin.H{"full_name": "Anita"}, 400)
-	var profileA, profileB models.StudentProfile
-	decode(request("POST", "/students/me/profile", studentAToken, gin.H{"full_name": "Anita", "college_id": collegeA.ID, "degree": "BSc CSIT", "current_semester": "7", "graduation_year": 2027,
-		"college": gin.H{"id": collegeB.ID, "name": "Tampered"}, "documents": []gin.H{{"object_key": "forged"}}}, 200), &profileA)
-	decode(request("POST", "/students/me/profile", studentBToken, gin.H{"full_name": "Bina", "college_id": collegeB.ID, "degree": "BIT", "current_semester": "6"}, 200), &profileB)
-	if profileA.College == nil || profileA.College.Name != "College A" {
-		t.Fatal("college association was not resolved by the server")
-	}
-	request("POST", "/students/me/profile", studentAToken, gin.H{"full_name": "Anita", "college_id": collegeB.ID}, 400)
-	request("DELETE", "/admin/colleges/"+collegeA.ID.String(), adminToken, nil, 409)
-	var storedCollege models.College
-	if err := db.First(&storedCollege, "id = ?", collegeB.ID).Error; err != nil || storedCollege.Name != "College B" {
-		t.Fatal("nested college input modified another college")
-	}
-
-	internship := models.Internship{IssuedBy: employer.ID, Title: "Backend Intern", Description: "Build useful APIs with Go", WorkMode: "remote", Status: enums.InternshipStatusPublished, IsActive: true}
-	if err := db.Omit("Issuer").Create(&internship).Error; err != nil {
-		t.Fatal(err)
-	}
-	legacyProfile := models.StudentProfile{UserID: legacyUser.ID, FullName: "Legacy student", CollegeName: "College A"}
-	create(&legacyProfile)
-	request("POST", "/internships/"+internship.ID.String()+"/apply", token(legacyUser), nil, 400)
-	var appA, appB models.InternshipApplication
-	decode(request("POST", "/internships/"+internship.ID.String()+"/apply", studentAToken, nil, 201), &appA)
-	decode(request("POST", "/internships/"+internship.ID.String()+"/apply", studentBToken, nil, 201), &appB)
-	request("POST", "/internships/"+internship.ID.String()+"/apply", studentAToken, nil, 409)
-	base := "/recruiters/applications/" + appA.ID.String()
-	read := "/internship-records/" + appA.ID.String()
-	request("PUT", base+"/status", strangerToken, gin.H{"status": "accepted"}, 403)
-	request("PUT", base+"/status", employerToken, gin.H{"status": "accepted", "employer_note": "private hiring discussion"}, 200)
-	var record services.InternshipRecord
-	raw := request("GET", read, collegeAToken, nil, 200)
-	decode(raw, &record)
-	if record.Status != "ongoing" || record.StartDate == nil || record.CompanyName != "Company X" || record.College.ID != collegeA.ID {
-		t.Fatalf("unexpected record: %s", raw)
-	}
-	if bytes.Contains(raw, []byte("private hiring discussion")) || bytes.Contains(raw, []byte("documents")) {
-		t.Fatal("private application data leaked to college")
-	}
-	request("GET", read, collegeBToken, nil, 404)
-	request("GET", read, studentBToken, nil, 404)
-	request("GET", read, strangerToken, nil, 404)
-	listCount("/colleges/me/internships", collegeAToken, 1, 1)
-	listCount("/colleges/me/internships?student_id="+profileB.ID.String(), collegeAToken, 0, 0)
-	listCount("/internship-records", adminToken, 2, 2)
-	listCount("/internship-records?student_name=ANITA&program=csit&semester=7&company=company&graduation_year=2027&status=ongoing", collegeAToken, 1, 1)
-	listCount("/internship-records?program=BIT", collegeAToken, 0, 0)
-	request("GET", "/internship-records?status=unknown", collegeAToken, nil, 400)
-	request("GET", "/internship-records?evaluation_status=unknown", collegeAToken, nil, 400)
-	request("GET", "/internship-records?certificate_status=unknown", collegeAToken, nil, 400)
-	request("GET", "/internship-records?student_id=bad", collegeAToken, nil, 400)
-
-	evaluation := gin.H{"scores": gin.H{"technical": 90, "communication": 85}, "remarks": "Delivered reliable work."}
-	request("POST", base+"/evaluation", collegeAToken, evaluation, 403)
-	request("POST", base+"/evaluation", strangerToken, evaluation, 404)
-	request("POST", base+"/evaluation", employerToken, evaluation, 409)
-	request("PUT", base+"/progress", employerToken, gin.H{"status": "completed", "end_date": time.Now().Add(-time.Hour)}, 400)
-	request("PUT", base+"/progress", employerToken, gin.H{"status": "completed", "end_date": time.Now().Add(time.Hour)}, 400)
-	request("PUT", base+"/progress", employerToken, gin.H{"status": "completed", "start_date": time.Now().AddDate(0, -3, 0), "end_date": time.Now().Add(-time.Hour)}, 200)
-	request("PUT", base+"/progress", employerToken, gin.H{"status": "ongoing"}, 409)
-	listCount("/internship-records?evaluation_status=pending&certificate_status=pending", collegeAToken, 1, 1)
-	var dashboard repositories.CollegeDashboard
-	decode(request("GET", "/colleges/me/dashboard", collegeAToken, nil, 200), &dashboard)
-	if dashboard.TotalStudents != 1 || dashboard.CompletedInternships != 1 || dashboard.PendingEvaluations != 1 || dashboard.PendingCertificates != 1 {
-		t.Fatalf("bad dashboard: %+v", dashboard)
-	}
-	pdf := []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\n%%EOF")
-	request("POST", base+"/certificate", employerToken, pdf, 409)
-	request("POST", base+"/evaluation", employerToken, gin.H{"scores": gin.H{"technical": 101}, "remarks": "Bad score"}, 400)
-	request("POST", base+"/evaluation", employerToken, evaluation, 201)
-	request("POST", base+"/evaluation", employerToken, evaluation, 409)
-	request("POST", base+"/certificate", collegeAToken, pdf, 403)
-	request("POST", base+"/certificate", strangerToken, pdf, 404)
-	request("POST", base+"/certificate", employerToken, []byte("not a PDF"), 400)
-	request("POST", base+"/certificate", employerToken, pdf, 201)
-	request("POST", base+"/certificate", employerToken, pdf, 409)
-	if puts != 1 {
-		t.Fatalf("expected one PDF upload, got %d", puts)
-	}
-	for _, accessToken := range []string{collegeAToken, studentAToken, employerToken, adminToken} {
-		raw := request("GET", read, accessToken, nil, 200)
-		decode(raw, &record)
-		if record.EvaluationStatus != "submitted" || record.CertificateStatus != "issued" || record.Evaluation.Scores["technical"] != 90 {
-			t.Fatalf("outcomes unavailable: %s", raw)
-		}
-		if bytes.Contains(raw, []byte("object_key")) {
-			t.Fatal("private storage key exposed")
-		}
-		var link struct {
-			URL       string `json:"url"`
-			ExpiresIn int    `json:"expires_in"`
-		}
-		decode(request("GET", read+"/certificate?download=true", accessToken, nil, 200), &link)
-		parsed, err := url.Parse(link.URL)
-		if err != nil || link.ExpiresIn != 300 || !strings.HasPrefix(parsed.Query().Get("response-content-disposition"), "attachment;") {
-			t.Fatalf("bad download: %+v", link)
-		}
-	}
-	request("GET", read+"/certificate", collegeBToken, nil, 404)
-	request("GET", read+"/certificate", studentBToken, nil, 404)
-	request("GET", read+"/certificate", strangerToken, nil, 404)
-	listCount("/internship-records?evaluation_status=submitted&certificate_status=issued", collegeAToken, 1, 1)
-	decode(request("GET", "/colleges/me/dashboard", collegeAToken, nil, 200), &dashboard)
-	if dashboard.EvaluationsReceived != 1 || dashboard.CertificatesReceived != 1 || dashboard.PendingEvaluations != 0 || dashboard.PendingCertificates != 0 {
-		t.Fatalf("bad completed metrics: %+v", dashboard)
-	}
-	decode(request("GET", "/colleges/me/dashboard", collegeBToken, nil, 200), &dashboard)
-	if dashboard.TotalStudents != 1 || dashboard.CompletedInternships != 0 || dashboard.EvaluationsReceived != 0 || dashboard.CertificatesReceived != 0 {
-		t.Fatalf("cross-college metrics leak: %+v", dashboard)
-	}
-
-	// A second application for the same student must not inflate the student count.
-	secondInternship := internship
-	secondInternship.ID = uuid.New()
-	if err := db.Omit("Issuer").Create(&secondInternship).Error; err != nil {
-		t.Fatal(err)
-	}
-	request("POST", "/internships/"+secondInternship.ID.String()+"/apply", studentAToken, nil, 201)
-	listCount("/colleges/me/internships?page_size=1", collegeAToken, 2, 1)
-	listCount("/colleges/me/internships?page_size=1&page=2", collegeAToken, 2, 1)
-	listCount("/colleges/me/internships?page_size=1&page=3", collegeAToken, 2, 0)
-	decode(request("GET", "/colleges/me/dashboard", collegeAToken, nil, 200), &dashboard)
-	if dashboard.TotalStudents != 1 {
-		t.Fatal("student count was inflated by multiple applications")
-	}
-
-	// Account reassignment is effective even with the old access token.
-	request("PUT", "/admin/users/"+coordinatorA.ID.String()+"/college", adminToken, gin.H{"college_id": collegeB.ID}, 200)
-	request("GET", read, collegeAToken, nil, 404)
-	request("GET", "/internship-records/"+appB.ID.String(), collegeAToken, nil, 200)
-	request("PUT", "/admin/colleges/"+collegeB.ID.String(), adminToken, gin.H{"name": collegeB.Name, "university": collegeB.University, "status": "inactive"}, 200)
-	request("GET", "/colleges/me/dashboard", collegeBToken, nil, 403)
-	request("GET", "/internship-records", collegeBToken, nil, 403)
-	request("GET", "/internship-records/"+appB.ID.String()+"/certificate", collegeBToken, nil, 403)
-	request("POST", "/internships/"+secondInternship.ID.String()+"/apply", studentBToken, nil, 400)
-	var publicColleges []models.College
-	decode(request("GET", "/colleges", "", nil, 200), &publicColleges)
-	if len(publicColleges) != 1 || publicColleges[0].ID != collegeA.ID {
-		t.Fatal("inactive college appeared in selection list")
-	}
-	request("PUT", "/admin/students/"+legacyProfile.ID.String()+"/college", adminToken, gin.H{"college_id": collegeA.ID}, 200)
-	request("POST", "/internships/"+internship.ID.String()+"/apply", token(legacyUser), nil, 201)
-	request("PUT", "/admin/users/"+coordinatorA.ID.String()+"/college", adminToken, gin.H{"college_id": collegeA.ID}, 200)
-
-	// Existing accepted/rejected/withdrawn rows receive progress defaults, while
-	// a free-text college name never grants a college access to historical data.
-	historicalUser := user(models.RoleStudent)
-	historicalStudent := models.StudentProfile{UserID: historicalUser.ID, FullName: "Historical student", CollegeName: collegeA.Name}
-	create(&historicalStudent)
-	var historicalApps []models.InternshipApplication
-	for _, status := range []models.InternshipApplicationStatus{models.ApplicationStatusAccepted, models.ApplicationStatusRejected, models.ApplicationStatusWithdrawn} {
-		posting := internship
-		posting.ID = uuid.New()
-		if err := db.Omit("Issuer").Create(&posting).Error; err != nil {
-			t.Fatal(err)
-		}
-		app := models.InternshipApplication{InternshipID: posting.ID, StudentID: historicalStudent.ID, Status: status, AppliedAt: time.Now().AddDate(0, -1, 0)}
-		create(&app)
-		historicalApps = append(historicalApps, app)
-	}
-
-	// Restart migrations preserve outcomes and require no guessed legacy affiliation.
-	// The installed SQLite driver rebuilds existing tables during AutoMigrate.
-	// Suspend FK enforcement only for that rebuild, then verify every FK below.
-	if db.Dialector.Name() == "sqlite" {
-		if err := db.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
-			t.Fatal(err)
-		}
-	}
-	if err := database.AutoMigrate(db); err != nil {
-		t.Fatalf("repeat migration: %v", err)
-	}
-	if db.Dialector.Name() == "sqlite" {
-		if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
-			t.Fatal(err)
-		}
-		rows, err := db.Raw("PRAGMA foreign_key_check").Rows()
-		if err != nil {
-			t.Fatal(err)
-		}
-		defer rows.Close()
-		if rows.Next() {
-			t.Fatal("foreign key violation after migration")
-		}
-		if err := rows.Err(); err != nil {
-			t.Fatal(err)
-		}
-		rows.Close()
-	}
-	decode(request("GET", read, studentAToken, nil, 200), &record)
-	if record.Status != "completed" || record.Certificate == nil {
-		t.Fatal("migration reset completed internship")
-	}
-	for _, app := range historicalApps {
-		decode(request("GET", "/internship-records/"+app.ID.String(), adminToken, nil, 200), &record)
-		if record.College != nil {
-			t.Fatal("migration guessed a historical student's college")
-		}
-		if app.Status == models.ApplicationStatusAccepted {
-			if record.Status != "ongoing" || record.StartDate == nil || !record.StartDate.Equal(record.AppliedAt) {
-				t.Fatal("migration did not backfill accepted application progress")
-			}
-		} else if record.Status != "cancelled" {
-			t.Fatal("migration did not backfill cancelled application progress")
-		}
-		request("GET", "/internship-records/"+app.ID.String(), collegeAToken, nil, 404)
-	}
-}

# internal/routes/routes.go

diff --git a/internal/routes/routes.go b/internal/routes/routes.go
index 895c4c3..a32e5e6 100644
--- a/internal/routes/routes.go
+++ b/internal/routes/routes.go
@@ -23,6 +23,8 @@ import (
 func Register(router *gin.Engine, db *gorm.DB, cfg *config.Config, log *logger.Logger, minioClient *minio.Client) {
 	userRepo := repositories.NewUserRepository(db)
 	studentRepo := repositories.NewStudentRepository(db)
+	collegeRepo := repositories.NewCollegeRepository(db)
+	recordRepo := repositories.NewInternshipRecordRepository(db)
 	recruiterProfileRepo := repositories.NewRecruiterProfileRepository(db)
 	organizationVerificationRepo := repositories.NewOrganizationVerificationRepository(db)
 	internshipRepo := repositories.NewInternshipRepository(db)
@@ -30,7 +32,9 @@ func Register(router *gin.Engine, db *gorm.DB, cfg *config.Config, log *logger.L
 	interviewRepo := repositories.NewInterviewRepository(db)
 
 	authSvc := services.NewAuthService(userRepo, cfg, log)
-	studentSvc := services.NewStudentService(studentRepo, minioClient, cfg, log)
+	studentSvc := services.NewStudentService(studentRepo, collegeRepo, minioClient, cfg, log)
+	collegeSvc := services.NewCollegeService(collegeRepo, userRepo, studentRepo, authSvc)
+	recordSvc := services.NewInternshipRecordService(recordRepo, userRepo, collegeSvc, minioClient, cfg.MinIO.CompanyDocBucket)
 	appSvc := services.NewInternshipApplicationService(appRepo, internshipRepo, studentRepo, minioClient, cfg.MinIO.StudentDocBucket)
 	internshipSvc := services.NewInternshipService(internshipRepo, recruiterProfileRepo)
 	interviewSvc := services.NewInterviewService(interviewRepo)
@@ -38,6 +42,8 @@ func Register(router *gin.Engine, db *gorm.DB, cfg *config.Config, log *logger.L
 	organizationVerificationSvc := services.NewOrganizationVerificationService(organizationVerificationRepo, recruiterProfileRepo, minioClient, cfg.MinIO.CompanyDocBucket)
 
 	authHandler := handlers.NewAuthHandler(authSvc, log)
+	collegeHandler := handlers.NewCollegeHandler(collegeSvc)
+	recordHandler := handlers.NewInternshipRecordHandler(recordSvc)
 	adminHandler := handlers.NewAdminHandler(userRepo, log)
 	studentHandler := handlers.NewStudentHandler(studentSvc, studentRepo, cfg, log)
 	recruiterProfileHandler := handlers.NewRecruiterProfileHandler(recruiterProfileSvc, log)
@@ -50,6 +56,20 @@ func Register(router *gin.Engine, db *gorm.DB, cfg *config.Config, log *logger.L
 
 	v1 := router.Group("/api/v1")
 	v1.GET("/health", healthCheck)
+	v1.GET("/universities", collegeHandler.Universities)
+	v1.GET("/colleges", collegeHandler.List(true))
+
+	collegeGroup := v1.Group("/colleges/me", middleware.JWTAuthMiddleware(cfg, log), middleware.RequireRoles(models.RoleCollege))
+	collegeGroup.GET("", collegeHandler.Me)
+	collegeGroup.GET("/dashboard", recordHandler.Dashboard)
+	collegeGroup.GET("/internships", recordHandler.List)
+	collegeGroup.GET("/internships/:id", recordHandler.Get)
+	collegeGroup.GET("/internships/:id/certificate", recordHandler.CertificateURL)
+
+	records := v1.Group("/internship-records", middleware.JWTAuthMiddleware(cfg, log), middleware.RequireRoles(models.RoleStudent, models.RoleEmployer, models.RoleCollege, models.RoleAdmin))
+	records.GET("", recordHandler.List)
+	records.GET("/:id", recordHandler.Get)
+	records.GET("/:id/certificate", recordHandler.CertificateURL)
 
 	authGroup := v1.Group("/auth")
 	authGroup.Use(middleware.RateLimitMiddleware())
@@ -63,6 +83,16 @@ func Register(router *gin.Engine, db *gorm.DB, cfg *config.Config, log *logger.L
 	adminGroup := v1.Group("/admin")
 	adminGroup.Use(middleware.JWTAuthMiddleware(cfg, log), middleware.RequireRoles(models.RoleAdmin))
 	adminGroup.GET("/users", adminHandler.ListUsers)
+	adminGroup.GET("/colleges", collegeHandler.List(false))
+	adminGroup.POST("/colleges", collegeHandler.Save)
+	adminGroup.GET("/colleges/:id", collegeHandler.Get)
+	adminGroup.PUT("/colleges/:id", collegeHandler.Save)
+	adminGroup.DELETE("/colleges/:id", collegeHandler.Delete)
+	adminGroup.GET("/colleges/:id/users", collegeHandler.ListUsers)
+	adminGroup.POST("/colleges/:id/users", collegeHandler.CreateUser)
+	adminGroup.PUT("/users/:id/college", collegeHandler.Assign(false))
+	adminGroup.PUT("/students/:id/college", collegeHandler.Assign(true))
+	adminGroup.PUT("/internship-records/:id/progress", recordHandler.UpdateProgress)
 	adminGroup.GET("/organization-verifications", organizationVerificationHandler.List)
 	adminGroup.GET("/organization-verifications/:id", organizationVerificationHandler.GetByID)
 	adminGroup.PUT("/organization-verifications/:id/review", organizationVerificationHandler.Review)
@@ -81,6 +111,9 @@ func Register(router *gin.Engine, db *gorm.DB, cfg *config.Config, log *logger.L
 	recruiterGroup.GET("/applications", appHandler.ListForRecruiter)
 	recruiterGroup.GET("/applications/:id", appHandler.GetForRecruiter)
 	recruiterGroup.PUT("/applications/:id/status", appHandler.UpdateStatus)
+	recruiterGroup.PUT("/applications/:id/progress", recordHandler.UpdateProgress)
+	recruiterGroup.POST("/applications/:id/evaluation", recordHandler.SubmitEvaluation)
+	recruiterGroup.POST("/applications/:id/certificate", recordHandler.IssueCertificate)
 	interviews := recruiterGroup.Group("/interviews")
 	interviews.POST("", interviewHandler.Schedule)
 	interviews.GET("", interviewHandler.ListEmployer)
diff --git a/internal/routes/routes.go b/internal/routes/routes.go
index a32e5e6..844d4f8 100644
--- a/internal/routes/routes.go
+++ b/internal/routes/routes.go
@@ -23,79 +23,37 @@ import (
 func Register(router *gin.Engine, db *gorm.DB, cfg *config.Config, log *logger.Logger, minioClient *minio.Client) {
 	userRepo := repositories.NewUserRepository(db)
 	studentRepo := repositories.NewStudentRepository(db)
-	collegeRepo := repositories.NewCollegeRepository(db)
-	recordRepo := repositories.NewInternshipRecordRepository(db)
 	recruiterProfileRepo := repositories.NewRecruiterProfileRepository(db)
-	organizationVerificationRepo := repositories.NewOrganizationVerificationRepository(db)
 	internshipRepo := repositories.NewInternshipRepository(db)
 	appRepo := repositories.NewInternshipApplicationRepository(db)
-	interviewRepo := repositories.NewInterviewRepository(db)
 
 	authSvc := services.NewAuthService(userRepo, cfg, log)
-	studentSvc := services.NewStudentService(studentRepo, collegeRepo, minioClient, cfg, log)
-	collegeSvc := services.NewCollegeService(collegeRepo, userRepo, studentRepo, authSvc)
-	recordSvc := services.NewInternshipRecordService(recordRepo, userRepo, collegeSvc, minioClient, cfg.MinIO.CompanyDocBucket)
-	appSvc := services.NewInternshipApplicationService(appRepo, internshipRepo, studentRepo, minioClient, cfg.MinIO.StudentDocBucket)
-	internshipSvc := services.NewInternshipService(internshipRepo, recruiterProfileRepo)
-	interviewSvc := services.NewInterviewService(interviewRepo)
+	studentSvc := services.NewStudentService(studentRepo, minioClient, cfg, log)
 	recruiterProfileSvc := services.NewRecruiterProfileService(recruiterProfileRepo, minioClient, cfg.MinIO.ProfileBucket)
-	organizationVerificationSvc := services.NewOrganizationVerificationService(organizationVerificationRepo, recruiterProfileRepo, minioClient, cfg.MinIO.CompanyDocBucket)
+	appSvc := services.NewInternshipApplicationService(appRepo, internshipRepo, studentRepo, minioClient, cfg.MinIO.StudentDocBucket, recruiterProfileSvc, userRepo, services.SMTPVerificationSender{Config: cfg.Email}, log)
+	internshipSvc := services.NewInternshipService(internshipRepo, recruiterProfileRepo, recruiterProfileSvc)
 
 	authHandler := handlers.NewAuthHandler(authSvc, log)
-	collegeHandler := handlers.NewCollegeHandler(collegeSvc)
-	recordHandler := handlers.NewInternshipRecordHandler(recordSvc)
-	adminHandler := handlers.NewAdminHandler(userRepo, log)
-	studentHandler := handlers.NewStudentHandler(studentSvc, studentRepo, cfg, log)
+	studentHandler := handlers.NewStudentHandler(studentSvc)
 	recruiterProfileHandler := handlers.NewRecruiterProfileHandler(recruiterProfileSvc, log)
-	organizationVerificationHandler := handlers.NewOrganizationVerificationHandler(organizationVerificationSvc, log)
 	internshipHandler := handlers.NewInternshipHandler(internshipSvc)
 	appHandler := handlers.NewApplicationHandler(appSvc, log)
-	interviewHandler := handlers.NewInterviewHandler(interviewSvc, interviewRepo, internshipRepo)
 
 	router.GET("/health", healthCheck)
 
 	v1 := router.Group("/api/v1")
 	v1.GET("/health", healthCheck)
-	v1.GET("/universities", collegeHandler.Universities)
-	v1.GET("/colleges", collegeHandler.List(true))
-
-	collegeGroup := v1.Group("/colleges/me", middleware.JWTAuthMiddleware(cfg, log), middleware.RequireRoles(models.RoleCollege))
-	collegeGroup.GET("", collegeHandler.Me)
-	collegeGroup.GET("/dashboard", recordHandler.Dashboard)
-	collegeGroup.GET("/internships", recordHandler.List)
-	collegeGroup.GET("/internships/:id", recordHandler.Get)
-	collegeGroup.GET("/internships/:id/certificate", recordHandler.CertificateURL)
-
-	records := v1.Group("/internship-records", middleware.JWTAuthMiddleware(cfg, log), middleware.RequireRoles(models.RoleStudent, models.RoleEmployer, models.RoleCollege, models.RoleAdmin))
-	records.GET("", recordHandler.List)
-	records.GET("/:id", recordHandler.Get)
-	records.GET("/:id/certificate", recordHandler.CertificateURL)
 
 	authGroup := v1.Group("/auth")
 	authGroup.Use(middleware.RateLimitMiddleware())
 	authGroup.POST("/register/student", authHandler.RegisterStudent)
 	authGroup.POST("/register/employer", authHandler.RegisterEmployer)
+	authGroup.POST("/verify-email", authHandler.VerifyEmail)
+	authGroup.POST("/resend-verification", authHandler.ResendVerification)
 	authGroup.POST("/login", authHandler.Login)
 	authGroup.POST("/refresh", authHandler.Refresh)
 	authGroup.POST("/logout", authHandler.Logout)
-	authGroup.GET("/me", middleware.JWTAuthMiddleware(cfg, log), authHandler.Me)
-
-	adminGroup := v1.Group("/admin")
-	adminGroup.Use(middleware.JWTAuthMiddleware(cfg, log), middleware.RequireRoles(models.RoleAdmin))
-	adminGroup.GET("/users", adminHandler.ListUsers)
-	adminGroup.GET("/colleges", collegeHandler.List(false))
-	adminGroup.POST("/colleges", collegeHandler.Save)
-	adminGroup.GET("/colleges/:id", collegeHandler.Get)
-	adminGroup.PUT("/colleges/:id", collegeHandler.Save)
-	adminGroup.DELETE("/colleges/:id", collegeHandler.Delete)
-	adminGroup.GET("/colleges/:id/users", collegeHandler.ListUsers)
-	adminGroup.POST("/colleges/:id/users", collegeHandler.CreateUser)
-	adminGroup.PUT("/users/:id/college", collegeHandler.Assign(false))
-	adminGroup.PUT("/students/:id/college", collegeHandler.Assign(true))
-	adminGroup.PUT("/internship-records/:id/progress", recordHandler.UpdateProgress)
-	adminGroup.GET("/organization-verifications", organizationVerificationHandler.List)
-	adminGroup.GET("/organization-verifications/:id", organizationVerificationHandler.GetByID)
-	adminGroup.PUT("/organization-verifications/:id/review", organizationVerificationHandler.Review)
+	v1.GET("/auth/me", middleware.JWTAuthMiddleware(cfg, log), middleware.RequireRoles(models.RoleStudent, models.RoleEmployer), authHandler.Me)
 
 	recruiterGroup := v1.Group("/recruiters")
 	recruiterGroup.Use(middleware.JWTAuthMiddleware(cfg, log), middleware.RequireRoles(models.RoleEmployer))
@@ -104,20 +62,9 @@ func Register(router *gin.Engine, db *gorm.DB, cfg *config.Config, log *logger.L
 	profile.POST("/logo", recruiterProfileHandler.UploadOrganizationLogo)
 	profile.PUT("", recruiterProfileHandler.UpsertMyProfile)
 	profile.DELETE("", recruiterProfileHandler.DeleteMyProfile)
-	verification := recruiterGroup.Group("/me/verification")
-	verification.GET("", organizationVerificationHandler.GetMyVerification)
-	verification.POST("", organizationVerificationHandler.SubmitVerification)
-	verification.POST("/document", organizationVerificationHandler.UploadDocument)
 	recruiterGroup.GET("/applications", appHandler.ListForRecruiter)
 	recruiterGroup.GET("/applications/:id", appHandler.GetForRecruiter)
 	recruiterGroup.PUT("/applications/:id/status", appHandler.UpdateStatus)
-	recruiterGroup.PUT("/applications/:id/progress", recordHandler.UpdateProgress)
-	recruiterGroup.POST("/applications/:id/evaluation", recordHandler.SubmitEvaluation)
-	recruiterGroup.POST("/applications/:id/certificate", recordHandler.IssueCertificate)
-	interviews := recruiterGroup.Group("/interviews")
-	interviews.POST("", interviewHandler.Schedule)
-	interviews.GET("", interviewHandler.ListEmployer)
-	interviews.DELETE("/:id", interviewHandler.Cancel)
 
 	studentGroup := v1.Group("/students")
 	me := studentGroup.Group("/me")
@@ -125,20 +72,18 @@ func Register(router *gin.Engine, db *gorm.DB, cfg *config.Config, log *logger.L
 
 	me.GET("/profile", studentHandler.GetProfile)
 	me.POST("/profile", studentHandler.UpsertProfile)
+	me.POST("/profile/picture", studentHandler.UploadProfilePicture)
+	me.GET("/resume", studentHandler.GetResume)
+	me.PUT("/resume", studentHandler.UploadResume)
+	me.DELETE("/resume", studentHandler.DeleteResume)
 
 	profileRequired := me.Group("")
 	profileRequired.Use(middleware.ResolveStudentProfileIDMiddleware(studentRepo, log))
 	profileRequired.GET("/stats", studentHandler.GetApplicationStats)
-	profileRequired.POST("/documents", studentHandler.UploadDocument)
-	profileRequired.GET("/documents", studentHandler.ListDocuments)
-	profileRequired.POST("/documents/:id/default", studentHandler.SetDefaultDocument)
-	profileRequired.DELETE("/documents/:id", studentHandler.DeleteDocument)
 	profileRequired.GET("/applications", appHandler.ListOwn)
 	profileRequired.GET("/applications/internships/:internship_id", appHandler.FindOwnForInternship)
 	profileRequired.GET("/applications/stats", studentHandler.GetApplicationStats)
 	profileRequired.PATCH("/applications/:id/withdraw", appHandler.Withdraw)
-	profileRequired.GET("/interviews", interviewHandler.ListStudent)
-	profileRequired.POST("/interviews/:id/respond", interviewHandler.Respond)
 
 	/* public routes*/
 	internships := v1.Group("/internships")

# internal/seeder/seeder.go

diff --git a/internal/seeder/seeder.go b/internal/seeder/seeder.go
index 9609023..9583810 100644
--- a/internal/seeder/seeder.go
+++ b/internal/seeder/seeder.go
@@ -19,17 +19,21 @@ const (
 	StudentEmail    = "seed.student@jobportal.test"
 	StudentTwoEmail = "seed.student2@jobportal.test"
 	EmployerEmail   = "seed.employer@jobportal.test"
+	CollegeEmail    = "seed.college@jobportal.test"
+	CollegeTwoEmail = "seed.college2@jobportal.test"
 )
 
 // Summary describes the records ensured by Run. Counts are the size of the
 // development dataset, whether each record was inserted or updated.
 type Summary struct {
+	Colleges                  int
 	Users                     int
 	StudentProfiles           int
 	RecruiterProfiles         int
 	OrganizationVerifications int
 	Internships               int
 	Applications              int
+	Evaluations               int
 }
 
 type seedUser struct {
@@ -56,6 +60,15 @@ func Run(db *gorm.DB, password string) (Summary, error) {
 	}
 
 	err = db.Transaction(func(tx *gorm.DB) error {
+		for _, college := range []models.College{
+			{ID: seedID("college", "student"), Name: "Amrit Science Campus", University: models.TribhuvanUniversity, Status: "active"},
+			{ID: seedID("college", "student2"), Name: "Prithvi Narayan Campus", University: models.TribhuvanUniversity, Status: "active"},
+		} {
+			if err := upsert(tx, &college, "id"); err != nil {
+				return err
+			}
+			summary.Colleges++
+		}
 		users, err := seedUsers(tx, string(passwordHash))
 		if err != nil {
 			return err
@@ -90,11 +103,17 @@ func Run(db *gorm.DB, password string) (Summary, error) {
 		}
 		summary.Internships = len(internships)
 
-		applicationCount, err := seedApplications(tx, students, internships)
+		applications, err := seedApplications(tx, students, internships)
+		if err != nil {
+			return err
+		}
+		summary.Applications = len(applications)
+
+		evaluationCount, err := seedEvaluations(tx, applications, users["employer"].ID)
 		if err != nil {
 			return err
 		}
-		summary.Applications = applicationCount
+		summary.Evaluations = evaluationCount
 		return nil
 	})
 	if err != nil {
@@ -108,6 +127,13 @@ func seedUsers(tx *gorm.DB, passwordHash string) (map[string]models.User, error)
 		{key: "admin", email: AdminEmail, fullName: "Portal Administrator", role: models.RoleAdmin},
 		{key: "student", email: StudentEmail, fullName: "Aarav Sharma", role: models.RoleStudent},
 		{key: "student2", email: StudentTwoEmail, fullName: "Maya Gurung", role: models.RoleStudent},
+		{key: "anisha", email: "seed.anisha@jobportal.test", fullName: "Anisha Shrestha", role: models.RoleStudent},
+		{key: "rohan", email: "seed.rohan@jobportal.test", fullName: "Rohan Karki", role: models.RoleStudent},
+		{key: "priya", email: "seed.priya@jobportal.test", fullName: "Priya Maharjan", role: models.RoleStudent},
+		{key: "sagar", email: "seed.sagar@jobportal.test", fullName: "Sagar Poudel", role: models.RoleStudent},
+		{key: "nima", email: "seed.nima@jobportal.test", fullName: "Nima Sherpa", role: models.RoleStudent},
+		{key: "college", email: CollegeEmail, fullName: "Amrit Internship Coordinator", role: models.RoleCollege},
+		{key: "college2", email: CollegeTwoEmail, fullName: "Prithvi Internship Coordinator", role: models.RoleCollege},
 		{key: "employer", email: EmployerEmail, fullName: "Nisha Adhikari", role: models.RoleEmployer},
 		{key: "employer_pending", email: "seed.employer.pending@jobportal.test", fullName: "Suman Karki", role: models.RoleEmployer},
 		{key: "employer_rejected", email: "seed.employer.rejected@jobportal.test", fullName: "Ritika Thapa", role: models.RoleEmployer},
@@ -122,6 +148,14 @@ func seedUsers(tx *gorm.DB, passwordHash string) (map[string]models.User, error)
 			FullName:     seed.fullName,
 			Role:         seed.role,
 		}
+		if seed.role == models.RoleCollege {
+			key := "student"
+			if seed.key == "college2" {
+				key = "student2"
+			}
+			collegeID := seedID("college", key)
+			user.CollegeID = &collegeID
+		}
 		var existing models.User
 		lookup := tx.Unscoped().Where("email = ?", seed.email).Limit(1).Find(&existing)
 		if lookup.Error != nil {
@@ -175,11 +209,52 @@ func seedStudentProfiles(tx *gorm.DB, users map[string]models.User) (map[string]
 			LinkedinURL: "https://www.linkedin.com/in/maya-gurung-seed", GithubURL: "https://github.com/maya-gurung-seed",
 			PortfolioURL: "https://maya-gurung.example", IsSearchable: true, ProfileCompletionPercent: 100,
 		}},
+		{key: "anisha", profile: models.StudentProfile{
+			ID: seedID("student-profile", "anisha"), UserID: users["anisha"].ID,
+			FullName: "Anisha Shrestha", Phone: "+977-9800000011", Location: "Kathmandu, Nepal",
+			Bio:         "Computer science student interested in frontend engineering and accessible product design.",
+			CollegeName: "Amrit Science Campus", Degree: "BSc CSIT", FacultyOrMajor: "Computer Science",
+			CurrentSemester: "8th Semester", GraduationYear: time.Now().Year(), IsSearchable: true, ProfileCompletionPercent: 100,
+		}},
+		{key: "rohan", profile: models.StudentProfile{
+			ID: seedID("student-profile", "rohan"), UserID: users["rohan"].ID,
+			FullName: "Rohan Karki", Phone: "+977-9800000012", Location: "Bhaktapur, Nepal",
+			Bio:         "Software engineering student focused on backend systems and reliable APIs.",
+			CollegeName: "Amrit Science Campus", Degree: "BSc CSIT", FacultyOrMajor: "Computer Science",
+			CurrentSemester: "7th Semester", GraduationYear: time.Now().Year() + 1, IsSearchable: true, ProfileCompletionPercent: 100,
+		}},
+		{key: "priya", profile: models.StudentProfile{
+			ID: seedID("student-profile", "priya"), UserID: users["priya"].ID,
+			FullName: "Priya Maharjan", Phone: "+977-9800000013", Location: "Lalitpur, Nepal",
+			Bio:         "Information technology student exploring data analytics and business intelligence.",
+			CollegeName: "Amrit Science Campus", Degree: "BIT", FacultyOrMajor: "Information Technology",
+			CurrentSemester: "6th Semester", GraduationYear: time.Now().Year() + 1, IsSearchable: true, ProfileCompletionPercent: 100,
+		}},
+		{key: "sagar", profile: models.StudentProfile{
+			ID: seedID("student-profile", "sagar"), UserID: users["sagar"].ID,
+			FullName: "Sagar Poudel", Phone: "+977-9800000014", Location: "Kathmandu, Nepal",
+			Bio:         "Computing student interested in quality assurance and test automation.",
+			CollegeName: "Amrit Science Campus", Degree: "BCA", FacultyOrMajor: "Computer Applications",
+			CurrentSemester: "7th Semester", GraduationYear: time.Now().Year() + 1, IsSearchable: true, ProfileCompletionPercent: 100,
+		}},
+		{key: "nima", profile: models.StudentProfile{
+			ID: seedID("student-profile", "nima"), UserID: users["nima"].ID,
+			FullName: "Nima Sherpa", Phone: "+977-9800000015", Location: "Kathmandu, Nepal",
+			Bio:         "Computer science student building practical mobile and web applications.",
+			CollegeName: "Amrit Science Campus", Degree: "BSc CSIT", FacultyOrMajor: "Computer Science",
+			CurrentSemester: "8th Semester", GraduationYear: time.Now().Year(), IsSearchable: true, ProfileCompletionPercent: 100,
+		}},
 	}
 
 	result := make(map[string]models.StudentProfile, len(profiles))
 	for _, item := range profiles {
 		profile := item.profile
+		collegeKey := "student"
+		if item.key == "student2" {
+			collegeKey = "student2"
+		}
+		collegeID := seedID("college", collegeKey)
+		profile.CollegeID = &collegeID
 		if err := saveStudentProfile(tx, &profile); err != nil {
 			return nil, fmt.Errorf("upsert student profile %s: %w", item.key, err)
 		}
@@ -316,7 +391,7 @@ func seedInternships(tx *gorm.DB, users map[string]models.User) (map[string]mode
 	titles := []string{"Software Engineering", "Frontend Development", "Backend Development", "Mobile Development", "Data Analytics", "UI/UX Design", "Quality Assurance", "DevOps", "Digital Marketing", "Business Analysis"}
 	locations := []string{"Kathmandu, Nepal", "Lalitpur, Nepal", "Bhaktapur, Nepal", "Pokhara, Nepal", "Remote"}
 	workModes := []string{"onsite", "hybrid", "remote"}
-	for i := 1; i <= 94; i++ {
+	for i := 1; i <= 104; i++ {
 		key := fmt.Sprintf("generated-%03d", i)
 		title := titles[(i-1)%len(titles)]
 		internship := models.Internship{
@@ -347,6 +422,12 @@ func seedInternships(tx *gorm.DB, users map[string]models.User) (map[string]mode
 			Status:              enums.InternshipStatusPublished,
 			IsActive:            true,
 		}
+		if i <= 10 {
+			internship.StartDate = past(210 - i*7)
+			internship.ApplicationDeadline = past(225 - i*7)
+			internship.Status = enums.InternshipStatusClosed
+			internship.IsActive = false
+		}
 		if err := upsert(tx, &internship, "id"); err != nil {
 			return nil, fmt.Errorf("upsert internship %s: %w", key, err)
 		}
@@ -355,23 +436,99 @@ func seedInternships(tx *gorm.DB, users map[string]models.User) (map[string]mode
 	return result, nil
 }
 
-func seedApplications(tx *gorm.DB, students map[string]models.StudentProfile, internships map[string]models.Internship) (int, error) {
+func seedApplications(tx *gorm.DB, students map[string]models.StudentProfile, internships map[string]models.Internship) (map[string]models.InternshipApplication, error) {
 	now := time.Now().UTC()
 	timePtr := func(value time.Time) *time.Time { return &value }
-	applications := []models.InternshipApplication{
-		{ID: seedID("application", "aarav-frontend"), InternshipID: internships["frontend"].ID, StudentID: students["student"].ID, Status: models.ApplicationStatusShortlisted, EmployerNote: "Strong frontend portfolio; invite to the technical conversation.", AppliedAt: now.AddDate(0, 0, -9), ReviewedAt: timePtr(now.AddDate(0, 0, -8)), ShortlistedAt: timePtr(now.AddDate(0, 0, -6))},
-		{ID: seedID("application", "aarav-backend"), InternshipID: internships["backend"].ID, StudentID: students["student"].ID, Status: models.ApplicationStatusSubmitted, AppliedAt: now.AddDate(0, 0, -2)},
-		{ID: seedID("application", "aarav-data"), InternshipID: internships["data-closed"].ID, StudentID: students["student"].ID, Status: models.ApplicationStatusRejected, EmployerNote: "Role required more advanced analytics experience for this intake.", AppliedAt: now.AddDate(0, -2, 0), ReviewedAt: timePtr(now.AddDate(0, -2, 2)), RejectedAt: timePtr(now.AddDate(0, -2, 3))},
-		{ID: seedID("application", "maya-frontend"), InternshipID: internships["frontend"].ID, StudentID: students["student2"].ID, Status: models.ApplicationStatusReviewing, EmployerNote: "Application is under portfolio review.", AppliedAt: now.AddDate(0, 0, -5), ReviewedAt: timePtr(now.AddDate(0, 0, -4))},
-		{ID: seedID("application", "maya-backend"), InternshipID: internships["backend"].ID, StudentID: students["student2"].ID, Status: models.ApplicationStatusAccepted, EmployerNote: "Offer accepted for the upcoming cohort.", AppliedAt: now.AddDate(0, -1, -5), ReviewedAt: timePtr(now.AddDate(0, -1, -3)), ShortlistedAt: timePtr(now.AddDate(0, -1, 1)), AcceptedAt: timePtr(now.AddDate(0, 0, -18))},
-		{ID: seedID("application", "maya-design"), InternshipID: internships["product-design"].ID, StudentID: students["student2"].ID, Status: models.ApplicationStatusWithdrawn, AppliedAt: now.AddDate(0, 0, -12), WithdrawnAt: timePtr(now.AddDate(0, 0, -10))},
-	}
-	for i := range applications {
-		if err := saveApplication(tx, &applications[i]); err != nil {
-			return 0, fmt.Errorf("upsert internship application: %w", err)
+	seeded := []struct {
+		key string
+		app models.InternshipApplication
+	}{
+		{key: "aarav-frontend", app: models.InternshipApplication{ID: seedID("application", "aarav-frontend"), InternshipID: internships["frontend"].ID, StudentID: students["student"].ID, Status: models.ApplicationStatusShortlisted, EmployerNote: "Strong frontend portfolio; invite to the technical conversation.", AppliedAt: now.AddDate(0, 0, -9), ReviewedAt: timePtr(now.AddDate(0, 0, -8)), ShortlistedAt: timePtr(now.AddDate(0, 0, -6))}},
+		{key: "aarav-backend", app: models.InternshipApplication{ID: seedID("application", "aarav-backend"), InternshipID: internships["backend"].ID, StudentID: students["student"].ID, Status: models.ApplicationStatusSubmitted, AppliedAt: now.AddDate(0, 0, -2)}},
+		{key: "aarav-data", app: models.InternshipApplication{ID: seedID("application", "aarav-data"), InternshipID: internships["data-closed"].ID, StudentID: students["student"].ID, Status: models.ApplicationStatusRejected, EmployerNote: "Role required more advanced analytics experience for this intake.", AppliedAt: now.AddDate(0, -2, 0), ReviewedAt: timePtr(now.AddDate(0, -2, 2)), RejectedAt: timePtr(now.AddDate(0, -2, 3))}},
+		{key: "maya-frontend", app: models.InternshipApplication{ID: seedID("application", "maya-frontend"), InternshipID: internships["frontend"].ID, StudentID: students["student2"].ID, Status: models.ApplicationStatusReviewing, EmployerNote: "Application is under portfolio review.", AppliedAt: now.AddDate(0, 0, -5), ReviewedAt: timePtr(now.AddDate(0, 0, -4))}},
+		{key: "maya-backend", app: models.InternshipApplication{ID: seedID("application", "maya-backend"), InternshipID: internships["backend"].ID, StudentID: students["student2"].ID, Status: models.ApplicationStatusAccepted, EmployerNote: "Offer accepted for the upcoming cohort.", AppliedAt: now.AddDate(0, -1, -5), ReviewedAt: timePtr(now.AddDate(0, -1, -3)), ShortlistedAt: timePtr(now.AddDate(0, -1, 1)), AcceptedAt: timePtr(now.AddDate(0, 0, -18))}},
+		{key: "maya-design", app: models.InternshipApplication{ID: seedID("application", "maya-design"), InternshipID: internships["product-design"].ID, StudentID: students["student2"].ID, Status: models.ApplicationStatusWithdrawn, AppliedAt: now.AddDate(0, 0, -12), WithdrawnAt: timePtr(now.AddDate(0, 0, -10))}},
+	}
+
+	ongoingStudents := []string{"anisha", "rohan", "priya", "sagar", "nima"}
+	for i, student := range ongoingStudents {
+		key := student + "-ongoing"
+		start := now.AddDate(0, 0, -(75 - i*12))
+		accepted := start.AddDate(0, 0, -3)
+		seeded = append(seeded, struct {
+			key string
+			app models.InternshipApplication
+		}{key: key, app: models.InternshipApplication{
+			ID: seedID("application", key), InternshipID: internships[fmt.Sprintf("generated-%03d", i+1)].ID,
+			StudentID: students[student].ID, Status: models.ApplicationStatusAccepted, ProgressStatus: "ongoing",
+			EmployerNote: "Selected for the internship cohort.", AppliedAt: accepted.AddDate(0, 0, -12),
+			ReviewedAt: timePtr(accepted.AddDate(0, 0, -8)), ShortlistedAt: timePtr(accepted.AddDate(0, 0, -5)),
+			AcceptedAt: timePtr(accepted), StartDate: timePtr(start),
+		}})
+	}
+
+	completedStudents := []string{"anisha", "rohan", "priya", "sagar", "nima"}
+	for i, student := range completedStudents {
+		key := student + "-completed"
+		end := now.AddDate(0, 0, -(10 + i*9))
+		start := end.AddDate(0, -3, 0)
+		accepted := start.AddDate(0, 0, -4)
+		seeded = append(seeded, struct {
+			key string
+			app models.InternshipApplication
+		}{key: key, app: models.InternshipApplication{
+			ID: seedID("application", key), InternshipID: internships[fmt.Sprintf("generated-%03d", i+6)].ID,
+			StudentID: students[student].ID, Status: models.ApplicationStatusAccepted, ProgressStatus: "completed",
+			EmployerNote: "Successfully completed the internship placement.", AppliedAt: accepted.AddDate(0, 0, -14),
+			ReviewedAt: timePtr(accepted.AddDate(0, 0, -10)), ShortlistedAt: timePtr(accepted.AddDate(0, 0, -7)),
+			AcceptedAt: timePtr(accepted), StartDate: timePtr(start), EndDate: timePtr(end),
+		}})
+	}
+
+	result := make(map[string]models.InternshipApplication, len(seeded))
+	for i := range seeded {
+		application := &seeded[i].app
+		if application.ProgressStatus == "" {
+			application.ProgressStatus = "applied"
+		}
+		switch application.Status {
+		case models.ApplicationStatusAccepted:
+			if application.ProgressStatus == "applied" {
+				application.ProgressStatus = "ongoing"
+				application.StartDate = application.AcceptedAt
+			}
+		case models.ApplicationStatusRejected, models.ApplicationStatusWithdrawn:
+			application.ProgressStatus = "cancelled"
 		}
+		if err := saveApplication(tx, application); err != nil {
+			return nil, fmt.Errorf("upsert internship application: %w", err)
+		}
+		result[seeded[i].key] = *application
 	}
-	return len(applications), nil
+	return result, nil
+}
+
+func seedEvaluations(tx *gorm.DB, applications map[string]models.InternshipApplication, employerID uuid.UUID) (int, error) {
+	keys := []string{"anisha-completed", "rohan-completed", "priya-completed", "sagar-completed", "nima-completed"}
+	remarks := []string{
+		"Delivered polished work consistently and communicated blockers early.",
+		"Built dependable backend features and responded well to review feedback.",
+		"Turned raw data into clear findings and presented them confidently.",
+		"Improved regression coverage and documented reproducible issues clearly.",
+		"Completed assigned mobile features on time and collaborated effectively.",
+	}
+	for i, key := range keys {
+		evaluation := models.InternshipEvaluation{
+			ID: seedID("evaluation", key), ApplicationID: applications[key].ID, SubmittedBy: employerID,
+			Scores:  map[string]int{"technical": 82 + i*2, "communication": 88 - i, "reliability": 90 + i},
+			Remarks: remarks[i], Status: "submitted", SubmittedAt: applications[key].EndDate.Add(24 * time.Hour),
+		}
+		if err := saveEvaluation(tx, &evaluation); err != nil {
+			return 0, fmt.Errorf("upsert internship evaluation: %w", err)
+		}
+	}
+	return len(keys), nil
 }
 
 func upsert(tx *gorm.DB, value interface{}, conflictColumn string) error {
@@ -442,6 +599,19 @@ func saveApplication(tx *gorm.DB, application *models.InternshipApplication) err
 	return tx.Save(application).Error
 }
 
+func saveEvaluation(tx *gorm.DB, evaluation *models.InternshipEvaluation) error {
+	var existing models.InternshipEvaluation
+	lookup := tx.Where("application_id = ?", evaluation.ApplicationID).Limit(1).Find(&existing)
+	if lookup.Error != nil {
+		return lookup.Error
+	}
+	if lookup.RowsAffected == 0 {
+		return tx.Create(evaluation).Error
+	}
+	evaluation.ID = existing.ID
+	return tx.Save(evaluation).Error
+}
+
 func seedID(kind, name string) uuid.UUID {
 	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("student-job-portal/seed/"+kind+"/"+name))
 }
diff --git a/internal/seeder/seeder.go b/internal/seeder/seeder.go
index 9583810..94e36a3 100644
--- a/internal/seeder/seeder.go
+++ b/internal/seeder/seeder.go
@@ -3,48 +3,24 @@ package seeder
 import (
 	"errors"
 	"fmt"
-	"time"
-
 	"github.com/google/uuid"
 	"github.com/prasanna/student-job-portal/backend/internal/enums"
 	"github.com/prasanna/student-job-portal/backend/internal/models"
 	"golang.org/x/crypto/bcrypt"
 	"gorm.io/gorm"
 	"gorm.io/gorm/clause"
+	"time"
 )
 
 const (
 	DefaultPassword = "Password123!"
-	AdminEmail      = "seed.admin@jobportal.test"
 	StudentEmail    = "seed.student@jobportal.test"
 	StudentTwoEmail = "seed.student2@jobportal.test"
 	EmployerEmail   = "seed.employer@jobportal.test"
-	CollegeEmail    = "seed.college@jobportal.test"
-	CollegeTwoEmail = "seed.college2@jobportal.test"
 )
 
-// Summary describes the records ensured by Run. Counts are the size of the
-// development dataset, whether each record was inserted or updated.
-type Summary struct {
-	Colleges                  int
-	Users                     int
-	StudentProfiles           int
-	RecruiterProfiles         int
-	OrganizationVerifications int
-	Internships               int
-	Applications              int
-	Evaluations               int
-}
+type Summary struct{ Users, StudentProfiles, RecruiterProfiles, Internships, Applications int }
 
-type seedUser struct {
-	key      string
-	email    string
-	fullName string
-	role     models.Role
-}
-
-// Run inserts or refreshes the development dataset in one transaction. Stable
-// UUIDs and conflict-aware writes make repeated runs safe.
 func Run(db *gorm.DB, password string) (Summary, error) {
 	var summary Summary
 	if db == nil {
@@ -53,67 +29,77 @@ func Run(db *gorm.DB, password string) (Summary, error) {
 	if password == "" {
 		password = DefaultPassword
 	}
-
-	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
+	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
 	if err != nil {
-		return summary, fmt.Errorf("hash seed password: %w", err)
+		return summary, err
 	}
-
 	err = db.Transaction(func(tx *gorm.DB) error {
-		for _, college := range []models.College{
-			{ID: seedID("college", "student"), Name: "Amrit Science Campus", University: models.TribhuvanUniversity, Status: "active"},
-			{ID: seedID("college", "student2"), Name: "Prithvi Narayan Campus", University: models.TribhuvanUniversity, Status: "active"},
-		} {
-			if err := upsert(tx, &college, "id"); err != nil {
+		users := map[string]models.User{
+			"student":           {Email: StudentEmail, FullName: "Aarav Sharma", Role: models.RoleStudent},
+			"student2":          {Email: StudentTwoEmail, FullName: "Maya Gurung", Role: models.RoleStudent},
+			"employer":          {Email: EmployerEmail, FullName: "Nisha Adhikari", Role: models.RoleEmployer},
+			"employer_pending":  {Email: "seed.employer.pending@jobportal.test", FullName: "Suman Karki", Role: models.RoleEmployer},
+			"employer_rejected": {Email: "seed.employer.rejected@jobportal.test", FullName: "Ritika Thapa", Role: models.RoleEmployer},
+		}
+		for key, user := range users {
+			now := time.Now().UTC()
+			user.EmailVerifiedAt = &now
+			user.ID, user.PasswordHash = seedID("user", key), string(hash)
+			var existing models.User
+			err := tx.Unscoped().Where("email = ?", user.Email).First(&existing).Error
+			if err == nil {
+				user.ID, user.CreatedAt = existing.ID, existing.CreatedAt
+			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
 				return err
 			}
-			summary.Colleges++
-		}
-		users, err := seedUsers(tx, string(passwordHash))
-		if err != nil {
-			return err
+			if err := tx.Unscoped().Save(&user).Error; err != nil {
+				return err
+			}
+			users[key] = user
 		}
 		summary.Users = len(users)
-
-		students, err := seedStudentProfiles(tx, users)
-		if err != nil {
-			return err
+		students := map[string]models.StudentProfile{}
+		for _, item := range []struct{ key, name, college string }{{"student", "aarav", "Amrit Science Campus"}, {"student2", "maya", "Prithvi Narayan Campus"}} {
+			profile := models.StudentProfile{ID: seedID("student-profile", item.name), UserID: users[item.key].ID, FullName: users[item.key].FullName, Phone: "+977-9800000001", Location: "Kathmandu, Nepal", CollegeName: item.college, Degree: "BCA", CurrentSemester: "4th Semester", GraduationYear: time.Now().Year() + 2}
+			if err := saveStudentProfile(tx, &profile); err != nil {
+				return err
+			}
+			students[item.key] = profile
 		}
 		summary.StudentProfiles = len(students)
-
-		recruiters, err := seedRecruiterProfiles(tx, users)
-		if err != nil {
-			return err
-		}
-		summary.RecruiterProfiles = len(recruiters)
-
-		if err := seedStudentDetails(tx, students); err != nil {
-			return err
-		}
-
-		verificationCount, err := seedVerifications(tx, users, recruiters)
-		if err != nil {
-			return err
+		for _, item := range []struct{ key, name, company string }{
+			{"employer", "techkarkhana", "TechKarkhana Nepal"},
+			{"employer_pending", "himalayan-analytics", "Himalayan Analytics"},
+			{"employer_rejected", "brightpath", "BrightPath Media"},
+		} {
+			profile := models.RecruiterProfile{ID: seedID("recruiter-profile", item.name), UserID: users[item.key].ID, OrganizationName: item.company, OrganizationAddress: "Kathmandu, Nepal"}
+			if err := saveRecruiterProfile(tx, &profile); err != nil {
+				return err
+			}
+			summary.RecruiterProfiles++
 		}
-		summary.OrganizationVerifications = verificationCount
-
 		internships, err := seedInternships(tx, users)
 		if err != nil {
 			return err
 		}
 		summary.Internships = len(internships)
-
-		applications, err := seedApplications(tx, students, internships)
-		if err != nil {
-			return err
-		}
-		summary.Applications = len(applications)
-
-		evaluationCount, err := seedEvaluations(tx, applications, users["employer"].ID)
-		if err != nil {
-			return err
+		for _, item := range []struct {
+			key, student, internship string
+			status                   models.InternshipApplicationStatus
+		}{
+			{"aarav-frontend", "student", "frontend", models.ApplicationStatusSubmitted},
+			{"aarav-backend", "student", "backend", models.ApplicationStatusSubmitted},
+			{"aarav-data", "student", "data-closed", models.ApplicationStatusRejected},
+			{"maya-frontend", "student2", "frontend", models.ApplicationStatusSubmitted},
+			{"maya-backend", "student2", "backend", models.ApplicationStatusAccepted},
+			{"maya-design", "student2", "product-design", models.ApplicationStatusWithdrawn},
+		} {
+			app := models.InternshipApplication{ID: seedID("application", item.key), StudentID: students[item.student].ID, InternshipID: internships[item.internship].ID, Status: item.status, AppliedAt: time.Now().AddDate(0, 0, -2)}
+			if err := saveApplication(tx, &app); err != nil {
+				return err
+			}
+			summary.Applications++
 		}
-		summary.Evaluations = evaluationCount
 		return nil
 	})
 	if err != nil {
@@ -122,261 +108,22 @@ func Run(db *gorm.DB, password string) (Summary, error) {
 	return summary, nil
 }
 
-func seedUsers(tx *gorm.DB, passwordHash string) (map[string]models.User, error) {
-	seeds := []seedUser{
-		{key: "admin", email: AdminEmail, fullName: "Portal Administrator", role: models.RoleAdmin},
-		{key: "student", email: StudentEmail, fullName: "Aarav Sharma", role: models.RoleStudent},
-		{key: "student2", email: StudentTwoEmail, fullName: "Maya Gurung", role: models.RoleStudent},
-		{key: "anisha", email: "seed.anisha@jobportal.test", fullName: "Anisha Shrestha", role: models.RoleStudent},
-		{key: "rohan", email: "seed.rohan@jobportal.test", fullName: "Rohan Karki", role: models.RoleStudent},
-		{key: "priya", email: "seed.priya@jobportal.test", fullName: "Priya Maharjan", role: models.RoleStudent},
-		{key: "sagar", email: "seed.sagar@jobportal.test", fullName: "Sagar Poudel", role: models.RoleStudent},
-		{key: "nima", email: "seed.nima@jobportal.test", fullName: "Nima Sherpa", role: models.RoleStudent},
-		{key: "college", email: CollegeEmail, fullName: "Amrit Internship Coordinator", role: models.RoleCollege},
-		{key: "college2", email: CollegeTwoEmail, fullName: "Prithvi Internship Coordinator", role: models.RoleCollege},
-		{key: "employer", email: EmployerEmail, fullName: "Nisha Adhikari", role: models.RoleEmployer},
-		{key: "employer_pending", email: "seed.employer.pending@jobportal.test", fullName: "Suman Karki", role: models.RoleEmployer},
-		{key: "employer_rejected", email: "seed.employer.rejected@jobportal.test", fullName: "Ritika Thapa", role: models.RoleEmployer},
-	}
-
-	users := make(map[string]models.User, len(seeds))
-	for _, seed := range seeds {
-		user := models.User{
-			ID:           seedID("user", seed.key),
-			Email:        seed.email,
-			PasswordHash: passwordHash,
-			FullName:     seed.fullName,
-			Role:         seed.role,
-		}
-		if seed.role == models.RoleCollege {
-			key := "student"
-			if seed.key == "college2" {
-				key = "student2"
-			}
-			collegeID := seedID("college", key)
-			user.CollegeID = &collegeID
-		}
-		var existing models.User
-		lookup := tx.Unscoped().Where("email = ?", seed.email).Limit(1).Find(&existing)
-		if lookup.Error != nil {
-			return nil, fmt.Errorf("find user %s: %w", seed.email, lookup.Error)
-		}
-		if lookup.RowsAffected > 0 {
-			user.ID = existing.ID
-			user.CreatedAt = existing.CreatedAt
-			if err := tx.Unscoped().Save(&user).Error; err != nil {
-				return nil, fmt.Errorf("update user %s: %w", seed.email, err)
-			}
-		} else {
-			if err := tx.Create(&user).Error; err != nil {
-				return nil, fmt.Errorf("create user %s: %w", seed.email, err)
-			}
-		}
-		if err := tx.Where("email = ?", seed.email).First(&user).Error; err != nil {
-			return nil, fmt.Errorf("upsert user %s: %w", seed.email, err)
-		}
-		users[seed.key] = user
-	}
-	return users, nil
-}
-
-func seedStudentProfiles(tx *gorm.DB, users map[string]models.User) (map[string]models.StudentProfile, error) {
-	profiles := []struct {
-		key     string
-		profile models.StudentProfile
-	}{
-		{key: "student", profile: models.StudentProfile{
-			ID: seedID("student-profile", "aarav"), UserID: users["student"].ID,
-			FullName: "Aarav Sharma", Phone: "+977-9800000001", Location: "Kathmandu, Nepal",
-			Bio:         "Computer science student focused on dependable web products and accessible user experiences.",
-			CollegeName: "Amrit Science Campus", Degree: "BSc CSIT", FacultyOrMajor: "Computer Science",
-			CurrentSemester: "7th Semester", GraduationYear: time.Now().Year() + 1,
-			PreferredJobCategories: "Software Engineering, Frontend Development, Backend Development",
-			PreferredLocations:     "Kathmandu, Lalitpur, Remote", PreferredWorkMode: "hybrid",
-			Availability: "Immediately", ExpectedSalary: "NPR 20,000/month",
-			LinkedinURL: "https://www.linkedin.com/in/aarav-sharma-seed", GithubURL: "https://github.com/aarav-sharma-seed",
-			PortfolioURL: "https://aarav-sharma.example", IsSearchable: true, ProfileCompletionPercent: 100,
-		}},
-		{key: "student2", profile: models.StudentProfile{
-			ID: seedID("student-profile", "maya"), UserID: users["student2"].ID,
-			FullName: "Maya Gurung", Phone: "+977-9800000002", Location: "Pokhara, Nepal",
-			Bio:         "Information technology student interested in product design, data, and thoughtful digital services.",
-			CollegeName: "Prithvi Narayan Campus", Degree: "BIT", FacultyOrMajor: "Information Technology",
-			CurrentSemester: "6th Semester", GraduationYear: time.Now().Year() + 1,
-			PreferredJobCategories: "Product Design, Data Analytics, Quality Assurance",
-			PreferredLocations:     "Pokhara, Kathmandu, Remote", PreferredWorkMode: "remote",
-			Availability: "Part-time", ExpectedSalary: "NPR 18,000/month",
-			LinkedinURL: "https://www.linkedin.com/in/maya-gurung-seed", GithubURL: "https://github.com/maya-gurung-seed",
-			PortfolioURL: "https://maya-gurung.example", IsSearchable: true, ProfileCompletionPercent: 100,
-		}},
-		{key: "anisha", profile: models.StudentProfile{
-			ID: seedID("student-profile", "anisha"), UserID: users["anisha"].ID,
-			FullName: "Anisha Shrestha", Phone: "+977-9800000011", Location: "Kathmandu, Nepal",
-			Bio:         "Computer science student interested in frontend engineering and accessible product design.",
-			CollegeName: "Amrit Science Campus", Degree: "BSc CSIT", FacultyOrMajor: "Computer Science",
-			CurrentSemester: "8th Semester", GraduationYear: time.Now().Year(), IsSearchable: true, ProfileCompletionPercent: 100,
-		}},
-		{key: "rohan", profile: models.StudentProfile{
-			ID: seedID("student-profile", "rohan"), UserID: users["rohan"].ID,
-			FullName: "Rohan Karki", Phone: "+977-9800000012", Location: "Bhaktapur, Nepal",
-			Bio:         "Software engineering student focused on backend systems and reliable APIs.",
-			CollegeName: "Amrit Science Campus", Degree: "BSc CSIT", FacultyOrMajor: "Computer Science",
-			CurrentSemester: "7th Semester", GraduationYear: time.Now().Year() + 1, IsSearchable: true, ProfileCompletionPercent: 100,
-		}},
-		{key: "priya", profile: models.StudentProfile{
-			ID: seedID("student-profile", "priya"), UserID: users["priya"].ID,
-			FullName: "Priya Maharjan", Phone: "+977-9800000013", Location: "Lalitpur, Nepal",
-			Bio:         "Information technology student exploring data analytics and business intelligence.",
-			CollegeName: "Amrit Science Campus", Degree: "BIT", FacultyOrMajor: "Information Technology",
-			CurrentSemester: "6th Semester", GraduationYear: time.Now().Year() + 1, IsSearchable: true, ProfileCompletionPercent: 100,
-		}},
-		{key: "sagar", profile: models.StudentProfile{
-			ID: seedID("student-profile", "sagar"), UserID: users["sagar"].ID,
-			FullName: "Sagar Poudel", Phone: "+977-9800000014", Location: "Kathmandu, Nepal",
-			Bio:         "Computing student interested in quality assurance and test automation.",
-			CollegeName: "Amrit Science Campus", Degree: "BCA", FacultyOrMajor: "Computer Applications",
-			CurrentSemester: "7th Semester", GraduationYear: time.Now().Year() + 1, IsSearchable: true, ProfileCompletionPercent: 100,
-		}},
-		{key: "nima", profile: models.StudentProfile{
-			ID: seedID("student-profile", "nima"), UserID: users["nima"].ID,
-			FullName: "Nima Sherpa", Phone: "+977-9800000015", Location: "Kathmandu, Nepal",
-			Bio:         "Computer science student building practical mobile and web applications.",
-			CollegeName: "Amrit Science Campus", Degree: "BSc CSIT", FacultyOrMajor: "Computer Science",
-			CurrentSemester: "8th Semester", GraduationYear: time.Now().Year(), IsSearchable: true, ProfileCompletionPercent: 100,
-		}},
-	}
-
-	result := make(map[string]models.StudentProfile, len(profiles))
-	for _, item := range profiles {
-		profile := item.profile
-		collegeKey := "student"
-		if item.key == "student2" {
-			collegeKey = "student2"
-		}
-		collegeID := seedID("college", collegeKey)
-		profile.CollegeID = &collegeID
-		if err := saveStudentProfile(tx, &profile); err != nil {
-			return nil, fmt.Errorf("upsert student profile %s: %w", item.key, err)
-		}
-		if err := tx.Where("user_id = ?", profile.UserID).First(&profile).Error; err != nil {
-			return nil, fmt.Errorf("load student profile %s: %w", item.key, err)
-		}
-		result[item.key] = profile
-	}
-	return result, nil
-}
-
-func seedRecruiterProfiles(tx *gorm.DB, users map[string]models.User) (map[string]models.RecruiterProfile, error) {
-	profiles := []struct {
-		key     string
-		profile models.RecruiterProfile
-	}{
-		{key: "employer", profile: models.RecruiterProfile{
-			ID: seedID("recruiter-profile", "techkarkhana"), UserID: users["employer"].ID,
-			OrganizationName: "TechKarkhana Nepal", Designation: "Talent Acquisition Lead",
-			OrganizationWebsite: "https://techkarkhana.example", OrganizationAddress: "Kupondole, Lalitpur",
-			Industry: "Software and Technology", OrganizationSize: "51-200",
-			OrganizationAbout:  "A Nepal-based product engineering company building practical software for growing businesses.",
-			VerificationStatus: string(enums.OrganizationVerificationApproved),
-		}},
-		{key: "employer_pending", profile: models.RecruiterProfile{
-			ID: seedID("recruiter-profile", "himalayan-analytics"), UserID: users["employer_pending"].ID,
-			OrganizationName: "Himalayan Analytics", Designation: "People Operations Manager",
-			OrganizationWebsite: "https://himalayan-analytics.example", OrganizationAddress: "Naxal, Kathmandu",
-			Industry: "Data and Analytics", OrganizationSize: "11-50",
-			OrganizationAbout:  "An analytics studio helping local organizations make responsible, data-informed decisions.",
-			VerificationStatus: string(enums.OrganizationVerificationPending),
-		}},
-		{key: "employer_rejected", profile: models.RecruiterProfile{
-			ID: seedID("recruiter-profile", "brightpath"), UserID: users["employer_rejected"].ID,
-			OrganizationName: "BrightPath Media", Designation: "Hiring Coordinator",
-			OrganizationWebsite: "https://brightpath-media.example", OrganizationAddress: "Baneshwor, Kathmandu",
-			Industry: "Marketing and Advertising", OrganizationSize: "1-10",
-			OrganizationAbout:  "A small digital media agency serving education and hospitality clients.",
-			VerificationStatus: string(enums.OrganizationVerificationRejected),
-		}},
-	}
-
-	result := make(map[string]models.RecruiterProfile, len(profiles))
-	for _, item := range profiles {
-		profile := item.profile
-		if err := saveRecruiterProfile(tx, &profile); err != nil {
-			return nil, fmt.Errorf("upsert recruiter profile %s: %w", item.key, err)
-		}
-		if err := tx.Where("user_id = ?", profile.UserID).First(&profile).Error; err != nil {
-			return nil, fmt.Errorf("load recruiter profile %s: %w", item.key, err)
-		}
-		result[item.key] = profile
-	}
-	return result, nil
-}
-
-func seedStudentDetails(tx *gorm.DB, students map[string]models.StudentProfile) error {
-	now := time.Now().UTC()
-	projectStart := now.AddDate(0, -5, 0)
-	projectEnd := now.AddDate(0, -2, 0)
-	issued := now.AddDate(0, -8, 0)
-
-	records := []interface{}{
-		&models.StudentEducation{ID: seedID("education", "aarav"), ProfileID: students["student"].ID, Institute: "Amrit Science Campus", Degree: "BSc CSIT", Field: "Computer Science", StartYear: now.Year() - 3, EndYear: now.Year() + 1, Description: "Coursework in software engineering, databases, networking, and distributed systems."},
-		&models.StudentEducation{ID: seedID("education", "maya"), ProfileID: students["student2"].ID, Institute: "Prithvi Narayan Campus", Degree: "BIT", Field: "Information Technology", StartYear: now.Year() - 3, EndYear: now.Year() + 1, Description: "Coursework in information systems, human-computer interaction, and data analysis."},
-		&models.StudentSkill{ID: seedID("skill", "aarav-go"), ProfileID: students["student"].ID, Name: "Go", Level: "intermediate"},
-		&models.StudentSkill{ID: seedID("skill", "aarav-react"), ProfileID: students["student"].ID, Name: "React", Level: "advanced"},
-		&models.StudentSkill{ID: seedID("skill", "aarav-postgresql"), ProfileID: students["student"].ID, Name: "PostgreSQL", Level: "intermediate"},
-		&models.StudentSkill{ID: seedID("skill", "maya-figma"), ProfileID: students["student2"].ID, Name: "Figma", Level: "advanced"},
-		&models.StudentSkill{ID: seedID("skill", "maya-sql"), ProfileID: students["student2"].ID, Name: "SQL", Level: "intermediate"},
-		&models.StudentSkill{ID: seedID("skill", "maya-python"), ProfileID: students["student2"].ID, Name: "Python", Level: "intermediate"},
-		&models.StudentProject{ID: seedID("project", "aarav-campus-events"), ProfileID: students["student"].ID, Title: "Campus Events Platform", Description: "A responsive event discovery and registration platform for student clubs.", Link: "https://github.com/aarav-sharma-seed/campus-events", StartDate: &projectStart, EndDate: &projectEnd},
-		&models.StudentProject{ID: seedID("project", "maya-transit"), ProfileID: students["student2"].ID, Title: "Pokhara Transit Study", Description: "A dashboard and UX study exploring common public transit pain points.", Link: "https://maya-gurung.example/transit-study", StartDate: &projectStart, EndDate: &projectEnd},
-		&models.StudentCertification{ID: seedID("certification", "aarav-web"), ProfileID: students["student"].ID, Name: "Web Development Fundamentals", Authority: "freeCodeCamp", License: "SEED-AARAV-WEB", URL: "https://freecodecamp.org", IssuedDate: &issued},
-		&models.StudentCertification{ID: seedID("certification", "maya-data"), ProfileID: students["student2"].ID, Name: "Data Analytics Foundations", Authority: "Coursera", License: "SEED-MAYA-DATA", URL: "https://coursera.org", IssuedDate: &issued},
-	}
-	for _, record := range records {
-		if err := upsert(tx, record, "id"); err != nil {
-			return fmt.Errorf("upsert student detail: %w", err)
-		}
-	}
-	return nil
-}
-
-func seedVerifications(tx *gorm.DB, users map[string]models.User, recruiters map[string]models.RecruiterProfile) (int, error) {
-	now := time.Now().UTC()
-	submitted := now.AddDate(0, 0, -10)
-	reviewed := now.AddDate(0, 0, -8)
-	pendingSubmitted := now.AddDate(0, 0, -2)
-	rejectedSubmitted := now.AddDate(0, 0, -14)
-	rejectedReviewed := now.AddDate(0, 0, -12)
-	adminID := users["admin"].ID
-
-	verifications := []models.OrganizationVerification{
-		{ID: seedID("verification", "techkarkhana"), RecruiterProfileID: recruiters["employer"].ID, Status: enums.OrganizationVerificationApproved, Method: enums.VerificationMethodManual, OrganizationEmail: "careers@techkarkhana.example", EmailDomain: "techkarkhana.example", ReviewedBy: &adminID, ReviewNotes: "Organization details confirmed for the development seed.", SubmittedAt: &submitted, ReviewedAt: &reviewed, VerifiedAt: &reviewed},
-		{ID: seedID("verification", "himalayan-analytics"), RecruiterProfileID: recruiters["employer_pending"].ID, Status: enums.OrganizationVerificationPending, Method: enums.VerificationMethodDocument, OrganizationEmail: "people@himalayan-analytics.example", EmailDomain: "himalayan-analytics.example", DocumentType: "Company registration certificate", SubmittedAt: &pendingSubmitted},
-		{ID: seedID("verification", "brightpath"), RecruiterProfileID: recruiters["employer_rejected"].ID, Status: enums.OrganizationVerificationRejected, Method: enums.VerificationMethodManual, OrganizationEmail: "hiring@brightpath-media.example", EmailDomain: "brightpath-media.example", ReviewedBy: &adminID, RejectionReason: "Organization registration details could not be confirmed.", ReviewNotes: "Employer may resubmit with a valid registration document.", SubmittedAt: &rejectedSubmitted, ReviewedAt: &rejectedReviewed},
-	}
-	for i := range verifications {
-		if err := saveVerification(tx, &verifications[i]); err != nil {
-			return 0, fmt.Errorf("upsert organization verification: %w", err)
-		}
-	}
-	return len(verifications), nil
-}
-
 func seedInternships(tx *gorm.DB, users map[string]models.User) (map[string]models.Internship, error) {
 	now := time.Now().UTC()
 	future := func(days int) *time.Time { value := now.AddDate(0, 0, days); return &value }
 	past := func(days int) *time.Time { value := now.AddDate(0, 0, -days); return &value }
-	approvedEmployer := users["employer"].ID
+	employerID := users["employer"].ID
 
 	items := []struct {
 		key        string
 		internship models.Internship
 	}{
-		{key: "frontend", internship: models.Internship{ID: seedID("internship", "frontend"), IssuedBy: approvedEmployer, Title: "Frontend Engineering Intern", Description: "Build accessible, responsive product interfaces with React and TypeScript alongside an experienced engineering team.", Location: "Lalitpur, Nepal", WorkMode: "hybrid", InternshipType: "paid", Duration: 3, DurationUnit: "months", WorkingHours: "10:00 AM - 5:00 PM", RequiredSkills: "JavaScript, React, HTML, CSS", PreferredSkills: "TypeScript, Next.js, Git", RequiredEducation: "Currently pursuing a computing-related degree", EligiblePrograms: "BSc CSIT, BIT, BCA, Computer Engineering", EligibleSemester: "5th semester and above", StipendAmount: 18000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 2, StartDate: future(30), ApplicationDeadline: future(21), ApplicationEmail: "careers@techkarkhana.example", Responsibilities: "Implement product features, review UI quality, and collaborate with design and backend teams.", Benefits: "Mentorship, lunch allowance, flexible hybrid schedule", Status: enums.InternshipStatusPublished, IsActive: true}},
-		{key: "backend", internship: models.Internship{ID: seedID("internship", "backend"), IssuedBy: approvedEmployer, Title: "Backend Go Intern", Description: "Help design and implement secure APIs and background services for a growing business software platform.", Location: "Kathmandu, Nepal", WorkMode: "hybrid", InternshipType: "paid", Duration: 4, DurationUnit: "months", WorkingHours: "9:30 AM - 5:30 PM", RequiredSkills: "Go, REST APIs, SQL", PreferredSkills: "PostgreSQL, Docker, Git", RequiredEducation: "Currently pursuing a computing-related degree", EligiblePrograms: "BSc CSIT, BIT, BCA, Computer Engineering", EligibleSemester: "5th semester and above", StipendAmount: 22000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 2, StartDate: future(35), ApplicationDeadline: future(28), ApplicationEmail: "careers@techkarkhana.example", Responsibilities: "Develop APIs, write tests, investigate defects, and contribute to technical documentation.", Benefits: "Engineering mentorship, device allowance, potential full-time offer", Status: enums.InternshipStatusPublished, IsActive: true}},
-		{key: "product-design", internship: models.Internship{ID: seedID("internship", "product-design"), IssuedBy: approvedEmployer, Title: "Product Design Intern", Description: "Research user needs and turn findings into clear flows, prototypes, and polished interface specifications.", Location: "Remote", WorkMode: "remote", InternshipType: "paid", Duration: 12, DurationUnit: "weeks", WorkingHours: "Flexible, 25 hours per week", RequiredSkills: "Figma, Wireframing, User Research", PreferredSkills: "Design systems, Prototyping, Accessibility", RequiredEducation: "Open to students from any relevant program", EligiblePrograms: "BIT, BCA, BIM, Design", EligibleSemester: "4th semester and above", StipendAmount: 16000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 1, StartDate: future(25), ApplicationDeadline: future(18), ApplicationEmail: "design@techkarkhana.example", Responsibilities: "Create prototypes, support interviews, document design decisions, and review implementation.", Benefits: "Design mentorship, flexible hours, remote work", Status: enums.InternshipStatusPublished, IsActive: true}},
-		{key: "data-closed", internship: models.Internship{ID: seedID("internship", "data-closed"), IssuedBy: approvedEmployer, Title: "Data Analytics Intern", Description: "Prepare operational datasets and build concise dashboards that help teams understand product performance.", Location: "Kathmandu, Nepal", WorkMode: "onsite", InternshipType: "paid", Duration: 3, DurationUnit: "months", WorkingHours: "10:00 AM - 5:00 PM", RequiredSkills: "SQL, Spreadsheets, Data Visualization", PreferredSkills: "Python, Power BI", StipendAmount: 15000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 1, StartDate: past(20), ApplicationDeadline: past(30), Responsibilities: "Clean data, validate reports, and present weekly insights.", Benefits: "Mentorship and completion certificate", Status: enums.InternshipStatusClosed, IsActive: false}},
-		{key: "qa-private", internship: models.Internship{ID: seedID("internship", "qa-private"), IssuedBy: users["employer_pending"].ID, Title: "QA Automation Intern", Description: "Draft internship awaiting organization verification before it can be published to students.", Location: "Kathmandu, Nepal", WorkMode: "onsite", InternshipType: "paid", Duration: 3, DurationUnit: "months", RequiredSkills: "Testing fundamentals, JavaScript", PreferredSkills: "Playwright, Cypress", StipendAmount: 17000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 1, StartDate: future(40), ApplicationDeadline: future(32), Responsibilities: "Create test cases and automate high-value product workflows.", Benefits: "QA mentorship", Status: enums.InternshipStatusPrivate, IsActive: false}},
-		{key: "marketing-private", internship: models.Internship{ID: seedID("internship", "marketing-private"), IssuedBy: users["employer_rejected"].ID, Title: "Digital Marketing Intern", Description: "Private internship retained to demonstrate posting restrictions for an unverified organization.", Location: "Kathmandu, Nepal", WorkMode: "hybrid", InternshipType: "paid", Duration: 8, DurationUnit: "weeks", RequiredSkills: "Content writing, Social media", PreferredSkills: "SEO, Analytics", StipendAmount: 12000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 1, StartDate: future(30), ApplicationDeadline: future(20), Responsibilities: "Support campaign planning, publishing, and reporting.", Benefits: "Flexible schedule", Status: enums.InternshipStatusPrivate, IsActive: false}},
+		{key: "frontend", internship: models.Internship{ID: seedID("internship", "frontend"), IssuedBy: employerID, Title: "Frontend Engineering Intern", Description: "Build accessible, responsive product interfaces with React and TypeScript alongside an experienced engineering team.", Location: "Lalitpur, Nepal", WorkMode: "hybrid", InternshipType: "paid", Duration: 3, DurationUnit: "months", WorkingHours: "10:00 AM - 5:00 PM", RequiredSkills: "JavaScript, React, HTML, CSS", PreferredSkills: "TypeScript, Next.js, Git", RequiredEducation: "Currently pursuing a computing-related degree", EligiblePrograms: "BSc CSIT, BIT, BCA, Computer Engineering", EligibleSemester: "5th semester and above", StipendAmount: 18000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 2, StartDate: future(30), ApplicationDeadline: future(21), ApplicationEmail: "careers@techkarkhana.example", Responsibilities: "Implement product features, review UI quality, and collaborate with design and backend teams.", Benefits: "Mentorship, lunch allowance, flexible hybrid schedule", Status: enums.InternshipStatusPublished}},
+		{key: "backend", internship: models.Internship{ID: seedID("internship", "backend"), IssuedBy: employerID, Title: "Backend Go Intern", Description: "Help design and implement secure APIs and background services for a growing business software platform.", Location: "Kathmandu, Nepal", WorkMode: "hybrid", InternshipType: "paid", Duration: 4, DurationUnit: "months", WorkingHours: "9:30 AM - 5:30 PM", RequiredSkills: "Go, REST APIs, SQL", PreferredSkills: "PostgreSQL, Docker, Git", RequiredEducation: "Currently pursuing a computing-related degree", EligiblePrograms: "BSc CSIT, BIT, BCA, Computer Engineering", EligibleSemester: "5th semester and above", StipendAmount: 22000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 2, StartDate: future(35), ApplicationDeadline: future(28), ApplicationEmail: "careers@techkarkhana.example", Responsibilities: "Develop APIs, write tests, investigate defects, and contribute to technical documentation.", Benefits: "Engineering mentorship, device allowance, potential full-time offer", Status: enums.InternshipStatusPublished}},
+		{key: "product-design", internship: models.Internship{ID: seedID("internship", "product-design"), IssuedBy: employerID, Title: "Product Design Intern", Description: "Research user needs and turn findings into clear flows, prototypes, and polished interface specifications.", Location: "Remote", WorkMode: "remote", InternshipType: "paid", Duration: 12, DurationUnit: "weeks", WorkingHours: "Flexible, 25 hours per week", RequiredSkills: "Figma, Wireframing, User Research", PreferredSkills: "Design systems, Prototyping, Accessibility", RequiredEducation: "Open to students from any relevant program", EligiblePrograms: "BIT, BCA, BIM, Design", EligibleSemester: "4th semester and above", StipendAmount: 16000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 1, StartDate: future(25), ApplicationDeadline: future(18), ApplicationEmail: "design@techkarkhana.example", Responsibilities: "Create prototypes, support interviews, document design decisions, and review implementation.", Benefits: "Design mentorship, flexible hours, remote work", Status: enums.InternshipStatusPublished}},
+		{key: "data-closed", internship: models.Internship{ID: seedID("internship", "data-closed"), IssuedBy: employerID, Title: "Data Analytics Intern", Description: "Prepare operational datasets and build concise dashboards that help teams understand product performance.", Location: "Kathmandu, Nepal", WorkMode: "onsite", InternshipType: "paid", Duration: 3, DurationUnit: "months", WorkingHours: "10:00 AM - 5:00 PM", RequiredSkills: "SQL, Spreadsheets, Data Visualization", PreferredSkills: "Python, Power BI", StipendAmount: 15000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 1, StartDate: past(20), ApplicationDeadline: past(30), Responsibilities: "Clean data, validate reports, and present weekly insights.", Benefits: "Mentorship and completion certificate", Status: enums.InternshipStatusClosed}},
+		{key: "qa-private", internship: models.Internship{ID: seedID("internship", "qa-private"), IssuedBy: users["employer_pending"].ID, Title: "QA Automation Intern", Description: "Draft internship being prepared before publication to students.", Location: "Kathmandu, Nepal", WorkMode: "onsite", InternshipType: "paid", Duration: 3, DurationUnit: "months", RequiredSkills: "Testing fundamentals, JavaScript", PreferredSkills: "Playwright, Cypress", StipendAmount: 17000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 1, StartDate: future(40), ApplicationDeadline: future(32), Responsibilities: "Create test cases and automate high-value product workflows.", Benefits: "QA mentorship", Status: enums.InternshipStatusPrivate}},
+		{key: "marketing-private", internship: models.Internship{ID: seedID("internship", "marketing-private"), IssuedBy: users["employer_rejected"].ID, Title: "Digital Marketing Intern", Description: "Private internship saved as a draft for the employer to finish later.", Location: "Kathmandu, Nepal", WorkMode: "hybrid", InternshipType: "paid", Duration: 8, DurationUnit: "weeks", RequiredSkills: "Content writing, Social media", PreferredSkills: "SEO, Analytics", StipendAmount: 12000, StipendCurrency: "NPR", StipendPeriod: "monthly", VacancyCount: 1, StartDate: future(30), ApplicationDeadline: future(20), Responsibilities: "Support campaign planning, publishing, and reporting.", Benefits: "Flexible schedule", Status: enums.InternshipStatusPrivate}},
 	}
 
 	result := make(map[string]models.Internship, len(items))
@@ -388,149 +135,8 @@ func seedInternships(tx *gorm.DB, users map[string]models.User) (map[string]mode
 		result[item.key] = internship
 	}
 
-	titles := []string{"Software Engineering", "Frontend Development", "Backend Development", "Mobile Development", "Data Analytics", "UI/UX Design", "Quality Assurance", "DevOps", "Digital Marketing", "Business Analysis"}
-	locations := []string{"Kathmandu, Nepal", "Lalitpur, Nepal", "Bhaktapur, Nepal", "Pokhara, Nepal", "Remote"}
-	workModes := []string{"onsite", "hybrid", "remote"}
-	for i := 1; i <= 104; i++ {
-		key := fmt.Sprintf("generated-%03d", i)
-		title := titles[(i-1)%len(titles)]
-		internship := models.Internship{
-			ID:                  seedID("internship", key),
-			IssuedBy:            approvedEmployer,
-			Title:               fmt.Sprintf("%s Intern %02d", title, i),
-			Description:         fmt.Sprintf("Gain practical %s experience while working with an experienced team on real projects.", title),
-			Location:            locations[(i-1)%len(locations)],
-			WorkMode:            workModes[(i-1)%len(workModes)],
-			InternshipType:      "paid",
-			Duration:            3 + i%3,
-			DurationUnit:        "months",
-			WorkingHours:        "10:00 AM - 5:00 PM",
-			RequiredSkills:      "Communication, Teamwork, Problem solving",
-			PreferredSkills:     "Git, Documentation",
-			RequiredEducation:   "Currently pursuing a relevant degree",
-			EligiblePrograms:    "BSc CSIT, BIT, BCA, BIM, Computer Engineering",
-			EligibleSemester:    "4th semester and above",
-			StipendAmount:       float64(12000 + i%11*1000),
-			StipendCurrency:     "NPR",
-			StipendPeriod:       "monthly",
-			VacancyCount:        1 + i%3,
-			StartDate:           future(30 + i%30),
-			ApplicationDeadline: future(14 + i%14),
-			ApplicationEmail:    "careers@techkarkhana.example",
-			Responsibilities:    "Support project delivery, collaborate with the team, and document completed work.",
-			Benefits:            "Mentorship, completion certificate, and flexible working arrangements",
-			Status:              enums.InternshipStatusPublished,
-			IsActive:            true,
-		}
-		if i <= 10 {
-			internship.StartDate = past(210 - i*7)
-			internship.ApplicationDeadline = past(225 - i*7)
-			internship.Status = enums.InternshipStatusClosed
-			internship.IsActive = false
-		}
-		if err := upsert(tx, &internship, "id"); err != nil {
-			return nil, fmt.Errorf("upsert internship %s: %w", key, err)
-		}
-		result[key] = internship
-	}
-	return result, nil
-}
-
-func seedApplications(tx *gorm.DB, students map[string]models.StudentProfile, internships map[string]models.Internship) (map[string]models.InternshipApplication, error) {
-	now := time.Now().UTC()
-	timePtr := func(value time.Time) *time.Time { return &value }
-	seeded := []struct {
-		key string
-		app models.InternshipApplication
-	}{
-		{key: "aarav-frontend", app: models.InternshipApplication{ID: seedID("application", "aarav-frontend"), InternshipID: internships["frontend"].ID, StudentID: students["student"].ID, Status: models.ApplicationStatusShortlisted, EmployerNote: "Strong frontend portfolio; invite to the technical conversation.", AppliedAt: now.AddDate(0, 0, -9), ReviewedAt: timePtr(now.AddDate(0, 0, -8)), ShortlistedAt: timePtr(now.AddDate(0, 0, -6))}},
-		{key: "aarav-backend", app: models.InternshipApplication{ID: seedID("application", "aarav-backend"), InternshipID: internships["backend"].ID, StudentID: students["student"].ID, Status: models.ApplicationStatusSubmitted, AppliedAt: now.AddDate(0, 0, -2)}},
-		{key: "aarav-data", app: models.InternshipApplication{ID: seedID("application", "aarav-data"), InternshipID: internships["data-closed"].ID, StudentID: students["student"].ID, Status: models.ApplicationStatusRejected, EmployerNote: "Role required more advanced analytics experience for this intake.", AppliedAt: now.AddDate(0, -2, 0), ReviewedAt: timePtr(now.AddDate(0, -2, 2)), RejectedAt: timePtr(now.AddDate(0, -2, 3))}},
-		{key: "maya-frontend", app: models.InternshipApplication{ID: seedID("application", "maya-frontend"), InternshipID: internships["frontend"].ID, StudentID: students["student2"].ID, Status: models.ApplicationStatusReviewing, EmployerNote: "Application is under portfolio review.", AppliedAt: now.AddDate(0, 0, -5), ReviewedAt: timePtr(now.AddDate(0, 0, -4))}},
-		{key: "maya-backend", app: models.InternshipApplication{ID: seedID("application", "maya-backend"), InternshipID: internships["backend"].ID, StudentID: students["student2"].ID, Status: models.ApplicationStatusAccepted, EmployerNote: "Offer accepted for the upcoming cohort.", AppliedAt: now.AddDate(0, -1, -5), ReviewedAt: timePtr(now.AddDate(0, -1, -3)), ShortlistedAt: timePtr(now.AddDate(0, -1, 1)), AcceptedAt: timePtr(now.AddDate(0, 0, -18))}},
-		{key: "maya-design", app: models.InternshipApplication{ID: seedID("application", "maya-design"), InternshipID: internships["product-design"].ID, StudentID: students["student2"].ID, Status: models.ApplicationStatusWithdrawn, AppliedAt: now.AddDate(0, 0, -12), WithdrawnAt: timePtr(now.AddDate(0, 0, -10))}},
-	}
-
-	ongoingStudents := []string{"anisha", "rohan", "priya", "sagar", "nima"}
-	for i, student := range ongoingStudents {
-		key := student + "-ongoing"
-		start := now.AddDate(0, 0, -(75 - i*12))
-		accepted := start.AddDate(0, 0, -3)
-		seeded = append(seeded, struct {
-			key string
-			app models.InternshipApplication
-		}{key: key, app: models.InternshipApplication{
-			ID: seedID("application", key), InternshipID: internships[fmt.Sprintf("generated-%03d", i+1)].ID,
-			StudentID: students[student].ID, Status: models.ApplicationStatusAccepted, ProgressStatus: "ongoing",
-			EmployerNote: "Selected for the internship cohort.", AppliedAt: accepted.AddDate(0, 0, -12),
-			ReviewedAt: timePtr(accepted.AddDate(0, 0, -8)), ShortlistedAt: timePtr(accepted.AddDate(0, 0, -5)),
-			AcceptedAt: timePtr(accepted), StartDate: timePtr(start),
-		}})
-	}
-
-	completedStudents := []string{"anisha", "rohan", "priya", "sagar", "nima"}
-	for i, student := range completedStudents {
-		key := student + "-completed"
-		end := now.AddDate(0, 0, -(10 + i*9))
-		start := end.AddDate(0, -3, 0)
-		accepted := start.AddDate(0, 0, -4)
-		seeded = append(seeded, struct {
-			key string
-			app models.InternshipApplication
-		}{key: key, app: models.InternshipApplication{
-			ID: seedID("application", key), InternshipID: internships[fmt.Sprintf("generated-%03d", i+6)].ID,
-			StudentID: students[student].ID, Status: models.ApplicationStatusAccepted, ProgressStatus: "completed",
-			EmployerNote: "Successfully completed the internship placement.", AppliedAt: accepted.AddDate(0, 0, -14),
-			ReviewedAt: timePtr(accepted.AddDate(0, 0, -10)), ShortlistedAt: timePtr(accepted.AddDate(0, 0, -7)),
-			AcceptedAt: timePtr(accepted), StartDate: timePtr(start), EndDate: timePtr(end),
-		}})
-	}
-
-	result := make(map[string]models.InternshipApplication, len(seeded))
-	for i := range seeded {
-		application := &seeded[i].app
-		if application.ProgressStatus == "" {
-			application.ProgressStatus = "applied"
-		}
-		switch application.Status {
-		case models.ApplicationStatusAccepted:
-			if application.ProgressStatus == "applied" {
-				application.ProgressStatus = "ongoing"
-				application.StartDate = application.AcceptedAt
-			}
-		case models.ApplicationStatusRejected, models.ApplicationStatusWithdrawn:
-			application.ProgressStatus = "cancelled"
-		}
-		if err := saveApplication(tx, application); err != nil {
-			return nil, fmt.Errorf("upsert internship application: %w", err)
-		}
-		result[seeded[i].key] = *application
-	}
 	return result, nil
 }
-
-func seedEvaluations(tx *gorm.DB, applications map[string]models.InternshipApplication, employerID uuid.UUID) (int, error) {
-	keys := []string{"anisha-completed", "rohan-completed", "priya-completed", "sagar-completed", "nima-completed"}
-	remarks := []string{
-		"Delivered polished work consistently and communicated blockers early.",
-		"Built dependable backend features and responded well to review feedback.",
-		"Turned raw data into clear findings and presented them confidently.",
-		"Improved regression coverage and documented reproducible issues clearly.",
-		"Completed assigned mobile features on time and collaborated effectively.",
-	}
-	for i, key := range keys {
-		evaluation := models.InternshipEvaluation{
-			ID: seedID("evaluation", key), ApplicationID: applications[key].ID, SubmittedBy: employerID,
-			Scores:  map[string]int{"technical": 82 + i*2, "communication": 88 - i, "reliability": 90 + i},
-			Remarks: remarks[i], Status: "submitted", SubmittedAt: applications[key].EndDate.Add(24 * time.Hour),
-		}
-		if err := saveEvaluation(tx, &evaluation); err != nil {
-			return 0, fmt.Errorf("upsert internship evaluation: %w", err)
-		}
-	}
-	return len(keys), nil
-}
-
 func upsert(tx *gorm.DB, value interface{}, conflictColumn string) error {
 	return tx.Clauses(clause.OnConflict{
 		Columns:   []clause.Column{{Name: conflictColumn}},
@@ -566,20 +172,6 @@ func saveRecruiterProfile(tx *gorm.DB, profile *models.RecruiterProfile) error {
 	return tx.Save(profile).Error
 }
 
-func saveVerification(tx *gorm.DB, verification *models.OrganizationVerification) error {
-	var existing models.OrganizationVerification
-	lookup := tx.Where("recruiter_profile_id = ?", verification.RecruiterProfileID).Limit(1).Find(&existing)
-	if lookup.Error != nil {
-		return lookup.Error
-	}
-	if lookup.RowsAffected == 0 {
-		return tx.Create(verification).Error
-	}
-	verification.ID = existing.ID
-	verification.CreatedAt = existing.CreatedAt
-	return tx.Save(verification).Error
-}
-
 func saveApplication(tx *gorm.DB, application *models.InternshipApplication) error {
 	var existing models.InternshipApplication
 	lookup := tx.Where(
@@ -599,19 +191,6 @@ func saveApplication(tx *gorm.DB, application *models.InternshipApplication) err
 	return tx.Save(application).Error
 }
 
-func saveEvaluation(tx *gorm.DB, evaluation *models.InternshipEvaluation) error {
-	var existing models.InternshipEvaluation
-	lookup := tx.Where("application_id = ?", evaluation.ApplicationID).Limit(1).Find(&existing)
-	if lookup.Error != nil {
-		return lookup.Error
-	}
-	if lookup.RowsAffected == 0 {
-		return tx.Create(evaluation).Error
-	}
-	evaluation.ID = existing.ID
-	return tx.Save(evaluation).Error
-}
-
 func seedID(kind, name string) uuid.UUID {
 	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("student-job-portal/seed/"+kind+"/"+name))
 }

# internal/seeder/seeder_test.go

diff --git a/internal/seeder/seeder_test.go b/internal/seeder/seeder_test.go
index 77e0048..a4cd7d5 100644
--- a/internal/seeder/seeder_test.go
+++ b/internal/seeder/seeder_test.go
@@ -26,8 +26,8 @@ func TestRunCreatesAnIdempotentDevelopmentDataset(t *testing.T) {
 		if err != nil {
 			t.Fatalf("seed run %d: %v", run, err)
 		}
-		if summary.Users != 6 || summary.StudentProfiles != 2 || summary.RecruiterProfiles != 3 ||
-			summary.OrganizationVerifications != 3 || summary.Internships != 100 || summary.Applications != 6 {
+		if summary.Colleges != 2 || summary.Users != 13 || summary.StudentProfiles != 7 || summary.RecruiterProfiles != 3 ||
+			summary.OrganizationVerifications != 3 || summary.Internships != 110 || summary.Applications != 16 || summary.Evaluations != 5 {
 			t.Fatalf("unexpected summary after run %d: %+v", run, summary)
 		}
 		if run == 1 {
@@ -44,12 +44,14 @@ func TestRunCreatesAnIdempotentDevelopmentDataset(t *testing.T) {
 		}
 	}
 
-	assertCount(t, db, &models.User{}, 6)
-	assertCount(t, db, &models.StudentProfile{}, 2)
+	assertCount(t, db, &models.College{}, 2)
+	assertCount(t, db, &models.User{}, 13)
+	assertCount(t, db, &models.StudentProfile{}, 7)
 	assertCount(t, db, &models.RecruiterProfile{}, 3)
 	assertCount(t, db, &models.OrganizationVerification{}, 3)
-	assertCount(t, db, &models.Internship{}, 100)
-	assertCount(t, db, &models.InternshipApplication{}, 6)
+	assertCount(t, db, &models.Internship{}, 110)
+	assertCount(t, db, &models.InternshipApplication{}, 16)
+	assertCount(t, db, &models.InternshipEvaluation{}, 5)
 
 	var student models.User
 	if err := db.Where("email = ?", seeder.StudentEmail).First(&student).Error; err != nil {
@@ -66,8 +68,8 @@ func TestRunCreatesAnIdempotentDevelopmentDataset(t *testing.T) {
 	if err := db.Model(&models.StudentProfile{}).Where("profile_completion_percent = ?", 100).Count(&completeProfiles).Error; err != nil {
 		t.Fatalf("count complete profiles: %v", err)
 	}
-	if completeProfiles != 2 {
-		t.Fatalf("complete student profiles = %d, want 2", completeProfiles)
+	if completeProfiles != 7 {
+		t.Fatalf("complete student profiles = %d, want 7", completeProfiles)
 	}
 
 	var approvedRecruiter models.RecruiterProfile
diff --git a/internal/seeder/seeder_test.go b/internal/seeder/seeder_test.go
index a4cd7d5..ef27d6c 100644
--- a/internal/seeder/seeder_test.go
+++ b/internal/seeder/seeder_test.go
@@ -1,99 +1,50 @@
 package seeder_test
 
 import (
-	"testing"
-
 	"github.com/prasanna/student-job-portal/backend/internal/database"
-	"github.com/prasanna/student-job-portal/backend/internal/enums"
 	"github.com/prasanna/student-job-portal/backend/internal/models"
 	"github.com/prasanna/student-job-portal/backend/internal/seeder"
 	"golang.org/x/crypto/bcrypt"
 	"gorm.io/driver/sqlite"
 	"gorm.io/gorm"
+	"testing"
 )
 
 func TestRunCreatesAnIdempotentDevelopmentDataset(t *testing.T) {
 	db, err := gorm.Open(sqlite.Open("file:seeder-test?mode=memory&cache=shared"), &gorm.Config{})
 	if err != nil {
-		t.Fatalf("open test database: %v", err)
+		t.Fatal(err)
 	}
 	if err := database.AutoMigrate(db); err != nil {
-		t.Fatalf("migrate test database: %v", err)
+		t.Fatal(err)
 	}
-
-	for run := 1; run <= 2; run++ {
+	for run := 0; run < 2; run++ {
 		summary, err := seeder.Run(db, seeder.DefaultPassword)
 		if err != nil {
-			t.Fatalf("seed run %d: %v", run, err)
+			t.Fatal(err)
 		}
-		if summary.Colleges != 2 || summary.Users != 13 || summary.StudentProfiles != 7 || summary.RecruiterProfiles != 3 ||
-			summary.OrganizationVerifications != 3 || summary.Internships != 110 || summary.Applications != 16 || summary.Evaluations != 5 {
-			t.Fatalf("unexpected summary after run %d: %+v", run, summary)
+		if summary != (seeder.Summary{Users: 5, StudentProfiles: 2, RecruiterProfiles: 3, Internships: 6, Applications: 6}) {
+			t.Fatalf("unexpected summary: %+v", summary)
 		}
-		if run == 1 {
-			var alternate models.Internship
-			if err := db.Where("title = ?", "Software Engineering Intern 01").First(&alternate).Error; err != nil {
-				t.Fatalf("load alternate internship: %v", err)
-			}
-			moved := db.Model(&models.InternshipApplication{}).
-				Where("status = ?", models.ApplicationStatusSubmitted).
-				Update("internship_id", alternate.ID)
-			if moved.Error != nil || moved.RowsAffected != 1 {
-				t.Fatalf("move seeded application: rows=%d error=%v", moved.RowsAffected, moved.Error)
-			}
+	}
+	for _, item := range []struct {
+		model interface{}
+		count int64
+	}{{&models.User{}, 5}, {&models.StudentProfile{}, 2}, {&models.RecruiterProfile{}, 3}, {&models.Internship{}, 6}, {&models.InternshipApplication{}, 6}} {
+		var count int64
+		if err := db.Model(item.model).Count(&count).Error; err != nil || count != item.count {
+			t.Fatalf("count %T = %d: %v", item.model, count, err)
 		}
 	}
-
-	assertCount(t, db, &models.College{}, 2)
-	assertCount(t, db, &models.User{}, 13)
-	assertCount(t, db, &models.StudentProfile{}, 7)
-	assertCount(t, db, &models.RecruiterProfile{}, 3)
-	assertCount(t, db, &models.OrganizationVerification{}, 3)
-	assertCount(t, db, &models.Internship{}, 110)
-	assertCount(t, db, &models.InternshipApplication{}, 16)
-	assertCount(t, db, &models.InternshipEvaluation{}, 5)
-
 	var student models.User
 	if err := db.Where("email = ?", seeder.StudentEmail).First(&student).Error; err != nil {
-		t.Fatalf("load seeded student: %v", err)
-	}
-	if student.Role != models.RoleStudent {
-		t.Fatalf("student role = %q, want %q", student.Role, models.RoleStudent)
+		t.Fatal(err)
 	}
 	if err := bcrypt.CompareHashAndPassword([]byte(student.PasswordHash), []byte(seeder.DefaultPassword)); err != nil {
-		t.Fatalf("seeded password does not match: %v", err)
-	}
-
-	var completeProfiles int64
-	if err := db.Model(&models.StudentProfile{}).Where("profile_completion_percent = ?", 100).Count(&completeProfiles).Error; err != nil {
-		t.Fatalf("count complete profiles: %v", err)
-	}
-	if completeProfiles != 7 {
-		t.Fatalf("complete student profiles = %d, want 7", completeProfiles)
-	}
-
-	var approvedRecruiter models.RecruiterProfile
-	if err := db.Where("verification_status = ?", enums.OrganizationVerificationApproved).First(&approvedRecruiter).Error; err != nil {
-		t.Fatalf("load approved recruiter: %v", err)
-	}
-	var published int64
-	if err := db.Model(&models.Internship{}).
-		Where("issued_by = ? AND status = ? AND is_active = ?", approvedRecruiter.UserID, enums.InternshipStatusPublished, true).
-		Count(&published).Error; err != nil {
-		t.Fatalf("count published internships: %v", err)
-	}
-	if published != 97 {
-		t.Fatalf("published internships = %d, want 97", published)
-	}
-}
-
-func assertCount(t *testing.T, db *gorm.DB, model interface{}, want int64) {
-	t.Helper()
-	var got int64
-	if err := db.Model(model).Count(&got).Error; err != nil {
-		t.Fatalf("count %T: %v", model, err)
+		t.Fatal(err)
 	}
-	if got != want {
-		t.Fatalf("count %T = %d, want %d", model, got, want)
+	var pending int64
+	if err := db.Model(&models.InternshipApplication{}).Where("status = ?", models.ApplicationStatusSubmitted).Count(&pending).Error; err != nil || pending != 3 {
+		t.Fatalf("submitted applications = %d: %v", pending, err)
 	}
 }

# internal/services/college_service.go

diff --git a/internal/services/college_service.go b/internal/services/college_service.go
new file mode 100644
index 0000000..3a1fada
--- /dev/null
+++ b/internal/services/college_service.go
@@ -0,0 +1,178 @@
+package services
+
+import (
+	"context"
+	"errors"
+	"fmt"
+	"net/mail"
+	"strings"
+	"time"
+
+	"github.com/google/uuid"
+	"github.com/prasanna/student-job-portal/backend/internal/models"
+	"github.com/prasanna/student-job-portal/backend/internal/repositories"
+	"gorm.io/gorm"
+)
+
+var (
+	ErrInvalidCollegeData = errors.New("invalid college data")
+	ErrCollegeNotFound    = errors.New("college not found")
+	ErrCollegeAccess      = errors.New("college access denied")
+	ErrCollegeConflict    = errors.New("college operation conflicts with existing records")
+)
+
+type CollegeService struct {
+	repo     *repositories.CollegeRepository
+	users    *repositories.UserRepository
+	students *repositories.StudentRepository
+	auth     *AuthService
+}
+
+func NewCollegeService(repo *repositories.CollegeRepository, users *repositories.UserRepository, students *repositories.StudentRepository, auth *AuthService) *CollegeService {
+	return &CollegeService{repo: repo, users: users, students: students, auth: auth}
+}
+
+func (s *CollegeService) Get(ctx context.Context, id uuid.UUID) (*models.College, error) {
+	if id == uuid.Nil {
+		return nil, ErrInvalidCollegeData
+	}
+	college, err := s.repo.GetByID(ctx, id)
+	if errors.Is(err, gorm.ErrRecordNotFound) {
+		return nil, ErrCollegeNotFound
+	}
+	return college, err
+}
+
+// Resolve reads current membership, so stale JWT claims cannot retain tenant access.
+func (s *CollegeService) Resolve(ctx context.Context, userID uuid.UUID) (*models.College, error) {
+	user, err := s.users.GetByIDContext(ctx, userID)
+	if errors.Is(err, gorm.ErrRecordNotFound) {
+		return nil, ErrCollegeAccess
+	}
+	if err != nil {
+		return nil, err
+	}
+	if user.Role != models.RoleCollege || user.CollegeID == nil || user.College == nil || user.College.Status != "active" {
+		return nil, ErrCollegeAccess
+	}
+	return user.College, nil
+}
+
+func (s *CollegeService) List(ctx context.Context, query string, activeOnly bool, page, pageSize int) ([]models.College, int64, error) {
+	return s.repo.List(ctx, query, activeOnly, page, pageSize)
+}
+
+func (s *CollegeService) Save(ctx context.Context, id uuid.UUID, college *models.College) error {
+	if college == nil {
+		return ErrInvalidCollegeData
+	}
+	college.Name = strings.TrimSpace(college.Name)
+	college.Address = strings.TrimSpace(college.Address)
+	college.ContactEmail = strings.TrimSpace(college.ContactEmail)
+	college.ContactPhone = strings.TrimSpace(college.ContactPhone)
+	if college.Status == "" {
+		college.Status = "active"
+	}
+	if college.Name == "" || len(college.Name) > 255 || !college.University.IsValid() || (college.Status != "active" && college.Status != "inactive") || len(college.Address) > 500 || len(college.ContactPhone) > 50 {
+		return fmt.Errorf("%w: name, supported university and active/inactive status are required", ErrInvalidCollegeData)
+	}
+	if college.ContactEmail != "" && !validCollegeEmail(college.ContactEmail) {
+		return fmt.Errorf("%w: invalid contact email", ErrInvalidCollegeData)
+	}
+	college.ID = id
+	college.CreatedAt, college.UpdatedAt = time.Time{}, time.Time{}
+	if id == uuid.Nil {
+		return s.repo.Create(ctx, college)
+	}
+	existing, err := s.Get(ctx, id)
+	if err != nil {
+		return err
+	}
+	college.CreatedAt = existing.CreatedAt
+	return s.repo.Update(ctx, college)
+}
+
+func (s *CollegeService) Delete(ctx context.Context, id uuid.UUID) error {
+	if _, err := s.Get(ctx, id); err != nil {
+		return err
+	}
+	hasMembers, err := s.repo.HasMembers(ctx, id)
+	if err != nil {
+		return err
+	}
+	if hasMembers {
+		return fmt.Errorf("%w: college has members; deactivate it instead", ErrCollegeConflict)
+	}
+	return s.repo.Delete(ctx, id)
+}
+
+func (s *CollegeService) CreateUser(ctx context.Context, collegeID uuid.UUID, email, fullName, password string) (*models.User, error) {
+	college, err := s.Get(ctx, collegeID)
+	if err != nil {
+		return nil, err
+	}
+	if college.Status != "active" {
+		return nil, fmt.Errorf("%w: college must be active", ErrInvalidCollegeData)
+	}
+	email, fullName = strings.ToLower(strings.TrimSpace(email)), strings.TrimSpace(fullName)
+	if !validCollegeEmail(email) || fullName == "" || len(fullName) > 255 || len(password) < 8 || len(password) > 72 {
+		return nil, fmt.Errorf("%w: valid email, name and password of 8–72 bytes are required", ErrInvalidCollegeData)
+	}
+	hash, err := s.auth.HashPassword(password)
+	if err != nil {
+		return nil, err
+	}
+	user := &models.User{Email: email, FullName: fullName, PasswordHash: hash, Role: models.RoleCollege, CollegeID: &collegeID}
+	created, err := s.repo.CreateUser(ctx, user)
+	if err != nil {
+		return nil, err
+	}
+	if !created {
+		return nil, fmt.Errorf("%w: email is already registered", ErrCollegeConflict)
+	}
+	return user, nil
+}
+
+func (s *CollegeService) ListUsers(ctx context.Context, collegeID uuid.UUID, page, pageSize int) ([]models.User, int64, error) {
+	if _, err := s.Get(ctx, collegeID); err != nil {
+		return nil, 0, err
+	}
+	return s.repo.ListUsers(ctx, collegeID, page, pageSize)
+}
+
+func (s *CollegeService) Assign(ctx context.Context, id, collegeID uuid.UUID, student bool) error {
+	if id == uuid.Nil {
+		return ErrInvalidCollegeData
+	}
+	college, err := s.Get(ctx, collegeID)
+	if err != nil {
+		return err
+	}
+	if college.Status != "active" {
+		return fmt.Errorf("%w: college must be active", ErrInvalidCollegeData)
+	}
+	if student {
+		if _, err := s.students.GetByID(ctx, id); errors.Is(err, gorm.ErrRecordNotFound) {
+			return ErrStudentProfileNotFound
+		} else if err != nil {
+			return err
+		}
+		return s.repo.AssignStudent(ctx, id, college)
+	}
+	user, err := s.users.GetByIDContext(ctx, id)
+	if errors.Is(err, gorm.ErrRecordNotFound) {
+		return fmt.Errorf("%w: user not found", ErrInvalidCollegeData)
+	}
+	if err != nil {
+		return err
+	}
+	if user.Role != models.RoleCollege {
+		return fmt.Errorf("%w: user must have the college role", ErrInvalidCollegeData)
+	}
+	return s.repo.AssignUser(ctx, id, collegeID)
+}
+
+func validCollegeEmail(value string) bool {
+	address, err := mail.ParseAddress(value)
+	return err == nil && address.Address == value && len(value) <= 255
+}
diff --git a/internal/services/college_service.go b/internal/services/college_service.go
deleted file mode 100644
index 3a1fada..0000000
--- a/internal/services/college_service.go
+++ /dev/null
@@ -1,178 +0,0 @@
-package services
-
-import (
-	"context"
-	"errors"
-	"fmt"
-	"net/mail"
-	"strings"
-	"time"
-
-	"github.com/google/uuid"
-	"github.com/prasanna/student-job-portal/backend/internal/models"
-	"github.com/prasanna/student-job-portal/backend/internal/repositories"
-	"gorm.io/gorm"
-)
-
-var (
-	ErrInvalidCollegeData = errors.New("invalid college data")
-	ErrCollegeNotFound    = errors.New("college not found")
-	ErrCollegeAccess      = errors.New("college access denied")
-	ErrCollegeConflict    = errors.New("college operation conflicts with existing records")
-)
-
-type CollegeService struct {
-	repo     *repositories.CollegeRepository
-	users    *repositories.UserRepository
-	students *repositories.StudentRepository
-	auth     *AuthService
-}
-
-func NewCollegeService(repo *repositories.CollegeRepository, users *repositories.UserRepository, students *repositories.StudentRepository, auth *AuthService) *CollegeService {
-	return &CollegeService{repo: repo, users: users, students: students, auth: auth}
-}
-
-func (s *CollegeService) Get(ctx context.Context, id uuid.UUID) (*models.College, error) {
-	if id == uuid.Nil {
-		return nil, ErrInvalidCollegeData
-	}
-	college, err := s.repo.GetByID(ctx, id)
-	if errors.Is(err, gorm.ErrRecordNotFound) {
-		return nil, ErrCollegeNotFound
-	}
-	return college, err
-}
-
-// Resolve reads current membership, so stale JWT claims cannot retain tenant access.
-func (s *CollegeService) Resolve(ctx context.Context, userID uuid.UUID) (*models.College, error) {
-	user, err := s.users.GetByIDContext(ctx, userID)
-	if errors.Is(err, gorm.ErrRecordNotFound) {
-		return nil, ErrCollegeAccess
-	}
-	if err != nil {
-		return nil, err
-	}
-	if user.Role != models.RoleCollege || user.CollegeID == nil || user.College == nil || user.College.Status != "active" {
-		return nil, ErrCollegeAccess
-	}
-	return user.College, nil
-}
-
-func (s *CollegeService) List(ctx context.Context, query string, activeOnly bool, page, pageSize int) ([]models.College, int64, error) {
-	return s.repo.List(ctx, query, activeOnly, page, pageSize)
-}
-
-func (s *CollegeService) Save(ctx context.Context, id uuid.UUID, college *models.College) error {
-	if college == nil {
-		return ErrInvalidCollegeData
-	}
-	college.Name = strings.TrimSpace(college.Name)
-	college.Address = strings.TrimSpace(college.Address)
-	college.ContactEmail = strings.TrimSpace(college.ContactEmail)
-	college.ContactPhone = strings.TrimSpace(college.ContactPhone)
-	if college.Status == "" {
-		college.Status = "active"
-	}
-	if college.Name == "" || len(college.Name) > 255 || !college.University.IsValid() || (college.Status != "active" && college.Status != "inactive") || len(college.Address) > 500 || len(college.ContactPhone) > 50 {
-		return fmt.Errorf("%w: name, supported university and active/inactive status are required", ErrInvalidCollegeData)
-	}
-	if college.ContactEmail != "" && !validCollegeEmail(college.ContactEmail) {
-		return fmt.Errorf("%w: invalid contact email", ErrInvalidCollegeData)
-	}
-	college.ID = id
-	college.CreatedAt, college.UpdatedAt = time.Time{}, time.Time{}
-	if id == uuid.Nil {
-		return s.repo.Create(ctx, college)
-	}
-	existing, err := s.Get(ctx, id)
-	if err != nil {
-		return err
-	}
-	college.CreatedAt = existing.CreatedAt
-	return s.repo.Update(ctx, college)
-}
-
-func (s *CollegeService) Delete(ctx context.Context, id uuid.UUID) error {
-	if _, err := s.Get(ctx, id); err != nil {
-		return err
-	}
-	hasMembers, err := s.repo.HasMembers(ctx, id)
-	if err != nil {
-		return err
-	}
-	if hasMembers {
-		return fmt.Errorf("%w: college has members; deactivate it instead", ErrCollegeConflict)
-	}
-	return s.repo.Delete(ctx, id)
-}
-
-func (s *CollegeService) CreateUser(ctx context.Context, collegeID uuid.UUID, email, fullName, password string) (*models.User, error) {
-	college, err := s.Get(ctx, collegeID)
-	if err != nil {
-		return nil, err
-	}
-	if college.Status != "active" {
-		return nil, fmt.Errorf("%w: college must be active", ErrInvalidCollegeData)
-	}
-	email, fullName = strings.ToLower(strings.TrimSpace(email)), strings.TrimSpace(fullName)
-	if !validCollegeEmail(email) || fullName == "" || len(fullName) > 255 || len(password) < 8 || len(password) > 72 {
-		return nil, fmt.Errorf("%w: valid email, name and password of 8–72 bytes are required", ErrInvalidCollegeData)
-	}
-	hash, err := s.auth.HashPassword(password)
-	if err != nil {
-		return nil, err
-	}
-	user := &models.User{Email: email, FullName: fullName, PasswordHash: hash, Role: models.RoleCollege, CollegeID: &collegeID}
-	created, err := s.repo.CreateUser(ctx, user)
-	if err != nil {
-		return nil, err
-	}
-	if !created {
-		return nil, fmt.Errorf("%w: email is already registered", ErrCollegeConflict)
-	}
-	return user, nil
-}
-
-func (s *CollegeService) ListUsers(ctx context.Context, collegeID uuid.UUID, page, pageSize int) ([]models.User, int64, error) {
-	if _, err := s.Get(ctx, collegeID); err != nil {
-		return nil, 0, err
-	}
-	return s.repo.ListUsers(ctx, collegeID, page, pageSize)
-}
-
-func (s *CollegeService) Assign(ctx context.Context, id, collegeID uuid.UUID, student bool) error {
-	if id == uuid.Nil {
-		return ErrInvalidCollegeData
-	}
-	college, err := s.Get(ctx, collegeID)
-	if err != nil {
-		return err
-	}
-	if college.Status != "active" {
-		return fmt.Errorf("%w: college must be active", ErrInvalidCollegeData)
-	}
-	if student {
-		if _, err := s.students.GetByID(ctx, id); errors.Is(err, gorm.ErrRecordNotFound) {
-			return ErrStudentProfileNotFound
-		} else if err != nil {
-			return err
-		}
-		return s.repo.AssignStudent(ctx, id, college)
-	}
-	user, err := s.users.GetByIDContext(ctx, id)
-	if errors.Is(err, gorm.ErrRecordNotFound) {
-		return fmt.Errorf("%w: user not found", ErrInvalidCollegeData)
-	}
-	if err != nil {
-		return err
-	}
-	if user.Role != models.RoleCollege {
-		return fmt.Errorf("%w: user must have the college role", ErrInvalidCollegeData)
-	}
-	return s.repo.AssignUser(ctx, id, collegeID)
-}
-
-func validCollegeEmail(value string) bool {
-	address, err := mail.ParseAddress(value)
-	return err == nil && address.Address == value && len(value) <= 255
-}

# internal/services/internship_application_service.go

diff --git a/internal/services/internship_application_service.go b/internal/services/internship_application_service.go
index 86466ff..5d2035d 100644
--- a/internal/services/internship_application_service.go
+++ b/internal/services/internship_application_service.go
@@ -199,14 +199,17 @@ func (s *InternshipApplicationService) CreateApplication(ctx context.Context, ap
 	}
 
 	// Verify student profile exists
-	studentID, err := s.studentRepo.ResolveProfileID(application.StudentID)
+	student, err := s.studentRepo.GetByUserIDContext(ctx, application.StudentID)
 	if err != nil {
 		if errors.Is(err, gorm.ErrRecordNotFound) {
 			return fmt.Errorf("%w: student profile not found", ErrInvalidApplicationData)
 		}
 		return fmt.Errorf("check student profile: %w", err)
 	}
-	application.StudentID = studentID
+	if student.CollegeID == nil || student.College == nil || student.College.Status != "active" {
+		return fmt.Errorf("%w: an active college assignment is required", ErrInvalidApplicationData)
+	}
+	application.StudentID = student.ID
 
 	// Check internship existence and eligibility
 	internship, err := s.internshipRepo.GetByID(ctx, application.InternshipID)
@@ -233,8 +236,8 @@ func (s *InternshipApplicationService) CreateApplication(ctx context.Context, ap
 		return fmt.Errorf("check existing application: %w", err)
 	}
 
-	application.Status = models.ApplicationStatusSubmitted
-	application.AppliedAt = time.Now()
+	*application = models.InternshipApplication{InternshipID: application.InternshipID, StudentID: student.ID,
+		Status: models.ApplicationStatusSubmitted, ProgressStatus: "applied", AppliedAt: time.Now()}
 
 	return s.repo.Create(ctx, application)
 }
@@ -471,6 +474,7 @@ func (s *InternshipApplicationService) UpdateStatus(
 	}
 
 	now := time.Now()
+	previousStatus := application.Status
 	statusChanged := application.Status != newStatus
 	application.Status = newStatus
 	if employerNote != nil {
@@ -484,13 +488,20 @@ func (s *InternshipApplicationService) UpdateStatus(
 		application.ShortlistedAt = &now
 	case statusChanged && newStatus == models.ApplicationStatusAccepted:
 		application.AcceptedAt = &now
+		application.ProgressStatus = "ongoing"
+		application.StartDate = &now
 	case statusChanged && newStatus == models.ApplicationStatusRejected:
 		application.RejectedAt = &now
+		application.ProgressStatus = "cancelled"
 	}
 
-	if err := s.repo.Update(ctx, application); err != nil {
+	updatedStatus, err := s.repo.UpdateApplicationStatus(ctx, application, previousStatus)
+	if err != nil {
 		return nil, err
 	}
+	if !updatedStatus {
+		return nil, ErrInvalidStatusTransition
+	}
 
 	updated := mapRecruiterApplication(application)
 	return &updated, nil
@@ -553,10 +564,19 @@ func (s *InternshipApplicationService) WithdrawApplication(ctx context.Context,
 	}
 
 	now := time.Now()
+	previousStatus := application.Status
 	application.Status = models.ApplicationStatusWithdrawn
 	application.WithdrawnAt = &now
+	application.ProgressStatus = "cancelled"
 
-	return s.repo.Update(ctx, application)
+	updated, err := s.repo.UpdateApplicationStatus(ctx, application, previousStatus)
+	if err != nil {
+		return err
+	}
+	if !updated {
+		return ErrInvalidStatusTransition
+	}
+	return nil
 }
 
 func isWithdrawableApplicationStatus(status models.InternshipApplicationStatus) bool {
diff --git a/internal/services/internship_application_service.go b/internal/services/internship_application_service.go
index 5d2035d..22f777d 100644
--- a/internal/services/internship_application_service.go
+++ b/internal/services/internship_application_service.go
@@ -12,6 +12,7 @@ import (
 	"gorm.io/gorm"
 
 	"github.com/prasanna/student-job-portal/backend/internal/enums"
+	"github.com/prasanna/student-job-portal/backend/internal/logger"
 	"github.com/prasanna/student-job-portal/backend/internal/models"
 	"github.com/prasanna/student-job-portal/backend/internal/repositories"
 )
@@ -27,6 +28,10 @@ var (
 )
 
 type InternshipApplicationService struct {
+	users          *repositories.UserRepository
+	sender         ApplicationStatusSender
+	log            *logger.Logger
+	profiles       *RecruiterProfileService
 	repo           *repositories.InternshipApplicationRepository
 	internshipRepo *repositories.InternshipRepository
 	studentRepo    *repositories.StudentRepository
@@ -52,16 +57,8 @@ type RecruiterApplicationFilter struct {
 	PageSize     int
 }
 
-type StudentApplicationStatusFilter string
-
-const (
-	StudentApplicationFilterPending  StudentApplicationStatusFilter = "pending"
-	StudentApplicationFilterAccepted StudentApplicationStatusFilter = "accepted"
-	StudentApplicationFilterRejected StudentApplicationStatusFilter = "rejected"
-)
-
 type StudentApplicationFilter struct {
-	Status   StudentApplicationStatusFilter
+	Status   models.InternshipApplicationStatus
 	Page     int
 	PageSize int
 }
@@ -77,7 +74,6 @@ type RecruiterCandidate struct {
 	FacultyOrMajor  string    `json:"faculty_or_major"`
 	CurrentSemester string    `json:"current_semester"`
 	GraduationYear  int       `json:"graduation_year"`
-	Availability    string    `json:"availability"`
 	LinkedinURL     string    `json:"linkedin_url"`
 	GithubURL       string    `json:"github_url"`
 	PortfolioURL    string    `json:"portfolio_url"`
@@ -103,21 +99,16 @@ type RecruiterInternshipSummary struct {
 }
 
 type RecruiterApplication struct {
-	ID            uuid.UUID                          `json:"id"`
-	InternshipID  uuid.UUID                          `json:"internship_id"`
-	StudentID     uuid.UUID                          `json:"student_id"`
-	Status        models.InternshipApplicationStatus `json:"status"`
-	EmployerNote  string                             `json:"employer_note"`
-	AppliedAt     time.Time                          `json:"applied_at"`
-	ReviewedAt    *time.Time                         `json:"reviewed_at"`
-	ShortlistedAt *time.Time                         `json:"shortlisted_at"`
-	AcceptedAt    *time.Time                         `json:"accepted_at"`
-	RejectedAt    *time.Time                         `json:"rejected_at"`
-	WithdrawnAt   *time.Time                         `json:"withdrawn_at"`
-	CreatedAt     time.Time                          `json:"created_at"`
-	UpdatedAt     time.Time                          `json:"updated_at"`
-	Student       *RecruiterCandidate                `json:"student"`
-	Internship    *RecruiterInternshipSummary        `json:"internship"`
+	ID           uuid.UUID                          `json:"id"`
+	InternshipID uuid.UUID                          `json:"internship_id"`
+	StudentID    uuid.UUID                          `json:"student_id"`
+	Status       models.InternshipApplicationStatus `json:"status"`
+	EmployerNote string                             `json:"employer_note"`
+	AppliedAt    time.Time                          `json:"applied_at"`
+	CreatedAt    time.Time                          `json:"created_at"`
+	UpdatedAt    time.Time                          `json:"updated_at"`
+	Student      *RecruiterCandidate                `json:"student"`
+	Internship   *RecruiterInternshipSummary        `json:"internship"`
 }
 
 type RecruiterApplicationSummary struct {
@@ -149,6 +140,7 @@ type StudentApplicationInternship struct {
 	ID               uuid.UUID `json:"id"`
 	Title            string    `json:"title"`
 	OrganizationName string    `json:"organization_name"`
+	OrganizationLogo string    `json:"organization_logo,omitempty"`
 	Location         string    `json:"location"`
 	WorkMode         string    `json:"work_mode"`
 	InternshipType   string    `json:"internship_type"`
@@ -157,18 +149,13 @@ type StudentApplicationInternship struct {
 }
 
 type StudentApplicationSummary struct {
-	ID            uuid.UUID                          `json:"id"`
-	InternshipID  uuid.UUID                          `json:"internship_id"`
-	Status        models.InternshipApplicationStatus `json:"status"`
-	EmployerNote  string                             `json:"employer_note"`
-	AppliedAt     time.Time                          `json:"applied_at"`
-	ReviewedAt    *time.Time                         `json:"reviewed_at"`
-	ShortlistedAt *time.Time                         `json:"shortlisted_at"`
-	AcceptedAt    *time.Time                         `json:"accepted_at"`
-	RejectedAt    *time.Time                         `json:"rejected_at"`
-	WithdrawnAt   *time.Time                         `json:"withdrawn_at"`
-	UpdatedAt     time.Time                          `json:"updated_at"`
-	Internship    *StudentApplicationInternship      `json:"internship"`
+	ID           uuid.UUID                          `json:"id"`
+	InternshipID uuid.UUID                          `json:"internship_id"`
+	Status       models.InternshipApplicationStatus `json:"status"`
+	EmployerNote string                             `json:"employer_note"`
+	AppliedAt    time.Time                          `json:"applied_at"`
+	UpdatedAt    time.Time                          `json:"updated_at"`
+	Internship   *StudentApplicationInternship      `json:"internship"`
 }
 
 func NewInternshipApplicationService(
@@ -177,8 +164,16 @@ func NewInternshipApplicationService(
 	studentRepo *repositories.StudentRepository,
 	minio documentURLSigner,
 	bucketName string,
+	profiles *RecruiterProfileService,
+	users *repositories.UserRepository,
+	sender ApplicationStatusSender,
+	log *logger.Logger,
 ) *InternshipApplicationService {
 	return &InternshipApplicationService{
+		users:          users,
+		sender:         sender,
+		log:            log,
+		profiles:       profiles,
 		repo:           repo,
 		internshipRepo: internshipRepo,
 		studentRepo:    studentRepo,
@@ -187,11 +182,11 @@ func NewInternshipApplicationService(
 	}
 }
 
-func (s *InternshipApplicationService) CreateApplication(ctx context.Context, application *models.InternshipApplication) error {
+func (s *InternshipApplicationService) CreateApplication(ctx context.Context, userID uuid.UUID, application *models.InternshipApplication) error {
 	if application == nil {
 		return fmt.Errorf("%w: application cannot be nil", ErrInvalidApplicationData)
 	}
-	if application.StudentID == uuid.Nil {
+	if userID == uuid.Nil {
 		return fmt.Errorf("%w: student ID is required", ErrInvalidApplicationData)
 	}
 	if application.InternshipID == uuid.Nil {
@@ -199,16 +194,14 @@ func (s *InternshipApplicationService) CreateApplication(ctx context.Context, ap
 	}
 
 	// Verify student profile exists
-	student, err := s.studentRepo.GetByUserIDContext(ctx, application.StudentID)
+	student, err := s.studentRepo.GetByUserIDContext(ctx, userID)
 	if err != nil {
 		if errors.Is(err, gorm.ErrRecordNotFound) {
 			return fmt.Errorf("%w: student profile not found", ErrInvalidApplicationData)
 		}
 		return fmt.Errorf("check student profile: %w", err)
 	}
-	if student.CollegeID == nil || student.College == nil || student.College.Status != "active" {
-		return fmt.Errorf("%w: an active college assignment is required", ErrInvalidApplicationData)
-	}
+
 	application.StudentID = student.ID
 
 	// Check internship existence and eligibility
@@ -220,7 +213,7 @@ func (s *InternshipApplicationService) CreateApplication(ctx context.Context, ap
 		return fmt.Errorf("check internship: %w", err)
 	}
 
-	if !internship.IsActive || internship.Status != enums.InternshipStatusPublished {
+	if internship.Status != enums.InternshipStatusPublished {
 		return ErrInternshipNotActive
 	}
 
@@ -228,6 +221,13 @@ func (s *InternshipApplicationService) CreateApplication(ctx context.Context, ap
 		return ErrApplicationClosed
 	}
 
+	if err := s.internshipRepo.LoadPublicIssuer(ctx, internship); err != nil {
+		return err
+	}
+	if internship.Issuer.UserID == uuid.Nil {
+		return ErrInternshipNotActive
+	}
+
 	// Check if student already applied
 	existing, err := s.repo.GetByStudentAndInternship(ctx, application.StudentID, application.InternshipID)
 	if err == nil && existing != nil {
@@ -236,8 +236,14 @@ func (s *InternshipApplicationService) CreateApplication(ctx context.Context, ap
 		return fmt.Errorf("check existing application: %w", err)
 	}
 
+	if _, err := s.studentRepo.GetResume(ctx, student.ID); errors.Is(err, gorm.ErrRecordNotFound) {
+		return fmt.Errorf("%w: upload your PDF CV before applying", ErrInvalidApplicationData)
+	} else if err != nil {
+		return err
+	}
+
 	*application = models.InternshipApplication{InternshipID: application.InternshipID, StudentID: student.ID,
-		Status: models.ApplicationStatusSubmitted, ProgressStatus: "applied", AppliedAt: time.Now()}
+		Status: models.ApplicationStatusSubmitted, AppliedAt: time.Now()}
 
 	return s.repo.Create(ctx, application)
 }
@@ -304,7 +310,7 @@ func (s *InternshipApplicationService) ListByStudent(
 		return nil, 0, err
 	}
 
-	return mapStudentApplications(apps), total, nil
+	return s.mapStudentApplications(ctx, apps), total, nil
 }
 
 // FindByStudentAndInternship returns the authenticated student's application
@@ -333,7 +339,7 @@ func (s *InternshipApplicationService) FindByStudentAndInternship(
 		return nil, fmt.Errorf("find student application: %w", err)
 	}
 
-	mapped := mapStudentApplications([]models.InternshipApplication{*application})
+	mapped := s.mapStudentApplications(ctx, []models.InternshipApplication{*application})
 	return &mapped[0], nil
 }
 
@@ -406,11 +412,13 @@ func (s *InternshipApplicationService) GetForRecruiter(
 	detail := &RecruiterApplicationDetail{
 		Application: mapRecruiterApplication(application),
 	}
-	if application.Student == nil || len(application.Student.Documents) == 0 {
+	document, err := s.studentRepo.GetResume(ctx, application.StudentID)
+	if errors.Is(err, gorm.ErrRecordNotFound) {
 		return detail, nil
 	}
-
-	document := application.Student.Documents[0]
+	if err != nil {
+		return nil, err
+	}
 	detail.Document = &RecruiterApplicationDocument{
 		ID:       document.ID,
 		FileName: document.FileName,
@@ -473,28 +481,12 @@ func (s *InternshipApplicationService) UpdateStatus(
 		)
 	}
 
-	now := time.Now()
 	previousStatus := application.Status
-	statusChanged := application.Status != newStatus
 	application.Status = newStatus
 	if employerNote != nil {
 		application.EmployerNote = strings.TrimSpace(*employerNote)
 	}
 
-	switch {
-	case statusChanged && newStatus == models.ApplicationStatusReviewing:
-		application.ReviewedAt = &now
-	case statusChanged && newStatus == models.ApplicationStatusShortlisted:
-		application.ShortlistedAt = &now
-	case statusChanged && newStatus == models.ApplicationStatusAccepted:
-		application.AcceptedAt = &now
-		application.ProgressStatus = "ongoing"
-		application.StartDate = &now
-	case statusChanged && newStatus == models.ApplicationStatusRejected:
-		application.RejectedAt = &now
-		application.ProgressStatus = "cancelled"
-	}
-
 	updatedStatus, err := s.repo.UpdateApplicationStatus(ctx, application, previousStatus)
 	if err != nil {
 		return nil, err
@@ -503,39 +495,24 @@ func (s *InternshipApplicationService) UpdateStatus(
 		return nil, ErrInvalidStatusTransition
 	}
 
+	// shortcut: delivery is attempted once after saving; use a durable outbox if retries are required.
+	if s.sender != nil && application.Student != nil {
+		user, err := s.users.GetByIDContext(ctx, application.Student.UserID)
+		if err == nil {
+			err = s.sender.SendApplicationStatus(ctx, user.Email, internship.Title, application.Status, application.EmployerNote)
+		}
+		if err != nil && s.log != nil {
+			// SMTP errors can contain addresses or message content.
+			s.log.Error("application status email failed", "application_id", application.ID)
+		}
+	}
+
 	updated := mapRecruiterApplication(application)
 	return &updated, nil
 }
 
-// isValidEmployerStatusTransition keeps the application workflow small and
-// prevents employers from setting student-only or unknown statuses.
 func isValidEmployerStatusTransition(current, next models.InternshipApplicationStatus) bool {
-	if current == next {
-		switch next {
-		case models.ApplicationStatusReviewing,
-			models.ApplicationStatusShortlisted:
-			return true
-		default:
-			return false
-		}
-	}
-
-	switch current {
-	case models.ApplicationStatusSubmitted:
-		return next == models.ApplicationStatusReviewing ||
-			next == models.ApplicationStatusShortlisted ||
-			next == models.ApplicationStatusAccepted ||
-			next == models.ApplicationStatusRejected
-	case models.ApplicationStatusReviewing:
-		return next == models.ApplicationStatusShortlisted ||
-			next == models.ApplicationStatusAccepted ||
-			next == models.ApplicationStatusRejected
-	case models.ApplicationStatusShortlisted:
-		return next == models.ApplicationStatusAccepted ||
-			next == models.ApplicationStatusRejected
-	default:
-		return false
-	}
+	return current == models.ApplicationStatusSubmitted && (next == models.ApplicationStatusAccepted || next == models.ApplicationStatusRejected)
 }
 
 func (s *InternshipApplicationService) WithdrawApplication(ctx context.Context, applicationID, studentProfileID uuid.UUID) error {
@@ -563,11 +540,8 @@ func (s *InternshipApplicationService) WithdrawApplication(ctx context.Context,
 		)
 	}
 
-	now := time.Now()
 	previousStatus := application.Status
 	application.Status = models.ApplicationStatusWithdrawn
-	application.WithdrawnAt = &now
-	application.ProgressStatus = "cancelled"
 
 	updated, err := s.repo.UpdateApplicationStatus(ctx, application, previousStatus)
 	if err != nil {
@@ -580,21 +554,12 @@ func (s *InternshipApplicationService) WithdrawApplication(ctx context.Context,
 }
 
 func isWithdrawableApplicationStatus(status models.InternshipApplicationStatus) bool {
-	switch status {
-	case models.ApplicationStatusSubmitted,
-		models.ApplicationStatusReviewing,
-		models.ApplicationStatusShortlisted:
-		return true
-	default:
-		return false
-	}
+	return status == models.ApplicationStatusSubmitted
 }
 
 func isKnownApplicationStatus(status models.InternshipApplicationStatus) bool {
 	switch status {
 	case models.ApplicationStatusSubmitted,
-		models.ApplicationStatusReviewing,
-		models.ApplicationStatusShortlisted,
 		models.ApplicationStatusRejected,
 		models.ApplicationStatusAccepted,
 		models.ApplicationStatusWithdrawn:
@@ -604,43 +569,27 @@ func isKnownApplicationStatus(status models.InternshipApplicationStatus) bool {
 	}
 }
 
-func statusesForStudentApplicationFilter(
-	filter StudentApplicationStatusFilter,
-) ([]models.InternshipApplicationStatus, error) {
-	switch filter {
-	case "":
+func statusesForStudentApplicationFilter(status models.InternshipApplicationStatus) ([]models.InternshipApplicationStatus, error) {
+	if status == "" {
 		return nil, nil
-	case StudentApplicationFilterPending:
-		return []models.InternshipApplicationStatus{
-			models.ApplicationStatusSubmitted,
-			models.ApplicationStatusReviewing,
-			models.ApplicationStatusShortlisted,
-		}, nil
-	case StudentApplicationFilterAccepted:
-		return []models.InternshipApplicationStatus{models.ApplicationStatusAccepted}, nil
-	case StudentApplicationFilterRejected:
-		return []models.InternshipApplicationStatus{models.ApplicationStatusRejected}, nil
-	default:
-		return nil, fmt.Errorf("%w: invalid student application status filter", ErrInvalidApplicationData)
 	}
+	if !isKnownApplicationStatus(status) {
+		return nil, fmt.Errorf("%w: invalid application status", ErrInvalidApplicationData)
+	}
+	return []models.InternshipApplicationStatus{status}, nil
 }
 
-func mapStudentApplications(applications []models.InternshipApplication) []StudentApplicationSummary {
+func (s *InternshipApplicationService) mapStudentApplications(ctx context.Context, applications []models.InternshipApplication) []StudentApplicationSummary {
 	mapped := make([]StudentApplicationSummary, len(applications))
 	for i := range applications {
 		application := &applications[i]
 		mapped[i] = StudentApplicationSummary{
-			ID:            application.ID,
-			InternshipID:  application.InternshipID,
-			Status:        application.Status,
-			EmployerNote:  application.EmployerNote,
-			AppliedAt:     application.AppliedAt,
-			ReviewedAt:    application.ReviewedAt,
-			ShortlistedAt: application.ShortlistedAt,
-			AcceptedAt:    application.AcceptedAt,
-			RejectedAt:    application.RejectedAt,
-			WithdrawnAt:   application.WithdrawnAt,
-			UpdatedAt:     application.UpdatedAt,
+			ID:           application.ID,
+			InternshipID: application.InternshipID,
+			Status:       application.Status,
+			EmployerNote: application.EmployerNote,
+			AppliedAt:    application.AppliedAt,
+			UpdatedAt:    application.UpdatedAt,
 		}
 		if application.Internship != nil {
 			mapped[i].Internship = &StudentApplicationInternship{
@@ -652,7 +601,9 @@ func mapStudentApplications(applications []models.InternshipApplication) []Stude
 				Duration:       application.Internship.Duration,
 				DurationUnit:   application.Internship.DurationUnit,
 			}
-			mapped[i].Internship.OrganizationName = application.Internship.Issuer.OrganizationName
+			issuer := application.Internship.Issuer
+			mapped[i].Internship.OrganizationName = issuer.OrganizationName
+			mapped[i].Internship.OrganizationLogo = s.profiles.listingLogoURL(ctx, issuer.OrganizationLogo)
 		}
 	}
 	return mapped
@@ -698,19 +649,14 @@ func mapRecruiterApplications(applications []models.InternshipApplication) []Rec
 
 func mapRecruiterApplication(application *models.InternshipApplication) RecruiterApplication {
 	mapped := RecruiterApplication{
-		ID:            application.ID,
-		InternshipID:  application.InternshipID,
-		StudentID:     application.StudentID,
-		Status:        application.Status,
-		EmployerNote:  application.EmployerNote,
-		AppliedAt:     application.AppliedAt,
-		ReviewedAt:    application.ReviewedAt,
-		ShortlistedAt: application.ShortlistedAt,
-		AcceptedAt:    application.AcceptedAt,
-		RejectedAt:    application.RejectedAt,
-		WithdrawnAt:   application.WithdrawnAt,
-		CreatedAt:     application.CreatedAt,
-		UpdatedAt:     application.UpdatedAt,
+		ID:           application.ID,
+		InternshipID: application.InternshipID,
+		StudentID:    application.StudentID,
+		Status:       application.Status,
+		EmployerNote: application.EmployerNote,
+		AppliedAt:    application.AppliedAt,
+		CreatedAt:    application.CreatedAt,
+		UpdatedAt:    application.UpdatedAt,
 	}
 	if application.Student != nil {
 		mapped.Student = &RecruiterCandidate{
@@ -724,7 +670,6 @@ func mapRecruiterApplication(application *models.InternshipApplication) Recruite
 			FacultyOrMajor:  application.Student.FacultyOrMajor,
 			CurrentSemester: application.Student.CurrentSemester,
 			GraduationYear:  application.Student.GraduationYear,
-			Availability:    application.Student.Availability,
 			LinkedinURL:     application.Student.LinkedinURL,
 			GithubURL:       application.Student.GithubURL,
 			PortfolioURL:    application.Student.PortfolioURL,

# internal/services/internship_application_service_test.go

diff --git a/internal/services/internship_application_service_test.go b/internal/services/internship_application_service_test.go
index 2578b6d..a3c1ace 100644
--- a/internal/services/internship_application_service_test.go
+++ b/internal/services/internship_application_service_test.go
@@ -374,7 +374,12 @@ func createServiceTestInternship(t *testing.T, db *gorm.DB, employerID uuid.UUID
 
 func createServiceTestStudent(t *testing.T, db *gorm.DB, fullName string) *models.StudentProfile {
 	t.Helper()
+	college := &models.College{Name: "Test College", University: models.TribhuvanUniversity, Status: "active"}
+	if err := db.Create(college).Error; err != nil {
+		t.Fatal(err)
+	}
 	student := &models.StudentProfile{
+		CollegeID:      &college.ID,
 		UserID:         uuid.New(),
 		FullName:       fullName,
 		CollegeName:    "Tribhuvan University",
diff --git a/internal/services/internship_application_service_test.go b/internal/services/internship_application_service_test.go
index a3c1ace..c08b10e 100644
--- a/internal/services/internship_application_service_test.go
+++ b/internal/services/internship_application_service_test.go
@@ -18,40 +18,13 @@ import (
 )
 
 func TestIsValidEmployerStatusTransition(t *testing.T) {
-	tests := []struct {
-		name    string
-		current models.InternshipApplicationStatus
-		next    models.InternshipApplicationStatus
-		want    bool
-	}{
-		{"submitted to reviewing", models.ApplicationStatusSubmitted, models.ApplicationStatusReviewing, true},
-		{"submitted to shortlisted", models.ApplicationStatusSubmitted, models.ApplicationStatusShortlisted, true},
-		{"submitted to accepted", models.ApplicationStatusSubmitted, models.ApplicationStatusAccepted, true},
-		{"submitted to rejected", models.ApplicationStatusSubmitted, models.ApplicationStatusRejected, true},
-		{"reviewing to shortlisted", models.ApplicationStatusReviewing, models.ApplicationStatusShortlisted, true},
-		{"reviewing to accepted", models.ApplicationStatusReviewing, models.ApplicationStatusAccepted, true},
-		{"reviewing to rejected", models.ApplicationStatusReviewing, models.ApplicationStatusRejected, true},
-		{"shortlisted to accepted", models.ApplicationStatusShortlisted, models.ApplicationStatusAccepted, true},
-		{"shortlisted to rejected", models.ApplicationStatusShortlisted, models.ApplicationStatusRejected, true},
-		{"same employer status is idempotent", models.ApplicationStatusReviewing, models.ApplicationStatusReviewing, true},
-		{"cannot move shortlisted backwards", models.ApplicationStatusShortlisted, models.ApplicationStatusReviewing, false},
-		{"employer cannot withdraw", models.ApplicationStatusSubmitted, models.ApplicationStatusWithdrawn, false},
-		{"employer cannot reset to submitted", models.ApplicationStatusReviewing, models.ApplicationStatusSubmitted, false},
-		{"accepted is final", models.ApplicationStatusAccepted, models.ApplicationStatusRejected, false},
-		{"accepted cannot be updated idempotently", models.ApplicationStatusAccepted, models.ApplicationStatusAccepted, false},
-		{"rejected is final", models.ApplicationStatusRejected, models.ApplicationStatusReviewing, false},
-		{"rejected cannot be updated idempotently", models.ApplicationStatusRejected, models.ApplicationStatusRejected, false},
-		{"withdrawn is final", models.ApplicationStatusWithdrawn, models.ApplicationStatusReviewing, false},
-		{"unknown status is rejected", models.ApplicationStatusSubmitted, models.InternshipApplicationStatus("unknown"), false},
-		{"empty status is rejected", models.ApplicationStatusSubmitted, "", false},
-	}
-
-	for _, tt := range tests {
-		t.Run(tt.name, func(t *testing.T) {
-			if got := isValidEmployerStatusTransition(tt.current, tt.next); got != tt.want {
-				t.Fatalf("isValidEmployerStatusTransition(%q, %q) = %v, want %v", tt.current, tt.next, got, tt.want)
+	for _, current := range []models.InternshipApplicationStatus{models.ApplicationStatusSubmitted, models.ApplicationStatusAccepted, models.ApplicationStatusRejected, models.ApplicationStatusWithdrawn} {
+		for _, next := range []models.InternshipApplicationStatus{models.ApplicationStatusSubmitted, models.ApplicationStatusAccepted, models.ApplicationStatusRejected, models.ApplicationStatusWithdrawn, "reviewing", "shortlisted", "unknown"} {
+			want := current == models.ApplicationStatusSubmitted && (next == models.ApplicationStatusAccepted || next == models.ApplicationStatusRejected)
+			if got := isValidEmployerStatusTransition(current, next); got != want {
+				t.Fatalf("transition %s -> %s = %v, want %v", current, next, got, want)
 			}
-		})
+		}
 	}
 }
 
@@ -84,7 +57,6 @@ func TestGetForRecruiterDocumentAndOwnership(t *testing.T) {
 		FileName:  "anita-resume.pdf",
 		MimeType:  "application/pdf",
 		Size:      2048,
-		IsDefault: true,
 	}
 	if err := db.Create(document).Error; err != nil {
 		t.Fatalf("create document: %v", err)
@@ -150,25 +122,25 @@ func TestUpdateStatusDirectDecisionsAndEmployerNote(t *testing.T) {
 	if err != nil {
 		t.Fatalf("accept submitted application: %v", err)
 	}
-	if accepted.Status != models.ApplicationStatusAccepted || accepted.AcceptedAt == nil {
-		t.Fatalf("accepted result = %#v, want accepted timestamp", accepted)
+	if accepted.Status != models.ApplicationStatusAccepted {
+		t.Fatalf("accepted result = %#v, want accepted status", accepted)
 	}
 	if accepted.EmployerNote != "Welcome to the team." {
 		t.Fatalf("accepted note = %q, want trimmed message", accepted.EmployerNote)
 	}
 
 	secondStudent := createServiceTestStudent(t, db, "Nima Sherpa")
-	rejectedApplication := createServiceTestApplication(t, db, internship.ID, secondStudent.ID, models.ApplicationStatusReviewing, "Existing message")
+	rejectedApplication := createServiceTestApplication(t, db, internship.ID, secondStudent.ID, models.ApplicationStatusSubmitted, "Existing message")
 	rejected, err := service.UpdateStatus(ctx, rejectedApplication.ID, employerID, models.ApplicationStatusRejected, nil)
 	if err != nil {
-		t.Fatalf("reject reviewing application: %v", err)
+		t.Fatalf("reject submitted application: %v", err)
 	}
-	if rejected.RejectedAt == nil || rejected.EmployerNote != "Existing message" {
-		t.Fatalf("rejected result = %#v, want timestamp and preserved note", rejected)
+	if rejected.Status != models.ApplicationStatusRejected || rejected.EmployerNote != "Existing message" {
+		t.Fatalf("rejected result = %#v, want rejected status and preserved note", rejected)
 	}
 
 	thirdStudent := createServiceTestStudent(t, db, "Kabita Lama")
-	clearedApplication := createServiceTestApplication(t, db, internship.ID, thirdStudent.ID, models.ApplicationStatusShortlisted, "Clear me")
+	clearedApplication := createServiceTestApplication(t, db, internship.ID, thirdStudent.ID, models.ApplicationStatusSubmitted, "Clear me")
 	emptyNote := "   "
 	cleared, err := service.UpdateStatus(ctx, clearedApplication.ID, employerID, models.ApplicationStatusRejected, &emptyNote)
 	if err != nil {
@@ -201,9 +173,8 @@ func TestListByStudentFiltersAndMapsApplications(t *testing.T) {
 	db := openApplicationServiceTestDB(t)
 	employerID := uuid.New()
 	if err := db.Create(&models.RecruiterProfile{
-		UserID:             employerID,
-		OrganizationName:   "Acme Nepal",
-		VerificationStatus: string(enums.OrganizationVerificationApproved),
+		UserID:           employerID,
+		OrganizationName: "Acme Nepal",
 	}).Error; err != nil {
 		t.Fatalf("create recruiter profile: %v", err)
 	}
@@ -212,8 +183,6 @@ func TestListByStudentFiltersAndMapsApplications(t *testing.T) {
 	service := newApplicationServiceForTest(db, nil)
 	statuses := []models.InternshipApplicationStatus{
 		models.ApplicationStatusSubmitted,
-		models.ApplicationStatusReviewing,
-		models.ApplicationStatusShortlisted,
 		models.ApplicationStatusAccepted,
 		models.ApplicationStatusRejected,
 	}
@@ -223,20 +192,18 @@ func TestListByStudentFiltersAndMapsApplications(t *testing.T) {
 	}
 
 	pending, total, err := service.ListByStudent(context.Background(), student.UserID, StudentApplicationFilter{
-		Status:   StudentApplicationFilterPending,
+		Status:   models.ApplicationStatusSubmitted,
 		Page:     1,
 		PageSize: 10,
 	})
 	if err != nil {
 		t.Fatalf("list pending student applications: %v", err)
 	}
-	if total != 3 || len(pending) != 3 {
-		t.Fatalf("pending total=%d len=%d, want three active applications", total, len(pending))
+	if total != 1 || len(pending) != 1 {
+		t.Fatalf("pending total=%d len=%d, want one submitted application", total, len(pending))
 	}
 	for _, application := range pending {
-		if application.Status != models.ApplicationStatusSubmitted &&
-			application.Status != models.ApplicationStatusReviewing &&
-			application.Status != models.ApplicationStatusShortlisted {
+		if application.Status != models.ApplicationStatusSubmitted {
 			t.Fatalf("pending filter returned terminal status %q", application.Status)
 		}
 		if application.Internship == nil || application.Internship.OrganizationName != "Acme Nepal" {
@@ -248,7 +215,7 @@ func TestListByStudentFiltersAndMapsApplications(t *testing.T) {
 	}
 
 	accepted, acceptedTotal, err := service.ListByStudent(context.Background(), student.UserID, StudentApplicationFilter{
-		Status:   StudentApplicationFilterAccepted,
+		Status:   models.ApplicationStatusAccepted,
 		Page:     1,
 		PageSize: 10,
 	})
@@ -306,7 +273,11 @@ func TestFindByStudentAndInternship(t *testing.T) {
 func TestCreateApplicationRejectsDuplicate(t *testing.T) {
 	db := openApplicationServiceTestDB(t)
 	student := createServiceTestStudent(t, db, "Maya Gurung")
-	internship := createServiceTestInternship(t, db, uuid.New(), "Backend Intern")
+	employerID := uuid.New()
+	if err := db.Create(&models.RecruiterProfile{UserID: employerID, OrganizationName: "Employer"}).Error; err != nil {
+		t.Fatal(err)
+	}
+	internship := createServiceTestInternship(t, db, employerID, "Backend Intern")
 	createServiceTestApplication(
 		t,
 		db,
@@ -317,7 +288,7 @@ func TestCreateApplicationRejectsDuplicate(t *testing.T) {
 	)
 	service := newApplicationServiceForTest(db, nil)
 
-	err := service.CreateApplication(context.Background(), &models.InternshipApplication{
+	err := service.CreateApplication(context.Background(), student.UserID, &models.InternshipApplication{
 		InternshipID: internship.ID,
 		StudentID:    student.UserID,
 	})
@@ -352,6 +323,10 @@ func newApplicationServiceForTest(db *gorm.DB, signer documentURLSigner) *Intern
 		repositories.NewStudentRepository(db),
 		signer,
 		"student-documents",
+		&RecruiterProfileService{},
+		repositories.NewUserRepository(db),
+		nil,
+		nil,
 	)
 }
 
@@ -364,7 +339,6 @@ func createServiceTestInternship(t *testing.T, db *gorm.DB, employerID uuid.UUID
 		WorkMode:       "remote",
 		InternshipType: "paid",
 		Status:         enums.InternshipStatusPublished,
-		IsActive:       true,
 	}
 	if err := db.Omit("Issuer").Create(internship).Error; err != nil {
 		t.Fatalf("create internship: %v", err)
@@ -374,12 +348,7 @@ func createServiceTestInternship(t *testing.T, db *gorm.DB, employerID uuid.UUID
 
 func createServiceTestStudent(t *testing.T, db *gorm.DB, fullName string) *models.StudentProfile {
 	t.Helper()
-	college := &models.College{Name: "Test College", University: models.TribhuvanUniversity, Status: "active"}
-	if err := db.Create(college).Error; err != nil {
-		t.Fatal(err)
-	}
 	student := &models.StudentProfile{
-		CollegeID:      &college.ID,
 		UserID:         uuid.New(),
 		FullName:       fullName,
 		CollegeName:    "Tribhuvan University",
@@ -420,8 +389,6 @@ func TestIsWithdrawableApplicationStatus(t *testing.T) {
 		want   bool
 	}{
 		{"submitted application", models.ApplicationStatusSubmitted, true},
-		{"reviewing application", models.ApplicationStatusReviewing, true},
-		{"shortlisted application", models.ApplicationStatusShortlisted, true},
 		{"accepted application is final", models.ApplicationStatusAccepted, false},
 		{"rejected application is final", models.ApplicationStatusRejected, false},
 		{"withdrawn application is already final", models.ApplicationStatusWithdrawn, false},
@@ -436,3 +403,67 @@ func TestIsWithdrawableApplicationStatus(t *testing.T) {
 		})
 	}
 }
+
+type applicationStatusSenderFunc func(context.Context, string, string, models.InternshipApplicationStatus, string) error
+
+func (f applicationStatusSenderFunc) SendApplicationStatus(ctx context.Context, to, title string, status models.InternshipApplicationStatus, note string) error {
+	return f(ctx, to, title, status, note)
+}
+
+func TestUpdateStatusNotifiesStudentAfterSaving(t *testing.T) {
+	for _, tc := range []struct {
+		name         string
+		status       models.InternshipApplicationStatus
+		failDelivery bool
+	}{
+		{"acceptance", models.ApplicationStatusAccepted, false},
+		{"rejection", models.ApplicationStatusRejected, false},
+		{"delivery failure preserves decision", models.ApplicationStatusAccepted, true},
+	} {
+		t.Run(tc.name, func(t *testing.T) {
+			db := openApplicationServiceTestDB(t)
+			if err := db.AutoMigrate(&models.User{}); err != nil {
+				t.Fatal(err)
+			}
+			employerID := uuid.New()
+			internship := createServiceTestInternship(t, db, employerID, "Engineering Intern")
+			student := createServiceTestStudent(t, db, "Student")
+			if err := db.Create(&models.User{ID: student.UserID, Email: "student@example.com", PasswordHash: "test", Role: models.RoleStudent}).Error; err != nil {
+				t.Fatal(err)
+			}
+			application := createServiceTestApplication(t, db, internship.ID, student.ID, models.ApplicationStatusSubmitted, "")
+			service := newApplicationServiceForTest(db, nil)
+			calls := 0
+			service.sender = applicationStatusSenderFunc(func(ctx context.Context, to, title string, status models.InternshipApplicationStatus, note string) error {
+				calls++
+				if to != "student@example.com" || title != internship.Title || status != tc.status || note != "Recruiter message" {
+					t.Fatalf("unexpected email: %q %q %q %q", to, title, status, note)
+				}
+				saved, err := service.repo.GetByID(ctx, application.ID)
+				if err != nil || saved.Status != status || saved.EmployerNote != note {
+					t.Fatalf("email sent before decision persisted: %#v, %v", saved, err)
+				}
+				if tc.failDelivery {
+					return errors.New("mail unavailable")
+				}
+				return nil
+			})
+			note := "  Recruiter message  "
+			ctx := context.Background()
+			if _, err := service.UpdateStatus(ctx, application.ID, uuid.New(), tc.status, &note); !errors.Is(err, ErrUnauthorizedAccess) || calls != 0 {
+				t.Fatalf("unauthorized decision: error=%v, email calls=%d", err, calls)
+			}
+			updated, err := service.UpdateStatus(ctx, application.ID, employerID, tc.status, &note)
+			if err != nil || updated.Status != tc.status || calls != 1 {
+				t.Fatalf("decision: result=%#v, error=%v, email calls=%d", updated, err, calls)
+			}
+			if _, err := service.UpdateStatus(ctx, application.ID, employerID, tc.status, &note); !errors.Is(err, ErrInvalidStatusTransition) || calls != 1 {
+				t.Fatalf("repeated decision: error=%v, email calls=%d", err, calls)
+			}
+			own, err := service.FindByStudentAndInternship(ctx, student.UserID, internship.ID)
+			if err != nil || own.Status != tc.status || own.EmployerNote != "Recruiter message" {
+				t.Fatalf("student decision: result=%#v, error=%v", own, err)
+			}
+		})
+	}
+}

# internal/services/internship_record_service.go

diff --git a/internal/services/internship_record_service.go b/internal/services/internship_record_service.go
new file mode 100644
index 0000000..50529de
--- /dev/null
+++ b/internal/services/internship_record_service.go
@@ -0,0 +1,345 @@
+package services
+
+import (
+	"bytes"
+	"context"
+	"errors"
+	"fmt"
+	"io"
+	"mime"
+	"mime/multipart"
+	"net/http"
+	"net/url"
+	"path/filepath"
+	"strings"
+	"time"
+
+	"github.com/google/uuid"
+	"github.com/minio/minio-go/v7"
+	"github.com/prasanna/student-job-portal/backend/internal/models"
+	"github.com/prasanna/student-job-portal/backend/internal/repositories"
+	"gorm.io/gorm"
+)
+
+const MaxCertificateSize = 10 << 20
+
+var (
+	ErrInvalidRecordData = errors.New("invalid internship record data")
+	ErrRecordNotFound    = errors.New("internship record not found")
+	ErrRecordConflict    = errors.New("internship record conflicts with current state")
+)
+
+type InternshipRecordService struct {
+	repo     *repositories.InternshipRecordRepository
+	users    *repositories.UserRepository
+	colleges *CollegeService
+	storage  *minio.Client
+	bucket   string
+}
+
+func NewInternshipRecordService(repo *repositories.InternshipRecordRepository, users *repositories.UserRepository, colleges *CollegeService, storage *minio.Client, bucket string) *InternshipRecordService {
+	return &InternshipRecordService{repo: repo, users: users, colleges: colleges, storage: storage, bucket: bucket}
+}
+
+type InternshipRecord struct {
+	ID                uuid.UUID                          `json:"id"`
+	StudentID         uuid.UUID                          `json:"student_id"`
+	StudentName       string                             `json:"student_name"`
+	College           *models.College                    `json:"college"`
+	Program           string                             `json:"program"`
+	FacultyOrMajor    string                             `json:"faculty_or_major"`
+	Semester          string                             `json:"semester"`
+	GraduationYear    int                                `json:"graduation_year"`
+	InternshipID      uuid.UUID                          `json:"internship_id"`
+	CompanyID         uuid.UUID                          `json:"company_id"`
+	CompanyName       string                             `json:"company_name"`
+	Position          string                             `json:"position"`
+	ApplicationStatus models.InternshipApplicationStatus `json:"application_status"`
+	Status            string                             `json:"status"`
+	StartDate         *time.Time                         `json:"start_date"`
+	EndDate           *time.Time                         `json:"end_date"`
+	AppliedAt         time.Time                          `json:"applied_at"`
+	EvaluationStatus  string                             `json:"evaluation_status"`
+	CertificateStatus string                             `json:"certificate_status"`
+	Evaluation        *models.InternshipEvaluation       `json:"evaluation"`
+	Certificate       *models.InternshipCertificate      `json:"certificate"`
+}
+
+func (s *InternshipRecordService) scope(ctx context.Context, userID uuid.UUID, write bool) (repositories.InternshipRecordScope, error) {
+	var scope repositories.InternshipRecordScope
+	if userID == uuid.Nil {
+		return scope, ErrCollegeAccess
+	}
+	user, err := s.users.GetByIDContext(ctx, userID)
+	if errors.Is(err, gorm.ErrRecordNotFound) {
+		return scope, ErrCollegeAccess
+	}
+	if err != nil {
+		return scope, err
+	}
+	if write && user.Role != models.RoleEmployer && user.Role != models.RoleAdmin {
+		return scope, ErrCollegeAccess
+	}
+	switch user.Role {
+	case models.RoleAdmin:
+	case models.RoleCollege:
+		if user.CollegeID == nil || user.College == nil || user.College.Status != "active" {
+			return scope, ErrCollegeAccess
+		}
+		scope.CollegeID = user.CollegeID
+	case models.RoleStudent:
+		scope.StudentUserID = &user.ID
+	case models.RoleEmployer:
+		scope.EmployerID = &user.ID
+	default:
+		return scope, ErrCollegeAccess
+	}
+	return scope, nil
+}
+
+func (s *InternshipRecordService) get(ctx context.Context, userID, id uuid.UUID, write bool) (*models.InternshipApplication, error) {
+	if id == uuid.Nil {
+		return nil, ErrInvalidRecordData
+	}
+	scope, err := s.scope(ctx, userID, write)
+	if err != nil {
+		return nil, err
+	}
+	app, err := s.repo.Get(ctx, scope, id)
+	if errors.Is(err, gorm.ErrRecordNotFound) {
+		return nil, ErrRecordNotFound
+	}
+	return app, err
+}
+
+func (s *InternshipRecordService) Get(ctx context.Context, userID, id uuid.UUID) (*InternshipRecord, error) {
+	app, err := s.get(ctx, userID, id, false)
+	if err != nil {
+		return nil, err
+	}
+	record := mapInternshipRecord(app)
+	return &record, nil
+}
+
+func (s *InternshipRecordService) List(ctx context.Context, userID uuid.UUID, filter repositories.InternshipRecordFilter) ([]InternshipRecord, int64, error) {
+	scope, err := s.scope(ctx, userID, false)
+	if err != nil {
+		return nil, 0, err
+	}
+	if err := validateRecordFilter(filter); err != nil {
+		return nil, 0, err
+	}
+	apps, total, err := s.repo.List(ctx, scope, filter)
+	if err != nil {
+		return nil, 0, err
+	}
+	out := make([]InternshipRecord, len(apps))
+	for i := range apps {
+		out[i] = mapInternshipRecord(&apps[i])
+	}
+	return out, total, nil
+}
+
+func (s *InternshipRecordService) Dashboard(ctx context.Context, userID uuid.UUID) (*repositories.CollegeDashboard, error) {
+	college, err := s.colleges.Resolve(ctx, userID)
+	if err != nil {
+		return nil, err
+	}
+	return s.repo.Dashboard(ctx, college.ID)
+}
+
+type InternshipProgressInput struct {
+	Status    string     `json:"status"`
+	StartDate *time.Time `json:"start_date"`
+	EndDate   *time.Time `json:"end_date"`
+}
+
+func (s *InternshipRecordService) UpdateProgress(ctx context.Context, userID, id uuid.UUID, input InternshipProgressInput) (*InternshipRecord, error) {
+	app, err := s.get(ctx, userID, id, true)
+	if err != nil {
+		return nil, err
+	}
+	if app.Status != models.ApplicationStatusAccepted || app.ProgressStatus != "ongoing" {
+		return nil, fmt.Errorf("%w: only ongoing internships can be updated", ErrRecordConflict)
+	}
+	if input.Status != "ongoing" && input.Status != "completed" && input.Status != "cancelled" {
+		return nil, fmt.Errorf("%w: status must be ongoing, completed or cancelled", ErrInvalidRecordData)
+	}
+	if input.StartDate != nil {
+		app.StartDate = input.StartDate
+	}
+	if input.EndDate != nil {
+		app.EndDate = input.EndDate
+	}
+	now := time.Now().UTC()
+	if input.Status != "ongoing" && app.EndDate == nil {
+		app.EndDate = &now
+	}
+	if app.StartDate == nil || app.StartDate.IsZero() || (app.EndDate != nil && app.EndDate.Before(*app.StartDate)) || (input.Status == "completed" && app.EndDate.After(now)) {
+		return nil, fmt.Errorf("%w: valid start/end dates are required and completed internships cannot end in the future", ErrInvalidRecordData)
+	}
+	previous := app.ProgressStatus
+	app.ProgressStatus = input.Status
+	updated, err := s.repo.UpdateProgress(ctx, app, previous)
+	if err != nil {
+		return nil, err
+	}
+	if !updated {
+		return nil, ErrRecordConflict
+	}
+	record := mapInternshipRecord(app)
+	return &record, nil
+}
+
+type InternshipEvaluationInput struct {
+	Scores  map[string]int `json:"scores"`
+	Remarks string         `json:"remarks"`
+}
+
+func (s *InternshipRecordService) SubmitEvaluation(ctx context.Context, userID, id uuid.UUID, input InternshipEvaluationInput) (*models.InternshipEvaluation, error) {
+	app, err := s.get(ctx, userID, id, true)
+	if err != nil {
+		return nil, err
+	}
+	if app.ProgressStatus != "completed" || app.Evaluation != nil {
+		return nil, fmt.Errorf("%w: evaluation requires a completed internship without an evaluation", ErrRecordConflict)
+	}
+	input.Remarks = strings.TrimSpace(input.Remarks)
+	if len(input.Scores) == 0 || len(input.Scores) > 20 || input.Remarks == "" || len(input.Remarks) > 10000 {
+		return nil, fmt.Errorf("%w: 1–20 scores and remarks of at most 10000 bytes are required", ErrInvalidRecordData)
+	}
+	for criterion, score := range input.Scores {
+		if strings.TrimSpace(criterion) == "" || len(criterion) > 100 || score < 0 || score > 100 {
+			return nil, fmt.Errorf("%w: each named score must be between 0 and 100", ErrInvalidRecordData)
+		}
+	}
+	evaluation := &models.InternshipEvaluation{ApplicationID: id, SubmittedBy: userID, Scores: input.Scores, Remarks: input.Remarks, Status: "submitted", SubmittedAt: time.Now().UTC()}
+	created, err := s.repo.CreateEvaluation(ctx, evaluation)
+	if err != nil {
+		return nil, err
+	}
+	if !created {
+		return nil, ErrRecordConflict
+	}
+	return evaluation, nil
+}
+
+func (s *InternshipRecordService) IssueCertificate(ctx context.Context, userID, id uuid.UUID, file *multipart.FileHeader) (*models.InternshipCertificate, error) {
+	app, err := s.get(ctx, userID, id, true)
+	if err != nil {
+		return nil, err
+	}
+	if app.ProgressStatus != "completed" || app.Evaluation == nil || app.Certificate != nil {
+		return nil, fmt.Errorf("%w: certificate requires a completed, evaluated internship without a certificate", ErrRecordConflict)
+	}
+	if file == nil || file.Size <= 0 || file.Size > MaxCertificateSize {
+		return nil, fmt.Errorf("%w: PDF file of at most 10 MiB is required", ErrInvalidRecordData)
+	}
+	reader, err := file.Open()
+	if err != nil {
+		return nil, err
+	}
+	defer reader.Close()
+	data, err := io.ReadAll(io.LimitReader(reader, MaxCertificateSize+1))
+	if err != nil {
+		return nil, err
+	}
+	if len(data) > MaxCertificateSize || http.DetectContentType(data) != "application/pdf" {
+		return nil, fmt.Errorf("%w: file must be a PDF of at most 10 MiB", ErrInvalidRecordData)
+	}
+	if s.storage == nil {
+		return nil, errors.New("certificate storage unavailable")
+	}
+	certificateID := uuid.New()
+	certificate := &models.InternshipCertificate{ID: certificateID, ApplicationID: id, IssuedBy: userID,
+		ObjectKey: fmt.Sprintf("internship-certificates/%s/%s.pdf", id, certificateID),
+		FileName:  filepath.Base(file.Filename), Size: int64(len(data)), Status: "issued", IssuedAt: time.Now().UTC()}
+	if len(certificate.FileName) > 255 {
+		return nil, fmt.Errorf("%w: filename is too long", ErrInvalidRecordData)
+	}
+	if _, err := s.storage.PutObject(ctx, s.bucket, certificate.ObjectKey, bytes.NewReader(data), certificate.Size, minio.PutObjectOptions{ContentType: "application/pdf"}); err != nil {
+		return nil, err
+	}
+	created, err := s.repo.CreateCertificate(ctx, certificate)
+	if err != nil || !created {
+		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
+		defer cancel()
+		cleanupErr := s.storage.RemoveObject(cleanupCtx, s.bucket, certificate.ObjectKey, minio.RemoveObjectOptions{})
+		if cleanupErr != nil {
+			return nil, fmt.Errorf("clean up certificate upload: %w", cleanupErr)
+		}
+		if err == nil {
+			err = ErrRecordConflict
+		}
+		return nil, err
+	}
+	return certificate, nil
+}
+
+func (s *InternshipRecordService) CertificateURL(ctx context.Context, userID, id uuid.UUID, download bool) (string, error) {
+	app, err := s.get(ctx, userID, id, false)
+	if err != nil {
+		return "", err
+	}
+	if app.Certificate == nil {
+		return "", ErrRecordNotFound
+	}
+	if s.storage == nil {
+		return "", errors.New("certificate storage unavailable")
+	}
+	disposition := "inline"
+	if download {
+		disposition = "attachment"
+	}
+	params := url.Values{"response-content-type": {"application/pdf"}, "response-content-disposition": {mime.FormatMediaType(disposition, map[string]string{"filename": app.Certificate.FileName})}}
+	link, err := s.storage.PresignedGetObject(ctx, s.bucket, app.Certificate.ObjectKey, 5*time.Minute, params)
+	if err != nil {
+		return "", err
+	}
+	return link.String(), nil
+}
+
+func validateRecordFilter(filter repositories.InternshipRecordFilter) error {
+	switch filter.Status {
+	case "", "applied", "ongoing", "completed", "cancelled":
+	default:
+		return fmt.Errorf("%w: invalid status", ErrInvalidRecordData)
+	}
+	switch filter.EvaluationStatus {
+	case "", "pending", "submitted", "not_due":
+	default:
+		return fmt.Errorf("%w: invalid evaluation_status", ErrInvalidRecordData)
+	}
+	switch filter.CertificateStatus {
+	case "", "pending", "issued", "not_due":
+	default:
+		return fmt.Errorf("%w: invalid certificate_status", ErrInvalidRecordData)
+	}
+	if filter.AcademicYear < 0 || filter.AcademicYear > 9999 || (filter.StudentID != nil && *filter.StudentID == uuid.Nil) {
+		return ErrInvalidRecordData
+	}
+	return nil
+}
+
+func mapInternshipRecord(app *models.InternshipApplication) InternshipRecord {
+	record := InternshipRecord{ID: app.ID, StudentID: app.StudentID, InternshipID: app.InternshipID,
+		ApplicationStatus: app.Status, Status: app.ProgressStatus, StartDate: app.StartDate, EndDate: app.EndDate,
+		AppliedAt: app.AppliedAt, Evaluation: app.Evaluation, Certificate: app.Certificate, EvaluationStatus: "not_due", CertificateStatus: "not_due"}
+	if app.Student != nil {
+		record.StudentName, record.College, record.Program = app.Student.FullName, app.Student.College, app.Student.Degree
+		record.FacultyOrMajor, record.Semester, record.GraduationYear = app.Student.FacultyOrMajor, app.Student.CurrentSemester, app.Student.GraduationYear
+	}
+	if app.Internship != nil {
+		record.CompanyID, record.CompanyName, record.Position = app.Internship.IssuedBy, app.Internship.Issuer.OrganizationName, app.Internship.Title
+	}
+	if app.ProgressStatus == "completed" {
+		record.EvaluationStatus, record.CertificateStatus = "pending", "pending"
+	}
+	if app.Evaluation != nil {
+		record.EvaluationStatus = app.Evaluation.Status
+	}
+	if app.Certificate != nil {
+		record.CertificateStatus = app.Certificate.Status
+	}
+	return record
+}
diff --git a/internal/services/internship_record_service.go b/internal/services/internship_record_service.go
deleted file mode 100644
index 50529de..0000000
--- a/internal/services/internship_record_service.go
+++ /dev/null
@@ -1,345 +0,0 @@
-package services
-
-import (
-	"bytes"
-	"context"
-	"errors"
-	"fmt"
-	"io"
-	"mime"
-	"mime/multipart"
-	"net/http"
-	"net/url"
-	"path/filepath"
-	"strings"
-	"time"
-
-	"github.com/google/uuid"
-	"github.com/minio/minio-go/v7"
-	"github.com/prasanna/student-job-portal/backend/internal/models"
-	"github.com/prasanna/student-job-portal/backend/internal/repositories"
-	"gorm.io/gorm"
-)
-
-const MaxCertificateSize = 10 << 20
-
-var (
-	ErrInvalidRecordData = errors.New("invalid internship record data")
-	ErrRecordNotFound    = errors.New("internship record not found")
-	ErrRecordConflict    = errors.New("internship record conflicts with current state")
-)
-
-type InternshipRecordService struct {
-	repo     *repositories.InternshipRecordRepository
-	users    *repositories.UserRepository
-	colleges *CollegeService
-	storage  *minio.Client
-	bucket   string
-}
-
-func NewInternshipRecordService(repo *repositories.InternshipRecordRepository, users *repositories.UserRepository, colleges *CollegeService, storage *minio.Client, bucket string) *InternshipRecordService {
-	return &InternshipRecordService{repo: repo, users: users, colleges: colleges, storage: storage, bucket: bucket}
-}
-
-type InternshipRecord struct {
-	ID                uuid.UUID                          `json:"id"`
-	StudentID         uuid.UUID                          `json:"student_id"`
-	StudentName       string                             `json:"student_name"`
-	College           *models.College                    `json:"college"`
-	Program           string                             `json:"program"`
-	FacultyOrMajor    string                             `json:"faculty_or_major"`
-	Semester          string                             `json:"semester"`
-	GraduationYear    int                                `json:"graduation_year"`
-	InternshipID      uuid.UUID                          `json:"internship_id"`
-	CompanyID         uuid.UUID                          `json:"company_id"`
-	CompanyName       string                             `json:"company_name"`
-	Position          string                             `json:"position"`
-	ApplicationStatus models.InternshipApplicationStatus `json:"application_status"`
-	Status            string                             `json:"status"`
-	StartDate         *time.Time                         `json:"start_date"`
-	EndDate           *time.Time                         `json:"end_date"`
-	AppliedAt         time.Time                          `json:"applied_at"`
-	EvaluationStatus  string                             `json:"evaluation_status"`
-	CertificateStatus string                             `json:"certificate_status"`
-	Evaluation        *models.InternshipEvaluation       `json:"evaluation"`
-	Certificate       *models.InternshipCertificate      `json:"certificate"`
-}
-
-func (s *InternshipRecordService) scope(ctx context.Context, userID uuid.UUID, write bool) (repositories.InternshipRecordScope, error) {
-	var scope repositories.InternshipRecordScope
-	if userID == uuid.Nil {
-		return scope, ErrCollegeAccess
-	}
-	user, err := s.users.GetByIDContext(ctx, userID)
-	if errors.Is(err, gorm.ErrRecordNotFound) {
-		return scope, ErrCollegeAccess
-	}
-	if err != nil {
-		return scope, err
-	}
-	if write && user.Role != models.RoleEmployer && user.Role != models.RoleAdmin {
-		return scope, ErrCollegeAccess
-	}
-	switch user.Role {
-	case models.RoleAdmin:
-	case models.RoleCollege:
-		if user.CollegeID == nil || user.College == nil || user.College.Status != "active" {
-			return scope, ErrCollegeAccess
-		}
-		scope.CollegeID = user.CollegeID
-	case models.RoleStudent:
-		scope.StudentUserID = &user.ID
-	case models.RoleEmployer:
-		scope.EmployerID = &user.ID
-	default:
-		return scope, ErrCollegeAccess
-	}
-	return scope, nil
-}
-
-func (s *InternshipRecordService) get(ctx context.Context, userID, id uuid.UUID, write bool) (*models.InternshipApplication, error) {
-	if id == uuid.Nil {
-		return nil, ErrInvalidRecordData
-	}
-	scope, err := s.scope(ctx, userID, write)
-	if err != nil {
-		return nil, err
-	}
-	app, err := s.repo.Get(ctx, scope, id)
-	if errors.Is(err, gorm.ErrRecordNotFound) {
-		return nil, ErrRecordNotFound
-	}
-	return app, err
-}
-
-func (s *InternshipRecordService) Get(ctx context.Context, userID, id uuid.UUID) (*InternshipRecord, error) {
-	app, err := s.get(ctx, userID, id, false)
-	if err != nil {
-		return nil, err
-	}
-	record := mapInternshipRecord(app)
-	return &record, nil
-}
-
-func (s *InternshipRecordService) List(ctx context.Context, userID uuid.UUID, filter repositories.InternshipRecordFilter) ([]InternshipRecord, int64, error) {
-	scope, err := s.scope(ctx, userID, false)
-	if err != nil {
-		return nil, 0, err
-	}
-	if err := validateRecordFilter(filter); err != nil {
-		return nil, 0, err
-	}
-	apps, total, err := s.repo.List(ctx, scope, filter)
-	if err != nil {
-		return nil, 0, err
-	}
-	out := make([]InternshipRecord, len(apps))
-	for i := range apps {
-		out[i] = mapInternshipRecord(&apps[i])
-	}
-	return out, total, nil
-}
-
-func (s *InternshipRecordService) Dashboard(ctx context.Context, userID uuid.UUID) (*repositories.CollegeDashboard, error) {
-	college, err := s.colleges.Resolve(ctx, userID)
-	if err != nil {
-		return nil, err
-	}
-	return s.repo.Dashboard(ctx, college.ID)
-}
-
-type InternshipProgressInput struct {
-	Status    string     `json:"status"`
-	StartDate *time.Time `json:"start_date"`
-	EndDate   *time.Time `json:"end_date"`
-}
-
-func (s *InternshipRecordService) UpdateProgress(ctx context.Context, userID, id uuid.UUID, input InternshipProgressInput) (*InternshipRecord, error) {
-	app, err := s.get(ctx, userID, id, true)
-	if err != nil {
-		return nil, err
-	}
-	if app.Status != models.ApplicationStatusAccepted || app.ProgressStatus != "ongoing" {
-		return nil, fmt.Errorf("%w: only ongoing internships can be updated", ErrRecordConflict)
-	}
-	if input.Status != "ongoing" && input.Status != "completed" && input.Status != "cancelled" {
-		return nil, fmt.Errorf("%w: status must be ongoing, completed or cancelled", ErrInvalidRecordData)
-	}
-	if input.StartDate != nil {
-		app.StartDate = input.StartDate
-	}
-	if input.EndDate != nil {
-		app.EndDate = input.EndDate
-	}
-	now := time.Now().UTC()
-	if input.Status != "ongoing" && app.EndDate == nil {
-		app.EndDate = &now
-	}
-	if app.StartDate == nil || app.StartDate.IsZero() || (app.EndDate != nil && app.EndDate.Before(*app.StartDate)) || (input.Status == "completed" && app.EndDate.After(now)) {
-		return nil, fmt.Errorf("%w: valid start/end dates are required and completed internships cannot end in the future", ErrInvalidRecordData)
-	}
-	previous := app.ProgressStatus
-	app.ProgressStatus = input.Status
-	updated, err := s.repo.UpdateProgress(ctx, app, previous)
-	if err != nil {
-		return nil, err
-	}
-	if !updated {
-		return nil, ErrRecordConflict
-	}
-	record := mapInternshipRecord(app)
-	return &record, nil
-}
-
-type InternshipEvaluationInput struct {
-	Scores  map[string]int `json:"scores"`
-	Remarks string         `json:"remarks"`
-}
-
-func (s *InternshipRecordService) SubmitEvaluation(ctx context.Context, userID, id uuid.UUID, input InternshipEvaluationInput) (*models.InternshipEvaluation, error) {
-	app, err := s.get(ctx, userID, id, true)
-	if err != nil {
-		return nil, err
-	}
-	if app.ProgressStatus != "completed" || app.Evaluation != nil {
-		return nil, fmt.Errorf("%w: evaluation requires a completed internship without an evaluation", ErrRecordConflict)
-	}
-	input.Remarks = strings.TrimSpace(input.Remarks)
-	if len(input.Scores) == 0 || len(input.Scores) > 20 || input.Remarks == "" || len(input.Remarks) > 10000 {
-		return nil, fmt.Errorf("%w: 1–20 scores and remarks of at most 10000 bytes are required", ErrInvalidRecordData)
-	}
-	for criterion, score := range input.Scores {
-		if strings.TrimSpace(criterion) == "" || len(criterion) > 100 || score < 0 || score > 100 {
-			return nil, fmt.Errorf("%w: each named score must be between 0 and 100", ErrInvalidRecordData)
-		}
-	}
-	evaluation := &models.InternshipEvaluation{ApplicationID: id, SubmittedBy: userID, Scores: input.Scores, Remarks: input.Remarks, Status: "submitted", SubmittedAt: time.Now().UTC()}
-	created, err := s.repo.CreateEvaluation(ctx, evaluation)
-	if err != nil {
-		return nil, err
-	}
-	if !created {
-		return nil, ErrRecordConflict
-	}
-	return evaluation, nil
-}
-
-func (s *InternshipRecordService) IssueCertificate(ctx context.Context, userID, id uuid.UUID, file *multipart.FileHeader) (*models.InternshipCertificate, error) {
-	app, err := s.get(ctx, userID, id, true)
-	if err != nil {
-		return nil, err
-	}
-	if app.ProgressStatus != "completed" || app.Evaluation == nil || app.Certificate != nil {
-		return nil, fmt.Errorf("%w: certificate requires a completed, evaluated internship without a certificate", ErrRecordConflict)
-	}
-	if file == nil || file.Size <= 0 || file.Size > MaxCertificateSize {
-		return nil, fmt.Errorf("%w: PDF file of at most 10 MiB is required", ErrInvalidRecordData)
-	}
-	reader, err := file.Open()
-	if err != nil {
-		return nil, err
-	}
-	defer reader.Close()
-	data, err := io.ReadAll(io.LimitReader(reader, MaxCertificateSize+1))
-	if err != nil {
-		return nil, err
-	}
-	if len(data) > MaxCertificateSize || http.DetectContentType(data) != "application/pdf" {
-		return nil, fmt.Errorf("%w: file must be a PDF of at most 10 MiB", ErrInvalidRecordData)
-	}
-	if s.storage == nil {
-		return nil, errors.New("certificate storage unavailable")
-	}
-	certificateID := uuid.New()
-	certificate := &models.InternshipCertificate{ID: certificateID, ApplicationID: id, IssuedBy: userID,
-		ObjectKey: fmt.Sprintf("internship-certificates/%s/%s.pdf", id, certificateID),
-		FileName:  filepath.Base(file.Filename), Size: int64(len(data)), Status: "issued", IssuedAt: time.Now().UTC()}
-	if len(certificate.FileName) > 255 {
-		return nil, fmt.Errorf("%w: filename is too long", ErrInvalidRecordData)
-	}
-	if _, err := s.storage.PutObject(ctx, s.bucket, certificate.ObjectKey, bytes.NewReader(data), certificate.Size, minio.PutObjectOptions{ContentType: "application/pdf"}); err != nil {
-		return nil, err
-	}
-	created, err := s.repo.CreateCertificate(ctx, certificate)
-	if err != nil || !created {
-		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
-		defer cancel()
-		cleanupErr := s.storage.RemoveObject(cleanupCtx, s.bucket, certificate.ObjectKey, minio.RemoveObjectOptions{})
-		if cleanupErr != nil {
-			return nil, fmt.Errorf("clean up certificate upload: %w", cleanupErr)
-		}
-		if err == nil {
-			err = ErrRecordConflict
-		}
-		return nil, err
-	}
-	return certificate, nil
-}
-
-func (s *InternshipRecordService) CertificateURL(ctx context.Context, userID, id uuid.UUID, download bool) (string, error) {
-	app, err := s.get(ctx, userID, id, false)
-	if err != nil {
-		return "", err
-	}
-	if app.Certificate == nil {
-		return "", ErrRecordNotFound
-	}
-	if s.storage == nil {
-		return "", errors.New("certificate storage unavailable")
-	}
-	disposition := "inline"
-	if download {
-		disposition = "attachment"
-	}
-	params := url.Values{"response-content-type": {"application/pdf"}, "response-content-disposition": {mime.FormatMediaType(disposition, map[string]string{"filename": app.Certificate.FileName})}}
-	link, err := s.storage.PresignedGetObject(ctx, s.bucket, app.Certificate.ObjectKey, 5*time.Minute, params)
-	if err != nil {
-		return "", err
-	}
-	return link.String(), nil
-}
-
-func validateRecordFilter(filter repositories.InternshipRecordFilter) error {
-	switch filter.Status {
-	case "", "applied", "ongoing", "completed", "cancelled":
-	default:
-		return fmt.Errorf("%w: invalid status", ErrInvalidRecordData)
-	}
-	switch filter.EvaluationStatus {
-	case "", "pending", "submitted", "not_due":
-	default:
-		return fmt.Errorf("%w: invalid evaluation_status", ErrInvalidRecordData)
-	}
-	switch filter.CertificateStatus {
-	case "", "pending", "issued", "not_due":
-	default:
-		return fmt.Errorf("%w: invalid certificate_status", ErrInvalidRecordData)
-	}
-	if filter.AcademicYear < 0 || filter.AcademicYear > 9999 || (filter.StudentID != nil && *filter.StudentID == uuid.Nil) {
-		return ErrInvalidRecordData
-	}
-	return nil
-}
-
-func mapInternshipRecord(app *models.InternshipApplication) InternshipRecord {
-	record := InternshipRecord{ID: app.ID, StudentID: app.StudentID, InternshipID: app.InternshipID,
-		ApplicationStatus: app.Status, Status: app.ProgressStatus, StartDate: app.StartDate, EndDate: app.EndDate,
-		AppliedAt: app.AppliedAt, Evaluation: app.Evaluation, Certificate: app.Certificate, EvaluationStatus: "not_due", CertificateStatus: "not_due"}
-	if app.Student != nil {
-		record.StudentName, record.College, record.Program = app.Student.FullName, app.Student.College, app.Student.Degree
-		record.FacultyOrMajor, record.Semester, record.GraduationYear = app.Student.FacultyOrMajor, app.Student.CurrentSemester, app.Student.GraduationYear
-	}
-	if app.Internship != nil {
-		record.CompanyID, record.CompanyName, record.Position = app.Internship.IssuedBy, app.Internship.Issuer.OrganizationName, app.Internship.Title
-	}
-	if app.ProgressStatus == "completed" {
-		record.EvaluationStatus, record.CertificateStatus = "pending", "pending"
-	}
-	if app.Evaluation != nil {
-		record.EvaluationStatus = app.Evaluation.Status
-	}
-	if app.Certificate != nil {
-		record.CertificateStatus = app.Certificate.Status
-	}
-	return record
-}

# internal/services/student_service.go

diff --git a/internal/services/student_service.go b/internal/services/student_service.go
index 7fd5af2..c165ce7 100644
--- a/internal/services/student_service.go
+++ b/internal/services/student_service.go
@@ -9,6 +9,7 @@ import (
 	"mime/multipart"
 	"net/http"
 	"path/filepath"
+	"time"
 
 	"github.com/google/uuid"
 	"github.com/minio/minio-go/v7"
@@ -29,6 +30,7 @@ var (
 // StudentService provides student-related operations that use repositories and MinIO.
 type StudentService struct {
 	repo        *repositories.StudentRepository
+	colleges    *repositories.CollegeRepository
 	minioClient *minio.Client
 	cfg         *config.Config
 	l           *logger.Logger
@@ -37,11 +39,12 @@ type StudentService struct {
 // NewStudentService constructs a new StudentService.
 func NewStudentService(
 	repo *repositories.StudentRepository,
+	colleges *repositories.CollegeRepository,
 	minioClient *minio.Client,
 	cfg *config.Config,
 	l *logger.Logger,
 ) *StudentService {
-	return &StudentService{repo: repo, minioClient: minioClient, cfg: cfg, l: l}
+	return &StudentService{repo: repo, colleges: colleges, minioClient: minioClient, cfg: cfg, l: l}
 }
 
 // CalculateProfileCompletion returns a simple profile completion percentage based
@@ -99,6 +102,9 @@ func (s *StudentService) GetProfile(userID uuid.UUID) (*models.StudentProfile, e
 	if errors.Is(err, gorm.ErrRecordNotFound) {
 		return nil, ErrStudentProfileNotFound
 	}
+	if err == nil && profile.College != nil {
+		profile.CollegeName = profile.College.Name
+	}
 	return profile, err
 }
 
@@ -113,11 +119,40 @@ func (s *StudentService) GetApplicationStats(
 	return s.repo.GetApplicationStats(ctx, profileID)
 }
 
-func (s *StudentService) CreateOrUpdateProfile(profile *models.StudentProfile) error {
+func (s *StudentService) CreateOrUpdateProfile(ctx context.Context, profile *models.StudentProfile) error {
 	if profile == nil || profile.UserID == uuid.Nil {
 		return fmt.Errorf("%w: profile and user id are required", ErrInvalidStudentData)
 	}
-	return s.repo.CreateOrUpdateProfile(profile)
+	existing, err := s.repo.GetByUserIDContext(ctx, profile.UserID)
+	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
+		return err
+	}
+	if existing != nil && existing.CollegeID != nil {
+		if profile.CollegeID != nil && *profile.CollegeID != *existing.CollegeID {
+			return fmt.Errorf("%w: college changes require an administrator", ErrInvalidStudentData)
+		}
+		profile.CollegeID = existing.CollegeID
+	}
+	if profile.CollegeID == nil || *profile.CollegeID == uuid.Nil {
+		return fmt.Errorf("%w: college_id is required", ErrInvalidStudentData)
+	}
+	college, err := s.colleges.GetByID(ctx, *profile.CollegeID)
+	if errors.Is(err, gorm.ErrRecordNotFound) {
+		return fmt.Errorf("%w: college not found", ErrInvalidStudentData)
+	}
+	if err != nil {
+		return err
+	}
+	if college.Status != "active" && (existing == nil || existing.CollegeID == nil) {
+		return fmt.Errorf("%w: college must be active", ErrInvalidStudentData)
+	}
+	profile.ID = uuid.Nil
+	profile.CreatedAt = time.Time{}
+	profile.DeletedAt = gorm.DeletedAt{}
+	profile.Documents = nil
+	profile.College, profile.CollegeName = college, college.Name
+	profile.ProfileCompletionPercent = s.CalculateProfileCompletion(profile)
+	return s.repo.CreateOrUpdateProfile(ctx, profile)
 }
 
 // UploadDocument uploads the provided multipart file header to MinIO under the configured
diff --git a/internal/services/student_service.go b/internal/services/student_service.go
index c165ce7..d55d078 100644
--- a/internal/services/student_service.go
+++ b/internal/services/student_service.go
@@ -9,277 +9,209 @@ import (
 	"mime/multipart"
 	"net/http"
 	"path/filepath"
+	"strings"
 	"time"
 
 	"github.com/google/uuid"
 	"github.com/minio/minio-go/v7"
-	"gorm.io/gorm"
-
 	"github.com/prasanna/student-job-portal/backend/internal/config"
 	"github.com/prasanna/student-job-portal/backend/internal/logger"
 	"github.com/prasanna/student-job-portal/backend/internal/models"
 	"github.com/prasanna/student-job-portal/backend/internal/repositories"
+	"gorm.io/gorm"
 )
 
 var (
-	ErrInvalidStudentData      = errors.New("invalid student data")
-	ErrStudentProfileNotFound  = errors.New("student profile not found")
-	ErrStudentDocumentNotFound = errors.New("student document not found")
+	ErrInvalidStudentData     = errors.New("invalid student data")
+	ErrStudentProfileNotFound = errors.New("student profile not found")
 )
 
-// StudentService provides student-related operations that use repositories and MinIO.
+const maxResumeSize = 10 << 20
+
 type StudentService struct {
 	repo        *repositories.StudentRepository
-	colleges    *repositories.CollegeRepository
 	minioClient *minio.Client
 	cfg         *config.Config
-	l           *logger.Logger
-}
-
-// NewStudentService constructs a new StudentService.
-func NewStudentService(
-	repo *repositories.StudentRepository,
-	colleges *repositories.CollegeRepository,
-	minioClient *minio.Client,
-	cfg *config.Config,
-	l *logger.Logger,
-) *StudentService {
-	return &StudentService{repo: repo, colleges: colleges, minioClient: minioClient, cfg: cfg, l: l}
+	log         *logger.Logger
 }
 
-// CalculateProfileCompletion returns a simple profile completion percentage based
-// on the fraction of non-empty key fields in the profile.
-func (s *StudentService) CalculateProfileCompletion(profile *models.StudentProfile) int {
-	if profile == nil {
-		return 0
-	}
-
-	// Choose a set of key fields to consider for completion.
-	fields := []string{
-		profile.FullName,
-		profile.Phone,
-		profile.Location,
-		profile.Bio,
-		profile.CollegeName,
-		profile.Degree,
-		profile.FacultyOrMajor,
-		profile.CurrentSemester,
-		profile.PreferredJobCategories,
-		profile.PreferredLocations,
-		profile.PreferredWorkMode,
-		profile.Availability,
-		profile.ExpectedSalary,
-		profile.LinkedinURL,
-		profile.GithubURL,
-		profile.PortfolioURL,
-		profile.ProfileImageKey,
-	}
-
-	total := len(fields) + 1 // include GraduationYear as a numeric field
-	present := 0
-	for _, f := range fields {
-		if f != "" {
-			present++
-		}
-	}
-	if profile.GraduationYear > 0 {
-		present++
-	}
-
-	if total == 0 {
-		return 0
-	}
-
-	percent := int(float64(present) / float64(total) * 100)
-	return percent
+func NewStudentService(repo *repositories.StudentRepository, storage *minio.Client, cfg *config.Config, log *logger.Logger) *StudentService {
+	return &StudentService{repo: repo, minioClient: storage, cfg: cfg, log: log}
 }
 
 func (s *StudentService) GetProfile(userID uuid.UUID) (*models.StudentProfile, error) {
-	if userID == uuid.Nil {
-		return nil, fmt.Errorf("%w: invalid user id", ErrInvalidStudentData)
-	}
 	profile, err := s.repo.GetByUserID(userID)
 	if errors.Is(err, gorm.ErrRecordNotFound) {
 		return nil, ErrStudentProfileNotFound
 	}
-	if err == nil && profile.College != nil {
-		profile.CollegeName = profile.College.Name
+	if err != nil {
+		return nil, err
 	}
-	return profile, err
+	return profile, s.setProfilePictureURL(context.Background(), profile)
 }
 
-func (s *StudentService) GetApplicationStats(
-	ctx context.Context,
-	profileID uuid.UUID,
-) (*repositories.StudentApplicationStats, error) {
-	if profileID == uuid.Nil {
-		return nil, fmt.Errorf("%w: invalid student profile id", ErrInvalidStudentData)
+func (s *StudentService) setProfilePictureURL(ctx context.Context, profile *models.StudentProfile) error {
+	if profile.ProfilePictureKey == "" {
+		return nil
+	}
+	if s.minioClient == nil {
+		return errors.New("profile picture storage unavailable")
 	}
+	url, err := s.minioClient.PresignedGetObject(ctx, s.cfg.MinIO.ProfileBucket, profile.ProfilePictureKey, time.Hour, nil)
+	if err != nil {
+		return err
+	}
+	profile.ProfilePicture = url.String()
+	return nil
+}
 
+func (s *StudentService) GetApplicationStats(ctx context.Context, profileID uuid.UUID) (*repositories.StudentApplicationStats, error) {
 	return s.repo.GetApplicationStats(ctx, profileID)
 }
 
 func (s *StudentService) CreateOrUpdateProfile(ctx context.Context, profile *models.StudentProfile) error {
 	if profile == nil || profile.UserID == uuid.Nil {
-		return fmt.Errorf("%w: profile and user id are required", ErrInvalidStudentData)
+		return ErrInvalidStudentData
 	}
-	existing, err := s.repo.GetByUserIDContext(ctx, profile.UserID)
-	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
-		return err
+	profile.FullName = strings.TrimSpace(profile.FullName)
+	if profile.FullName == "" || len(profile.FullName) > 255 || len(profile.Phone) > 50 || len(profile.Location) > 255 || len(profile.Bio) > 5000 || len(profile.CollegeName) > 255 || len(profile.Degree) > 255 || len(profile.FacultyOrMajor) > 255 || len(profile.CurrentSemester) > 64 || profile.GraduationYear < 0 || profile.GraduationYear > 9999 {
+		return fmt.Errorf("%w: name is required and profile fields must fit their limits", ErrInvalidStudentData)
 	}
-	if existing != nil && existing.CollegeID != nil {
-		if profile.CollegeID != nil && *profile.CollegeID != *existing.CollegeID {
-			return fmt.Errorf("%w: college changes require an administrator", ErrInvalidStudentData)
+	for _, link := range []string{profile.LinkedinURL, profile.GithubURL, profile.PortfolioURL} {
+		if len(link) > 255 || (link != "" && !strings.HasPrefix(link, "https://") && !strings.HasPrefix(link, "http://")) {
+			return fmt.Errorf("%w: links must use http or https and be at most 255 characters", ErrInvalidStudentData)
 		}
-		profile.CollegeID = existing.CollegeID
-	}
-	if profile.CollegeID == nil || *profile.CollegeID == uuid.Nil {
-		return fmt.Errorf("%w: college_id is required", ErrInvalidStudentData)
 	}
-	college, err := s.colleges.GetByID(ctx, *profile.CollegeID)
-	if errors.Is(err, gorm.ErrRecordNotFound) {
-		return fmt.Errorf("%w: college not found", ErrInvalidStudentData)
-	}
-	if err != nil {
+	profile.ID, profile.CreatedAt, profile.DeletedAt = uuid.Nil, time.Time{}, gorm.DeletedAt{}
+	profile.ProfilePictureKey, profile.ProfilePicture = "", ""
+	if err := s.repo.CreateOrUpdateProfile(ctx, profile); err != nil {
 		return err
 	}
-	if college.Status != "active" && (existing == nil || existing.CollegeID == nil) {
-		return fmt.Errorf("%w: college must be active", ErrInvalidStudentData)
-	}
-	profile.ID = uuid.Nil
-	profile.CreatedAt = time.Time{}
-	profile.DeletedAt = gorm.DeletedAt{}
-	profile.Documents = nil
-	profile.College, profile.CollegeName = college, college.Name
-	profile.ProfileCompletionPercent = s.CalculateProfileCompletion(profile)
-	return s.repo.CreateOrUpdateProfile(ctx, profile)
+	return s.setProfilePictureURL(ctx, profile)
 }
 
-// UploadDocument uploads the provided multipart file header to MinIO under the configured
-// student documents bucket. Returns the object key, size in bytes, detected mime type and error.
-func (s *StudentService) UploadDocument(userID, profileID uuid.UUID, fileHeader *multipart.FileHeader) (string, int64, string, error) {
-	if userID == uuid.Nil || profileID == uuid.Nil {
-		return "", 0, "", fmt.Errorf("%w: user id and profile id are required", ErrInvalidStudentData)
+func (s *StudentService) UploadProfilePicture(ctx context.Context, userID uuid.UUID, file *multipart.FileHeader) (*models.StudentProfile, error) {
+	profile, err := s.GetProfile(userID)
+	if err != nil {
+		return nil, err
 	}
-	if fileHeader == nil {
-		return "", 0, "", fmt.Errorf("%w: file is required", ErrInvalidStudentData)
+	data, contentType, extension, err := readProfilePicture(file)
+	if err != nil {
+		return nil, err
 	}
-	if fileHeader.Size <= 0 {
-		return "", 0, "", fmt.Errorf("%w: uploaded file is empty", ErrInvalidStudentData)
+	if s.minioClient == nil {
+		return nil, errors.New("profile picture storage unavailable")
 	}
-
-	f, err := fileHeader.Open()
+	key := fmt.Sprintf("student-pictures/%s/%s%s", userID, uuid.New(), extension)
+	bucket := s.cfg.MinIO.ProfileBucket
+	if _, err := s.minioClient.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: contentType}); err != nil {
+		return nil, err
+	}
+	oldKey, err := s.repo.UpdateProfilePicture(ctx, userID, key)
+	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
+	defer cancel()
 	if err != nil {
-		return "", 0, "", fmt.Errorf("opening uploaded file: %w", err)
+		if cleanupErr := s.minioClient.RemoveObject(cleanupCtx, bucket, key, minio.RemoveObjectOptions{}); cleanupErr != nil {
+			s.log.Error("clean up failed profile picture upload: %v", cleanupErr)
+		}
+		return nil, err
 	}
-	defer func() { _ = f.Close() }()
-
-	// Read up to 512 bytes for content type detection
-	buf := make([]byte, 512)
-	n, _ := f.Read(buf)
-	contentType := fileHeader.Header.Get("Content-Type")
-	if contentType == "" {
-		contentType = http.DetectContentType(buf[:n])
+	if oldKey != "" {
+		if err := s.minioClient.RemoveObject(cleanupCtx, bucket, oldKey, minio.RemoveObjectOptions{}); err != nil {
+			s.log.Error("clean up replaced profile picture: %v", err)
+		}
 	}
+	profile.ProfilePictureKey = key
+	return profile, s.setProfilePictureURL(ctx, profile)
+}
 
-	var reader io.Reader
-	// Try to rewind the file if it supports seeking
-	if seeker, ok := f.(io.Seeker); ok {
-		_, _ = seeker.Seek(0, io.SeekStart)
-		reader = f
-	} else {
-		// Non-seekable: prepend the portion we already read
-		reader = io.MultiReader(bytes.NewReader(buf[:n]), f)
+func (s *StudentService) GetResume(ctx context.Context, userID uuid.UUID) (*models.StudentDocument, error) {
+	profile, err := s.GetProfile(userID)
+	if err != nil {
+		return nil, err
+	}
+	resume, err := s.repo.GetResume(ctx, profile.ID)
+	if errors.Is(err, gorm.ErrRecordNotFound) {
+		return nil, nil
 	}
-
-	// Build object key: students/{userID}/{profileID}/{uuid}{ext}
-	ext := filepath.Ext(fileHeader.Filename)
-	objectKey := fmt.Sprintf("students/%s/%s/%s%s", userID.String(), profileID.String(), uuid.New().String(), ext)
-
-	ctx := context.Background()
-	bucket := s.cfg.MinIO.StudentDocBucket
-	putOpts := minio.PutObjectOptions{ContentType: contentType}
-
-	info, err := s.minioClient.PutObject(ctx, bucket, objectKey, reader, fileHeader.Size, putOpts)
 	if err != nil {
-		s.l.Error("failed to upload object to minio", err)
-		return "", 0, "", fmt.Errorf("put object: %w", err)
+		return nil, err
 	}
-
-	return objectKey, info.Size, contentType, nil
-}
-
-func (s *StudentService) AddDocument(document *models.StudentDocument) error {
-	if document == nil || document.UserID == uuid.Nil || document.ProfileID == uuid.Nil || document.ObjectKey == "" {
-		return fmt.Errorf("%w: complete document metadata is required", ErrInvalidStudentData)
+	if s.minioClient == nil {
+		return nil, errors.New("CV storage unavailable")
+	}
+	link, err := s.minioClient.PresignedGetObject(ctx, s.cfg.MinIO.StudentDocBucket, resume.ObjectKey, 5*time.Minute, nil)
+	if err != nil {
+		return nil, err
 	}
-	return s.repo.AddDocument(document)
+	resume.URL = link.String()
+	return resume, nil
 }
 
-func (s *StudentService) ListDocuments(userID uuid.UUID) ([]models.StudentDocument, error) {
+func (s *StudentService) UploadResume(ctx context.Context, userID uuid.UUID, file *multipart.FileHeader) (*models.StudentDocument, error) {
 	profile, err := s.GetProfile(userID)
 	if err != nil {
 		return nil, err
 	}
-	return s.repo.ListDocuments(profile.ID)
-}
-
-func (s *StudentService) SetDefaultDocument(userID, documentID uuid.UUID) error {
-	if documentID == uuid.Nil {
-		return fmt.Errorf("%w: invalid document id", ErrInvalidStudentData)
+	if file == nil || file.Size <= 0 || file.Size > maxResumeSize {
+		return nil, fmt.Errorf("%w: upload a PDF CV of at most 10 MiB", ErrInvalidStudentData)
 	}
-	profile, err := s.GetProfile(userID)
+	reader, err := file.Open()
 	if err != nil {
-		return err
+		return nil, err
 	}
-	document, err := s.getDocument(documentID)
+	defer reader.Close()
+	data, err := io.ReadAll(io.LimitReader(reader, maxResumeSize+1))
 	if err != nil {
-		return err
+		return nil, err
 	}
-	if document.ProfileID != profile.ID {
-		return ErrStudentDocumentNotFound
+	if len(data) > maxResumeSize || http.DetectContentType(data) != "application/pdf" {
+		return nil, fmt.Errorf("%w: upload a PDF CV of at most 10 MiB", ErrInvalidStudentData)
 	}
-	return s.repo.SetDefaultDocument(profile.ID, documentID)
-}
-
-func (s *StudentService) DeleteDocumentByUserID(userID, documentID uuid.UUID) error {
-	if userID == uuid.Nil || documentID == uuid.Nil {
-		return fmt.Errorf("%w: user id and document id are required", ErrInvalidStudentData)
+	if s.minioClient == nil {
+		return nil, errors.New("CV storage unavailable")
 	}
-	document, err := s.getDocument(documentID)
-	if err != nil {
-		return err
+	resume := &models.StudentDocument{UserID: userID, ProfileID: profile.ID, FileName: filepath.Base(file.Filename), MimeType: "application/pdf", Size: int64(len(data)), ObjectKey: fmt.Sprintf("students/%s/%s.pdf", userID, uuid.New())}
+	bucket := s.cfg.MinIO.StudentDocBucket
+	if _, err := s.minioClient.PutObject(ctx, bucket, resume.ObjectKey, bytes.NewReader(data), resume.Size, minio.PutObjectOptions{ContentType: "application/pdf"}); err != nil {
+		return nil, err
 	}
-	if document.UserID != userID {
-		return ErrStudentDocumentNotFound
+	oldKey, err := s.repo.SaveResume(ctx, resume)
+	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
+	defer cancel()
+	if err != nil {
+		if cleanupErr := s.minioClient.RemoveObject(cleanupCtx, bucket, resume.ObjectKey, minio.RemoveObjectOptions{}); cleanupErr != nil {
+			s.log.Error("clean up failed CV upload: %v", cleanupErr)
+		}
+		return nil, err
 	}
-	if err := s.DeleteDocument(document.ObjectKey); err != nil {
-		// Preserve the prior behavior: a storage cleanup failure must not leave
-		// inaccessible document metadata behind.
-		s.l.Error("failed to delete student document from storage: %v", err)
+	if oldKey != "" {
+		if err := s.minioClient.RemoveObject(cleanupCtx, bucket, oldKey, minio.RemoveObjectOptions{}); err != nil {
+			s.log.Error("clean up replaced CV: %v", err)
+		}
 	}
-	return s.repo.DeleteDocumentByID(document.ID)
+	return resume, nil
 }
 
-func (s *StudentService) getDocument(id uuid.UUID) (*models.StudentDocument, error) {
-	document, err := s.repo.GetDocumentByID(id)
-	if errors.Is(err, gorm.ErrRecordNotFound) {
-		return nil, ErrStudentDocumentNotFound
+func (s *StudentService) DeleteResume(ctx context.Context, userID uuid.UUID) error {
+	profile, err := s.GetProfile(userID)
+	if err != nil {
+		return err
 	}
-	return document, err
-}
-
-// DeleteDocument removes the object identified by objectKey from the student documents bucket.
-func (s *StudentService) DeleteDocument(objectKey string) error {
-	if objectKey == "" {
-		return fmt.Errorf("%w: object key is required", ErrInvalidStudentData)
+	if s.minioClient == nil {
+		return errors.New("CV storage unavailable")
 	}
-	ctx := context.Background()
-	bucket := s.cfg.MinIO.StudentDocBucket
-	err := s.minioClient.RemoveObject(ctx, bucket, objectKey, minio.RemoveObjectOptions{})
+	documents, err := s.repo.DeleteResume(ctx, profile.ID, userID)
 	if err != nil {
-		s.l.Error("failed to remove object from minio", err)
 		return err
 	}
+	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
+	defer cancel()
+	// shortcut: storage cleanup is best effort, add retries if storage failures persist.
+	for _, document := range documents {
+		if err := s.minioClient.RemoveObject(cleanupCtx, s.cfg.MinIO.StudentDocBucket, document.ObjectKey, minio.RemoveObjectOptions{}); err != nil {
+			s.log.Error("clean up deleted CV: %v", err)
+		}
+	}
 	return nil
 }

# .env.example

diff --git a/.env.example b/.env.example
index 96356d8..c86292c 100644
--- a/.env.example
+++ b/.env.example
@@ -12,12 +12,20 @@ DB_SSL_MODE=disable
 
 # JWT
 JWT_ACCESS_SECRET=your-access-secret
-JWT_REFRESH_SECRET=your-refresh-secret
 JWT_ACCESS_EXPIRY=2h
 JWT_REFRESH_EXPIRY=168h
 
+# Oracle Email Delivery (regional endpoint and approved sender)
+SMTP_HOST=smtp.email.your-region.oci.oraclecloud.com
+SMTP_PORT=587
+SMTP_USERNAME=your-oracle-smtp-username
+SMTP_PASSWORD=your-oracle-smtp-password
+SMTP_FROM=no-reply@example.com
+# Generate a separate random secret, e.g. openssl rand -hex 32
+EMAIL_OTP_SECRET=replace-with-a-random-secret-of-at-least-32-characters
+
 # MinIO
-MINIO_ENDPOINT=localhost:9000
+MINIO_ENDPOINT=localhost:9002
 MINIO_ACCESS_KEY=your-minio-access-key
 MINIO_SECRET_KEY=your-minio-secret-key
 MINIO_USE_SSL=false
@@ -25,9 +33,3 @@ MINIO_USE_SSL=false
 # MinIO Buckets
 MINIO_PROFILE_BUCKET=profile-images
 MINIO_STUDENT_DOCUMENT_BUCKET=student-documents
-MINIO_COMPANY_LOGO_BUCKET=company-logos
-MINIO_COMPANY_DOCUMENT_BUCKET=company-documents
-
-# Admin user (for seeding)
-ADMIN_EMAIL=admin@example.com
-ADMIN_PASSWORD=your-admin-password
\ No newline at end of file

# BACKEND_README.md

diff --git a/BACKEND_README.md b/BACKEND_README.md
index 6f9be34..682020a 100644
--- a/BACKEND_README.md
+++ b/BACKEND_README.md
@@ -1,626 +1,15 @@
-# Student Job Portal - Backend API
-
-A production-ready MVP backend for a student-focused job portal built with Go, REST API, PostgreSQL, MinIO, JWT authentication, and role-based access control.
-
-## 📋 Table of Contents
-
-- [Overview](#overview)
-- [Technology Stack](#technology-stack)
-- [Project Status](#project-status)
-- [Quick Start](#quick-start)
-- [Development](#development)
-- [Docker Deployment](#docker-deployment)
-- [Project Structure](#project-structure)
-- [API Endpoints](#api-endpoints)
-- [Configuration](#configuration)
-- [Architecture](#architecture)
-- [Testing](#testing)
-- [Security](#security)
-- [Contributing](#contributing)
-
-## Overview
-
-This backend serves a comprehensive student job portal platform with three main user roles:
-
-1. **Students**: Create profiles, upload CVs, search for jobs, apply for positions, and track applications
-2. **Employers**: Create company profiles, post jobs, review applications, schedule interviews
-3. **Admins**: Manage users, verify employers, approve jobs, handle reports
-
-### Core Workflow
-
-```
-Student Registration
-    ↓
-Profile Completion
-    ↓
-Job Search & Filtering
-    ↓
-Application Submission
-    ↓
-Employer Review
-    ↓
-Application Status Change
-    ↓
-Notifications
-```
-
-## Technology Stack
-
-| Component | Technology | Purpose |
-|-----------|-----------|---------|
-| **Language** | Go 1.24.0 | High-performance backend |
-| **Web Framework** | Gin | REST API with middleware support |
-| **Database** | PostgreSQL 16 | Relational data storage |
-| **ORM** | GORM | Type-safe database operations |
-| **Storage** | MinIO | Object storage for files |
-| **Authentication** | JWT | Token-based authentication |
-| **Containerization** | Docker | Application deployment |
-| **Orchestration** | Docker Compose | Multi-service coordination |
-| **API Documentation** | Swagger/OpenAPI | API documentation |
-
-## Project Status
-
-### ✅ Phase 1: Complete
-
-- Project initialization
-- Docker setup (Dockerfile, docker-compose.yml)
-- PostgreSQL connection
-- MinIO integration
-- Configuration management
-- Logging system
-- Common response structures
-- Makefile for development
-
-**Status**: Production-ready foundation ✓
-
-### 🔄 Phases 2-8: Upcoming
-
-See [Implementation Roadmap](#implementation-roadmap) for details.
-
-## Quick Start
-
-### Prerequisites
-
-- Docker and Docker Compose installed
-- Git installed
-- Go 1.24.0+ (for local development)
-
-### Start the Application
-
-```bash
-# Clone the repository
-cd /home/prasanna/Projects/student_job_portal/backend
-
-# Copy environment variables
-cp .env.example .env
-
-# Start all services (PostgreSQL, MinIO, API)
-docker-compose up --build
-
-# Verify it's running
-curl http://localhost:8080/health
-```
-
-### Stop the Application
-
-```bash
-docker-compose down
-```
-
-### View Logs
-
-```bash
-docker-compose logs -f api
-docker-compose logs -f postgres
-docker-compose logs -f minio
-```
-
-## Development
-
-### Local Setup
-
-#### Prerequisites for Local Development
-
-- Go 1.24.0+
-- PostgreSQL 16
-- MinIO or S3-compatible storage
-- `make` command-line tool
-
-#### Running Locally
-
-```bash
-# Copy environment template
-cp .env.example .env
-
-# Edit .env for local development
-# Update DB_HOST, MINIO_ENDPOINT if not using Docker for these services
-
-# Start database and MinIO with Docker
-make dev
-
-# In another terminal, run the API
-go run ./cmd/api/main.go
-
-# Stop services
-make dev-down
-```
-
-### Common Make Commands
-
-```bash
-# Development
-make dev              # Start PostgreSQL and MinIO with Docker
-make dev-down         # Stop local services
-
-# Docker
-make docker-up        # Start complete Docker stack
-make docker-down      # Stop all services
-make docker-rebuild   # Rebuild Docker images
-make docker-logs      # View API logs
-
-# Code Quality
-make fmt              # Format code with go fmt
-make lint             # Run linter (requires golangci-lint)
-make test             # Run tests with coverage
-
-# Build
-make build            # Build binary to bin/api
-make clean            # Remove build artifacts
-```
-
-### Building from Source
-
-```bash
-# Download dependencies
-go mod download
-
-# Build the application
-make build
-
-# Run the binary
-./bin/api
-```
-
-## Docker Deployment
-
-### Docker Compose Stack
-
-The [docker-compose.yml](/home/prasanna/Projects/student_job_portal/backend/docker-compose.yml) orchestrates:
-
-1. **PostgreSQL Service**
-   - Port: 5432
-   - Database: student_job_portal
-   - Health check: Built-in pg_isready
-   - Volume: postgres_data (persistent)
-
-2. **MinIO Service**
-   - API Port: 9000
-   - Console Port: 9001
-   - Access Key: minioadmin
-   - Health check: MinIO health endpoint
-   - Volume: minio_data (persistent)
-
-3. **API Service**
-   - Port: 8080
-   - Depends on: PostgreSQL and MinIO
-   - Health check: /health endpoint
-   - Automatic restart policy
-
-### Building Docker Image
-
-```bash
-# Build locally
-docker build -t student-job-portal-api:latest .
-
-# Run locally
-docker run -p 8080:8080 \
-  -e APP_PORT=8080 \
-  -e DB_HOST=postgres \
-  -e MINIO_ENDPOINT=minio:9000 \
-  student-job-portal-api:latest
-```
-
-### Multi-Stage Build
-
-The [Dockerfile](/home/prasanna/Projects/student_job_portal/backend/Dockerfile) uses multi-stage builds:
-
-1. **Builder Stage**: Compiles Go code
-2. **Runtime Stage**: Alpine Linux base with only the binary
-
-Benefits:
-- Smaller final image (~20-30MB)
-- No build tools in production
-- Improved security
-
-## Project Structure
-
-```
-backend/
-├── cmd/
-│   └── api/
-│       └── main.go                 # Application entry point
-│
-├── internal/
-│   ├── config/
-│   │   └── config.go              # Configuration management
-│   ├── database/
-│   │   └── connection.go          # Database connection & pooling
-│   ├── logger/
-│   │   └── logger.go              # Structured logging
-│   ├── responses/
-│   │   └── response.go            # Consistent JSON responses
-│   ├── storage/
-│   │   └── minio.go               # MinIO client & bucket management
-│   ├── models/                     # (Phase 2+) GORM database models
-│   ├── repositories/              # (Phase 2+) Data access layer
-│   ├── services/                  # (Phase 2+) Business logic
-│   ├── handlers/                  # (Phase 2+) HTTP request handlers
-│   ├── middleware/                # (Phase 2+) HTTP middleware
-│   ├── routes/                    # (Phase 2+) Route definitions
-│   ├── validation/                # (Phase 2+) Request validation
-│   └── utils/                     # (Phase 2+) Utility functions
-│
-├── migrations/                     # (Phase 2+) SQL migration files
-├── docs/                          # Documentation
-│   ├── PHASE1.md                 # Phase 1 implementation details
-│   └── ...
-│
-├── Dockerfile                      # Multi-stage Docker build
-├── docker-compose.yml             # Complete stack configuration
-├── Makefile                       # Development commands
-├── go.mod                         # Go module definition
-├── go.sum                         # Dependency checksums
-├── .env.example                   # Environment variables template
-├── .gitignore                     # Git ignore rules
-└── readme.md                      # Project specification (original)
-```
-
-## API Endpoints
-
-### Phase 1 (Current)
-
-#### Health Check
-```
-GET /health
-GET /api/v1/health
-```
-
-Response:
-```json
-{
-  "success": true,
-  "message": "Health check passed",
-  "data": {
-    "status": "ok",
-    "timestamp": "2024-08-05T18:10:38Z"
-  }
-}
-```
-
-### Phase 2-8 (Upcoming)
-
-Detailed endpoint documentation will be added as each phase is completed. See [implementation roadmap](#implementation-roadmap) for planned endpoints.
-
-## Configuration
-
-### Environment Variables
-
-All configuration is managed through environment variables. Copy [.env.example](/home/prasanna/Projects/student_job_portal/backend/.env.example) and customize:
-
-```bash
-cp .env.example .env
-```
-
-### Available Variables
-
-```env
-# Application
-APP_ENV=development              # Environment: development, staging, production
-APP_PORT=8080                    # API server port
-
-# Database (PostgreSQL)
-DB_HOST=postgres                 # PostgreSQL host
-DB_PORT=5432                     # PostgreSQL port
-DB_NAME=student_job_portal       # Database name
-DB_USER=postgres                 # Database user
-DB_PASSWORD=postgres             # Database password
-DB_SSL_MODE=disable              # SSL mode: disable, require, verify-full
-
-# JWT (CHANGE IN PRODUCTION!)
-JWT_ACCESS_SECRET=your-secret    # Access token signing key
-JWT_REFRESH_SECRET=your-secret   # Refresh token signing key
-JWT_ACCESS_EXPIRY=15m            # Access token lifetime
-JWT_REFRESH_EXPIRY=168h          # Refresh token lifetime
-
-# MinIO (Object Storage)
-MINIO_ENDPOINT=minio:9000        # MinIO API endpoint
-MINIO_ACCESS_KEY=minioadmin      # MinIO access key
-MINIO_SECRET_KEY=minioadmin      # MinIO secret key
-MINIO_USE_SSL=false              # Use HTTPS for MinIO
-MINIO_PROFILE_BUCKET=profile-images
-MINIO_STUDENT_DOCUMENT_BUCKET=student-documents
-MINIO_COMPANY_LOGO_BUCKET=company-logos
-MINIO_COMPANY_DOCUMENT_BUCKET=company-documents
-```
-
-### Configuration Validation
-
-The application validates required environment variables on startup. Missing or invalid configuration will cause the application to fail with descriptive error messages.
-
-**Required Variables:**
-- APP_ENV
-- DB_HOST, DB_NAME, DB_USER, DB_PASSWORD
-- JWT_ACCESS_SECRET, JWT_REFRESH_SECRET
-- MINIO_ACCESS_KEY, MINIO_SECRET_KEY
-
-## Architecture
-
-### Layered Architecture
-
-```
-┌─────────────────────────────────┐
-│   HTTP Request (Router)         │
-├─────────────────────────────────┤
-│   Middleware Layer              │
-│   (Auth, Logging, Recovery)     │
-├─────────────────────────────────┤
-│   Handler Layer                 │
-│   (Request validation/response) │
-├─────────────────────────────────┤
-│   Service Layer                 │
-│   (Business logic)              │
-├─────────────────────────────────┤
-│   Repository Layer              │
-│   (Database queries)            │
-├─────────────────────────────────┤
-│   Database Layer                │
-│   (PostgreSQL + GORM)           │
-└─────────────────────────────────┘
-          ↕ (Parallel)
-    ┌──────────────┐
-    │ MinIO Storage│
-    │ (File I/O)   │
-    └──────────────┘
-```
-
-### Key Design Principles
-
-1. **Separation of Concerns**: Clear boundaries between layers
-2. **Dependency Injection**: Dependencies passed to functions
-3. **Error Handling**: Comprehensive error handling at each layer
-4. **Transaction Management**: Database transactions for critical operations
-5. **Security First**: Security checks at middleware level
-6. **Performance**: Connection pooling, query optimization, caching (Phase 5+)
-
-## Testing
-
-### Test Strategy
-
-```
-Unit Tests (Services, Validators)
-  ↓
-Integration Tests (Handlers + Repositories)
-  ↓
-End-to-End Tests (Full API workflows)
-```
-
-### Running Tests
-
-```bash
-# Run all tests with coverage
-make test
-
-# Run specific test file
-go test -v ./internal/config
-
-# Run with race detector
-go test -race ./...
-
-# Generate coverage report
-go test -v -coverprofile=coverage.out ./...
-go tool cover -html=coverage.out
-```
-
-### Test Files Location
-
-```
-internal/
-├── config/config_test.go
-├── database/connection_test.go
-├── logger/logger_test.go
-├── handlers/*/handler_test.go
-├── services/*/service_test.go
-└── repositories/*/repository_test.go
-```
-
-### Testing with Docker
-
-```bash
-# Run tests in Docker container
-docker-compose exec api go test ./...
-```
-
-## Security
-
-### Implemented (Phase 1)
-
-✅ Environment-based configuration (no hardcoded secrets)
-✅ Non-root Docker user
-✅ Connection pooling (prevents resource exhaustion)
-✅ CORS configuration
-✅ Panic recovery middleware
-
-### To Be Implemented
-
-⏳ Password hashing (bcrypt) - Phase 2
-⏳ JWT authentication - Phase 2
-⏳ Role-based access control (RBAC) - Phase 2
-⏳ Rate limiting - Phase 2
-⏳ Request validation - Phase 2
-⏳ SQL injection prevention - Phase 3+
-⏳ HTTPS support - Phase 8
-⏳ Audit logging - Phase 7
-
-### Security Best Practices
-
-1. **Never commit .env files** with secrets
-2. **Rotate JWT secrets** regularly
-3. **Use strong passwords** for database and MinIO
-4. **Enable HTTPS** in production
-5. **Implement rate limiting** for authentication endpoints
-6. **Use database transactions** for critical operations
-7. **Validate all inputs** at handler level
-8. **Audit important actions** (Phase 7)
-
-## Implementation Roadmap
-
-### Phase 2: User Authentication (🔄 Next)
-- User model and database schema
-- JWT token generation and validation
-- User registration and login
-- Refresh token management
-- RBAC middleware
-- Estimated: 2-3 weeks
-
-### Phase 3: Student Profile
-- Student profile model
-- Education, skills, projects, certifications
-- CV and profile image uploads
-- Profile completion tracking
-- Estimated: 2 weeks
-
-### Phase 4: Companies & Employers
-- Company profile
-- Employer verification workflow
-- Company document uploads
-- Estimated: 1-2 weeks
-
-### Phase 5: Jobs & Search
-- Job posting model
-- Job search and filtering
-- Saved jobs functionality
-- Pagination and sorting
-- Estimated: 2-3 weeks
-
-### Phase 6: Applications
-- Application model
-- Application status tracking
-- Interview scheduling
-- Status history logging
-- Estimated: 2-3 weeks
-
-### Phase 7: Advanced Features
-- Notifications system
-- Reporting and moderation
-- Admin dashboard APIs
-- Audit logging
-- Estimated: 2-3 weeks
-
-### Phase 8: Polish & Deployment
-- Unit and integration tests
-- Swagger documentation
-- Database seeding
-- README and deployment guides
-- Security review and hardening
-- Estimated: 1-2 weeks
-
-**Total Timeline**: 14-21 weeks
-
-## Contributing
-
-### Code Style
-
-```bash
-# Format code
-make fmt
-
-# Run linter (requires golangci-lint)
-make lint
-```
-
-### Git Workflow
-
-1. Create a feature branch: `git checkout -b feature/feature-name`
-2. Make your changes
-3. Run tests: `make test`
-4. Format code: `make fmt`
-5. Commit with descriptive messages
-6. Push and create a pull request
-
-### Commit Message Format
-
-```
-type: description
-
-Longer explanation if needed.
-
-Related to #123
-```
-
-Types: feat, fix, docs, test, refactor, chore
-
-## Troubleshooting
-
-### Docker Issues
-
 ```bash
-# Clean all containers and volumes
-docker-compose down -v
-
-# Rebuild and start
-docker-compose up --build
-
-# Check service status
-docker-compose ps
-
-# View logs
-docker-compose logs -f [service-name]
+cd backend
+[ -f .env ] || cp .env.example .env
+docker compose up --build
 ```
 
-### Database Connection Issues
-
-1. Verify PostgreSQL is running: `docker-compose logs postgres`
-2. Check DB_HOST in .env (should be `postgres` when using Docker)
-3. Verify credentials match docker-compose.yml
-4. Ensure database exists: Created automatically on first run
-
-### MinIO Issues
-
-1. Check MinIO logs: `docker-compose logs minio`
-2. Access console: http://localhost:9001
-3. Verify credentials in .env
-4. Check bucket permissions
-
-### API Won't Start
-
-```bash
-# Check for port conflicts
-lsof -i :8080
-
-# Check logs
-docker-compose logs api
-
-# Verify environment variables
-docker-compose config | grep API
-```
-
-## Support & Documentation
-
-- **Phase 1 Details**: [docs/PHASE1.md](/home/prasanna/Projects/student_job_portal/backend/docs/PHASE1.md)
-- **Original Specification**: [readme.md](/home/prasanna/Projects/student_job_portal/backend/readme.md)
-- **API Documentation**: (Coming in Phase 8 with Swagger)
-- **Database Schema**: (Coming in Phase 2 with migrations)
-
-## License
-
-This project is proprietary and confidential.
-
-## Contact
-
-For questions or issues, please contact the development team.
-
----
+New accounts require email confirmation. Configure Oracle SMTP and a separate
+OTP secret using the [email verification setup](docs/EMAIL_VERIFICATION.md).
 
-**Last Updated**: August 5, 2024
-**Status**: Phase 1 Complete ✓
-**Next Phase**: Phase 2 (User Authentication)
+The supported roles are `student` and `employer`. Employers can publish internships
+as soon as they confirm their email and create a company profile. Admin pages,
+API endpoints, and employer approval have been removed. Existing admin accounts
+cannot sign in or renew sessions; their stored records are retained. Legacy
+employer approval values are ignored. Existing private drafts stay private until
+their owner publishes them.

# cmd/api/main.go

diff --git a/cmd/api/main.go b/cmd/api/main.go
index ca93c62..f86cf4d 100644
--- a/cmd/api/main.go
+++ b/cmd/api/main.go
@@ -3,22 +3,15 @@ package main
 import (
 	"fmt"
 	"net/http"
-	"os"
-	"strings"
 	"time"
 
 	"github.com/gin-contrib/cors"
 	"github.com/gin-gonic/gin"
-	"gorm.io/gorm"
 
 	"github.com/prasanna/student-job-portal/backend/internal/config"
 	"github.com/prasanna/student-job-portal/backend/internal/database"
 	"github.com/prasanna/student-job-portal/backend/internal/logger"
-	"github.com/prasanna/student-job-portal/backend/internal/models"
-	"github.com/prasanna/student-job-portal/backend/internal/repositories"
 	"github.com/prasanna/student-job-portal/backend/internal/routes"
-	"github.com/prasanna/student-job-portal/backend/internal/seeder"
-	"github.com/prasanna/student-job-portal/backend/internal/services"
 	"github.com/prasanna/student-job-portal/backend/internal/storage"
 )
 
@@ -43,26 +36,17 @@ func main() {
 		log.Fatal("AutoMigrate failed: %v", err)
 	}
 
-	// 3. Seed Initial Data
-	if !strings.EqualFold(cfg.App.Env, "production") {
-		if _, err := seeder.Run(db, os.Getenv("SEED_PASSWORD")); err != nil {
-			log.Fatal("Failed to seed development data: %v", err)
-		}
-		log.Info("Development database seed complete")
-	}
-	seedAdminUser(db, cfg, log)
-
-	// 4. Connect MinIO Storage
+	// 3. Connect MinIO Storage
 	minioClient, err := storage.ConnectMinIO(&cfg.MinIO, log)
 	if err != nil {
 		log.Fatal("Failed to connect to MinIO: %v", err)
 	}
 
-	// 5. Initialize Engine & Register Routes
+	// 4. Initialize Engine & Register Routes
 	router := initRouter(cfg)
 	routes.Register(router, db, cfg, log, minioClient)
 
-	// 6. Start Server
+	// 5. Start Server
 	startServer(router, cfg, log)
 }
 
@@ -70,40 +54,6 @@ func main() {
 // INITIALIZATION HELPERS
 // ============================================================================
 
-func seedAdminUser(db *gorm.DB, cfg *config.Config, log *logger.Logger) {
-	adminEmail := getenv("ADMIN_EMAIL", "")
-	adminPassword := getenv("ADMIN_PASSWORD", "")
-
-	if adminEmail == "" || adminPassword == "" {
-		return
-	}
-
-	userRepo := repositories.NewUserRepository(db)
-	if _, err := userRepo.GetByEmail(adminEmail); err == nil {
-		return // Admin user already exists
-	}
-
-	hash, err := services.NewAuthService(userRepo, cfg, log).HashPassword(adminPassword)
-	if err != nil {
-		log.Error("Failed to hash admin password: %v", err)
-		return
-	}
-
-	admin := &models.User{
-		Email:        adminEmail,
-		PasswordHash: hash,
-		FullName:     "Admin",
-		Role:         models.RoleAdmin,
-	}
-
-	if err := userRepo.Create(admin); err != nil {
-		log.Error("Failed to create admin user: %v", err)
-		return
-	}
-
-	log.Info("Seeded admin user: %s", adminEmail)
-}
-
 func initRouter(cfg *config.Config) *gin.Engine {
 	if cfg.App.Env == "production" {
 		gin.SetMode(gin.ReleaseMode)
@@ -136,15 +86,8 @@ func CORSMiddleware() gin.HandlerFunc {
 		AllowOrigins:     []string{"http://localhost:3000"},
 		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
 		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Requested-With"},
-		ExposeHeaders:    []string{"Content-Length"},
+		ExposeHeaders:    []string{"Content-Length", "Retry-After"},
 		AllowCredentials: true,
 		MaxAge:           12 * time.Hour,
 	})
 }
-
-func getenv(key, def string) string {
-	if v, ok := os.LookupEnv(key); ok {
-		return v
-	}
-	return def
-}

# cmd/seed/main.go

diff --git a/cmd/seed/main.go b/cmd/seed/main.go
index d0a0155..1f3becb 100644
--- a/cmd/seed/main.go
+++ b/cmd/seed/main.go
@@ -38,15 +38,7 @@ func main() {
 		panic(err)
 	}
 
-	fmt.Printf(
-		"Seed complete: %d users, %d student profiles, %d employer profiles, %d verifications, %d internships, %d applications.\n",
-		summary.Users,
-		summary.StudentProfiles,
-		summary.RecruiterProfiles,
-		summary.OrganizationVerifications,
-		summary.Internships,
-		summary.Applications,
-	)
+	fmt.Printf("Seed complete: %d users, %d student profiles, %d employer profiles, %d internships, %d applications.\n", summary.Users, summary.StudentProfiles, summary.RecruiterProfiles, summary.Internships, summary.Applications)
 	if usingDefaultPassword {
 		fmt.Printf("Development login password: %s (override with SEED_PASSWORD).\n", seeder.DefaultPassword)
 	} else {

# docker-compose.yml

diff --git a/docker-compose.yml b/docker-compose.yml
index 89688f1..cc5e15a 100644
--- a/docker-compose.yml
+++ b/docker-compose.yml
@@ -1,4 +1,3 @@
-
 services:
   postgres:
     image: postgres:16-alpine
@@ -26,8 +25,8 @@ services:
       MINIO_ROOT_USER: ${MINIO_ACCESS_KEY:-minioadmin}
       MINIO_ROOT_PASSWORD: ${MINIO_SECRET_KEY:-minioadmin}
     ports:
-      - "9000:9000"
-      - "9001:9001"
+      - "9002:9000"
+      - "9003:9001"
     volumes:
       - minio_data:/minio_data
     command: server /minio_data --console-address ":9001"
@@ -59,17 +58,20 @@ services:
       DB_PASSWORD: ${DB_PASSWORD:-postgres}
       DB_SSL_MODE: ${DB_SSL_MODE:-disable}
       JWT_ACCESS_SECRET: ${JWT_ACCESS_SECRET}
-      JWT_REFRESH_SECRET: ${JWT_REFRESH_SECRET}
       JWT_ACCESS_EXPIRY: ${JWT_ACCESS_EXPIRY:-15m}
       JWT_REFRESH_EXPIRY: ${JWT_REFRESH_EXPIRY:-168h}
+      SMTP_HOST: ${SMTP_HOST:-}
+      SMTP_PORT: ${SMTP_PORT:-587}
+      SMTP_USERNAME: ${SMTP_USERNAME:-}
+      SMTP_PASSWORD: ${SMTP_PASSWORD:-}
+      SMTP_FROM: ${SMTP_FROM:-}
+      EMAIL_OTP_SECRET: ${EMAIL_OTP_SECRET:-}
       MINIO_ENDPOINT: ${MINIO_DOCKER_ENDPOINT:-minio:9000}
       MINIO_ACCESS_KEY: ${MINIO_ACCESS_KEY:-minioadmin}
       MINIO_SECRET_KEY: ${MINIO_SECRET_KEY:-minioadmin}
       MINIO_USE_SSL: ${MINIO_USE_SSL:-false}
       MINIO_PROFILE_BUCKET: ${MINIO_PROFILE_BUCKET:-profile-images}
       MINIO_STUDENT_DOCUMENT_BUCKET: ${MINIO_STUDENT_DOCUMENT_BUCKET:-student-documents}
-      MINIO_COMPANY_LOGO_BUCKET: ${MINIO_COMPANY_LOGO_BUCKET:-company-logos}
-      MINIO_COMPANY_DOCUMENT_BUCKET: ${MINIO_COMPANY_DOCUMENT_BUCKET:-company-documents}
     ports:
       - "8080:8080"
     healthcheck:

# docs/SEEDING.md

diff --git a/docs/SEEDING.md b/docs/SEEDING.md
index ef65802..30d7b98 100644
--- a/docs/SEEDING.md
+++ b/docs/SEEDING.md
@@ -1,5 +1,8 @@
 # Development database seeding
 
+API startup does not create demo data. Run the command below only when you want
+to populate the database with the development dataset.
+
 From the `backend` directory, run:
 
 ```bash
@@ -16,19 +19,19 @@ password for every seeded account.
 
 | Role | Email |
 | --- | --- |
-| Admin | `seed.admin@jobportal.test` |
 | Student | `seed.student@jobportal.test` |
 | Student | `seed.student2@jobportal.test` |
-| Approved employer | `seed.employer@jobportal.test` |
-| Pending employer | `seed.employer.pending@jobportal.test` |
-| Rejected employer | `seed.employer.rejected@jobportal.test` |
+| Employer | `seed.employer@jobportal.test` |
+| Employer | `seed.employer.pending@jobportal.test` |
+| Employer | `seed.employer.rejected@jobportal.test` |
 
 ## Dataset coverage
 
-- Two 100% complete student profiles with education, skills, projects, and certifications.
-- Three complete employer profiles with approved, pending, and rejected organization verifications.
-- Six internships: three published, two private, and one closed.
-- Six applications: submitted, reviewing, shortlisted, accepted, rejected, and withdrawn.
+- Two student profiles with college and degree details.
+- Three employer profiles, ready to publish without approval.
+- Six internships: three published, two private drafts, and one closed.
+- Six applications covering submitted, accepted, rejected, and withdrawn states.
 
-The seed intentionally omits uploaded document and logo records because their
-database metadata would point to MinIO objects that do not exist.
+The older `pending` and `rejected` email names are retained for existing demo logins;
+they do not restrict employer access. All seeded accounts have confirmed email addresses. The seed omits uploaded
+CVs and logos because their metadata would point to MinIO objects that do not exist.

# internal/config/config.go

diff --git a/internal/config/config.go b/internal/config/config.go
index fa1da0e..446c818 100644
--- a/internal/config/config.go
+++ b/internal/config/config.go
@@ -2,7 +2,9 @@ package config
 
 import (
 	"fmt"
+	"net/mail"
 	"os"
+	"strings"
 	"time"
 
 	"github.com/joho/godotenv"
@@ -14,6 +16,33 @@ type Config struct {
 	JWT      JWTConfig
 	MinIO    MinIOConfig
 	Server   ServerConfig
+	Email    EmailConfig
+}
+
+type EmailConfig struct {
+	Host      string
+	Port      string
+	Username  string
+	Password  string
+	From      string
+	OTPSecret string
+}
+
+func (e EmailConfig) Validate() error {
+	if e.Host == "" || strings.ContainsAny(e.Host, "\r\n /:") || e.Username == "" || e.Password == "" {
+		return fmt.Errorf("SMTP_HOST, SMTP_USERNAME and SMTP_PASSWORD are required")
+	}
+	if e.Port != "465" && e.Port != "587" {
+		return fmt.Errorf("SMTP_PORT must be 465 (TLS) or 587 (STARTTLS)")
+	}
+	address, err := mail.ParseAddress(e.From)
+	if err != nil || address.Address != e.From || strings.ContainsAny(e.From, "\r\n") {
+		return fmt.Errorf("SMTP_FROM must be a plain approved sender email address")
+	}
+	if len(e.OTPSecret) < 32 {
+		return fmt.Errorf("EMAIL_OTP_SECRET must contain at least 32 characters")
+	}
+	return nil
 }
 
 type AppConfig struct {
@@ -32,27 +61,22 @@ type DatabaseConfig struct {
 
 type JWTConfig struct {
 	AccessSecret  string
-	RefreshSecret string
 	AccessExpiry  time.Duration
 	RefreshExpiry time.Duration
 }
 
 type MinIOConfig struct {
-	Endpoint               string
-	AccessKey              string
-	SecretKey              string
-	UseSSL                 bool
-	ProfileBucket          string
-	OrganizationLogoBucket string
-	StudentDocBucket       string
-	CompanyLogoBucket      string
-	CompanyDocBucket       string
+	Endpoint         string
+	AccessKey        string
+	SecretKey        string
+	UseSSL           bool
+	ProfileBucket    string
+	StudentDocBucket string
 }
 
 type ServerConfig struct {
 	ReadTimeout  time.Duration
 	WriteTimeout time.Duration
-	MaxBodySize  int64
 }
 
 func Load() (*Config, error) {
@@ -74,25 +98,28 @@ func Load() (*Config, error) {
 		},
 		JWT: JWTConfig{
 			AccessSecret:  getEnv("JWT_ACCESS_SECRET", ""),
-			RefreshSecret: getEnv("JWT_REFRESH_SECRET", ""),
 			AccessExpiry:  parseDuration(getEnv("JWT_ACCESS_EXPIRY", "15m")),
 			RefreshExpiry: parseDuration(getEnv("JWT_REFRESH_EXPIRY", "168h")),
 		},
 		MinIO: MinIOConfig{
-			Endpoint:               getEnv("MINIO_ENDPOINT", "localhost:9000"),
-			AccessKey:              getEnv("MINIO_ACCESS_KEY", ""),
-			SecretKey:              getEnv("MINIO_SECRET_KEY", ""),
-			UseSSL:                 getEnv("MINIO_USE_SSL", "false") == "true",
-			ProfileBucket:          getEnv("MINIO_PROFILE_BUCKET", "profile-images"),
-			OrganizationLogoBucket: getEnv("MINIO_ORGANIZATION_LOGO_BUCKET", "organization-logos"),
-			StudentDocBucket:       getEnv("MINIO_STUDENT_DOCUMENT_BUCKET", "student-documents"),
-			CompanyLogoBucket:      getEnv("MINIO_COMPANY_LOGO_BUCKET", "company-logos"),
-			CompanyDocBucket:       getEnv("MINIO_COMPANY_DOCUMENT_BUCKET", "company-documents"),
+			Endpoint:         getEnv("MINIO_ENDPOINT", "localhost:9000"),
+			AccessKey:        getEnv("MINIO_ACCESS_KEY", ""),
+			SecretKey:        getEnv("MINIO_SECRET_KEY", ""),
+			UseSSL:           getEnv("MINIO_USE_SSL", "false") == "true",
+			ProfileBucket:    getEnv("MINIO_PROFILE_BUCKET", "profile-images"),
+			StudentDocBucket: getEnv("MINIO_STUDENT_DOCUMENT_BUCKET", "student-documents"),
 		},
 		Server: ServerConfig{
 			ReadTimeout:  15 * time.Second,
 			WriteTimeout: 15 * time.Second,
-			MaxBodySize:  50 * 1024 * 1024, // 50MB
+		},
+		Email: EmailConfig{
+			Host:      getEnv("SMTP_HOST", ""),
+			Port:      getEnv("SMTP_PORT", "587"),
+			Username:  getEnv("SMTP_USERNAME", ""),
+			Password:  getEnv("SMTP_PASSWORD", ""),
+			From:      getEnv("SMTP_FROM", ""),
+			OTPSecret: getEnv("EMAIL_OTP_SECRET", ""),
 		},
 	}
 
@@ -116,15 +143,17 @@ func (c *Config) Validate() error {
 	if c.JWT.AccessSecret == "" {
 		return fmt.Errorf("JWT_ACCESS_SECRET is required")
 	}
-	if c.JWT.RefreshSecret == "" {
-		return fmt.Errorf("JWT_REFRESH_SECRET is required")
-	}
 	if c.MinIO.AccessKey == "" {
 		return fmt.Errorf("MINIO_ACCESS_KEY is required")
 	}
 	if c.MinIO.SecretKey == "" {
 		return fmt.Errorf("MINIO_SECRET_KEY is required")
 	}
+	if c.App.Env == "production" || c.Email.Host != "" || c.Email.Username != "" || c.Email.Password != "" || c.Email.From != "" || c.Email.OTPSecret != "" {
+		if err := c.Email.Validate(); err != nil {
+			return err
+		}
+	}
 	return nil
 }
 

# internal/enums/enums.go

diff --git a/internal/enums/enums.go b/internal/enums/enums.go
index 9bbe6a7..776da07 100644
--- a/internal/enums/enums.go
+++ b/internal/enums/enums.go
@@ -1,17 +1,8 @@
 package enums
 
-type OrganizationVerificationStatus string
 type InternshipStatus string
 
 const (
-	OrganizationVerificationPending  OrganizationVerificationStatus = "pending"
-	OrganizationVerificationApproved OrganizationVerificationStatus = "approved"
-	OrganizationVerificationRejected OrganizationVerificationStatus = "rejected"
-
-	//for reviewed verifications, which are either approved or rejected
-	OrganizationVerificationReviewed OrganizationVerificationStatus = "reviewed"
-
-	// Internships
 	InternshipStatusPrivate   InternshipStatus = "private"
 	InternshipStatusPublished InternshipStatus = "published"
 	InternshipStatusClosed    InternshipStatus = "closed"
@@ -29,14 +20,3 @@ func (s InternshipStatus) IsValid() bool {
 		return false
 	}
 }
-
-func (s InternshipStatus) IsActive() bool {
-	return s == InternshipStatusPublished
-}
-
-type OrganizationVerificationMethod string
-
-const (
-	VerificationMethodDocument OrganizationVerificationMethod = "document"
-	VerificationMethodManual   OrganizationVerificationMethod = "manual"
-)

# internal/handlers/admin_handler.go

diff --git a/internal/handlers/admin_handler.go b/internal/handlers/admin_handler.go
deleted file mode 100644
index eff8896..0000000
--- a/internal/handlers/admin_handler.go
+++ /dev/null
@@ -1,31 +0,0 @@
-package handlers
-
-import (
-	"net/http"
-
-	"github.com/gin-gonic/gin"
-	"github.com/prasanna/student-job-portal/backend/internal/logger"
-	"github.com/prasanna/student-job-portal/backend/internal/repositories"
-	"github.com/prasanna/student-job-portal/backend/internal/responses"
-)
-
-type AdminHandler struct {
-	UserRepo *repositories.UserRepository
-	Logger   *logger.Logger
-}
-
-func NewAdminHandler(repo *repositories.UserRepository, l *logger.Logger) *AdminHandler {
-	return &AdminHandler{UserRepo: repo, Logger: l}
-}
-
-// ListUsers returns a minimal list of users (id, email, full_name, role)
-func (h *AdminHandler) ListUsers(c *gin.Context) {
-	page, pageSize := parsePagination(c)
-
-	users, total, err := h.UserRepo.List(page, pageSize)
-	if err != nil {
-		responses.Error(c, http.StatusInternalServerError, "Failed to query users")
-		return
-	}
-	responses.SuccessWithPagination(c, http.StatusOK, "users list", users, responses.CalculatePagination(int64(page), int64(pageSize), total))
-}

# internal/handlers/application_handler.go

diff --git a/internal/handlers/application_handler.go b/internal/handlers/application_handler.go
index b7a41f1..cabb33c 100644
--- a/internal/handlers/application_handler.go
+++ b/internal/handlers/application_handler.go
@@ -48,10 +48,9 @@ func (h *ApplicationHandler) Apply(c *gin.Context) {
 
 	app := &models.InternshipApplication{
 		InternshipID: internshipID,
-		StudentID:    uid,
 	}
 
-	if err := h.Svc.CreateApplication(c.Request.Context(), app); err != nil {
+	if err := h.Svc.CreateApplication(c.Request.Context(), uid, app); err != nil {
 		h.handleApplicationError(c, err)
 		return
 	}
@@ -69,7 +68,7 @@ func (h *ApplicationHandler) ListOwn(c *gin.Context) {
 
 	page, pageSize := parsePaginationParams(c)
 	filter := services.StudentApplicationFilter{
-		Status:   services.StudentApplicationStatusFilter(strings.TrimSpace(c.Query("status"))),
+		Status:   models.InternshipApplicationStatus(strings.TrimSpace(c.Query("status"))),
 		Page:     page,
 		PageSize: pageSize,
 	}

# internal/handlers/auth_handler.go

diff --git a/internal/handlers/auth_handler.go b/internal/handlers/auth_handler.go
index c0bbe9b..0d60a3c 100644
--- a/internal/handlers/auth_handler.go
+++ b/internal/handlers/auth_handler.go
@@ -1,14 +1,18 @@
 package handlers
 
 import (
+	"errors"
 	"net/http"
+	"strings"
 	"time"
 
 	"github.com/gin-gonic/gin"
 	"github.com/google/uuid"
+	"gorm.io/gorm"
 
 	"github.com/prasanna/student-job-portal/backend/internal/logger"
 	"github.com/prasanna/student-job-portal/backend/internal/models"
+	"github.com/prasanna/student-job-portal/backend/internal/repositories"
 	"github.com/prasanna/student-job-portal/backend/internal/responses"
 	"github.com/prasanna/student-job-portal/backend/internal/services"
 )
@@ -24,9 +28,9 @@ func NewAuthHandler(svc *services.AuthService, l *logger.Logger) *AuthHandler {
 
 // DTOs
 type registerRequest struct {
-	Email    string `json:"email" binding:"required,email"`
-	Password string `json:"password" binding:"required,min=8"`
-	FullName string `json:"full_name" binding:"required"`
+	Email    string `json:"email" binding:"required,email,max=254"`
+	Password string `json:"password" binding:"required,min=8,max=72"`
+	FullName string `json:"full_name" binding:"required,max=255"`
 }
 
 type loginRequest struct {
@@ -42,61 +46,100 @@ type tokenResponse struct {
 
 // RegisterStudent handles student registration
 func (h *AuthHandler) RegisterStudent(c *gin.Context) {
+	h.register(c, models.RoleStudent)
+}
+
+// RegisterEmployer handles employer registration
+func (h *AuthHandler) RegisterEmployer(c *gin.Context) {
+	h.register(c, models.RoleEmployer)
+}
+
+func (h *AuthHandler) register(c *gin.Context, role models.Role) {
 	var req registerRequest
 	if err := c.ShouldBindJSON(&req); err != nil {
-		responses.ErrorWithDetails(c, http.StatusBadRequest, "Invalid input", gin.H{"error": err.Error()})
+		responses.Error(c, http.StatusBadRequest, "Enter a valid email, full name and password of 8 to 72 characters")
 		return
 	}
-
-	// Hash password
-	hash, err := h.AuthSvc.HashPassword(req.Password)
+	if strings.TrimSpace(req.FullName) == "" || len(req.Password) > 72 {
+		responses.Error(c, http.StatusBadRequest, "Full name is required and password must not exceed 72 bytes")
+		return
+	}
+	user, sent, err := h.AuthSvc.Register(c.Request.Context(), req.Email, req.Password, req.FullName, role)
 	if err != nil {
-		responses.Error(c, http.StatusInternalServerError, "Failed to process password")
+		h.verificationError(c, err)
 		return
 	}
-
-	user := &models.User{
-		Email:        req.Email,
-		PasswordHash: hash,
-		FullName:     req.FullName,
-		Role:         models.RoleStudent,
+	message := "Account created. Check your email for a confirmation code."
+	if !sent {
+		message = "Account created, but we could not send your code. Try resending in 60 seconds."
 	}
+	responses.Success(c, http.StatusCreated, message, gin.H{
+		"user_id": user.ID, "email": user.Email, "verification_required": true,
+		"email_sent": sent, "resend_after_seconds": 60,
+	})
+}
 
-	if err := h.AuthSvc.Repo.Create(user); err != nil {
-		responses.Error(c, http.StatusInternalServerError, "Failed to create user")
+func (h *AuthHandler) VerifyEmail(c *gin.Context) {
+	var req struct {
+		Email string `json:"email" binding:"required,email,max=254"`
+		Code  string `json:"code" binding:"required,len=6"`
+	}
+	if err := c.ShouldBindJSON(&req); err != nil {
+		responses.Error(c, http.StatusBadRequest, "Enter your email and six-digit confirmation code")
 		return
 	}
-
-	responses.Success(c, http.StatusCreated, "Student registered successfully", gin.H{"user_id": user.ID})
+	if err := h.AuthSvc.VerifyEmail(req.Email, req.Code); err != nil {
+		h.verificationError(c, err)
+		return
+	}
+	responses.Success(c, http.StatusOK, "Email confirmed. You can now sign in.", nil)
 }
 
-// RegisterEmployer handles employer registration
-func (h *AuthHandler) RegisterEmployer(c *gin.Context) {
-	var req registerRequest
+func (h *AuthHandler) ResendVerification(c *gin.Context) {
+	var req struct {
+		Email string `json:"email" binding:"required,email,max=254"`
+	}
 	if err := c.ShouldBindJSON(&req); err != nil {
-		responses.ErrorWithDetails(c, http.StatusBadRequest, "Invalid input", gin.H{"error": err.Error()})
+		responses.Error(c, http.StatusBadRequest, "Enter a valid email address")
 		return
 	}
-
-	hash, err := h.AuthSvc.HashPassword(req.Password)
-	if err != nil {
-		responses.Error(c, http.StatusInternalServerError, "Failed to process password")
+	user, err := h.AuthSvc.Repo.GetByEmail(req.Email)
+	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
+		h.verificationError(c, err)
 		return
 	}
-
-	user := &models.User{
-		Email:        req.Email,
-		PasswordHash: hash,
-		FullName:     req.FullName,
-		Role:         models.RoleEmployer,
+	if err == nil && !user.CanSignIn() {
+		if err := h.AuthSvc.SendVerification(c.Request.Context(), user); err != nil {
+			// Keep account existence and delivery/limit state private on this public endpoint.
+			if !errors.Is(err, repositories.ErrVerificationCooldown) && !errors.Is(err, repositories.ErrVerificationSendLimit) && !errors.Is(err, repositories.ErrVerificationAttemptLimit) && !errors.Is(err, repositories.ErrAlreadyVerified) && !errors.Is(err, services.ErrEmailUnavailable) {
+				h.verificationError(c, err)
+				return
+			}
+		}
 	}
+	responses.Success(c, http.StatusAccepted, "If this account needs confirmation and its sending limits allow it, we will send a new code. Check your inbox and spam folder. If you have requested five codes, wait an hour before trying again.", gin.H{"resend_after_seconds": 60})
+}
 
-	if err := h.AuthSvc.Repo.Create(user); err != nil {
-		responses.Error(c, http.StatusInternalServerError, "Failed to create user")
-		return
+func (h *AuthHandler) verificationError(c *gin.Context, err error) {
+	switch {
+	case errors.Is(err, services.ErrEmailNotVerified):
+		c.JSON(http.StatusForbidden, gin.H{"success": false, "code": "EMAIL_NOT_VERIFIED", "message": err.Error()})
+	case errors.Is(err, services.ErrEmailExists):
+		responses.Error(c, http.StatusConflict, err.Error())
+	case errors.Is(err, repositories.ErrInvalidVerificationCode):
+		responses.Error(c, http.StatusBadRequest, err.Error())
+	case errors.Is(err, repositories.ErrVerificationCooldown):
+		c.Header("Retry-After", "60")
+		responses.Error(c, http.StatusTooManyRequests, err.Error())
+	case errors.Is(err, repositories.ErrVerificationSendLimit), errors.Is(err, repositories.ErrVerificationAttemptLimit):
+		c.Header("Retry-After", "3600")
+		responses.Error(c, http.StatusTooManyRequests, err.Error())
+	case errors.Is(err, services.ErrEmailUnavailable):
+		responses.Error(c, http.StatusServiceUnavailable, "We could not send a confirmation code. Please try again later.")
+	default:
+		h.Logger.Error("email verification operation failed")
+		responses.Error(c, http.StatusInternalServerError, "Unable to process email confirmation. Please try again.")
 	}
-
-	responses.Success(c, http.StatusCreated, "Employer registered successfully", gin.H{"user_id": user.ID})
 }
 
 // Login authenticates user and returns tokens
@@ -114,10 +157,19 @@ func (h *AuthHandler) Login(c *gin.Context) {
 		return
 	}
 
+	if user.Role != models.RoleStudent && user.Role != models.RoleEmployer {
+		responses.Error(c, http.StatusForbidden, "account role is no longer supported")
+		return
+	}
+
 	if err := h.AuthSvc.ComparePassword(user.PasswordHash, req.Password); err != nil {
 		responses.Error(c, http.StatusUnauthorized, "Invalid credentials")
 		return
 	}
+	if !user.CanSignIn() {
+		h.verificationError(c, services.ErrEmailNotVerified)
+		return
+	}
 
 	access, err := h.AuthSvc.GenerateAccessToken(user)
 	if err != nil {
@@ -188,6 +240,14 @@ func (h *AuthHandler) Refresh(c *gin.Context) {
 		responses.Error(c, http.StatusInternalServerError, "User not found")
 		return
 	}
+	if user.Role != models.RoleStudent && user.Role != models.RoleEmployer {
+		responses.Error(c, http.StatusForbidden, "account role is no longer supported")
+		return
+	}
+	if !user.CanSignIn() {
+		h.verificationError(c, services.ErrEmailNotVerified)
+		return
+	}
 
 	// Revoke used refresh token
 	if err := h.AuthSvc.RevokeRefreshToken(rt.ID); err != nil {
@@ -274,6 +334,10 @@ func (h *AuthHandler) Me(c *gin.Context) {
 	}
 
 	// Hide sensitive fields
+	if !user.CanSignIn() {
+		h.verificationError(c, services.ErrEmailNotVerified)
+		return
+	}
 	user.PasswordHash = ""
 	responses.Success(c, http.StatusOK, "Current user fetched", user)
 }

# internal/handlers/internship_handler.go

diff --git a/internal/handlers/internship_handler.go b/internal/handlers/internship_handler.go
index 355d5c0..59849d0 100644
--- a/internal/handlers/internship_handler.go
+++ b/internal/handlers/internship_handler.go
@@ -37,7 +37,7 @@ func (h *InternshipHandler) GetInternship(c *gin.Context) {
 		responses.Error(c, http.StatusBadRequest, "invalid internship ID format")
 		return
 	}
-	internship, err := h.service.GetInternship(c.Request.Context(), id)
+	internship, err := h.service.GetInternshipDetail(c.Request.Context(), id)
 	if errors.Is(err, services.ErrInternshipNotFound) {
 		responses.Error(c, http.StatusNotFound, "internship not found")
 		return
@@ -170,11 +170,6 @@ func parseInternshipSearchFilter(c *gin.Context) repositories.InternshipSearchFi
 	if id, err := uuid.Parse(c.Query("employer_id")); err == nil {
 		f.EmployerID = &id
 	}
-	if value := c.Query("is_active"); value != "" {
-		if b, err := strconv.ParseBool(value); err == nil {
-			f.IsActive = &b
-		}
-	}
 	if value := c.Query("min_stipend"); value != "" {
 		if amount, err := strconv.ParseFloat(value, 64); err == nil {
 			f.MinStipend = &amount

# internal/handlers/interview_handler.go

diff --git a/internal/handlers/interview_handler.go b/internal/handlers/interview_handler.go
deleted file mode 100644
index febd531..0000000
--- a/internal/handlers/interview_handler.go
+++ /dev/null
@@ -1,192 +0,0 @@
-package handlers
-
-import (
-	"net/http"
-	"time"
-
-	"github.com/gin-gonic/gin"
-	"github.com/google/uuid"
-
-	"github.com/prasanna/student-job-portal/backend/internal/models"
-	"github.com/prasanna/student-job-portal/backend/internal/repositories"
-	"github.com/prasanna/student-job-portal/backend/internal/responses"
-	"github.com/prasanna/student-job-portal/backend/internal/services"
-)
-
-type InterviewHandler struct {
-	Svc            *services.InterviewService
-	Repo           *repositories.InterviewRepository
-	InternshipRepo *repositories.InternshipRepository
-}
-
-func NewInterviewHandler(svc *services.InterviewService, repo *repositories.InterviewRepository, internshipRepo *repositories.InternshipRepository) *InterviewHandler {
-	return &InterviewHandler{Svc: svc, Repo: repo, InternshipRepo: internshipRepo}
-}
-
-// Employer: schedule an interview
-func (h *InterviewHandler) Schedule(c *gin.Context) {
-	ctx := c.Request.Context()
-	uid, ok := c.Get("user_id")
-	if !ok {
-		responses.Error(c, http.StatusUnauthorized, "missing user in context")
-		return
-	}
-	employerStr, _ := uid.(string)
-	employerID, err := uuid.Parse(employerStr)
-	if err != nil {
-		responses.Error(c, http.StatusUnauthorized, "invalid user id")
-		return
-	}
-	var payload models.Interview
-	if err := c.ShouldBindJSON(&payload); err != nil {
-		responses.Error(c, http.StatusBadRequest, "invalid payload")
-		return
-	}
-	// ensure employer owns the job
-	job, err := h.InternshipRepo.GetByID(ctx, payload.JobID)
-	if err != nil {
-		responses.Error(c, http.StatusBadRequest, "job not found")
-		return
-	}
-	if job.IssuedBy != employerID {
-		responses.Error(c, http.StatusForbidden, "not allowed")
-		return
-	}
-	payload.EmployerID = employerID
-	if payload.ScheduledAt.IsZero() {
-		payload.ScheduledAt = time.Now().Add(24 * time.Hour)
-	}
-	if err := h.Svc.ScheduleInterview(&payload); err != nil {
-		responses.Error(c, http.StatusInternalServerError, "failed to schedule")
-		return
-	}
-	responses.Success(c, http.StatusCreated, "scheduled", payload)
-}
-
-// Employer: list interviews for employer
-func (h *InterviewHandler) ListEmployer(c *gin.Context) {
-	uid, ok := c.Get("user_id")
-	if !ok {
-		responses.Error(c, http.StatusUnauthorized, "missing user in context")
-		return
-	}
-	employerStr, _ := uid.(string)
-	employerID, err := uuid.Parse(employerStr)
-	if err != nil {
-		responses.Error(c, http.StatusUnauthorized, "invalid user id")
-		return
-	}
-	out, err := h.Svc.ListByEmployer(employerID)
-	if err != nil {
-		responses.Error(c, http.StatusInternalServerError, "failed to list")
-		return
-	}
-	responses.Success(c, http.StatusOK, "interviews", out)
-}
-
-// Student: list own interviews
-func (h *InterviewHandler) ListStudent(c *gin.Context) {
-	uid, ok := c.Get("user_id")
-	if !ok {
-		responses.Error(c, http.StatusUnauthorized, "missing user in context")
-		return
-	}
-	studentStr, _ := uid.(string)
-	studentID, err := uuid.Parse(studentStr)
-	if err != nil {
-		responses.Error(c, http.StatusUnauthorized, "invalid user id")
-		return
-	}
-	out, err := h.Svc.ListByStudent(studentID)
-	if err != nil {
-		responses.Error(c, http.StatusInternalServerError, "failed to list")
-		return
-	}
-	responses.Success(c, http.StatusOK, "interviews", out)
-}
-
-// Student: respond to interview (accept/decline)
-func (h *InterviewHandler) Respond(c *gin.Context) {
-	idStr := c.Param("id")
-	id, err := uuid.Parse(idStr)
-	if err != nil {
-		responses.Error(c, http.StatusBadRequest, "invalid id")
-		return
-	}
-	var payload struct {
-		Action string `json:"action"`
-	}
-	if err := c.ShouldBindJSON(&payload); err != nil {
-		responses.Error(c, http.StatusBadRequest, "invalid payload")
-		return
-	}
-	uid, ok := c.Get("user_id")
-	if !ok {
-		responses.Error(c, http.StatusUnauthorized, "missing user in context")
-		return
-	}
-	studentStr, _ := uid.(string)
-	studentID, err := uuid.Parse(studentStr)
-	if err != nil {
-		responses.Error(c, http.StatusUnauthorized, "invalid user id")
-		return
-	}
-	i, err := h.Svc.GetInterview(id)
-	if err != nil {
-		responses.Error(c, http.StatusNotFound, "interview not found")
-		return
-	}
-	if i.StudentID != studentID {
-		responses.Error(c, http.StatusForbidden, "not allowed")
-		return
-	}
-	switch payload.Action {
-	case "accept":
-		i.Status = "accepted"
-	case "decline":
-		i.Status = "declined"
-	default:
-		responses.Error(c, http.StatusBadRequest, "invalid action")
-		return
-	}
-	if _, err := h.Svc.UpdateStatus(id, i.Status); err != nil {
-		responses.Error(c, http.StatusInternalServerError, "failed to update")
-		return
-	}
-	responses.Success(c, http.StatusOK, "updated", i)
-}
-
-// Employer: cancel interview
-func (h *InterviewHandler) Cancel(c *gin.Context) {
-	idStr := c.Param("id")
-	id, err := uuid.Parse(idStr)
-	if err != nil {
-		responses.Error(c, http.StatusBadRequest, "invalid id")
-		return
-	}
-	uid, ok := c.Get("user_id")
-	if !ok {
-		responses.Error(c, http.StatusUnauthorized, "missing user in context")
-		return
-	}
-	employerStr, _ := uid.(string)
-	employerID, err := uuid.Parse(employerStr)
-	if err != nil {
-		responses.Error(c, http.StatusUnauthorized, "invalid user id")
-		return
-	}
-	i, err := h.Svc.GetInterview(id)
-	if err != nil {
-		responses.Error(c, http.StatusNotFound, "interview not found")
-		return
-	}
-	if i.EmployerID != employerID {
-		responses.Error(c, http.StatusForbidden, "not allowed")
-		return
-	}
-	if err := h.Svc.CancelInterview(id); err != nil {
-		responses.Error(c, http.StatusInternalServerError, "failed to cancel")
-		return
-	}
-	responses.Success(c, http.StatusOK, "cancelled", nil)
-}

# internal/handlers/organization_verification_handler.go

diff --git a/internal/handlers/organization_verification_handler.go b/internal/handlers/organization_verification_handler.go
deleted file mode 100644
index 4eb0e84..0000000
--- a/internal/handlers/organization_verification_handler.go
+++ /dev/null
@@ -1,166 +0,0 @@
-package handlers
-
-import (
-	"errors"
-	"net/http"
-
-	"github.com/gin-gonic/gin"
-	"github.com/google/uuid"
-
-	"github.com/prasanna/student-job-portal/backend/internal/enums"
-	"github.com/prasanna/student-job-portal/backend/internal/logger"
-	"github.com/prasanna/student-job-portal/backend/internal/models"
-	"github.com/prasanna/student-job-portal/backend/internal/repositories"
-	"github.com/prasanna/student-job-portal/backend/internal/responses"
-	"github.com/prasanna/student-job-portal/backend/internal/services"
-)
-
-type OrganizationVerificationHandler struct {
-	Svc *services.OrganizationVerificationService
-	Log *logger.Logger
-}
-
-func NewOrganizationVerificationHandler(svc *services.OrganizationVerificationService, l *logger.Logger) *OrganizationVerificationHandler {
-	return &OrganizationVerificationHandler{Svc: svc, Log: l}
-}
-
-type submitOrganizationVerificationInput struct {
-	DocumentType      string `json:"document_type"`
-	OrganizationEmail string `json:"organization_email"`
-}
-type reviewOrganizationVerificationInput struct {
-	Status          enums.OrganizationVerificationStatus `json:"status"`
-	RejectionReason string                               `json:"rejection_reason"`
-	ReviewNotes     string                               `json:"review_notes"`
-}
-
-func (h *OrganizationVerificationHandler) SubmitVerification(c *gin.Context) {
-	userID, err := getUserIDFromContext(c)
-	if err != nil {
-		responses.Error(c, http.StatusUnauthorized, err.Error())
-		return
-	}
-	var input submitOrganizationVerificationInput
-	if err := c.ShouldBindJSON(&input); err != nil {
-		responses.Error(c, http.StatusBadRequest, "invalid request payload")
-		return
-	}
-	verification, err := h.Svc.Submit(c.Request.Context(), userID, enums.VerificationMethodDocument, input.OrganizationEmail, input.DocumentType)
-	if err != nil {
-		h.respondError(c, err)
-		return
-	}
-	responses.Success(c, http.StatusOK, "organization verification submitted successfully", verification)
-}
-
-func (h *OrganizationVerificationHandler) GetMyVerification(c *gin.Context) {
-	userID, err := getUserIDFromContext(c)
-	if err != nil {
-		responses.Error(c, http.StatusUnauthorized, err.Error())
-		return
-	}
-	verification, err := h.Svc.GetMy(c.Request.Context(), userID)
-	if err != nil {
-		h.respondError(c, err)
-		return
-	}
-	h.respondVerification(c, http.StatusOK, "organization verification retrieved successfully", verification)
-}
-
-func (h *OrganizationVerificationHandler) UploadDocument(c *gin.Context) {
-	userID, err := getUserIDFromContext(c)
-	if err != nil {
-		responses.Error(c, http.StatusUnauthorized, err.Error())
-		return
-	}
-	file, err := c.FormFile("document")
-	if err != nil {
-		responses.Error(c, http.StatusBadRequest, "verification document is required")
-		return
-	}
-	verification, err := h.Svc.UploadDocument(c.Request.Context(), userID, file)
-	if err != nil {
-		h.respondError(c, err)
-		return
-	}
-	h.respondVerification(c, http.StatusOK, "verification document uploaded successfully", verification)
-}
-
-func (h *OrganizationVerificationHandler) List(c *gin.Context) {
-	filters := parseVerificationFilter(c)
-	items, total, err := h.Svc.List(c.Request.Context(), filters)
-	if err != nil {
-		h.respondError(c, err)
-		return
-	}
-	responses.SuccessWithPagination(c, http.StatusOK, "organization verifications retrieved successfully", items, responses.CalculatePagination(int64(filters.Page), int64(filters.PageSize), total))
-}
-
-func (h *OrganizationVerificationHandler) GetByID(c *gin.Context) {
-	id, err := uuid.Parse(c.Param("id"))
-	if err != nil {
-		responses.Error(c, http.StatusBadRequest, "invalid verification ID")
-		return
-	}
-	verification, err := h.Svc.GetByID(c.Request.Context(), id)
-	if err != nil {
-		h.respondError(c, err)
-		return
-	}
-	h.respondVerification(c, http.StatusOK, "organization verification retrieved successfully", verification)
-}
-
-func (h *OrganizationVerificationHandler) Review(c *gin.Context) {
-	reviewerID, err := getUserIDFromContext(c)
-	if err != nil {
-		responses.Error(c, http.StatusUnauthorized, err.Error())
-		return
-	}
-	id, err := uuid.Parse(c.Param("id"))
-	if err != nil {
-		responses.Error(c, http.StatusBadRequest, "invalid verification ID")
-		return
-	}
-	var input reviewOrganizationVerificationInput
-	if err := c.ShouldBindJSON(&input); err != nil {
-		responses.Error(c, http.StatusBadRequest, "invalid request payload")
-		return
-	}
-	if err := h.Svc.Review(c.Request.Context(), id, reviewerID, input.Status, input.RejectionReason, input.ReviewNotes); err != nil {
-		h.respondError(c, err)
-		return
-	}
-	responses.Success(c, http.StatusOK, "organization verification reviewed successfully", nil)
-}
-
-func (h *OrganizationVerificationHandler) respondVerification(c *gin.Context, status int, message string, verification *models.OrganizationVerification) {
-	documentURL, err := h.Svc.GetDocumentURL(c.Request.Context(), verification.DocumentObjectKey)
-	if err != nil {
-		h.Log.Error("failed to create verification document URL: %v", err)
-		responses.Error(c, http.StatusInternalServerError, "failed to retrieve verification document")
-		return
-	}
-	responses.Success(c, status, message, gin.H{"verification": verification, "document_url": documentURL})
-}
-
-func (h *OrganizationVerificationHandler) respondError(c *gin.Context, err error) {
-	switch {
-	case errors.Is(err, services.ErrOrganizationVerificationNotFound):
-		responses.Error(c, http.StatusNotFound, err.Error())
-	case errors.Is(err, services.ErrRecruiterProfileNotFound):
-		responses.Error(c, http.StatusNotFound, "recruiter profile not found")
-	case errors.Is(err, services.ErrOrganizationEmailAlreadyRegistered):
-		responses.Error(c, http.StatusConflict, err.Error())
-	case errors.Is(err, services.ErrInvalidOrganizationVerification), errors.Is(err, services.ErrInvalidVerificationState):
-		responses.Error(c, http.StatusBadRequest, err.Error())
-	default:
-		h.Log.Error("organization verification request failed: %v", err)
-		responses.Error(c, http.StatusInternalServerError, "organization verification request failed")
-	}
-}
-
-func parseVerificationFilter(c *gin.Context) repositories.VerificationSearchFilter {
-	page, pageSize := parsePagination(c)
-	f := repositories.VerificationSearchFilter{Status: enums.OrganizationVerificationStatus(c.Query("status")), Page: page, PageSize: pageSize}
-	return f
-}

# internal/handlers/recruiter_profile_handler.go

diff --git a/internal/handlers/recruiter_profile_handler.go b/internal/handlers/recruiter_profile_handler.go
index c9692cd..0d14133 100644
--- a/internal/handlers/recruiter_profile_handler.go
+++ b/internal/handlers/recruiter_profile_handler.go
@@ -190,9 +190,10 @@ func (h *RecruiterProfileHandler) UploadOrganizationLogo(c *gin.Context) {
 		return
 	}
 
+	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, (5<<20)+(1<<20))
 	file, err := c.FormFile("logo")
 	if err != nil {
-		responses.Error(c, http.StatusBadRequest, "organization logo is required")
+		responses.Error(c, http.StatusBadRequest, services.ErrInvalidProfilePicture.Error())
 		return
 	}
 
@@ -229,7 +230,7 @@ func (h *RecruiterProfileHandler) UploadOrganizationLogo(c *gin.Context) {
 // It returns true when it has written a response.
 func writeRecruiterProfileError(c *gin.Context, err error, fallback string) bool {
 	switch {
-	case errors.Is(err, services.ErrInvalidRecruiterProfileData):
+	case errors.Is(err, services.ErrInvalidRecruiterProfileData), errors.Is(err, services.ErrInvalidProfilePicture):
 		responses.Error(c, http.StatusBadRequest, err.Error())
 	case errors.Is(err, services.ErrRecruiterProfileNotFound):
 		responses.Error(c, http.StatusNotFound, "recruiter profile not found")

# internal/models/internship.go

diff --git a/internal/models/internship.go b/internal/models/internship.go
index 02ad151..8c40644 100644
--- a/internal/models/internship.go
+++ b/internal/models/internship.go
@@ -70,8 +70,6 @@ type Internship struct {
 	// Status
 	Status enums.InternshipStatus `gorm:"size:30;not null;index" json:"status"`
 
-	IsActive bool `json:"is_active"`
-
 	CreatedAt time.Time `json:"created_at"`
 	UpdatedAt time.Time `json:"updated_at"`
 }

# internal/models/interview.go

diff --git a/internal/models/interview.go b/internal/models/interview.go
deleted file mode 100644
index c69af01..0000000
--- a/internal/models/interview.go
+++ /dev/null
@@ -1,31 +0,0 @@
-package models
-
-import (
-	"time"
-
-	"github.com/google/uuid"
-	"gorm.io/gorm"
-)
-
-// Interview represents a scheduled interview between employer and student for an application or job
-type Interview struct {
-	ID            uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
-	JobID         uuid.UUID `gorm:"type:uuid;index;not null" json:"job_id"`
-	ApplicationID uuid.UUID `gorm:"type:uuid;index" json:"application_id"`
-	EmployerID    uuid.UUID `gorm:"type:uuid;index;not null" json:"employer_id"`
-	StudentID     uuid.UUID `gorm:"type:uuid;index;not null" json:"student_id"`
-	ScheduledAt   time.Time `json:"scheduled_at"`
-	DurationMins  int       `json:"duration_mins"`
-	Location      string    `gorm:"size:255" json:"location"`
-	Remote        bool      `json:"remote"`
-	Status        string    `gorm:"size:64;default:'scheduled'" json:"status"` // scheduled, accepted, declined, cancelled, completed
-	CreatedAt     time.Time `json:"created_at"`
-	UpdatedAt     time.Time `json:"updated_at"`
-}
-
-func (i *Interview) BeforeCreate(tx *gorm.DB) (err error) {
-	if i.ID == uuid.Nil {
-		i.ID = uuid.New()
-	}
-	return nil
-}

# internal/models/organization_verification.go

diff --git a/internal/models/organization_verification.go b/internal/models/organization_verification.go
deleted file mode 100644
index 2b0c2b5..0000000
--- a/internal/models/organization_verification.go
+++ /dev/null
@@ -1,43 +0,0 @@
-package models
-
-import (
-	"time"
-
-	"github.com/google/uuid"
-	"github.com/prasanna/student-job-portal/backend/internal/enums"
-)
-
-type OrganizationVerification struct {
-	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
-
-	RecruiterProfileID uuid.UUID `gorm:"type:uuid;uniqueIndex;not null" json:"recruiter_profile_id"`
-
-	Status enums.OrganizationVerificationStatus `gorm:"size:30;not null;default:'pending'" json:"status"`
-
-	Method enums.OrganizationVerificationMethod `gorm:"size:30" json:"method,omitempty"`
-
-	// Domain/email verification
-	OrganizationEmail string `gorm:"size:255" json:"organization_email,omitempty"`
-	EmailDomain       string `gorm:"size:255" json:"email_domain,omitempty"`
-
-	// Document verification
-	DocumentType string `gorm:"size:100" json:"document_type,omitempty"`
-
-	// Store MinIO object key only, NOT presigned URL
-	DocumentObjectKey string `gorm:"type:text" json:"-"`
-
-	// Reviewer information
-	ReviewedBy *uuid.UUID `gorm:"type:uuid" json:"reviewed_by,omitempty"`
-
-	RejectionReason string `gorm:"type:text" json:"rejection_reason,omitempty"`
-	ReviewNotes     string `gorm:"type:text" json:"review_notes,omitempty"`
-
-	SubmittedAt *time.Time `json:"submitted_at,omitempty"`
-	ReviewedAt  *time.Time `json:"reviewed_at,omitempty"`
-	VerifiedAt  *time.Time `json:"verified_at,omitempty"`
-
-	RecruiterProfile *RecruiterProfile `gorm:"foreignKey:RecruiterProfileID" json:"recruiter_profile,omitempty"`
-
-	CreatedAt time.Time `json:"created_at"`
-	UpdatedAt time.Time `json:"updated_at"`
-}

# internal/models/recruiter_profile.go

diff --git a/internal/models/recruiter_profile.go b/internal/models/recruiter_profile.go
index 7f97f78..48706a7 100644
--- a/internal/models/recruiter_profile.go
+++ b/internal/models/recruiter_profile.go
@@ -20,10 +20,8 @@ type RecruiterProfile struct {
 	Industry            string `gorm:"size:150" json:"industry,omitempty"`
 	OrganizationSize    string `gorm:"size:50" json:"organization_size,omitempty"`
 	OrganizationAbout   string `gorm:"type:text" json:"organization_about,omitempty"`
-	VerificationStatus  string `gorm:"default:draft" json:"verification_status"`
 
-	User         *User                     `gorm:"foreignKey:UserID" json:"user,omitempty"`
-	Verification *OrganizationVerification `gorm:"foreignKey:RecruiterProfileID" json:"verification,omitempty"`
+	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
 
 	CreatedAt time.Time `json:"created_at"`
 	UpdatedAt time.Time `json:"updated_at"`

# internal/models/student_certification.go

diff --git a/internal/models/student_certification.go b/internal/models/student_certification.go
deleted file mode 100644
index 18ab069..0000000
--- a/internal/models/student_certification.go
+++ /dev/null
@@ -1,29 +0,0 @@
-package models
-
-import (
-	"time"
-
-	"github.com/google/uuid"
-	"gorm.io/gorm"
-)
-
-type StudentCertification struct {
-	ID         uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
-	ProfileID  uuid.UUID      `gorm:"type:uuid;index;not null" json:"profile_id"`
-	Name       string         `gorm:"size:255" json:"name"`
-	Authority  string         `gorm:"size:255" json:"authority"`
-	License    string         `gorm:"size:255" json:"license"`
-	URL        string         `gorm:"size:512" json:"url"`
-	IssuedDate *time.Time     `json:"issued_date"`
-	ExpiryDate *time.Time     `json:"expiry_date"`
-	CreatedAt  time.Time      `json:"created_at"`
-	UpdatedAt  time.Time      `json:"updated_at"`
-	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
-}
-
-func (s *StudentCertification) BeforeCreate(tx *gorm.DB) (err error) {
-	if s.ID == uuid.Nil {
-		s.ID = uuid.New()
-	}
-	return nil
-}

# internal/models/student_document.go

diff --git a/internal/models/student_document.go b/internal/models/student_document.go
index 7ad8f9d..7709469 100644
--- a/internal/models/student_document.go
+++ b/internal/models/student_document.go
@@ -7,26 +7,16 @@ import (
 	"gorm.io/gorm"
 )
 
-// StudentDocument represents a document belonging to a student's profile.
-// Fields:
-//   - ID: UUID primary key
-//   - ProfileID: related profile's UUID
-//   - UserID: owner user's UUID
-//   - ObjectKey: object storage key (e.g., S3 key)
-//   - FileName: original file name
-//   - MimeType: MIME type of the file
-//   - Size: file size in bytes
-//   - IsDefault: whether this is the default document for the profile
-//   - CreatedAt, UpdatedAt, DeletedAt: timestamps managed by GORM
+// StudentDocument stores the student CV.
 type StudentDocument struct {
+	URL       string         `gorm:"-" json:"url,omitempty"`
 	ID        uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
 	ProfileID uuid.UUID      `gorm:"type:uuid;not null;index" json:"profile_id"`
 	UserID    uuid.UUID      `gorm:"type:uuid;not null;index" json:"user_id"`
-	ObjectKey string         `gorm:"type:text;not null" json:"object_key"`
+	ObjectKey string         `gorm:"type:text;not null" json:"-"`
 	FileName  string         `gorm:"type:text;not null" json:"file_name"`
 	MimeType  string         `gorm:"type:text" json:"mime_type"`
 	Size      int64          `json:"size"`
-	IsDefault bool           `gorm:"default:false" json:"is_default"`
 	CreatedAt time.Time      `json:"created_at"`
 	UpdatedAt time.Time      `json:"updated_at"`
 	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`

# internal/models/student_education.go

diff --git a/internal/models/student_education.go b/internal/models/student_education.go
deleted file mode 100644
index 77e74f2..0000000
--- a/internal/models/student_education.go
+++ /dev/null
@@ -1,29 +0,0 @@
-package models
-
-import (
-	"time"
-
-	"github.com/google/uuid"
-	"gorm.io/gorm"
-)
-
-type StudentEducation struct {
-	ID          uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
-	ProfileID   uuid.UUID      `gorm:"type:uuid;index;not null" json:"profile_id"`
-	Institute   string         `gorm:"size:255" json:"institute"`
-	Degree      string         `gorm:"size:255" json:"degree"`
-	Field       string         `gorm:"size:255" json:"field_of_study"`
-	StartYear   int            `json:"start_year"`
-	EndYear     int            `json:"end_year"`
-	Description string         `gorm:"type:text" json:"description"`
-	CreatedAt   time.Time      `json:"created_at"`
-	UpdatedAt   time.Time      `json:"updated_at"`
-	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
-}
-
-func (s *StudentEducation) BeforeCreate(tx *gorm.DB) (err error) {
-	if s.ID == uuid.Nil {
-		s.ID = uuid.New()
-	}
-	return nil
-}

# internal/models/student_project.go

diff --git a/internal/models/student_project.go b/internal/models/student_project.go
deleted file mode 100644
index 5f2428e..0000000
--- a/internal/models/student_project.go
+++ /dev/null
@@ -1,30 +0,0 @@
-package models
-
-import (
-	"time"
-
-	"github.com/google/uuid"
-	"gorm.io/gorm"
-)
-
-// StudentProject represents a project on a student's profile.
-type StudentProject struct {
-	ID          uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
-	ProfileID   uuid.UUID      `gorm:"type:uuid;not null;index" json:"profile_id"`
-	Title       string         `gorm:"size:255;not null" json:"title"`
-	Description string         `gorm:"type:text" json:"description"`
-	Link        string         `gorm:"size:1024" json:"link"`
-	StartDate   *time.Time     `json:"start_date"`
-	EndDate     *time.Time     `json:"end_date"`
-	CreatedAt   time.Time      `json:"created_at"`
-	UpdatedAt   time.Time      `json:"updated_at"`
-	DeletedAt   gorm.DeletedAt `gorm:"index" json:"deleted_at"`
-}
-
-// BeforeCreate is a GORM hook that sets the UUID primary key before inserting.
-func (sp *StudentProject) BeforeCreate(tx *gorm.DB) (err error) {
-	if sp.ID == uuid.Nil {
-		sp.ID = uuid.New()
-	}
-	return nil
-}

# internal/models/student_skill.go

diff --git a/internal/models/student_skill.go b/internal/models/student_skill.go
deleted file mode 100644
index 4759a5e..0000000
--- a/internal/models/student_skill.go
+++ /dev/null
@@ -1,25 +0,0 @@
-package models
-
-import (
-	"time"
-
-	"github.com/google/uuid"
-	"gorm.io/gorm"
-)
-
-type StudentSkill struct {
-	ID        uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
-	ProfileID uuid.UUID      `gorm:"type:uuid;index;not null" json:"profile_id"`
-	Name      string         `gorm:"size:255;index" json:"name"`
-	Level     string         `gorm:"size:64" json:"level"`
-	CreatedAt time.Time      `json:"created_at"`
-	UpdatedAt time.Time      `json:"updated_at"`
-	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
-}
-
-func (s *StudentSkill) BeforeCreate(tx *gorm.DB) (err error) {
-	if s.ID == uuid.Nil {
-		s.ID = uuid.New()
-	}
-	return nil
-}

# internal/repositories/internship_application_repository_test.go

diff --git a/internal/repositories/internship_application_repository_test.go b/internal/repositories/internship_application_repository_test.go
index 884e21a..ee98343 100644
--- a/internal/repositories/internship_application_repository_test.go
+++ b/internal/repositories/internship_application_repository_test.go
@@ -28,7 +28,7 @@ func TestInternshipApplicationRepositoryListForEmployer(t *testing.T) {
 	baseTime := time.Now().Add(-time.Hour)
 	createRepositoryTestApplication(t, db, firstInternship.ID, alice.ID, models.ApplicationStatusSubmitted, baseTime)
 	newest := createRepositoryTestApplication(t, db, secondInternship.ID, bob.ID, models.ApplicationStatusRejected, baseTime.Add(30*time.Minute))
-	createRepositoryTestApplication(t, db, otherInternship.ID, alice.ID, models.ApplicationStatusReviewing, baseTime.Add(45*time.Minute))
+	createRepositoryTestApplication(t, db, otherInternship.ID, alice.ID, models.ApplicationStatusSubmitted, baseTime.Add(45*time.Minute))
 
 	repo := NewInternshipApplicationRepository(db)
 	ctx := context.Background()
@@ -128,7 +128,7 @@ func TestInternshipApplicationRepositoryListByStudent(t *testing.T) {
 	otherInternship := createRepositoryTestInternship(t, db, employerID, "Data Intern")
 
 	createRepositoryTestApplication(t, db, submittedInternship.ID, student.ID, models.ApplicationStatusSubmitted, baseTime)
-	newestPending := createRepositoryTestApplication(t, db, reviewingInternship.ID, student.ID, models.ApplicationStatusReviewing, baseTime.Add(time.Hour))
+	newestPending := createRepositoryTestApplication(t, db, reviewingInternship.ID, student.ID, models.ApplicationStatusSubmitted, baseTime.Add(time.Hour))
 	createRepositoryTestApplication(t, db, acceptedInternship.ID, student.ID, models.ApplicationStatusAccepted, baseTime.Add(30*time.Minute))
 	createRepositoryTestApplication(t, db, otherInternship.ID, otherStudent.ID, models.ApplicationStatusRejected, baseTime.Add(90*time.Minute))
 
@@ -137,8 +137,6 @@ func TestInternshipApplicationRepositoryListByStudent(t *testing.T) {
 		StudentID: student.ID,
 		Statuses: []models.InternshipApplicationStatus{
 			models.ApplicationStatusSubmitted,
-			models.ApplicationStatusReviewing,
-			models.ApplicationStatusShortlisted,
 		},
 		Page:     1,
 		PageSize: 1,
@@ -198,7 +196,6 @@ func createRepositoryTestInternship(t *testing.T, db *gorm.DB, employerID uuid.U
 		WorkMode:       "remote",
 		InternshipType: "paid",
 		Status:         enums.InternshipStatusPublished,
-		IsActive:       true,
 	}
 	if err := db.Omit("Issuer").Create(internship).Error; err != nil {
 		t.Fatalf("create internship: %v", err)

# internal/repositories/internship_repository.go

diff --git a/internal/repositories/internship_repository.go b/internal/repositories/internship_repository.go
index 4d378f4..f7ee79b 100644
--- a/internal/repositories/internship_repository.go
+++ b/internal/repositories/internship_repository.go
@@ -6,6 +6,7 @@ import (
 
 	"github.com/google/uuid"
 	"gorm.io/gorm"
+	"gorm.io/gorm/clause"
 
 	"github.com/prasanna/student-job-portal/backend/internal/enums"
 	"github.com/prasanna/student-job-portal/backend/internal/models"
@@ -27,7 +28,6 @@ type InternshipSearchFilter struct {
 	WorkMode       string // onsite, remote, hybrid
 	InternshipType string // paid, unpaid
 	Status         enums.InternshipStatus
-	IsActive       *bool
 	MinStipend     *float64
 	ExcludeExpired bool
 	Page           int
@@ -36,12 +36,12 @@ type InternshipSearchFilter struct {
 
 // Create inserts a new Internship record.
 func (r *InternshipRepository) Create(ctx context.Context, i *models.Internship) error {
-	return r.db.WithContext(ctx).Create(i).Error
+	return r.db.WithContext(ctx).Omit(clause.Associations).Create(i).Error
 }
 
 // Update saves all modified fields of an Internship record.
 func (r *InternshipRepository) Update(ctx context.Context, i *models.Internship) error {
-	return r.db.WithContext(ctx).Save(i).Error
+	return r.db.WithContext(ctx).Omit(clause.Associations).Save(i).Error
 }
 
 // GetByID fetches a single internship by ID.
@@ -54,6 +54,12 @@ func (r *InternshipRepository) GetByID(ctx context.Context, id uuid.UUID) (*mode
 	return &internship, nil
 }
 
+func (r *InternshipRepository) LoadPublicIssuer(ctx context.Context, internship *models.Internship) error {
+	return r.db.WithContext(ctx).Model(internship).
+		Select("id", "user_id", "organization_name", "organization_logo", "organization_about", "organization_website").
+		Association("Issuer").Find(&internship.Issuer)
+}
+
 // DeleteByID removes an internship record by ID.
 func (r *InternshipRepository) DeleteByID(ctx context.Context, id uuid.UUID) error {
 	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.Internship{}).Error
@@ -79,21 +85,17 @@ func (r *InternshipRepository) ListByEmployer(ctx context.Context, employerID uu
 	return internships, total, err
 }
 
-/*
-Search executes advanced dynamic filtering and pagination for internships.
-It ensures that only internships from approved recruiters are returned.
-*/
+// Search executes dynamic filtering and pagination for internships with a company profile.
 func (r *InternshipRepository) Search(ctx context.Context, filter InternshipSearchFilter) ([]models.Internship, int64, error) {
 	var internships []models.Internship
 	var total int64
 
 	query := r.db.WithContext(ctx).Model(&models.Internship{}).
-		Joins("JOIN recruiter_profiles ON recruiter_profiles.user_id = internships.issued_by").
-		Where("recruiter_profiles.verification_status = ?", enums.OrganizationVerificationApproved)
+		Joins("JOIN recruiter_profiles ON recruiter_profiles.user_id = internships.issued_by")
 
 	if filter.Query != "" {
 		like := "%" + filter.Query + "%"
-		query = query.Where("title ILIKE ? OR description ILIKE ? OR required_skills ILIKE ?", like, like, like)
+		query = query.Where("LOWER(title) LIKE LOWER(?) OR LOWER(description) LIKE LOWER(?) OR LOWER(required_skills) LIKE LOWER(?)", like, like, like)
 	}
 
 	if filter.EmployerID != nil {
@@ -101,7 +103,7 @@ func (r *InternshipRepository) Search(ctx context.Context, filter InternshipSear
 	}
 
 	if filter.Location != "" {
-		query = query.Where("location ILIKE ?", "%"+filter.Location+"%")
+		query = query.Where("LOWER(location) LIKE LOWER(?)", "%"+filter.Location+"%")
 	}
 
 	if filter.WorkMode != "" {
@@ -116,10 +118,6 @@ func (r *InternshipRepository) Search(ctx context.Context, filter InternshipSear
 		query = query.Where("status = ?", filter.Status)
 	}
 
-	if filter.IsActive != nil {
-		query = query.Where("is_active = ?", *filter.IsActive)
-	}
-
 	if filter.MinStipend != nil {
 		query = query.Where("stipend_amount >= ?", *filter.MinStipend)
 	}
@@ -134,7 +132,7 @@ func (r *InternshipRepository) Search(ctx context.Context, filter InternshipSear
 
 	off, limit := getPagination(filter.Page, filter.PageSize)
 
-	err := query.Preload("Issuer").Order("created_at DESC").
+	err := query.Preload("Issuer").Order("internships.created_at DESC").
 		Offset(off).
 		Limit(limit).
 		Find(&internships).Error

# internal/repositories/interview_repository.go

diff --git a/internal/repositories/interview_repository.go b/internal/repositories/interview_repository.go
deleted file mode 100644
index ba68a67..0000000
--- a/internal/repositories/interview_repository.go
+++ /dev/null
@@ -1,60 +0,0 @@
-package repositories
-
-import (
-	"github.com/google/uuid"
-	"gorm.io/gorm"
-
-	"github.com/prasanna/student-job-portal/backend/internal/models"
-)
-
-type InterviewRepository struct {
-	DB *gorm.DB
-}
-
-func NewInterviewRepository(db *gorm.DB) *InterviewRepository {
-	return &InterviewRepository{DB: db}
-}
-
-func (r *InterviewRepository) Create(i *models.Interview) error {
-	return r.DB.Create(i).Error
-}
-
-func (r *InterviewRepository) GetByID(id uuid.UUID) (*models.Interview, error) {
-	var out models.Interview
-	if err := r.DB.First(&out, "id = ?", id).Error; err != nil {
-		return nil, err
-	}
-	return &out, nil
-}
-
-func (r *InterviewRepository) ListByJob(jobID uuid.UUID) ([]models.Interview, error) {
-	var out []models.Interview
-	if err := r.DB.Where("job_id = ?", jobID).Order("scheduled_at desc").Find(&out).Error; err != nil {
-		return nil, err
-	}
-	return out, nil
-}
-
-func (r *InterviewRepository) ListByStudent(studentID uuid.UUID) ([]models.Interview, error) {
-	var out []models.Interview
-	if err := r.DB.Where("student_id = ?", studentID).Order("scheduled_at desc").Find(&out).Error; err != nil {
-		return nil, err
-	}
-	return out, nil
-}
-
-func (r *InterviewRepository) ListByEmployer(employerID uuid.UUID) ([]models.Interview, error) {
-	var out []models.Interview
-	if err := r.DB.Where("employer_id = ?", employerID).Order("scheduled_at desc").Find(&out).Error; err != nil {
-		return nil, err
-	}
-	return out, nil
-}
-
-func (r *InterviewRepository) Update(i *models.Interview) error {
-	return r.DB.Save(i).Error
-}
-
-func (r *InterviewRepository) DeleteByID(id uuid.UUID) error {
-	return r.DB.Where("id = ?", id).Delete(&models.Interview{}).Error
-}

# internal/repositories/organization_verification_repository.go

diff --git a/internal/repositories/organization_verification_repository.go b/internal/repositories/organization_verification_repository.go
deleted file mode 100644
index 2b88e13..0000000
--- a/internal/repositories/organization_verification_repository.go
+++ /dev/null
@@ -1,282 +0,0 @@
-package repositories
-
-import (
-	"context"
-
-	"github.com/google/uuid"
-	"github.com/prasanna/student-job-portal/backend/internal/enums"
-	"github.com/prasanna/student-job-portal/backend/internal/models"
-	"gorm.io/gorm"
-	"gorm.io/gorm/clause"
-)
-
-type OrganizationVerificationRepository struct {
-	DB *gorm.DB
-}
-
-func NewOrganizationVerificationRepository(
-	db *gorm.DB,
-) *OrganizationVerificationRepository {
-	return &OrganizationVerificationRepository{
-		DB: db,
-	}
-}
-
-type VerificationSearchFilter struct {
-	Status   enums.OrganizationVerificationStatus
-	Page     int
-	PageSize int
-}
-
-// recruiter submits/updates verification request
-func (r *OrganizationVerificationRepository) Upsert(
-	ctx context.Context,
-	verification *models.OrganizationVerification,
-) error {
-	return r.DB.
-		WithContext(ctx).
-		Clauses(clause.OnConflict{
-			Columns: []clause.Column{
-				{Name: "recruiter_profile_id"},
-			},
-			DoUpdates: clause.AssignmentColumns([]string{
-				"method",
-				"organization_email",
-				"email_domain",
-				"document_type",
-				"document_object_key",
-				"status",
-				"submitted_at",
-				"reviewed_by",
-				"rejection_reason",
-				"review_notes",
-				"reviewed_at",
-				"verified_at",
-				"updated_at",
-			}),
-		}).
-		Create(verification).
-		Error
-}
-
-// recruiter views own verification
-func (r *OrganizationVerificationRepository) GetByRecruiterProfileID(
-	ctx context.Context,
-	recruiterProfileID uuid.UUID,
-) (*models.OrganizationVerification, error) {
-
-	var verification models.OrganizationVerification
-
-	err := r.DB.Preload("RecruiterProfile").
-		WithContext(ctx).
-		Where(
-			"recruiter_profile_id = ?",
-			recruiterProfileID,
-		).
-		First(&verification).
-		Error
-
-	if err != nil {
-		return nil, err
-	}
-
-	return &verification, nil
-}
-
-// GetByOrganizationEmail returns a verification request submitted with the given email.
-func (r *OrganizationVerificationRepository) GetByOrganizationEmail(
-	ctx context.Context,
-	organizationEmail string,
-) (*models.OrganizationVerification, error) {
-	var verification models.OrganizationVerification
-
-	err := r.DB.
-		WithContext(ctx).
-		Where("LOWER(organization_email) = ?", organizationEmail).
-		First(&verification).
-		Error
-	if err != nil {
-		return nil, err
-	}
-
-	return &verification, nil
-}
-
-// admin views one verification
-func (r *OrganizationVerificationRepository) GetByID(
-	ctx context.Context,
-	id uuid.UUID,
-) (*models.OrganizationVerification, error) {
-
-	var verification models.OrganizationVerification
-
-	err := r.DB.
-		WithContext(ctx).
-		Preload("RecruiterProfile").
-		Preload("RecruiterProfile.User").
-		First(&verification, "id = ?", id).
-		Error
-
-	if err != nil {
-		return nil, err
-	}
-
-	return &verification, nil
-}
-
-// update MinIO document object key
-func (r *OrganizationVerificationRepository) UpdateDocument(
-	ctx context.Context,
-	id uuid.UUID,
-	documentType string,
-	objectKey string,
-) error {
-
-	return r.DB.
-		WithContext(ctx).
-		Model(&models.OrganizationVerification{}).
-		Where("id = ?", id).
-		Updates(map[string]interface{}{
-			"document_type":       documentType,
-			"document_object_key": objectKey,
-		}).
-		Error
-}
-
-func (r *OrganizationVerificationRepository) UpdateReview(
-	ctx context.Context,
-	id uuid.UUID,
-	updates map[string]interface{},
-) error {
-
-	return r.DB.
-		WithContext(ctx).
-		Model(&models.OrganizationVerification{}).
-		Where("id = ?", id).
-		Updates(updates).
-		Error
-}
-
-// admin sees pending requests
-func (r *OrganizationVerificationRepository) GetPending(
-	ctx context.Context,
-	limit int,
-	offset int,
-) ([]models.OrganizationVerification, error) {
-
-	var verifications []models.OrganizationVerification
-
-	err := r.DB.
-		WithContext(ctx).
-		Preload("RecruiterProfile").
-		Preload("RecruiterProfile.User").
-		Where(
-			"status = ?",
-			enums.OrganizationVerificationPending,
-		).
-		Order("submitted_at ASC").
-		Limit(limit).
-		Offset(offset).
-		Find(&verifications).
-		Error
-
-	if err != nil {
-		return nil, err
-	}
-
-	return verifications, nil
-}
-
-func (r *OrganizationVerificationRepository) CountPending(
-	ctx context.Context,
-) (int64, error) {
-
-	var count int64
-
-	err := r.DB.
-		WithContext(ctx).
-		Model(&models.OrganizationVerification{}).
-		Where(
-			"status = ?",
-			enums.OrganizationVerificationPending,
-		).
-		Count(&count).
-		Error
-
-	return count, err
-}
-
-func (r *OrganizationVerificationRepository) CountByStatus(
-	ctx context.Context,
-	status enums.OrganizationVerificationStatus,
-) (int64, error) {
-	var count int64
-
-	query := r.DB.WithContext(ctx).Model(&models.OrganizationVerification{})
-	if status != "" {
-		query = query.Where("status = ?", status)
-	}
-
-	err := query.Count(&count).Error
-	return count, err
-}
-
-// admin filtering
-func (r *OrganizationVerificationRepository) GetByStatus(
-	ctx context.Context,
-	status enums.OrganizationVerificationStatus,
-	limit int,
-	offset int,
-) ([]models.OrganizationVerification, error) {
-	var verifications []models.OrganizationVerification
-	query := r.DB.
-		WithContext(ctx).
-		Preload("RecruiterProfile").
-		Preload("RecruiterProfile.User").
-		Order("created_at DESC").
-		Limit(limit).
-		Offset(offset)
-
-	switch status {
-	case "":
-		// no filter — all statuses
-	case enums.OrganizationVerificationReviewed:
-		query = query.Where(
-			"status IN (?, ?)",
-			enums.OrganizationVerificationApproved,
-			enums.OrganizationVerificationRejected,
-		)
-	default:
-		query = query.Where("status = ?", status)
-	}
-
-	if err := query.Find(&verifications).Error; err != nil {
-		return nil, err
-	}
-	return verifications, nil
-}
-
-func (r *OrganizationVerificationRepository) ExistsByRecruiterProfileID(
-
-	ctx context.Context,
-	recruiterProfileID uuid.UUID,
-) (bool, error) {
-
-	var count int64
-
-	err := r.DB.
-		WithContext(ctx).
-		Model(&models.OrganizationVerification{}).
-		Where(
-			"recruiter_profile_id = ?",
-			recruiterProfileID,
-		).
-		Count(&count).
-		Error
-
-	if err != nil {
-		return false, err
-	}
-
-	return count > 0, nil
-}

# internal/repositories/recruiter_profile_repository.go

diff --git a/internal/repositories/recruiter_profile_repository.go b/internal/repositories/recruiter_profile_repository.go
index c76b66b..9a17652 100644
--- a/internal/repositories/recruiter_profile_repository.go
+++ b/internal/repositories/recruiter_profile_repository.go
@@ -23,14 +23,13 @@ func NewRecruiterProfileRepository(db *gorm.DB) *RecruiterProfileRepository {
 // Upsert creates a new recruiter profile or updates the existing
 // organization/profile information when user_id already exists.
 func (r *RecruiterProfileRepository) Upsert(ctx context.Context, profile *models.RecruiterProfile) error {
-	return r.DB.Clauses(clause.OnConflict{
+	return r.DB.WithContext(ctx).Omit(clause.Associations).Clauses(clause.OnConflict{
 		Columns: []clause.Column{
 			{Name: "user_id"},
 		},
 		DoUpdates: clause.AssignmentColumns([]string{
 			"organization_name",
 			"designation",
-			"organization_logo",
 			"organization_website",
 			"organization_address",
 			"organization_about",
@@ -51,7 +50,6 @@ func (r *RecruiterProfileRepository) GetByUserID(
 
 	err := r.DB.
 		Preload("User").
-		Preload("Verification").
 		Where("user_id = ?", userID).
 		First(&profile).
 		Error
@@ -80,28 +78,6 @@ func (r *RecruiterProfileRepository) GetByID(
 	return &profile, nil
 }
 
-func (r *RecruiterProfileRepository) UpdateEmployeeVerification(ctx context.Context, userID uuid.UUID, isVerified bool) error {
-	return r.DB.Model(&models.RecruiterProfile{}).
-		Where("user_id = ?", userID).
-		Update("verification_status", isVerified).
-		Error
-}
-
-// UpdateVerificationStatus keeps the profile-level status in sync with the
-// associated organization verification review.
-func (r *RecruiterProfileRepository) UpdateVerificationStatus(
-	ctx context.Context,
-	recruiterProfileID uuid.UUID,
-	status string,
-) error {
-	return r.DB.
-		WithContext(ctx).
-		Model(&models.RecruiterProfile{}).
-		Where("id = ?", recruiterProfileID).
-		Update("verification_status", status).
-		Error
-}
-
 // DeleteByUserID removes an recruiter profile associated with the given user.
 func (r *RecruiterProfileRepository) DeleteByUserID(
 	ctx context.Context,

# internal/repositories/student_repository_test.go

diff --git a/internal/repositories/student_repository_test.go b/internal/repositories/student_repository_test.go
index 3ad9ad5..f831bf9 100644
--- a/internal/repositories/student_repository_test.go
+++ b/internal/repositories/student_repository_test.go
@@ -2,6 +2,7 @@ package repositories
 
 import (
 	"context"
+	"errors"
 	"fmt"
 	"testing"
 
@@ -32,8 +33,6 @@ func TestStudentRepositoryGetApplicationStats(t *testing.T) {
 	statuses := []models.InternshipApplicationStatus{
 		models.ApplicationStatusSubmitted,
 		models.ApplicationStatusSubmitted,
-		models.ApplicationStatusReviewing,
-		models.ApplicationStatusShortlisted,
 		models.ApplicationStatusAccepted,
 		models.ApplicationStatusAccepted,
 		models.ApplicationStatusRejected,
@@ -55,14 +54,11 @@ func TestStudentRepositoryGetApplicationStats(t *testing.T) {
 	}
 
 	want := StudentApplicationStats{
-		TotalApplications:       8,
-		ActiveApplications:      4,
-		ApprovedApplications:    2,
-		RejectedApplications:    1,
-		PendingApplications:     2,
-		UnderReviewApplications: 1,
-		ShortlistedApplications: 1,
-		WithdrawnApplications:   1,
+		TotalApplications:     6,
+		ApprovedApplications:  2,
+		RejectedApplications:  1,
+		PendingApplications:   2,
+		WithdrawnApplications: 1,
 	}
 	if *stats != want {
 		t.Fatalf("stats = %+v, want %+v", *stats, want)
@@ -76,3 +72,69 @@ func TestStudentRepositoryGetApplicationStats(t *testing.T) {
 		t.Fatalf("empty stats = %+v, want zero values", *emptyStats)
 	}
 }
+
+func TestSaveResumeReplacesCurrentCV(t *testing.T) {
+	db := openApplicationRepositoryTestDB(t)
+	student := createRepositoryTestStudent(t, db, "Student")
+	repo := NewStudentRepository(db)
+	ctx := context.Background()
+	first := &models.StudentDocument{ProfileID: student.ID, UserID: student.UserID, FileName: "first.pdf", ObjectKey: "first.pdf"}
+	if _, err := repo.SaveResume(ctx, first); err != nil {
+		t.Fatal(err)
+	}
+	replacement := &models.StudentDocument{ProfileID: student.ID, UserID: student.UserID, FileName: "replacement.pdf", ObjectKey: "replacement.pdf"}
+	oldKey, err := repo.SaveResume(ctx, replacement)
+	if err != nil || oldKey != "first.pdf" || replacement.ID != first.ID {
+		t.Fatalf("replacement: old=%s id=%s error=%v", oldKey, replacement.ID, err)
+	}
+	var count int64
+	if err := db.Model(&models.StudentDocument{}).Where("profile_id = ?", student.ID).Count(&count).Error; err != nil || count != 1 {
+		t.Fatalf("CV count=%d: %v", count, err)
+	}
+	if _, err := repo.SaveResume(ctx, &models.StudentDocument{ProfileID: student.ID, UserID: uuid.New(), ObjectKey: "other.pdf"}); err == nil {
+		t.Fatal("another user replaced the CV")
+	}
+	current, err := repo.GetResume(ctx, student.ID)
+	if err != nil || current.ObjectKey != "replacement.pdf" {
+		t.Fatalf("CV=%+v: %v", current, err)
+	}
+}
+
+func TestDeleteResumeRemovesLegacyCVsAndChecksOwnership(t *testing.T) {
+	db := openApplicationRepositoryTestDB(t)
+	student := createRepositoryTestStudent(t, db, "Student")
+	other := createRepositoryTestStudent(t, db, "Other student")
+	repo := NewStudentRepository(db)
+	ctx := context.Background()
+	for _, document := range []models.StudentDocument{
+		{ProfileID: student.ID, UserID: student.UserID, FileName: "old.pdf", ObjectKey: "old.pdf"},
+		{ProfileID: student.ID, UserID: student.UserID, FileName: "current.pdf", ObjectKey: "current.pdf"},
+		{ProfileID: other.ID, UserID: other.UserID, FileName: "other.pdf", ObjectKey: "other.pdf"},
+	} {
+		if err := db.Create(&document).Error; err != nil {
+			t.Fatal(err)
+		}
+	}
+	if _, err := repo.DeleteResume(ctx, student.ID, other.UserID); !errors.Is(err, gorm.ErrRecordNotFound) {
+		t.Fatalf("another user deleted the CV: %v", err)
+	}
+	documents, err := repo.DeleteResume(ctx, student.ID, student.UserID)
+	if err != nil || len(documents) != 2 {
+		t.Fatalf("deleted documents=%v: %v", documents, err)
+	}
+	if _, err := repo.GetResume(ctx, student.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
+		t.Fatalf("deleted CV fell back to a legacy file: %v", err)
+	}
+	if document, err := repo.GetResume(ctx, other.ID); err != nil || document.ObjectKey != "other.pdf" {
+		t.Fatalf("other student's CV changed: %v, %v", document, err)
+	}
+	if documents, err := repo.DeleteResume(ctx, student.ID, student.UserID); err != nil || len(documents) != 0 {
+		t.Fatalf("repeat deletion: %v, %v", documents, err)
+	}
+	if _, err := repo.SaveResume(ctx, &models.StudentDocument{ProfileID: student.ID, UserID: student.UserID, FileName: "new.pdf", ObjectKey: "new.pdf"}); err != nil {
+		t.Fatalf("upload after deletion: %v", err)
+	}
+	if document, err := repo.GetResume(ctx, student.ID); err != nil || document.ObjectKey != "new.pdf" {
+		t.Fatalf("new CV unavailable: %v, %v", document, err)
+	}
+}

# internal/services/auth_service.go

diff --git a/internal/services/auth_service.go b/internal/services/auth_service.go
index 3c709ae..4380771 100644
--- a/internal/services/auth_service.go
+++ b/internal/services/auth_service.go
@@ -21,10 +21,11 @@ type AuthService struct {
 	Repo   *repositories.UserRepository
 	Cfg    *config.Config
 	Logger *logger.Logger
+	Sender VerificationSender
 }
 
 func NewAuthService(repo *repositories.UserRepository, cfg *config.Config, l *logger.Logger) *AuthService {
-	return &AuthService{Repo: repo, Cfg: cfg, Logger: l}
+	return &AuthService{Repo: repo, Cfg: cfg, Logger: l, Sender: SMTPVerificationSender{Config: cfg.Email}}
 }
 
 // HashPassword hashes the plaintext password using bcrypt
@@ -44,6 +45,9 @@ func (s *AuthService) ComparePassword(hash, password string) error {
 
 // GenerateAccessToken creates a signed JWT access token
 func (s *AuthService) GenerateAccessToken(user *models.User) (string, error) {
+	if !user.CanSignIn() {
+		return "", ErrEmailNotVerified
+	}
 	claims := jwt.MapClaims{
 		"sub":  user.ID.String(),
 		"role": user.Role,
@@ -61,6 +65,9 @@ func (s *AuthService) GenerateAccessToken(user *models.User) (string, error) {
 
 // GenerateRefreshToken creates a secure random refresh token string and persists its hash
 func (s *AuthService) GenerateRefreshToken(user *models.User) (string, error) {
+	if !user.CanSignIn() {
+		return "", ErrEmailNotVerified
+	}
 	b := make([]byte, 32)
 	if _, err := rand.Read(b); err != nil {
 		s.Logger.Error("failed to read random bytes: %v", err)

# internal/services/internship_service.go

diff --git a/internal/services/internship_service.go b/internal/services/internship_service.go
index 2467b15..42d70ee 100644
--- a/internal/services/internship_service.go
+++ b/internal/services/internship_service.go
@@ -20,12 +20,13 @@ var (
 )
 
 type InternshipService struct {
+	profiles      *RecruiterProfileService
 	repo          *repositories.InternshipRepository
 	recruiterRepo *repositories.RecruiterProfileRepository
 }
 
-func NewInternshipService(repo *repositories.InternshipRepository, recruiterRepo *repositories.RecruiterProfileRepository) *InternshipService {
-	return &InternshipService{repo: repo, recruiterRepo: recruiterRepo}
+func NewInternshipService(repo *repositories.InternshipRepository, recruiterRepo *repositories.RecruiterProfileRepository, profiles *RecruiterProfileService) *InternshipService {
+	return &InternshipService{repo: repo, recruiterRepo: recruiterRepo, profiles: profiles}
 }
 
 func (s *InternshipService) CreateInternship(ctx context.Context, internship *models.Internship) error {
@@ -38,7 +39,10 @@ func (s *InternshipService) CreateInternship(ctx context.Context, internship *mo
 	if err := validateInternship(internship); err != nil {
 		return err
 	}
-	recruiterProfile, err := s.recruiterRepo.GetByUserID(ctx, internship.IssuedBy)
+	_, err := s.recruiterRepo.GetByUserID(ctx, internship.IssuedBy)
+	if errors.Is(err, gorm.ErrRecordNotFound) {
+		return fmt.Errorf("%w: complete your company profile before creating an internship", ErrInvalidInternshipData)
+	}
 	if err != nil {
 		return fmt.Errorf("get recruiter profile: %w", err)
 	}
@@ -48,10 +52,6 @@ func (s *InternshipService) CreateInternship(ctx context.Context, internship *mo
 	if !internship.Status.IsValid() {
 		return fmt.Errorf("%w: invalid internship status", ErrInvalidInternshipData)
 	}
-	if recruiterProfile.VerificationStatus != string(enums.OrganizationVerificationApproved) {
-		internship.Status = enums.InternshipStatusPrivate
-	}
-	internship.IsActive = internship.Status.IsActive()
 
 	if internship.StipendCurrency == "" {
 		internship.StipendCurrency = "NPR"
@@ -74,7 +74,7 @@ func (s *InternshipService) UpdateInternship(ctx context.Context, internship *mo
 	if err != nil {
 		return err
 	}
-	recruiterProfile, err := s.recruiterRepo.GetByUserID(ctx, existing.IssuedBy)
+	_, err = s.recruiterRepo.GetByUserID(ctx, existing.IssuedBy)
 	if err != nil {
 		return fmt.Errorf("get recruiter profile: %w", err)
 	}
@@ -86,10 +86,6 @@ func (s *InternshipService) UpdateInternship(ctx context.Context, internship *mo
 	if !internship.Status.IsValid() {
 		return fmt.Errorf("%w: invalid internship status", ErrInvalidInternshipData)
 	}
-	if recruiterProfile.VerificationStatus != string(enums.OrganizationVerificationApproved) {
-		internship.Status = enums.InternshipStatusPrivate
-	}
-	internship.IsActive = internship.Status.IsActive()
 	return s.repo.Update(ctx, internship)
 }
 
@@ -104,6 +100,18 @@ func (s *InternshipService) GetInternship(ctx context.Context, id uuid.UUID) (*m
 	return internship, err
 }
 
+func (s *InternshipService) GetInternshipDetail(ctx context.Context, id uuid.UUID) (*models.Internship, error) {
+	internship, err := s.GetInternship(ctx, id)
+	if err != nil {
+		return nil, err
+	}
+	if err := s.repo.LoadPublicIssuer(ctx, internship); err != nil {
+		return nil, err
+	}
+	internship.Issuer.OrganizationLogo = s.profiles.listingLogoURL(ctx, internship.Issuer.OrganizationLogo)
+	return internship, nil
+}
+
 func (s *InternshipService) DeleteInternship(ctx context.Context, id uuid.UUID) error {
 	if id == uuid.Nil {
 		return fmt.Errorf("%w: invalid internship ID", ErrInvalidInternshipData)
@@ -123,9 +131,13 @@ func (s *InternshipService) ListByEmployer(ctx context.Context, employerID uuid.
 
 func (s *InternshipService) SearchInternships(ctx context.Context, filter repositories.InternshipSearchFilter) ([]models.Internship, int64, error) {
 	filter.Status = enums.InternshipStatusPublished
-	isActive := true
-	filter.IsActive = &isActive
-	return s.repo.Search(ctx, filter)
+	filter.ExcludeExpired = true
+	internships, total, err := s.repo.Search(ctx, filter)
+	for i := range internships {
+		issuer := &internships[i].Issuer
+		issuer.OrganizationLogo = s.profiles.listingLogoURL(ctx, issuer.OrganizationLogo)
+	}
+	return internships, total, err
 }
 
 func validateInternship(internship *models.Internship) error {

# internal/services/interview_service.go

diff --git a/internal/services/interview_service.go b/internal/services/interview_service.go
deleted file mode 100644
index 9c581f2..0000000
--- a/internal/services/interview_service.go
+++ /dev/null
@@ -1,61 +0,0 @@
-package services
-
-import (
-	"time"
-
-	"github.com/google/uuid"
-
-	"github.com/prasanna/student-job-portal/backend/internal/models"
-	"github.com/prasanna/student-job-portal/backend/internal/repositories"
-)
-
-type InterviewService struct {
-	Repo *repositories.InterviewRepository
-}
-
-func NewInterviewService(repo *repositories.InterviewRepository) *InterviewService {
-	return &InterviewService{Repo: repo}
-}
-
-func (s *InterviewService) ScheduleInterview(i *models.Interview) error {
-	// basic validations could be added here (e.g., job exists)
-	if i.ScheduledAt.IsZero() {
-		i.ScheduledAt = time.Now().Add(24 * time.Hour)
-	}
-	if err := s.Repo.Create(i); err != nil {
-		return err
-	}
-	return nil
-}
-
-func (s *InterviewService) GetInterview(id uuid.UUID) (*models.Interview, error) {
-	return s.Repo.GetByID(id)
-}
-
-func (s *InterviewService) ListByJob(jobID uuid.UUID) ([]models.Interview, error) {
-	return s.Repo.ListByJob(jobID)
-}
-
-func (s *InterviewService) ListByStudent(studentID uuid.UUID) ([]models.Interview, error) {
-	return s.Repo.ListByStudent(studentID)
-}
-
-func (s *InterviewService) ListByEmployer(employerID uuid.UUID) ([]models.Interview, error) {
-	return s.Repo.ListByEmployer(employerID)
-}
-
-func (s *InterviewService) UpdateStatus(id uuid.UUID, status string) (*models.Interview, error) {
-	i, err := s.Repo.GetByID(id)
-	if err != nil {
-		return nil, err
-	}
-	i.Status = status
-	if err := s.Repo.Update(i); err != nil {
-		return nil, err
-	}
-	return i, nil
-}
-
-func (s *InterviewService) CancelInterview(id uuid.UUID) error {
-	return s.Repo.DeleteByID(id)
-}

# internal/services/organization_verification_service.go

diff --git a/internal/services/organization_verification_service.go b/internal/services/organization_verification_service.go
deleted file mode 100644
index 8646a60..0000000
--- a/internal/services/organization_verification_service.go
+++ /dev/null
@@ -1,293 +0,0 @@
-package services
-
-import (
-	"context"
-	"errors"
-	"fmt"
-	"mime/multipart"
-	"net/http"
-	"net/mail"
-	"strings"
-	"time"
-
-	"github.com/google/uuid"
-	"github.com/minio/minio-go/v7"
-	"gorm.io/gorm"
-
-	"github.com/prasanna/student-job-portal/backend/internal/enums"
-	"github.com/prasanna/student-job-portal/backend/internal/models"
-	"github.com/prasanna/student-job-portal/backend/internal/repositories"
-)
-
-var (
-	ErrInvalidOrganizationVerification    = errors.New("invalid organization verification request")
-	ErrOrganizationVerificationNotFound   = errors.New("organization verification not found")
-	ErrInvalidVerificationState           = errors.New("invalid organization verification state")
-	ErrOrganizationEmailAlreadyRegistered = errors.New("organization email is already registered for verification")
-)
-
-const maxOrganizationVerificationDocumentSize = 10 * 1024 * 1024 // 10 MB
-
-type OrganizationVerificationService struct {
-	repo                 *repositories.OrganizationVerificationRepository
-	recruiterProfileRepo *repositories.RecruiterProfileRepository
-	minioClient          *minio.Client
-	bucketName           string
-}
-
-func NewOrganizationVerificationService(
-	repo *repositories.OrganizationVerificationRepository,
-	recruiterProfileRepo *repositories.RecruiterProfileRepository,
-	minioClient *minio.Client,
-	bucketName string,
-) *OrganizationVerificationService {
-	return &OrganizationVerificationService{
-		repo: repo, recruiterProfileRepo: recruiterProfileRepo,
-		minioClient: minioClient, bucketName: bucketName,
-	}
-}
-
-// Submit creates or resubmits an employer's verification request.
-func (s *OrganizationVerificationService) Submit(
-	ctx context.Context,
-	userID uuid.UUID,
-	method enums.OrganizationVerificationMethod,
-	organizationEmail string,
-	documentType string,
-) (*models.OrganizationVerification, error) {
-	if userID == uuid.Nil {
-		return nil, fmt.Errorf("%w: invalid user id", ErrInvalidOrganizationVerification)
-	}
-	organizationEmail = strings.TrimSpace(strings.ToLower(organizationEmail))
-	address, err := mail.ParseAddress(organizationEmail)
-	if err != nil || address.Address != organizationEmail {
-		return nil, fmt.Errorf("%w: a valid organization email is required", ErrInvalidOrganizationVerification)
-	}
-	parts := strings.Split(address.Address, "@")
-	if len(parts) != 2 || parts[1] == "" {
-		return nil, fmt.Errorf("%w: a valid organization email is required", ErrInvalidOrganizationVerification)
-	}
-	if _, err := s.repo.GetByOrganizationEmail(ctx, organizationEmail); err == nil {
-		return nil, ErrOrganizationEmailAlreadyRegistered
-	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
-		return nil, fmt.Errorf("check organization email verification: %w", err)
-	}
-
-	recruiterProfile, err := s.recruiterProfileRepo.GetByUserID(ctx, userID)
-	if errors.Is(err, gorm.ErrRecordNotFound) {
-		return nil, ErrRecruiterProfileNotFound
-	}
-	if err != nil {
-		return nil, fmt.Errorf("get recruiter profile: %w", err)
-	}
-	var oldDocumentObjectKey string
-	if existing, err := s.repo.GetByRecruiterProfileID(ctx, recruiterProfile.ID); err == nil {
-		oldDocumentObjectKey = existing.DocumentObjectKey
-	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
-		return nil, fmt.Errorf("get existing organization verification: %w", err)
-	}
-
-	now := time.Now().UTC()
-	verification := &models.OrganizationVerification{
-		ID:                 uuid.New(),
-		RecruiterProfileID: recruiterProfile.ID,
-		Status:             enums.OrganizationVerificationPending,
-		OrganizationEmail:  organizationEmail,
-		EmailDomain:        address.Address[strings.Index(address.Address, "@")+1:],
-		SubmittedAt:        &now,
-		DocumentType:       strings.TrimSpace(documentType),
-	}
-
-	if err := s.repo.Upsert(ctx, verification); err != nil {
-		return nil, fmt.Errorf("save organization verification: %w", err)
-	}
-	if err := s.recruiterProfileRepo.UpdateVerificationStatus(ctx, recruiterProfile.ID, string(enums.OrganizationVerificationPending)); err != nil {
-		return nil, fmt.Errorf("update recruiter verification status: %w", err)
-	}
-	if oldDocumentObjectKey != "" {
-		_ = s.minioClient.RemoveObject(ctx, s.bucketName, oldDocumentObjectKey, minio.RemoveObjectOptions{})
-	}
-
-	return s.GetMy(ctx, userID)
-}
-
-func (s *OrganizationVerificationService) GetMy(ctx context.Context, userID uuid.UUID) (*models.OrganizationVerification, error) {
-	if userID == uuid.Nil {
-		return nil, fmt.Errorf("%w: invalid user id", ErrInvalidOrganizationVerification)
-	}
-	recruiterProfile, err := s.recruiterProfileRepo.GetByUserID(ctx, userID)
-	if errors.Is(err, gorm.ErrRecordNotFound) {
-		return nil, ErrRecruiterProfileNotFound
-	}
-	if err != nil {
-		return nil, fmt.Errorf("get recruiter profile: %w", err)
-	}
-	verification, err := s.repo.GetByRecruiterProfileID(ctx, recruiterProfile.ID)
-	if errors.Is(err, gorm.ErrRecordNotFound) {
-		return nil, ErrOrganizationVerificationNotFound
-	}
-	return verification, err
-}
-
-/*
-UploadDocument uploads a verification document for an employer's verification request.
-It can only be called if the verification method is "document" and the status is not "approved".
-The document must be a PDF, JPEG, or PNG file and must be smaller than 10MB.
-*/
-func (s *OrganizationVerificationService) UploadDocument(ctx context.Context, userID uuid.UUID, fileHeader *multipart.FileHeader) (*models.OrganizationVerification, error) {
-	if fileHeader == nil || fileHeader.Size <= 0 {
-		return nil, fmt.Errorf("%w: verification document is required", ErrInvalidOrganizationVerification)
-	}
-	if fileHeader.Size > maxOrganizationVerificationDocumentSize {
-		return nil, fmt.Errorf("%w: verification document must be smaller than 10MB", ErrInvalidOrganizationVerification)
-	}
-
-	verification, err := s.GetMy(ctx, userID)
-	if err != nil {
-		return nil, err
-	}
-	if verification.Status == enums.OrganizationVerificationApproved {
-		return nil, fmt.Errorf("%w: document cannot be uploaded for this request", ErrInvalidVerificationState)
-	}
-
-	file, err := fileHeader.Open()
-	if err != nil {
-		return nil, fmt.Errorf("open verification document: %w", err)
-	}
-	defer file.Close()
-
-	buffer := make([]byte, 512)
-	n, readErr := file.Read(buffer)
-	if readErr != nil && n == 0 {
-		return nil, fmt.Errorf("read verification document: %w", readErr)
-	}
-	contentType := http.DetectContentType(buffer[:n])
-	extension, err := verificationDocumentExtension(contentType)
-	if err != nil {
-		return nil, err
-	}
-	if _, err := file.Seek(0, 0); err != nil {
-		return nil, fmt.Errorf("reset verification document: %w", err)
-	}
-
-	objectKey := fmt.Sprintf("organization-verifications/%s/%s%s", verification.RecruiterProfileID, uuid.New(), extension)
-	if _, err := s.minioClient.PutObject(ctx, s.bucketName, objectKey, file, fileHeader.Size, minio.PutObjectOptions{ContentType: contentType}); err != nil {
-		return nil, fmt.Errorf("upload verification document: %w", err)
-	}
-	if err := s.repo.UpdateDocument(ctx, verification.ID, strings.TrimSpace(fileHeader.Filename), objectKey); err != nil {
-		_ = s.minioClient.RemoveObject(ctx, s.bucketName, objectKey, minio.RemoveObjectOptions{})
-		return nil, fmt.Errorf("save verification document: %w", err)
-	}
-	if verification.DocumentObjectKey != "" && verification.DocumentObjectKey != objectKey {
-		_ = s.minioClient.RemoveObject(ctx, s.bucketName, verification.DocumentObjectKey, minio.RemoveObjectOptions{})
-	}
-	return s.GetMy(ctx, userID)
-}
-
-/*
-review updates the status of an organization verification request.
-It can only be called by an admin user.
-The status must be either "approved" or "rejected".
-If the status is "rejected", a rejection reason must be provided.
-The function also updates the employer profile's verification status accordingly.
-*/
-func (s *OrganizationVerificationService) Review(ctx context.Context, id, reviewerID uuid.UUID, status enums.OrganizationVerificationStatus, rejectionReason, reviewNotes string) error {
-	if id == uuid.Nil || reviewerID == uuid.Nil {
-		return fmt.Errorf("%w: invalid verification or reviewer id", ErrInvalidOrganizationVerification)
-	}
-	if status != enums.OrganizationVerificationApproved && status != enums.OrganizationVerificationRejected {
-		return fmt.Errorf("%w: review status must be approved or rejected", ErrInvalidOrganizationVerification)
-	}
-	rejectionReason, reviewNotes = strings.TrimSpace(rejectionReason), strings.TrimSpace(reviewNotes)
-	if status == enums.OrganizationVerificationRejected && rejectionReason == "" {
-		return fmt.Errorf("%w: rejection reason is required", ErrInvalidOrganizationVerification)
-	}
-
-	verification, err := s.repo.GetByID(ctx, id)
-	if errors.Is(err, gorm.ErrRecordNotFound) {
-		return ErrOrganizationVerificationNotFound
-	}
-	if err != nil {
-		return err
-	}
-	if verification.Status != enums.OrganizationVerificationPending {
-		return fmt.Errorf("%w: only pending requests can be reviewed", ErrInvalidVerificationState)
-	}
-
-	now := time.Now().UTC()
-	updates := map[string]interface{}{"status": status, "reviewed_by": reviewerID, "reviewed_at": now, "review_notes": reviewNotes, "rejection_reason": rejectionReason}
-	if status == enums.OrganizationVerificationApproved {
-		updates["verified_at"] = now
-	}
-
-	return s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
-		verificationRepo := repositories.NewOrganizationVerificationRepository(tx)
-		profileRepo := repositories.NewRecruiterProfileRepository(tx)
-		if err := verificationRepo.UpdateReview(ctx, id, updates); err != nil {
-			return err
-		}
-		return profileRepo.UpdateVerificationStatus(ctx, verification.RecruiterProfileID, string(status))
-	})
-}
-
-func (s *OrganizationVerificationService) GetByID(ctx context.Context, id uuid.UUID) (*models.OrganizationVerification, error) {
-	if id == uuid.Nil {
-		return nil, fmt.Errorf("%w: invalid verification id", ErrInvalidOrganizationVerification)
-	}
-	verification, err := s.repo.GetByID(ctx, id)
-	if errors.Is(err, gorm.ErrRecordNotFound) {
-		return nil, ErrOrganizationVerificationNotFound
-	}
-	return verification, err
-}
-
-/*
-List retrieves a paginated list of organization verification requests filtered by status.
-The status can be "pending", "approved", or "rejected". If no status is provided, all requests are returned.
-The page and pageSize parameters control the pagination of the results.
-*/
-func (s *OrganizationVerificationService) List(ctx context.Context, filters repositories.VerificationSearchFilter) ([]models.OrganizationVerification, int64, error) {
-	status := filters.Status
-	page := filters.Page
-	pageSize := filters.PageSize
-
-	if page < 1 {
-		page = 1
-	}
-	if pageSize < 1 || pageSize > 100 {
-		pageSize = 10
-	}
-	items, err := s.repo.GetByStatus(ctx, status, pageSize, (page-1)*pageSize)
-	if err != nil {
-		return nil, 0, err
-	}
-	total, err := s.repo.CountByStatus(ctx, status)
-	return items, total, err
-}
-
-/*
-GetDocumentURL generates a pre-signed URL for accessing a verification document in the object storage.
-*/
-func (s *OrganizationVerificationService) GetDocumentURL(ctx context.Context, objectKey string) (string, error) {
-	if objectKey == "" {
-		return "", nil
-	}
-	url, err := s.minioClient.PresignedGetObject(ctx, s.bucketName, objectKey, time.Hour, nil)
-	if err != nil {
-		return "", fmt.Errorf("create document URL: %w", err)
-	}
-	return url.String(), nil
-}
-
-func verificationDocumentExtension(contentType string) (string, error) {
-	switch strings.ToLower(contentType) {
-	case "application/pdf":
-		return ".pdf", nil
-	case "image/jpeg":
-		return ".jpg", nil
-	case "image/png":
-		return ".png", nil
-	default:
-		return "", fmt.Errorf("%w: only PDF, JPEG, and PNG documents are allowed", ErrInvalidOrganizationVerification)
-	}
-}

# internal/services/recruiter_profile_service.go

diff --git a/internal/services/recruiter_profile_service.go b/internal/services/recruiter_profile_service.go
index ec27733..609bf50 100644
--- a/internal/services/recruiter_profile_service.go
+++ b/internal/services/recruiter_profile_service.go
@@ -1,11 +1,12 @@
 package services
 
 import (
+	"bytes"
 	"context"
 	"errors"
 	"fmt"
+	"log/slog"
 	"mime/multipart"
-	"net/http"
 	"strings"
 	"time"
 
@@ -28,10 +29,6 @@ type RecruiterProfileService struct {
 	bucketName  string
 }
 
-const (
-	maxOrganizationLogoSize = 5 * 1024 * 1024 // 5 MB
-)
-
 func NewRecruiterProfileService(
 	repo *repositories.RecruiterProfileRepository,
 	minioClient *minio.Client,
@@ -74,16 +71,16 @@ func (s *RecruiterProfileService) CreateOrUpdateProfile(
 	// Do not trust user_id coming from request body.
 	// Always use authenticated user's ID.
 	profile.UserID = userID
+	profile.User = nil
+	profile.OrganizationLogo = ""
 
-	if profile.ID == uuid.Nil {
-		profile.ID = uuid.New()
-	}
+	profile.ID = uuid.New()
 
 	if err := s.repo.Upsert(ctx, profile); err != nil {
 		return nil, err
 	}
 
-	return s.repo.GetByUserID(ctx, userID)
+	return s.GetByUserID(ctx, userID)
 }
 
 // GetByUserID returns an recruiter profile by user ID.
@@ -150,16 +147,12 @@ func (s *RecruiterProfileService) UploadOrganizationLogo(
 		return "", fmt.Errorf("%w: invalid user id", ErrInvalidRecruiterProfileData)
 	}
 
-	if fileHeader == nil {
-		return "", fmt.Errorf("%w: organization logo is required", ErrInvalidRecruiterProfileData)
-	}
-
-	if fileHeader.Size <= 0 {
-		return "", fmt.Errorf("%w: uploaded file is empty", ErrInvalidRecruiterProfileData)
+	data, contentType, extension, err := readProfilePicture(fileHeader)
+	if err != nil {
+		return "", err
 	}
-
-	if fileHeader.Size > maxOrganizationLogoSize {
-		return "", fmt.Errorf("%w: organization logo must be smaller than 5MB", ErrInvalidRecruiterProfileData)
+	if s.minioClient == nil {
+		return "", errors.New("profile picture storage unavailable")
 	}
 
 	// Make sure recruiter profile exists.
@@ -171,33 +164,6 @@ func (s *RecruiterProfileService) UploadOrganizationLogo(
 		return "", fmt.Errorf("failed to get recruiter profile: %w", err)
 	}
 
-	file, err := fileHeader.Open()
-	if err != nil {
-		return "", fmt.Errorf("failed to open uploaded file: %w", err)
-	}
-	defer file.Close()
-
-	// Read first 512 bytes for MIME detection.
-	buffer := make([]byte, 512)
-
-	n, err := file.Read(buffer)
-	if err != nil && n == 0 {
-		return "", fmt.Errorf("failed to read uploaded file: %w", err)
-	}
-
-	contentType := http.DetectContentType(buffer[:n])
-
-	// Only allow actual image formats that we support.
-	extension, err := getImageExtension(contentType)
-	if err != nil {
-		return "", err
-	}
-
-	// Reset file pointer after MIME detection.
-	if _, err := file.Seek(0, 0); err != nil {
-		return "", fmt.Errorf("failed to reset uploaded file: %w", err)
-	}
-
 	objectName := fmt.Sprintf(
 		"organization-logos/%s/%s%s",
 		userID.String(),
@@ -209,8 +175,8 @@ func (s *RecruiterProfileService) UploadOrganizationLogo(
 		ctx,
 		s.bucketName,
 		objectName,
-		file,
-		fileHeader.Size,
+		bytes.NewReader(data),
+		int64(len(data)),
 		minio.PutObjectOptions{
 			ContentType: contentType,
 		},
@@ -220,6 +186,8 @@ func (s *RecruiterProfileService) UploadOrganizationLogo(
 		return "", fmt.Errorf("failed to upload organization logo: %w", err)
 	}
 
+	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
+	defer cancel()
 	oldLogo := profile.OrganizationLogo
 
 	// Store only MinIO object key in DB.
@@ -231,7 +199,7 @@ func (s *RecruiterProfileService) UploadOrganizationLogo(
 	if err != nil {
 		// DB update failed, so remove newly uploaded object.
 		_ = s.minioClient.RemoveObject(
-			ctx,
+			cleanupCtx,
 			s.bucketName,
 			objectName,
 			minio.RemoveObjectOptions{},
@@ -243,7 +211,7 @@ func (s *RecruiterProfileService) UploadOrganizationLogo(
 	// to the new object.
 	if oldLogo != "" && oldLogo != objectName {
 		_ = s.minioClient.RemoveObject(
-			ctx,
+			cleanupCtx,
 			s.bucketName,
 			oldLogo,
 			minio.RemoveObjectOptions{},
@@ -266,6 +234,9 @@ func (s *RecruiterProfileService) GetLogoURL(
 		return "", nil
 	}
 
+	if s.minioClient == nil {
+		return "", errors.New("profile picture storage unavailable")
+	}
 	url, err := s.minioClient.PresignedGetObject(
 		ctx,
 		s.bucketName,
@@ -281,19 +252,11 @@ func (s *RecruiterProfileService) GetLogoURL(
 	return url.String(), nil
 }
 
-func getImageExtension(contentType string) (string, error) {
-	switch strings.ToLower(contentType) {
-
-	case "image/jpeg":
-		return ".jpg", nil
-
-	case "image/png":
-		return ".png", nil
-
-	case "image/webp":
-		return ".webp", nil
-
-	default:
-		return "", fmt.Errorf("%w: unsupported image type; only JPEG, PNG and WEBP are allowed", ErrInvalidRecruiterProfileData)
+// listingLogoURL keeps listings available if the optional logo cannot be signed.
+func (s *RecruiterProfileService) listingLogoURL(ctx context.Context, objectKey string) string {
+	url, err := s.GetLogoURL(ctx, objectKey)
+	if err != nil {
+		slog.WarnContext(ctx, "Could not sign company logo", "error", err)
 	}
+	return url
 }

# internal/storage/minio.go

diff --git a/internal/storage/minio.go b/internal/storage/minio.go
index 0c8c77d..2a7f808 100644
--- a/internal/storage/minio.go
+++ b/internal/storage/minio.go
@@ -34,8 +34,6 @@ func ensureBucketsExist(ctx context.Context, client *minio.Client, cfg *config.M
 	buckets := []string{
 		cfg.ProfileBucket,
 		cfg.StudentDocBucket,
-		cfg.CompanyLogoBucket,
-		cfg.CompanyDocBucket,
 	}
 
 	for _, bucket := range buckets {

# readme.md

diff --git a/readme.md b/readme.md
index c3d9a0f..b20e947 100644
--- a/readme.md
+++ b/readme.md
@@ -1,803 +1,5 @@
-Act as a senior Go backend engineer and build a production-ready MVP backend for a **student-focused job portal**.
-
-## Technology Stack
-
-Use:
-
-* Go
-* REST API
-* PostgreSQL
-* GORM
-* MinIO for CVs, profile images, company logos, and documents
-* Docker and Docker Compose
-* JWT authentication
-* Role-based access control
-* `golang-migrate` or GORM migrations
-* Swagger/OpenAPI documentation
-* Structured logging
-* Environment-based configuration
-
-Use Docker Compose to run:
-
-* Backend API
-* PostgreSQL
-* MinIO
-
-Do not run the Go API only on the host machine. The complete application should be runnable with:
-
 ```bash
+cd backend
+[ -f .env ] || cp .env.example .env
 docker compose up --build
 ```
-
-## User Roles
-
-Support three roles:
-
-1. Student
-2. Employer
-3. Admin
-
-Implement authorization middleware so users can access only the endpoints allowed for their role.
-
-## Core Workflow
-
-The main MVP workflow is:
-
-```text
-Student registers
-→ completes profile
-→ searches jobs
-→ applies with a CV
-→ employer reviews application
-→ employer changes application status
-→ student receives a notification
-```
-
-## Project Structure
-
-Use a clean, modular structure such as:
-
-```text
-cmd/
-  api/
-
-internal/
-  config/
-  database/
-  models/
-  repositories/
-  services/
-  handlers/
-  middleware/
-  routes/
-  validation/
-  storage/
-  responses/
-  utils/
-
-migrations/
-docs/
-Dockerfile
-docker-compose.yml
-.env.example
-Makefile
-README.md
-```
-
-Keep responsibilities separated:
-
-* Handler: HTTP request and response
-* Service: business logic
-* Repository: database operations
-* Model: GORM database structure
-* DTO: request and response structures
-* Middleware: authentication, authorization, logging, recovery
-* Storage: MinIO integration
-
-Do not put all logic inside handlers.
-
-## Authentication
-
-Implement:
-
-* User registration
-* Login
-* Password hashing using bcrypt
-* Access token
-* Refresh token
-* Logout or refresh-token revocation
-* Current-user endpoint
-* Role-based authorization
-
-Suggested endpoints:
-
-```text
-POST /api/v1/auth/register/student
-POST /api/v1/auth/register/employer
-POST /api/v1/auth/login
-POST /api/v1/auth/refresh
-POST /api/v1/auth/logout
-GET  /api/v1/auth/me
-```
-
-Use secure JWT signing from environment variables.
-
-Do not store plain-text passwords or refresh tokens.
-
-## Student Module
-
-Students should be able to:
-
-* Create and update their profile
-* Add education records
-* Add skills
-* Add projects
-* Add certifications
-* Upload a profile image
-* Upload multiple CVs
-* Select a default CV
-* View profile completion percentage
-* Save jobs
-* Apply for jobs
-* Withdraw applications
-* Track application statuses
-* View notifications
-
-Student profile fields should include:
-
-```text
-user_id
-full_name
-phone
-location
-bio
-college_name
-degree
-faculty_or_major
-current_semester
-graduation_year
-preferred_job_categories
-preferred_locations
-preferred_work_mode
-availability
-expected_salary
-linkedin_url
-github_url
-portfolio_url
-profile_image_key
-is_searchable
-profile_completion_percentage
-```
-
-Use related tables for:
-
-* Student education
-* Student skills
-* Student projects
-* Student certifications
-* Student documents
-
-## Employer and Company Module
-
-Employers should be able to:
-
-* Register an employer account
-* Create a company profile
-* Upload a company logo
-* Request company verification
-* Create job posts
-* Update their own jobs
-* Close jobs
-* View applicants
-* Shortlist applicants
-* Reject applicants
-* Schedule interviews
-* Mark candidates as selected
-
-Company fields should include:
-
-```text
-name
-description
-industry
-website
-email
-phone
-location
-logo_key
-verification_status
-verified_at
-```
-
-A company verification status can be:
-
-```text
-pending
-approved
-rejected
-```
-
-Only approved employers should be able to publish jobs.
-
-## Job Module
-
-Support these opportunity types:
-
-```text
-internship
-entry_level
-part_time
-freelance
-volunteer
-graduate_trainee
-```
-
-Job fields should include:
-
-```text
-company_id
-created_by
-title
-slug
-description
-responsibilities
-requirements
-education_requirement
-experience_requirement
-opportunity_type
-employment_type
-work_mode
-location
-salary_min
-salary_max
-salary_currency
-stipend_amount
-is_paid
-is_fresher_friendly
-training_provided
-number_of_openings
-application_deadline
-status
-published_at
-closed_at
-```
-
-Job status values:
-
-```text
-draft
-pending_approval
-published
-rejected
-closed
-expired
-```
-
-Implement job skills and application questions using related tables.
-
-Students should be able to search and filter jobs by:
-
-* Keyword
-* Opportunity type
-* Category
-* Location
-* Work mode
-* Paid or unpaid
-* Salary range
-* Required skill
-* Fresher-friendly status
-* Application deadline
-
-Implement pagination, sorting, and filtering.
-
-Suggested public endpoints:
-
-```text
-GET /api/v1/jobs
-GET /api/v1/jobs/:id
-GET /api/v1/companies/:id
-```
-
-## Application Module
-
-A student can apply only once to the same job.
-
-An application should contain:
-
-```text
-job_id
-student_id
-student_document_id
-cover_letter
-status
-applied_at
-withdrawn_at
-```
-
-Application statuses:
-
-```text
-applied
-under_review
-shortlisted
-interview_scheduled
-selected
-rejected
-withdrawn
-```
-
-Store every status change in an application status history table containing:
-
-```text
-application_id
-previous_status
-new_status
-changed_by
-reason
-created_at
-```
-
-Business rules:
-
-* Students cannot apply to draft, closed, rejected, or expired jobs.
-* Students cannot apply after the application deadline.
-* Students can withdraw only active applications.
-* Employers can manage only applications belonging to their own company’s jobs.
-* Status transitions must be validated.
-* Every application status change must create a history record.
-* Historical applications must remain available even when a job is closed or soft-deleted.
-
-Suggested endpoints:
-
-```text
-POST  /api/v1/jobs/:jobId/applications
-GET   /api/v1/students/me/applications
-GET   /api/v1/students/me/applications/:id
-PATCH /api/v1/students/me/applications/:id/withdraw
-
-GET   /api/v1/employers/jobs/:jobId/applications
-GET   /api/v1/employers/applications/:id
-PATCH /api/v1/employers/applications/:id/status
-```
-
-## Saved Jobs
-
-Implement:
-
-```text
-POST   /api/v1/students/me/saved-jobs/:jobId
-DELETE /api/v1/students/me/saved-jobs/:jobId
-GET    /api/v1/students/me/saved-jobs
-```
-
-Prevent duplicate saved-job records.
-
-## Notifications
-
-Create in-app notifications for:
-
-* Application submitted
-* Application status changed
-* Interview scheduled
-* Employer verification approved or rejected
-* Job approved or rejected
-* Job deadline approaching
-
-Notification fields:
-
-```text
-user_id
-type
-title
-message
-reference_type
-reference_id
-is_read
-read_at
-created_at
-```
-
-Provide endpoints to list notifications and mark them as read.
-
-## Admin Module
-
-Admins should be able to:
-
-* View users
-* Suspend or activate users
-* Review employer verification requests
-* Approve or reject employers
-* Review pending jobs
-* Approve or reject jobs
-* Remove suspicious jobs
-* View reports
-* View basic dashboard statistics
-* Review admin action history
-
-All important admin actions should be stored in an audit log.
-
-## Reporting Module
-
-Students should be able to report:
-
-* Fake jobs
-* Jobs requesting money
-* Misleading salary information
-* Inappropriate content
-* Suspicious employers
-
-Report fields:
-
-```text
-reported_by
-target_type
-target_id
-reason
-description
-status
-reviewed_by
-reviewed_at
-resolution
-```
-
-Report status values:
-
-```text
-pending
-reviewing
-resolved
-dismissed
-```
-
-## MinIO Integration
-
-Use MinIO for:
-
-* Student profile images
-* Student CVs
-* Certificates
-* Company logos
-* Company verification documents
-
-Store only object keys and metadata in PostgreSQL.
-
-Do not store file binaries in PostgreSQL.
-
-Implement:
-
-* File upload
-* File validation
-* File deletion
-* Secure download
-* Presigned URLs
-* File-size limits
-* MIME-type validation
-* Unique object names
-* Ownership checks
-
-Suggested buckets:
-
-```text
-profile-images
-student-documents
-company-logos
-company-documents
-```
-
-Do not expose MinIO administrative credentials to API clients.
-
-## GORM Requirements
-
-Use GORM with PostgreSQL.
-
-Models should include:
-
-* UUID primary keys
-* CreatedAt
-* UpdatedAt
-* Soft deletion where appropriate
-* Foreign-key constraints
-* Unique indexes
-* Composite indexes
-* Check constraints where useful
-
-Use database transactions for:
-
-* Application creation
-* Application status updates
-* Employer verification
-* Job approval
-* File metadata creation with related profile updates
-
-Avoid N+1 queries.
-
-Use preloading only when necessary.
-
-Create explicit repository methods instead of directly querying GORM throughout handlers.
-
-## Suggested Main Entities
-
-Implement these entities:
-
-```text
-users
-refresh_tokens
-student_profiles
-student_educations
-skills
-student_skills
-student_projects
-student_certifications
-student_documents
-
-companies
-employer_members
-company_verification_requests
-company_documents
-
-job_categories
-jobs
-job_skills
-job_questions
-saved_jobs
-
-applications
-interviews
-
-notifications
-reports
-admin_action_logs
-```
-
-## API Standards
-
-Use a consistent JSON response format.
-
-Success response:
-
-```json
-{
-  "success": true,
-  "message": "Job created successfully",
-  "data": {}
-}
-```
-
-Paginated response:
-
-```json
-{
-  "success": true,
-  "message": "Jobs retrieved successfully",
-  "data": [],
-  "pagination": {
-    "page": 1,
-    "page_size": 10,
-    "total_size": 50,
-    "total_pages": 5
-  }
-}
-```
-
-Error response:
-
-```json
-{
-  "success": false,
-  "message": "Validation failed",
-  "errors": {
-    "title": "Title is required"
-  }
-}
-```
-
-Use appropriate HTTP status codes.
-
-## Validation and Security
-
-Implement:
-
-* Request DTO validation
-* Email normalization
-* Password strength validation
-* SQL injection protection through parameterized GORM queries
-* CORS configuration
-* Rate limiting for authentication endpoints
-* Panic recovery
-* Request logging
-* Secure headers
-* File upload restrictions
-* Ownership checks
-* Role authorization
-* Resource-level authorization
-* Environment-secret validation
-* Protection against duplicate submissions
-
-Never trust role, user ID, company ID, or student ID values sent by clients. Read identity from the authenticated token.
-
-## Docker Requirements
-
-Create a multi-stage Dockerfile for the Go API.
-
-Create a Docker Compose configuration containing:
-
-```text
-api
-postgres
-minio
-minio-init
-```
-
-The MinIO initialization service should create the required buckets automatically.
-
-Add health checks for:
-
-* PostgreSQL
-* MinIO
-* API
-
-The API should wait until PostgreSQL and MinIO are healthy.
-
-Use named volumes for:
-
-* PostgreSQL data
-* MinIO data
-
-Provide an `.env.example` containing:
-
-```env
-APP_ENV=development
-APP_PORT=8080
-
-DB_HOST=postgres
-DB_PORT=5432
-DB_NAME=student_job_portal
-DB_USER=postgres
-DB_PASSWORD=postgres
-DB_SSL_MODE=disable
-
-JWT_ACCESS_SECRET=
-JWT_REFRESH_SECRET=
-JWT_ACCESS_EXPIRY=15m
-JWT_REFRESH_EXPIRY=168h
-
-MINIO_ENDPOINT=minio:9000
-MINIO_ACCESS_KEY=
-MINIO_SECRET_KEY=
-MINIO_USE_SSL=false
-
-MINIO_PROFILE_BUCKET=profile-images
-MINIO_STUDENT_DOCUMENT_BUCKET=student-documents
-MINIO_COMPANY_LOGO_BUCKET=company-logos
-MINIO_COMPANY_DOCUMENT_BUCKET=company-documents
-```
-
-Do not hardcode credentials.
-
-## Database Initialization
-
-Create migrations and seed data for:
-
-* Admin user
-* Default roles
-* Job categories
-* Common skills
-* Application statuses
-* Report reasons
-
-The seeded admin password must come from environment variables.
-
-## Testing
-
-Write tests for:
-
-* Registration and login
-* JWT middleware
-* Role authorization
-* Job creation
-* Job search filters
-* Duplicate application prevention
-* Application status transitions
-* Employer ownership checks
-* Student document ownership
-* MinIO upload validation
-* Admin approval workflow
-
-Use mocks where appropriate and integration tests with PostgreSQL for critical repository behavior.
-
-## Documentation
-
-Generate:
-
-* Complete README
-* Local setup instructions
-* Docker commands
-* Environment variable documentation
-* Migration commands
-* Seed commands
-* API endpoint list
-* Example request and response payloads
-* Swagger documentation
-* Architecture explanation
-* Database relationship explanation
-
-## Implementation Order
-
-Build the backend in these phases:
-
-### Phase 1
-
-* Project initialization
-* Docker setup
-* PostgreSQL connection
-* MinIO connection
-* Configuration
-* Common response structure
-* Logging and error handling
-
-### Phase 2
-
-* User model
-* Authentication
-* JWT
-* Refresh tokens
-* RBAC middleware
-
-### Phase 3
-
-* Student profile
-* Education
-* Skills
-* Projects
-* CV and profile-image uploads
-
-### Phase 4
-
-* Companies
-* Employers
-* Employer verification
-
-### Phase 5
-
-* Job creation
-* Job approval
-* Job search and filtering
-* Saved jobs
-
-### Phase 6
-
-* Applications
-* Application answers
-* Status transitions
-* Status history
-* Interview scheduling
-
-### Phase 7
-
-* Notifications
-* Reports
-* Admin dashboard APIs
-* Audit logs
-
-### Phase 8
-
-* Tests
-* Swagger
-* Seed data
-* README
-* Security review
-
-For each phase:
-
-1. Explain the design briefly.
-2. Show the files being created.
-3. Generate complete, compilable code.
-4. Include migrations where required.
-5. Include endpoint examples.
-6. Include tests for important logic.
-7. Verify imports, interfaces, and dependencies.
-8. Do not leave placeholder code such as `TODO`.
-9. Do not skip error handling.
-10. Ensure the project still compiles before moving to the next phase.
-
-Start by generating the project architecture, database entity relationships, Docker Compose file, Dockerfile, environment configuration, Go module dependencies, and Phase 1 implementation.

# .skills/development-mode.md

diff --git a/.skills/development-mode.md b/.skills/development-mode.md
new file mode 100644
index 0000000..3cb8ea5
--- /dev/null
+++ b/.skills/development-mode.md
@@ -0,0 +1,91 @@
+## Development Mode — Keep Verification Lightweight
+
+Work only on the requested scope. Prioritize implementation speed and correctness without performing exhaustive repository-wide validation.
+
+### Testing rules
+
+* Run only **targeted tests directly related to the files/packages/features you changed**.
+* Do **not** run the full test suite (`go test ./...`, full frontend tests, full E2E suite, etc.) unless I explicitly request it.
+* Do not repeatedly rerun tests that already passed unless a subsequent change could reasonably affect them.
+* If there are no existing targeted tests for the changed code, perform a minimal compile/type-check or the smallest relevant verification instead.
+
+### Do NOT perform automatically
+
+Unless explicitly requested, do not:
+
+* run full repository test suites
+* run race detection
+* run coverage analysis
+* replay database migrations multiple times
+* test upgrades from legacy database schemas
+* create temporary PostgreSQL/MySQL databases solely for migration verification
+* perform exhaustive migration compatibility testing
+* run extensive regression suites
+* run `go vet ./...` or equivalent repository-wide linting
+* run repository-wide builds when a package-level build/check is sufficient
+* perform repeated formatting verification across the entire repository
+* update documentation, README files, API guides, or changelogs
+* refactor unrelated code
+* clean up unrelated warnings or technical debt
+* add extra regression tests for unrelated edge cases discovered during implementation
+* investigate speculative edge cases outside the requested feature
+* make unrelated architectural improvements
+
+### Migration changes
+
+When modifying the database:
+
+* implement the required migration
+* verify that the migration is structurally correct
+* run only the minimum test necessary for the requested feature
+* do not simulate historical/legacy upgrades unless specifically requested
+* do not repeatedly migrate fresh databases just to prove idempotency unless the task requires it
+
+### Validation budget
+
+Use this order of verification:
+
+1. Format only changed files if necessary.
+2. Compile/type-check the affected package/module.
+3. Run the smallest relevant targeted test(s).
+4. Stop when those checks pass.
+
+Do not escalate to broader validation unless the targeted checks fail in a way that requires further investigation.
+
+### Failure handling
+
+If a targeted test fails:
+
+* investigate only the failure relevant to the requested change
+* make the smallest necessary fix
+* rerun that targeted test
+* do not automatically broaden into repository-wide debugging
+
+If you discover an unrelated problem, mention it briefly in the final response instead of fixing or deeply investigating it.
+
+### Scope discipline
+
+Do not expand the task beyond what I explicitly requested.
+
+For example, if I ask for:
+
+* a model → don't also implement handlers and frontend
+* an API endpoint → don't redesign adjacent APIs
+* a frontend page → don't modify unrelated pages
+* a bug fix → don't refactor the subsystem
+* a UI redesign → don't change backend behavior
+
+Before modifying a file, it should have a clear reason related to the requested task.
+
+### Completion
+
+Once the requested implementation works and the smallest relevant verification passes, **stop**.
+
+In the final response, report only:
+
+* what was changed
+* which targeted verification was run
+* whether it passed
+* any important issue you intentionally left untouched because it was outside scope
+
+Do not perform additional “just to be safe” validation after the requested task is already working.

# docs/COLLEGE.md

diff --git a/docs/COLLEGE.md b/docs/COLLEGE.md
new file mode 100644
index 0000000..97971c7
--- /dev/null
+++ b/docs/COLLEGE.md
@@ -0,0 +1,188 @@
+> Historical design: college monitoring and admin endpoints are no longer supported.
+> The current application supports only students and employers.
+
+# College API and migration guide
+
+All paths below start with `/api/v1`. Authentication uses the existing access-token
+cookie or `Authorization: Bearer <token>`. The role is `college`, following the
+existing lowercase `student`, `employer`, and `admin` roles.
+
+## Migration and rollout
+
+The API runs `database.AutoMigrate` at startup. It adds colleges, college membership,
+internship progress, evaluations, and certificate metadata with foreign keys and
+unique constraints. Universities are four fixed values returned by `GET /universities`;
+there is no university table or CRUD API.
+
+Existing students keep their free-text `college_name` and a nullable `college_id`.
+Migration does not infer membership from a college name. New profiles require an
+active college, and unassigned students must select one before applying again.
+Once assigned, only an admin can change a student's college.
+
+Existing application statuses remain intact. Migration backfills progress as follows:
+
+| Existing application status | Internship progress |
+| --- | --- |
+| submitted, reviewing, shortlisted | applied |
+| accepted | ongoing; start date uses accepted date or applied date |
+| rejected, withdrawn | cancelled |
+
+Repeating migration preserves completed internships and their evaluations/certificates.
+Before deploying, back up the database and run the migration against a staging copy.
+Create colleges and coordinators through the admin endpoints, then assign existing
+student profiles. College access follows current student membership, including past
+internship records; an admin reassignment moves that monitoring access.
+
+Certificates use the existing private `MINIO_COMPANY_DOCUMENT_BUCKET`. Keep it
+private and configure a MinIO endpoint reachable by browsers for signed URLs.
+No new dependency or storage configuration variable is required.
+
+## College setup
+
+| Method | Path | Access / purpose |
+| --- | --- | --- |
+| GET | `/universities` | Public fixed `{value, label}` options |
+| GET | `/colleges` | Public active colleges; `q`, `page`, `page_size` |
+| GET, POST | `/admin/colleges` | Admin list (including inactive) / create |
+| GET, PUT, DELETE | `/admin/colleges/:id` | Admin read / replace / delete unused college |
+| GET, POST | `/admin/colleges/:id/users` | Admin list / create college coordinators |
+| PUT | `/admin/users/:id/college` | Admin reassign an existing college user |
+| PUT | `/admin/students/:id/college` | Admin assign a **student profile UUID**, not user UUID |
+| GET | `/colleges/me` | Current college and university affiliation |
+
+Create/update college payload:
+
+```json
+{
+  "name": "ABC College",
+  "university": "TRIBHUVAN_UNIVERSITY",
+  "address": "Kathmandu",
+  "contact_email": "internships@example.edu.np",
+  "contact_phone": "01-5550000",
+  "status": "active"
+}
+```
+
+`name` and a supported `university` are required. `status` defaults to `active` and
+accepts `active` or `inactive`. A college with members cannot be deleted; deactivate
+it instead. Deactivation denies college monitoring access and new assignments/applications.
+
+Create coordinator payload: `{"email":"coordinator@example.edu.np","full_name":"Coordinator","password":"Password123!"}`.
+Passwords must be 8–72 bytes. The server sets the role and college. There is no public
+college registration. Coordinators use `/auth/login`; `/auth/me` includes their
+role, `college_id`, and college details.
+
+Assignment payload: `{"college_id":"<college UUID>"}`. Student profile creation at
+`POST /students/me/profile` also accepts `college_id`, with university derived from
+the resolved college. Client-supplied nested college/document objects cannot change
+those related records.
+
+## Monitoring and access
+
+| Method | Path | Purpose |
+| --- | --- | --- |
+| GET | `/colleges/me/dashboard` | College's summary metrics |
+| GET | `/colleges/me/internships` | College's paginated internship records |
+| GET | `/colleges/me/internships/:id` | College's record and outcomes |
+| GET | `/colleges/me/internships/:id/certificate` | Authorized signed certificate URL |
+| GET | `/internship-records` | Paginated records for any authenticated role |
+| GET | `/internship-records/:id` | Record and outcomes for any authenticated role |
+| GET | `/internship-records/:id/certificate` | Authorized signed certificate URL |
+
+Here `:id` is the **application UUID**, which identifies one student at one internship.
+Students see their own records; employers see applications to their own postings;
+colleges see only their current students; admins see all records. College membership
+is reloaded on every request. Shared university affiliation grants no access.
+An out-of-scope record returns `404`; inactive/unassigned college accounts receive `403`.
+
+Record fields include `id`, `student_id`, `student_name`, `college` (with university),
+`program`, `faculty_or_major`, `semester`, `graduation_year`, `internship_id`,
+`company_id` (employer user UUID), `company_name`, `position`, `application_status`,
+`status` (internship progress), `start_date`, `end_date`, `applied_at`, `evaluation_status`,
+`certificate_status`, `evaluation`, and `certificate`. Missing dates/outcomes are null.
+Monitoring responses exclude employer notes, CVs, and private storage keys.
+
+All record lists accept these filters together, before pagination:
+
+| Query | Values / matching |
+| --- | --- |
+| `student_name` | Case-insensitive substring |
+| `program` | Case-insensitive degree or faculty/major substring |
+| `semester` | Case-insensitive exact current semester |
+| `graduation_year` | Existing student graduation year |
+| `company` | Case-insensitive company-name substring |
+| `student_id` | Student profile UUID; still restricted to caller's scope |
+| `status` | applied, ongoing, completed, cancelled |
+| `evaluation_status` | not_due, pending, submitted |
+| `certificate_status` | not_due, pending, issued |
+| `page`, `page_size` | Existing pagination; defaults 1 and 10, size up to 100 |
+
+Responses use the existing `{success, message, data}` envelope. Lists also include
+`pagination: {page, page_size, total_size, total_pages}` with a filtered total.
+
+Dashboard keys are `total_students_in_internships`, `ongoing_internships`,
+`completed_internships`, `pending_evaluations`, `evaluations_received`,
+`pending_certificates`, and `certificates_received`. Students are counted distinctly
+across all internship activity; other metrics count application records.
+Missing evaluations/certificates are `pending` only after completion, otherwise `not_due`.
+
+## Company completion workflow
+
+1. Student applies using existing `POST /internships/:id/apply`.
+2. Employer accepts using `PUT /recruiters/applications/:id/status` with
+   `{"status":"accepted"}`. Progress becomes `ongoing`, with the current start date.
+3. Employer updates dates/completes using `PUT /recruiters/applications/:id/progress`:
+
+   ```json
+   {"status":"completed","start_date":"2026-05-01T00:00:00Z","end_date":"2026-08-01T00:00:00Z"}
+   ```
+
+   Status accepts `ongoing`, `completed`, or `cancelled`. Omitted dates retain their
+   values; a missing final end date defaults to now. End must not precede start,
+   and completion cannot end in the future. Completed/cancelled records are final.
+   Admins can use `PUT /admin/internship-records/:id/progress` with the same payload.
+4. Employer submits `POST /recruiters/applications/:id/evaluation`:
+
+   ```json
+   {"scores":{"technical":90,"communication":85},"remarks":"Delivered reliable work."}
+   ```
+
+   Requires completion, 1–20 named integer scores from 0 to 100, and nonblank remarks
+   up to 10,000 bytes. There is one immutable evaluation per application, with
+   `submitted_by`, `submitted_at`, and status `submitted`.
+5. Employer uploads `POST /recruiters/applications/:id/certificate`, multipart field
+   `file`, containing a PDF up to 10 MiB. Requires completion and an evaluation.
+   One certificate per application stores issuer, filename, size, issue time, and
+   status `issued`. The PDF content type is detected on the server.
+6. Student/company/college read the record to see outcomes. Fetch its certificate
+   endpoint for `{url, expires_in:300}`. Add `?download=true` for attachment download;
+   default is inline viewing. URLs expire after five minutes; a URL already issued
+   remains usable until expiry even if membership changes.
+
+Invalid input returns `400`, unauthorized roles `403`, inaccessible/missing records
+`404`, and invalid lifecycle transitions or duplicate outcomes `409`. College users
+cannot update progress, evaluations, or certificates.
+
+## Verification and development data
+
+Run from `backend`:
+
+```bash
+go test -race ./...
+go vet ./...
+go build ./...
+```
+
+`internal/routes/college_test.go` exercises real routes, JWT checks, repositories,
+database constraints, pagination, same-university college isolation, the outcome
+workflow, and repeat migration. Only the external object store uses a test transport.
+It defaults to in-memory SQLite. To verify PostgreSQL, point the test at an **empty,
+disposable UTF-8 database** (the test writes fixtures and does not clean its tables):
+
+```bash
+COLLEGE_TEST_DATABASE_URL='host=localhost port=5432 user=postgres dbname=college_test sslmode=disable' \
+  go test ./internal/routes -run TestCollegeInternshipWorkflow -count=1
+```
+
+`make db-seed` supplies two colleges in the same university and two coordinators;
+see [seed accounts and data](SEEDING.md). Frontend implementation follows this API contract.

# docs/EMAIL_VERIFICATION.md

diff --git a/docs/EMAIL_VERIFICATION.md b/docs/EMAIL_VERIFICATION.md
new file mode 100644
index 0000000..b70e4f4
--- /dev/null
+++ b/docs/EMAIL_VERIFICATION.md
@@ -0,0 +1,75 @@
+# Account email confirmation
+
+New student and employer accounts must confirm their email before signing in.
+Employers can publish internships after confirming their email and saving a company profile.
+There is no admin approval step.
+
+## Oracle Email Delivery setup
+
+1. Configure an email domain, SPF, DKIM, an approved sender, and SMTP credentials
+   in Oracle Cloud. Use the SMTP endpoint for the approved sender's region.
+2. Set these values in the backend environment (see `.env.example`):
+
+   ```dotenv
+   SMTP_HOST=smtp.email.your-region.oci.oraclecloud.com
+   SMTP_PORT=587
+   SMTP_USERNAME=your-oracle-smtp-username
+   SMTP_PASSWORD=your-oracle-smtp-password
+   SMTP_FROM=no-reply@your-domain.com
+   EMAIL_OTP_SECRET=your-separate-random-secret
+   ```
+
+   Generate the OTP secret with `openssl rand -hex 32`. Keep credentials and the
+   OTP secret on the backend. All API replicas need the same OTP secret.
+   Port 587 requires STARTTLS; port 465 uses direct TLS. Certificate validation
+   is mandatory and sends have an eight-second deadline.
+3. Restart the backend. Production startup rejects missing or invalid mail
+   configuration. Development may start without SMTP, but registration remains
+   unavailable without an OTP secret; there is no confirmation bypass.
+4. Send a signup email to an inbox you control, confirm its code, then sign in.
+   Check Oracle delivery logs/suppressions if accepted mail does not arrive.
+
+Oracle references: [Sending an email](https://docs.oracle.com/en-us/iaas/Content/Email/Reference/gettingstarted_topic-Begin_sending_email.htm),
+[SMTP configuration](https://docs.oracle.com/en-us/iaas/Content/Email/Reference/gettingstarted_topic-Configure_the_SMTP_connection.htm).
+
+## Behavior and API
+
+- `POST /api/v1/auth/register/student` and `/register/employer` return HTTP 201
+  with `user_id`, `email`, `verification_required`, `email_sent`, and
+  `resend_after_seconds`. No session is created. If delivery fails after account
+  creation, `email_sent` is false and the pending account can use resend.
+- `POST /api/v1/auth/verify-email` accepts `{ "email": "...", "code": "000123" }`.
+  The code is a string, exactly six digits. Success consumes it and permits login.
+- `POST /api/v1/auth/resend-verification` accepts `{ "email": "..." }` and uses
+  the same generic HTTP 202 response for missing, confirmed, pending, limited,
+  and temporarily undeliverable accounts. Acceptance does not promise delivery.
+- Codes expire after ten minutes. Each account can request five sends per hour,
+  at least sixty seconds apart. Five incorrect codes cause a one-hour wait.
+  Resending does not reset attempt limits. These limits persist in the database;
+  the existing IP limiter also applies to all auth endpoints.
+- A failed send invalidates its code and still counts toward send limits.
+  Codes and SMTP errors containing message data are never logged. Only an HMAC
+  of each code is stored. Rotating `EMAIL_OTP_SECRET` invalidates outstanding codes.
+- Login and refresh return HTTP 403 with `code: "EMAIL_NOT_VERIFIED"` for
+  unconfirmed accounts. The client sends them to `/verify-email`.
+- Repeated signup returns HTTP 409 without changing existing credentials or role.
+
+## Existing accounts and deployment
+
+The first migration adds `email_verified_at` and grandfathers existing accounts
+in the same transaction. This migration timestamp represents continued access,
+not historical proof of email ownership. Later startups do not confirm pending
+accounts. Development seed accounts are preconfirmed.
+
+A unique index enforces email uniqueness ignoring case and surrounding whitespace.
+Existing collisions stop the migration with an error; resolve them explicitly
+before deployment. The migration never deletes or merges accounts. Back up the
+database before rollout and restart all API replicas together so old code cannot
+issue sessions to new pending accounts.
+
+## Validation
+
+Run `go test -race ./...`. To also exercise PostgreSQL row locks, set
+`OTP_TEST_POSTGRES_DSN` to a **disposable test database** before running the tests.
+The PostgreSQL test runs migrations and inserts accounts; never point it at an
+application database. Unit tests use a fake mail sender and do not send email.

# internal/config/email_test.go

diff --git a/internal/config/email_test.go b/internal/config/email_test.go
new file mode 100644
index 0000000..1bd9983
--- /dev/null
+++ b/internal/config/email_test.go
@@ -0,0 +1,30 @@
+package config
+
+import (
+	"strings"
+	"testing"
+)
+
+func TestEmailConfigRequiresTLSAndSafeAddresses(t *testing.T) {
+	valid := EmailConfig{Host: "smtp.example.com", Port: "587", Username: "user", Password: "password", From: "no-reply@example.com", OTPSecret: strings.Repeat("secret", 6)}
+	if err := valid.Validate(); err != nil {
+		t.Fatal(err)
+	}
+	valid.Port = "465"
+	if err := valid.Validate(); err != nil {
+		t.Fatal(err)
+	}
+	for _, mutate := range []func(*EmailConfig){
+		func(c *EmailConfig) { c.Port = "25" },
+		func(c *EmailConfig) { c.From = "no-reply@example.com\r\nBcc: victim@example.com" },
+		func(c *EmailConfig) { c.Host = "smtp.example.com:25" },
+		func(c *EmailConfig) { c.Password = "" },
+		func(c *EmailConfig) { c.OTPSecret = "short" },
+	} {
+		invalid := valid
+		mutate(&invalid)
+		if err := invalid.Validate(); err == nil {
+			t.Fatal("invalid mail configuration accepted")
+		}
+	}
+}

# internal/database/migrations_test.go

diff --git a/internal/database/migrations_test.go b/internal/database/migrations_test.go
new file mode 100644
index 0000000..6fd8494
--- /dev/null
+++ b/internal/database/migrations_test.go
@@ -0,0 +1,51 @@
+package database
+
+import (
+	"testing"
+	"time"
+
+	"github.com/google/uuid"
+	"github.com/prasanna/student-job-portal/backend/internal/models"
+	"gorm.io/driver/sqlite"
+	"gorm.io/gorm"
+)
+
+func TestEmailVerificationMigrationGrandfathersOnlyExistingUsers(t *testing.T) {
+	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
+	if err != nil {
+		t.Fatal(err)
+	}
+	sqlDB, _ := db.DB()
+	defer sqlDB.Close()
+	if err := db.AutoMigrate(&models.User{}); err != nil {
+		t.Fatal(err)
+	}
+	if err := db.Migrator().DropColumn(&models.User{}, "email_verified_at"); err != nil {
+		t.Fatal(err)
+	}
+	id := uuid.New()
+	if err := db.Exec("INSERT INTO users (id, email, password_hash, role, created_at) VALUES (?, ?, ?, ?, ?)", id, "legacy@example.com", "hash", "student", time.Now()).Error; err != nil {
+		t.Fatal(err)
+	}
+	if err := AutoMigrate(db); err != nil {
+		t.Fatal(err)
+	}
+	var legacy models.User
+	if err := db.First(&legacy, "id = ?", id).Error; err != nil || legacy.EmailVerifiedAt == nil {
+		t.Fatalf("legacy account locked out: %v", err)
+	}
+	pending := &models.User{Email: "pending@example.com", PasswordHash: "hash", Role: models.RoleStudent}
+	if err := db.Create(pending).Error; err != nil {
+		t.Fatal(err)
+	}
+	if err := AutoMigrate(db); err != nil {
+		t.Fatal(err)
+	}
+	if err := db.First(pending, "id = ?", pending.ID).Error; err != nil || pending.EmailVerifiedAt != nil {
+		t.Fatalf("restart verified pending account: %v", err)
+	}
+	duplicate := &models.User{Email: "PENDING@example.com", PasswordHash: "hash", Role: models.RoleEmployer}
+	if err := db.Create(duplicate).Error; err == nil {
+		t.Fatal("canonical duplicate accepted")
+	}
+}

# internal/handlers/auth_handler_test.go

diff --git a/internal/handlers/auth_handler_test.go b/internal/handlers/auth_handler_test.go
new file mode 100644
index 0000000..0bb8c8c
--- /dev/null
+++ b/internal/handlers/auth_handler_test.go
@@ -0,0 +1,133 @@
+package handlers
+
+import (
+	"bytes"
+	"context"
+	"encoding/json"
+	"net/http"
+	"net/http/httptest"
+	"strings"
+	"testing"
+	"time"
+
+	"github.com/gin-gonic/gin"
+	"github.com/google/uuid"
+	"github.com/prasanna/student-job-portal/backend/internal/config"
+	"github.com/prasanna/student-job-portal/backend/internal/database"
+	"github.com/prasanna/student-job-portal/backend/internal/logger"
+	"github.com/prasanna/student-job-portal/backend/internal/models"
+	"github.com/prasanna/student-job-portal/backend/internal/repositories"
+	"github.com/prasanna/student-job-portal/backend/internal/services"
+	"gorm.io/driver/sqlite"
+	"gorm.io/gorm"
+	gormlog "gorm.io/gorm/logger"
+)
+
+type testVerificationSender struct{ code string }
+
+func (s *testVerificationSender) SendVerification(_ context.Context, _, code string) error {
+	s.code = code
+	return nil
+}
+
+func TestRegistrationConfirmationAndLogin(t *testing.T) {
+	gin.SetMode(gin.TestMode)
+	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{Logger: gormlog.Default.LogMode(gormlog.Silent)})
+	if err != nil {
+		t.Fatal(err)
+	}
+	sqlDB, _ := db.DB()
+	defer sqlDB.Close()
+	if err := database.AutoMigrate(db); err != nil {
+		t.Fatal(err)
+	}
+	cfg := &config.Config{JWT: config.JWTConfig{AccessSecret: "test-secret", AccessExpiry: time.Hour, RefreshExpiry: time.Hour}, Email: config.EmailConfig{OTPSecret: strings.Repeat("otp-test", 4)}}
+	log := logger.New("test")
+	svc := services.NewAuthService(repositories.NewUserRepository(db), cfg, log)
+	sender := &testVerificationSender{}
+	svc.Sender = sender
+	handler := NewAuthHandler(svc, log)
+	router := gin.New()
+	router.POST("/register/student", handler.RegisterStudent)
+	router.POST("/register/employer", handler.RegisterEmployer)
+	router.POST("/verify-email", handler.VerifyEmail)
+	router.POST("/resend-verification", handler.ResendVerification)
+	router.POST("/login", handler.Login)
+	router.POST("/refresh", handler.Refresh)
+	request := func(path string, body any, want int) *httptest.ResponseRecorder {
+		t.Helper()
+		payload, _ := json.Marshal(body)
+		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
+		req.Header.Set("Content-Type", "application/json")
+		response := httptest.NewRecorder()
+		router.ServeHTTP(response, req)
+		if response.Code != want {
+			t.Fatalf("%s: got %d want %d: %s", path, response.Code, want, response.Body)
+		}
+		return response
+	}
+	for _, role := range []string{"student", "employer"} {
+		email := role + "@example.com"
+		credentials := gin.H{"email": email, "password": "Password123!"}
+		response := request("/register/"+role, gin.H{"email": email, "password": "Password123!", "full_name": "New User"}, 201)
+		if len(response.Result().Cookies()) != 0 || !strings.Contains(response.Body.String(), `"verification_required":true`) {
+			t.Fatal("registration should require verification without cookies")
+		}
+		response = request("/login", credentials, 403)
+		if len(response.Result().Cookies()) != 0 || !strings.Contains(response.Body.String(), "EMAIL_NOT_VERIFIED") {
+			t.Fatal("unverified login issued cookies or lost error code")
+		}
+		request("/login", gin.H{"email": email, "password": "wrong-password"}, 401)
+		user, _ := svc.Repo.GetByEmail(email)
+		// A stale/externally created refresh token must not bypass verification.
+		if err := svc.Repo.SaveRefreshToken(user.ID, "test-refresh-"+role, time.Now().Add(time.Hour)); err != nil {
+			t.Fatal(err)
+		}
+		request("/refresh", gin.H{"refresh_token": "test-refresh-" + role}, 403)
+		request("/verify-email", gin.H{"email": email, "code": "12e345"}, 400)
+		request("/verify-email", gin.H{"email": email, "code": sender.code}, 200)
+		response = request("/login", credentials, 200)
+		if len(response.Result().Cookies()) != 2 {
+			t.Fatal("verified login did not set session cookies")
+		}
+		request("/verify-email", gin.H{"email": email, "code": sender.code}, 400)
+		// Email confirmation must not create an employer profile.
+		var count int64
+		if err := db.Model(&models.RecruiterProfile{}).Where("user_id = ?", user.ID).Count(&count).Error; err != nil || count != 0 {
+			t.Fatalf("confirmation created an employer profile: %v", err)
+		}
+	}
+	// Retired admin accounts cannot log in, renew old tokens, or obtain new ones.
+	student, err := svc.Repo.GetByEmail("student@example.com")
+	if err != nil {
+		t.Fatal(err)
+	}
+	now := time.Now().UTC()
+	legacy := &models.User{Email: "legacy-admin@example.com", PasswordHash: student.PasswordHash, Role: "admin", EmailVerifiedAt: &now}
+	if err := db.Create(legacy).Error; err != nil {
+		t.Fatal(err)
+	}
+	request("/login", gin.H{"email": legacy.Email, "password": "Password123!"}, 403)
+	if err := svc.Repo.SaveRefreshToken(legacy.ID, "legacy-admin-refresh", time.Now().Add(time.Hour)); err != nil {
+		t.Fatal(err)
+	}
+	request("/refresh", gin.H{"refresh_token": "legacy-admin-refresh"}, 403)
+	if _, err := svc.GenerateAccessToken(legacy); err == nil {
+		t.Fatal("retired admin obtained an access token")
+	}
+	if _, err := svc.GenerateRefreshToken(legacy); err == nil {
+		t.Fatal("retired admin obtained a refresh token")
+	}
+	unknown := request("/resend-verification", gin.H{"email": "unknown@example.com"}, 202).Body.String()
+	verified := request("/resend-verification", gin.H{"email": "student@example.com"}, 202).Body.String()
+	if unknown != verified {
+		t.Fatal("resend response reveals existing accounts")
+	}
+	request("/register/student", gin.H{"email": "pending@example.com", "password": "Password123!", "full_name": "New User"}, 201)
+	cooldown := request("/resend-verification", gin.H{"email": "pending@example.com"}, 202).Body.String()
+	if cooldown != unknown {
+		t.Fatal("resend limits reveal pending accounts")
+	}
+	request("/register/employer", gin.H{"email": "PENDING@example.com", "password": "Different123!", "full_name": "Changed"}, 409)
+	request("/register/student", gin.H{"email": "empty@example.com", "password": "Password123!", "full_name": "   "}, 400)
+}

# internal/models/email_verification.go

diff --git a/internal/models/email_verification.go b/internal/models/email_verification.go
new file mode 100644
index 0000000..ae2cda0
--- /dev/null
+++ b/internal/models/email_verification.go
@@ -0,0 +1,20 @@
+package models
+
+import (
+	"time"
+
+	"github.com/google/uuid"
+)
+
+// EmailVerification keeps account limits across replacement codes and server restarts.
+type EmailVerification struct {
+	UserID             uuid.UUID `gorm:"type:uuid;primaryKey"`
+	User               User      `gorm:"constraint:OnDelete:CASCADE"`
+	CodeHash           string    `gorm:"not null" json:"-"`
+	ExpiresAt          time.Time
+	LastSentAt         *time.Time
+	SendWindowStart    time.Time
+	SendCount          int
+	AttemptWindowStart time.Time
+	FailedAttempts     int
+}

# internal/repositories/email_verification_repository.go

diff --git a/internal/repositories/email_verification_repository.go b/internal/repositories/email_verification_repository.go
new file mode 100644
index 0000000..8a80801
--- /dev/null
+++ b/internal/repositories/email_verification_repository.go
@@ -0,0 +1,113 @@
+package repositories
+
+import (
+	"crypto/subtle"
+	"errors"
+	"time"
+
+	"github.com/google/uuid"
+	"github.com/prasanna/student-job-portal/backend/internal/models"
+	"gorm.io/gorm"
+	"gorm.io/gorm/clause"
+)
+
+var (
+	ErrInvalidVerificationCode  = errors.New("code is invalid or expired; request a new code if needed")
+	ErrVerificationCooldown     = errors.New("wait 60 seconds before requesting another code")
+	ErrVerificationSendLimit    = errors.New("too many verification emails; try again in an hour")
+	ErrVerificationAttemptLimit = errors.New("too many incorrect codes; try again in an hour")
+	ErrAlreadyVerified          = errors.New("email is already verified")
+)
+
+func (r *UserRepository) CreatePendingUser(user *models.User) error {
+	return r.DB.Transaction(func(tx *gorm.DB) error {
+		if err := tx.Create(user).Error; err != nil {
+			return err
+		}
+		return tx.Create(&models.EmailVerification{UserID: user.ID}).Error
+	})
+}
+
+func (r *UserRepository) ReserveVerificationCode(userID uuid.UUID, hash string, now time.Time) error {
+	return r.DB.Transaction(func(tx *gorm.DB) error {
+		// Both send and verify lock the user first, serializing account changes across replicas.
+		var user models.User
+		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", userID).Error; err != nil {
+			return err
+		}
+		if user.CanSignIn() {
+			return ErrAlreadyVerified
+		}
+		var verification models.EmailVerification
+		if err := tx.First(&verification, "user_id = ?", userID).Error; err != nil {
+			return err
+		}
+		//compare with last timestamp to check the limit
+		if verification.LastSentAt != nil && now.Sub(*verification.LastSentAt) < time.Minute {
+			return ErrVerificationCooldown
+		}
+		//resets verification after 1 hour
+		if now.Sub(verification.SendWindowStart) >= time.Hour {
+			verification.SendWindowStart, verification.SendCount = now, 0
+		}
+		if now.Sub(verification.AttemptWindowStart) >= time.Hour {
+			verification.AttemptWindowStart, verification.FailedAttempts = now, 0
+		}
+		//checks the limit, if it exceeds count 5, restrict
+		if verification.SendCount >= 5 {
+			return ErrVerificationSendLimit
+		}
+		if verification.FailedAttempts >= 5 {
+			return ErrVerificationAttemptLimit
+		}
+		//set the verified values
+		verification.CodeHash, verification.ExpiresAt, verification.LastSentAt = hash, now.Add(10*time.Minute), &now
+		verification.SendCount++
+		return tx.Save(&verification).Error
+	})
+}
+
+func (r *UserRepository) VerifyEmailCode(userID uuid.UUID, hash string, now time.Time) error {
+	var result error
+	err := r.DB.Transaction(func(tx *gorm.DB) error {
+		var user models.User
+		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", userID).Error; err != nil {
+			return err
+		}
+		if user.CanSignIn() {
+			return ErrInvalidVerificationCode
+		}
+		var verification models.EmailVerification
+		if err := tx.First(&verification, "user_id = ?", userID).Error; err != nil {
+			return err
+		}
+		if verification.CodeHash == "" || !now.Before(verification.ExpiresAt) {
+			return ErrInvalidVerificationCode
+		}
+		//resets the attemptwindowstart and failed attemp once 1 hour is passed after last attempt
+		if now.Sub(verification.AttemptWindowStart) >= time.Hour {
+			verification.AttemptWindowStart, verification.FailedAttempts = now, 0
+		}
+		if verification.FailedAttempts >= 5 {
+			return ErrVerificationAttemptLimit
+		}
+		//verify the code hash, if failed increase the failed attempt
+		if subtle.ConstantTimeCompare([]byte(verification.CodeHash), []byte(hash)) != 1 {
+			verification.FailedAttempts++
+			result = ErrInvalidVerificationCode
+			if verification.FailedAttempts >= 5 {
+				result = ErrVerificationAttemptLimit
+			}
+			// Return nil after saving so wrong guesses commit instead of rolling back.
+			return tx.Save(&verification).Error
+		}
+		if err := tx.Model(&user).Update("email_verified_at", now).Error; err != nil {
+			return err
+		}
+		return tx.Model(&verification).Update("code_hash", "").Error
+	})
+	if err != nil {
+		return err
+	}
+	return result
+}

# internal/repositories/internship_repository_test.go

diff --git a/internal/repositories/internship_repository_test.go b/internal/repositories/internship_repository_test.go
new file mode 100644
index 0000000..5bef976
--- /dev/null
+++ b/internal/repositories/internship_repository_test.go
@@ -0,0 +1,49 @@
+package repositories
+
+import (
+	"context"
+	"testing"
+
+	"github.com/google/uuid"
+	"github.com/prasanna/student-job-portal/backend/internal/enums"
+	"github.com/prasanna/student-job-portal/backend/internal/models"
+)
+
+func TestLegacyEmployerApprovalDoesNotRestrictPublicInternships(t *testing.T) {
+	db := openApplicationRepositoryTestDB(t)
+	repo := NewInternshipRepository(db)
+	// Existing databases retain this retired column, but it must not affect access.
+	if err := db.Exec("ALTER TABLE recruiter_profiles ADD COLUMN verification_status TEXT DEFAULT 'pending'").Error; err != nil {
+		t.Fatal(err)
+	}
+	for _, status := range []string{"approved", "pending", "rejected", "draft"} {
+		t.Run(status, func(t *testing.T) {
+			profile := models.RecruiterProfile{
+				UserID: uuid.New(), OrganizationName: "TechKarkhana Nepal",
+				OrganizationWebsite: "https://techkarkhana.example",
+				OrganizationAddress: "Profile address",
+			}
+			if err := db.Create(&profile).Error; err != nil {
+				t.Fatal(err)
+			}
+			if err := db.Model(&profile).Update("verification_status", status).Error; err != nil {
+				t.Fatal(err)
+			}
+			internship := createRepositoryTestInternship(t, db, profile.UserID, "Backend Intern")
+			if err := repo.LoadPublicIssuer(context.Background(), internship); err != nil {
+				t.Fatal(err)
+			}
+			issuer := internship.Issuer
+			if issuer.OrganizationName != profile.OrganizationName || issuer.OrganizationWebsite != profile.OrganizationWebsite {
+				t.Fatalf("public issuer fields not loaded: %+v", issuer)
+			}
+			if issuer.OrganizationAddress != "" || issuer.User != nil {
+				t.Fatalf("extra issuer fields loaded: %+v", issuer)
+			}
+			results, total, err := repo.Search(context.Background(), InternshipSearchFilter{EmployerID: &profile.UserID, Status: enums.InternshipStatusPublished})
+			if err != nil || total != 1 || len(results) != 1 || results[0].ID != internship.ID {
+				t.Fatalf("legacy status %s hid a published internship: total=%d err=%v", status, total, err)
+			}
+		})
+	}
+}

# internal/routes/core_workflow_test.go

diff --git a/internal/routes/core_workflow_test.go b/internal/routes/core_workflow_test.go
new file mode 100644
index 0000000..d7d6d25
--- /dev/null
+++ b/internal/routes/core_workflow_test.go
@@ -0,0 +1,319 @@
+package routes_test
+
+import (
+	"bytes"
+	"encoding/json"
+	"fmt"
+	"image"
+	"image/png"
+	"io"
+	"mime/multipart"
+	"net/http"
+	"net/http/httptest"
+	"strings"
+	"testing"
+	"time"
+
+	"github.com/gin-gonic/gin"
+	"github.com/golang-jwt/jwt/v5"
+	"github.com/google/uuid"
+	"github.com/minio/minio-go/v7"
+	"github.com/minio/minio-go/v7/pkg/credentials"
+	"github.com/prasanna/student-job-portal/backend/internal/config"
+	"github.com/prasanna/student-job-portal/backend/internal/database"
+	"github.com/prasanna/student-job-portal/backend/internal/logger"
+	"github.com/prasanna/student-job-portal/backend/internal/models"
+	"github.com/prasanna/student-job-portal/backend/internal/repositories"
+	"github.com/prasanna/student-job-portal/backend/internal/routes"
+	"github.com/prasanna/student-job-portal/backend/internal/seeder"
+	"github.com/prasanna/student-job-portal/backend/internal/services"
+	"gorm.io/driver/sqlite"
+	"gorm.io/gorm"
+)
+
+type storageTransport func(*http.Request) (*http.Response, error)
+
+func (f storageTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
+
+func TestCoreRecruitmentWorkflow(t *testing.T) {
+	gin.SetMode(gin.TestMode)
+	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared&_foreign_keys=on"), &gorm.Config{})
+	if err != nil {
+		t.Fatal(err)
+	}
+	sqlDB, _ := db.DB()
+	defer sqlDB.Close()
+	if err := database.AutoMigrate(db); err != nil {
+		t.Fatal(err)
+	}
+	if _, err := seeder.Run(db, ""); err != nil {
+		t.Fatal(err)
+	}
+	cfg := &config.Config{}
+	cfg.JWT.AccessSecret, cfg.JWT.AccessExpiry = "test-secret", time.Hour
+	cfg.Email.OTPSecret = strings.Repeat("test-secret-", 4)
+	cfg.MinIO.StudentDocBucket = "student-documents"
+	cfg.MinIO.ProfileBucket = "profiles"
+	log := logger.New("test")
+	puts, deletes := 0, 0
+	failUploads := false
+	storage, err := minio.New("storage.test", &minio.Options{Secure: true, Region: "us-east-1", Creds: credentials.NewStaticV4("key", "secret", ""), Transport: storageTransport(func(req *http.Request) (*http.Response, error) {
+		switch req.Method {
+		case http.MethodPut:
+			if failUploads {
+				return &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
+			}
+			puts++
+			_, _ = io.Copy(io.Discard, req.Body)
+		case http.MethodDelete:
+			deletes++
+		default:
+			return nil, fmt.Errorf("unexpected storage request: %s", req.Method)
+		}
+		return &http.Response{StatusCode: 200, Header: http.Header{"Etag": {"test"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
+	})})
+	if err != nil {
+		t.Fatal(err)
+	}
+	router := gin.New()
+	routes.Register(router, db, cfg, log, storage)
+	auth := services.NewAuthService(repositories.NewUserRepository(db), cfg, log)
+	token := func(email string) string {
+		t.Helper()
+		user, err := repositories.NewUserRepository(db).GetByEmail(email)
+		if err != nil {
+			t.Fatal(err)
+		}
+		value, err := auth.GenerateAccessToken(user)
+		if err != nil {
+			t.Fatal(err)
+		}
+		return value
+	}
+	employer, student, other := token(seeder.EmployerEmail), token(seeder.StudentEmail), token(seeder.StudentTwoEmail)
+	request := func(method, path, token string, payload interface{}, want int) json.RawMessage {
+		t.Helper()
+		var body io.Reader
+		contentType := "application/json"
+		if data, ok := payload.([]byte); ok {
+			var buf bytes.Buffer
+			writer := multipart.NewWriter(&buf)
+			field := "file"
+			if strings.HasSuffix(path, "/logo") {
+				field = "logo"
+			}
+			part, _ := writer.CreateFormFile(field, "upload.png")
+			_, _ = part.Write(data)
+			_ = writer.Close()
+			body = &buf
+			contentType = writer.FormDataContentType()
+		} else if payload != nil {
+			data, err := json.Marshal(payload)
+			if err != nil {
+				t.Fatal(err)
+			}
+			body = bytes.NewReader(data)
+		}
+		req := httptest.NewRequest(method, "/api/v1"+path, body)
+		req.Header.Set("Content-Type", contentType)
+		if token != "" {
+			req.Header.Set("Authorization", "Bearer "+token)
+		}
+		response := httptest.NewRecorder()
+		router.ServeHTTP(response, req)
+		if response.Code != want {
+			t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, response.Code, want, response.Body)
+		}
+		var result struct {
+			Data json.RawMessage `json:"data"`
+		}
+		_ = json.Unmarshal(response.Body.Bytes(), &result)
+		return result.Data
+	}
+	// Server-rendered pages validate sessions on every request without consuming login attempts.
+	for i := 0; i < 12; i++ {
+		request("GET", "/auth/me", student, nil, 200)
+	}
+	request("GET", "/auth/me", "", nil, 401)
+	request("GET", "/auth/me", student+"invalid", nil, 401)
+	cfg.JWT.AccessExpiry = -time.Minute
+	expired := token(seeder.StudentEmail)
+	cfg.JWT.AccessExpiry = time.Hour
+	request("GET", "/auth/me", expired, nil, 401)
+
+	legacyAdmin, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
+		"sub": uuid.NewString(), "role": "admin", "exp": time.Now().Add(time.Hour).Unix(),
+	}).SignedString([]byte(cfg.JWT.AccessSecret))
+	if err != nil {
+		t.Fatal(err)
+	}
+	for _, path := range []string{"/auth/me", "/recruiters/me/profile", "/students/me/profile"} {
+		request("GET", path, legacyAdmin, nil, 403)
+	}
+	// Fresh employers must save a company profile before posting an internship.
+	request("POST", "/auth/register/employer", "", map[string]string{"email": "new.employer@example.com", "password": "new.employer@example.com", "full_name": "New Employer"}, 201)
+	// Email confirmation itself is covered by the auth tests; this fixture exercises hiring.
+	if err := db.Model(&models.User{}).Where("email = ?", "new.employer@example.com").Update("email_verified_at", time.Now().UTC()).Error; err != nil {
+		t.Fatal(err)
+	}
+	newEmployer := token("new.employer@example.com")
+	posting := map[string]string{"title": "First internship", "description": "A useful internship for a student project.", "work_mode": "remote", "status": "published"}
+	request("POST", "/recruiters/me/internships", newEmployer, posting, 400)
+	request("PUT", "/recruiters/me/profile", newEmployer, map[string]string{"organization_name": "New Company"}, 200)
+	var firstPosting models.Internship
+	if err := json.Unmarshal(request("POST", "/recruiters/me/internships", newEmployer, posting, 201), &firstPosting); err != nil || firstPosting.Status != "published" {
+		t.Fatalf("first posting=%s: %v", firstPosting.Status, err)
+	}
+
+	request("POST", "/students/me/profile", student, map[string]interface{}{"full_name": "Student", "college_name": "Any college", "degree": "BCA"}, 200)
+	var internship models.Internship
+	if err := db.Where("title = ?", "Product Design Intern").First(&internship).Error; err != nil {
+		t.Fatal(err)
+	}
+	applyPath := "/internships/" + internship.ID.String() + "/apply"
+	request("POST", applyPath, student, nil, 400) // A CV is required.
+	request("PUT", "/students/me/resume", student, []byte("this is not a PDF"), 400)
+	pdf := []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\n%%EOF")
+	request("PUT", "/students/me/resume", student, pdf, 200)
+	request("PUT", "/students/me/resume", student, pdf, 200)
+	var count int64
+	db.Model(&models.StudentDocument{}).Count(&count)
+	if count != 1 || puts != 2 || deletes != 1 {
+		t.Fatalf("CV replacement: rows=%d puts=%d deletes=%d", count, puts, deletes)
+	}
+	request("GET", "/students/me/resume", student, nil, 200)
+	var app models.InternshipApplication
+	if err := json.Unmarshal(request("POST", applyPath, student, nil, 201), &app); err != nil {
+		t.Fatal(err)
+	}
+	if app.Status != models.ApplicationStatusSubmitted {
+		t.Fatalf("application status=%s", app.Status)
+	}
+	request("POST", applyPath, student, nil, 409)
+	decisionPath := "/recruiters/applications/" + app.ID.String() + "/status"
+	request("PUT", decisionPath, token("seed.employer.pending@jobportal.test"), map[string]string{"status": "accepted"}, 403)
+	request("PATCH", "/students/me/applications/"+app.ID.String()+"/withdraw", other, nil, 403)
+	request("PUT", decisionPath, employer, map[string]string{"status": "reviewing"}, 400)
+	request("PUT", decisionPath, employer, map[string]string{"status": "accepted"}, 200)
+	request("PUT", decisionPath, employer, map[string]string{"status": "rejected"}, 400)
+	request("PATCH", "/students/me/applications/"+app.ID.String()+"/withdraw", student, nil, 400)
+	request("GET", "/recruiters/applications/"+app.ID.String(), employer, nil, 200)
+	request("GET", "/students/me/applications?status=submitted", student, nil, 200)
+	request("GET", "/students/me/applications?status=pending", student, nil, 400)
+	var submitted models.InternshipApplication
+	if err := db.Where("student_id = ? AND status = ?", app.StudentID, "submitted").First(&submitted).Error; err != nil {
+		t.Fatal(err)
+	}
+	request("PATCH", "/students/me/applications/"+submitted.ID.String()+"/withdraw", student, nil, 200)
+	// Employers can publish and edit immediately, without an approval step.
+	secondEmployer := token("seed.employer.pending@jobportal.test")
+	var published models.Internship
+	data := request("POST", "/recruiters/me/internships", secondEmployer, posting, 201)
+	if err := json.Unmarshal(data, &published); err != nil || published.Status != "published" {
+		t.Fatalf("employer posting=%s: %v", published.Status, err)
+	}
+	postingPath := "/recruiters/me/internships/" + published.ID.String()
+	request("PUT", postingPath, employer, map[string]string{"status": "private"}, 403)
+	request("PUT", postingPath, secondEmployer, map[string]string{"status": "private"}, 200)
+	request("PUT", postingPath, secondEmployer, map[string]string{"status": "published"}, 200)
+	request("PUT", "/recruiters/me/profile", secondEmployer, map[string]string{"organization_name": "Updated company"}, 200)
+	if err := db.First(&published, "id = ?", published.ID).Error; err != nil || published.Status != "published" {
+		t.Fatalf("updated posting=%s: %v", published.Status, err)
+	}
+	request("GET", "/internships/"+published.ID.String(), "", nil, 200)
+	if data := request("GET", "/internships?q=First", "", nil, 200); !strings.Contains(string(data), published.ID.String()) {
+		t.Fatal("new employer's published internship is missing from search")
+	}
+	for _, authToken := range []string{"", student, employer} {
+		request("GET", "/admin/users", authToken, nil, 404)
+		request("GET", "/admin/employers", authToken, nil, 404)
+		request("PUT", "/admin/employers/"+published.IssuedBy.String()+"/approval", authToken, map[string]string{"status": "approved"}, 404)
+	}
+	request("DELETE", "/students/me/resume", "", nil, 401)
+	request("DELETE", "/students/me/resume", employer, nil, 403)
+	request("DELETE", "/students/me/resume", other, nil, 200)
+	if data := request("GET", "/students/me/resume", student, nil, 200); string(data) == "null" {
+		t.Fatal("deleting another student's CV removed the current student's CV")
+	}
+	request("DELETE", "/students/me/resume", student, nil, 200)
+	if data := request("GET", "/students/me/resume", student, nil, 200); string(data) != "null" {
+		t.Fatalf("CV remains after deletion: %s", data)
+	}
+	if deletes != 2 {
+		t.Fatalf("CV was not removed from storage: deletes=%d", deletes)
+	}
+	request("DELETE", "/students/me/resume", student, nil, 200)
+	if deletes != 2 {
+		t.Fatalf("repeat deletion removed unexpected storage objects: deletes=%d", deletes)
+	}
+	request("PUT", "/students/me/resume", student, pdf, 200)
+	if data := request("GET", "/students/me/resume", student, nil, 200); string(data) == "null" {
+		t.Fatal("CV upload after deletion did not restore the CV")
+	}
+
+	var picture bytes.Buffer
+	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
+		t.Fatal(err)
+	}
+	for _, upload := range []struct{ path, profilePath, token, wrongRole, method, field string }{
+		{"/students/me/profile/picture", "/students/me/profile", student, employer, "POST", "profile_picture"},
+		{"/recruiters/me/profile/logo", "/recruiters/me/profile", employer, student, "PUT", "organization_logo"},
+	} {
+		t.Run(upload.field, func(t *testing.T) {
+			initialPuts, initialDeletes := puts, deletes
+			request("POST", upload.path, "", picture.Bytes(), 401)
+			request("POST", upload.path, upload.wrongRole, picture.Bytes(), 403)
+			request("POST", upload.path, upload.token, []byte("<svg>not an allowed image</svg>"), 400)
+			request("POST", upload.path, upload.token, []byte{}, 400)
+			request("POST", upload.path, upload.token, bytes.Repeat([]byte("x"), (5<<20)+1), 400)
+			request("POST", upload.path, upload.token, picture.Bytes(), 200)
+			var result map[string]interface{}
+			if err := json.Unmarshal(request("GET", upload.profilePath, upload.token, nil, 200), &result); err != nil {
+				t.Fatal(err)
+			}
+			firstURL, _ := result[upload.field].(string)
+			if !strings.Contains(firstURL, "X-Amz-Signature=") || !strings.Contains(firstURL, "/profiles/") {
+				t.Fatalf("missing signed picture URL: %s", firstURL)
+			}
+			request("POST", upload.path, upload.token, picture.Bytes(), 200)
+			if puts != initialPuts+2 || deletes != initialDeletes+1 {
+				t.Fatalf("picture replacement: puts=%d deletes=%d", puts-initialPuts, deletes-initialDeletes)
+			}
+			if err := json.Unmarshal(request("GET", upload.profilePath, upload.token, nil, 200), &result); err != nil {
+				t.Fatal(err)
+			}
+			url := result[upload.field]
+			if url == firstURL {
+				t.Fatal("replacement did not change the picture")
+			}
+			failUploads = true
+			request("POST", upload.path, upload.token, picture.Bytes(), 500)
+			failUploads = false
+			result["full_name"], result["organization_name"] = "Updated name", "Updated company"
+			result[upload.field] = "https://untrusted.test/image.png"
+			result["profile_picture_key"] = "another-user/image.png"
+			if err := json.Unmarshal(request(upload.method, upload.profilePath, upload.token, result, 200), &result); err != nil {
+				t.Fatal(err)
+			}
+			if result[upload.field] != url {
+				t.Fatal("profile edits or failed uploads replaced the saved image")
+			}
+			if puts != initialPuts+2 || deletes != initialDeletes+1 {
+				t.Fatal("failed upload changed stored images")
+			}
+		})
+	}
+
+	// Internship cards, details, and student applications share the uploaded logo.
+	request("PUT", "/recruiters/me/internships/"+internship.ID.String(), employer, map[string]string{"status": "published"}, 200)
+	for _, path := range []string{"/internships", "/internships/" + internship.ID.String(), "/students/me/applications"} {
+		data := request("GET", path, student, nil, 200)
+		if !strings.Contains(string(data), `"organization_logo":"https://storage.test/profiles/organization-logos/`) || !strings.Contains(string(data), "X-Amz-Signature=") {
+			t.Fatalf("missing signed company logo in %s", path)
+		}
+	}
+
+	for _, path := range []string{"/colleges", "/internship-records", "/recruiters/me/verification", "/recruiters/interviews", "/students/me/documents", "/admin/organization-verifications"} {
+		request("GET", path, employer, nil, 404)
+	}
+}

# internal/services/email_sender.go

diff --git a/internal/services/email_sender.go b/internal/services/email_sender.go
new file mode 100644
index 0000000..6f1f270
--- /dev/null
+++ b/internal/services/email_sender.go
@@ -0,0 +1,111 @@
+package services
+
+import (
+	"context"
+	"crypto/tls"
+	"fmt"
+	"net"
+	"net/mail"
+	"net/smtp"
+	"strings"
+	"time"
+
+	"github.com/prasanna/student-job-portal/backend/internal/config"
+	"github.com/prasanna/student-job-portal/backend/internal/models"
+)
+
+type VerificationSender interface {
+	SendVerification(context.Context, string, string) error
+}
+
+type SMTPVerificationSender struct{ Config config.EmailConfig }
+
+func (s SMTPVerificationSender) SendVerification(ctx context.Context, to, code string) error {
+	if len(code) != 6 || strings.IndexFunc(code, func(r rune) bool { return r < '0' || r > '9' }) != -1 {
+		return fmt.Errorf("invalid verification code")
+	}
+	return s.send(ctx, to, "Confirm your Student Job Portal account", fmt.Sprintf("Your confirmation code is: %s\r\n\r\nThis code expires in 10 minutes. Only the latest code works.\r\nEnter it only if you created this account. Do not share it.\r\nIf you did not create this account, ignore this email.\r\n", code))
+}
+
+type ApplicationStatusSender interface {
+	SendApplicationStatus(context.Context, string, string, models.InternshipApplicationStatus, string) error
+}
+
+func (s SMTPVerificationSender) SendApplicationStatus(ctx context.Context, to, title string, status models.InternshipApplicationStatus, note string) error {
+	if status != models.ApplicationStatusAccepted && status != models.ApplicationStatusRejected {
+		return fmt.Errorf("invalid application decision")
+	}
+	body := fmt.Sprintf("Your application for %s has been %s.\r\n", title, status)
+	if note != "" {
+		body += "\r\nMessage from recruiter:\r\n" + note + "\r\n"
+	}
+	body += "\r\nSign in to Student Job Portal to view your applications.\r\n"
+	return s.send(ctx, to, "Your application status has changed", body)
+}
+
+func (s SMTPVerificationSender) send(ctx context.Context, to, subject, body string) error {
+	if err := s.Config.Validate(); err != nil {
+		return err
+	}
+	address, err := mail.ParseAddress(to)
+	if err != nil || address.Address != to || strings.ContainsAny(to, "\r\n") {
+		return fmt.Errorf("invalid recipient")
+	}
+	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
+	defer cancel()
+	dialer := &net.Dialer{}
+	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(s.Config.Host, s.Config.Port))
+	if err != nil {
+		return err
+	}
+	defer conn.Close()
+	deadline, _ := ctx.Deadline()
+	if err := conn.SetDeadline(deadline); err != nil {
+		return err
+	}
+	rawConn := conn
+	stop := context.AfterFunc(ctx, func() { _ = rawConn.Close() })
+	defer stop()
+	tlsConfig := &tls.Config{ServerName: s.Config.Host, MinVersion: tls.VersionTLS12}
+	if s.Config.Port == "465" {
+		secure := tls.Client(conn, tlsConfig)
+		if err := secure.HandshakeContext(ctx); err != nil {
+			return err
+		}
+		conn = secure
+	}
+	client, err := smtp.NewClient(conn, s.Config.Host)
+	if err != nil {
+		return err
+	}
+	defer client.Close()
+	if s.Config.Port == "587" {
+		if err := client.StartTLS(tlsConfig); err != nil {
+			return err
+		}
+	}
+	if err := client.Auth(smtp.PlainAuth("", s.Config.Username, s.Config.Password, s.Config.Host)); err != nil {
+		return err
+	}
+	if err := client.Mail(s.Config.From); err != nil {
+		return err
+	}
+	if err := client.Rcpt(to); err != nil {
+		return err
+	}
+	writer, err := client.Data()
+	if err != nil {
+		return err
+	}
+	body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")
+	message := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s", s.Config.From, to, subject, time.Now().UTC().Format(time.RFC1123Z), body)
+	if _, err := writer.Write([]byte(message)); err != nil {
+		return err
+	}
+	if err := writer.Close(); err != nil {
+		return err
+	}
+	// DATA acceptance is success even if the server closes before QUIT.
+	_ = client.Quit()
+	return nil
+}

# internal/services/email_sender_test.go

diff --git a/internal/services/email_sender_test.go b/internal/services/email_sender_test.go
new file mode 100644
index 0000000..be1bbe4
--- /dev/null
+++ b/internal/services/email_sender_test.go
@@ -0,0 +1,70 @@
+package services
+
+import (
+	"bufio"
+	"context"
+	"fmt"
+	"net"
+	"strings"
+	"testing"
+	"time"
+
+	"github.com/prasanna/student-job-portal/backend/internal/config"
+	"github.com/prasanna/student-job-portal/backend/internal/models"
+)
+
+func TestSMTPRejectsUnencryptedServer(t *testing.T) {
+	listener, err := net.Listen("tcp", "127.0.0.1:587")
+	if err != nil {
+		t.Skipf("local SMTP submission port unavailable: %v", err)
+	}
+	defer listener.Close()
+	commands := make(chan string, 1)
+	go func() {
+		conn, err := listener.Accept()
+		if err != nil {
+			commands <- ""
+			return
+		}
+		defer conn.Close()
+		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
+		_, _ = fmt.Fprint(conn, "220 localhost ESMTP\r\n")
+		reader := bufio.NewReader(conn)
+		var transcript strings.Builder
+		for {
+			line, err := reader.ReadString('\n')
+			if err != nil {
+				break
+			}
+			transcript.WriteString(line)
+			if strings.HasPrefix(line, "EHLO") {
+				_, _ = fmt.Fprint(conn, "250 localhost\r\n")
+			} else {
+				_, _ = fmt.Fprint(conn, "454 TLS unavailable\r\n")
+			}
+		}
+		commands <- transcript.String()
+	}()
+	sender := SMTPVerificationSender{Config: config.EmailConfig{Host: "127.0.0.1", Port: "587", Username: "secret-user", Password: "secret-password", From: "no-reply@example.com", OTPSecret: strings.Repeat("test", 8)}}
+	if err := sender.SendVerification(context.Background(), "recipient@example.com", "000123"); err == nil {
+		t.Fatal("unencrypted SMTP server accepted")
+	}
+	transcript := <-commands
+	if strings.Contains(transcript, "AUTH") || strings.Contains(transcript, "MAIL FROM") || strings.Contains(transcript, "000123") {
+		t.Fatal("credentials or message sent before TLS")
+	}
+}
+
+func TestApplicationStatusEmailValidation(t *testing.T) {
+	sender := SMTPVerificationSender{Config: config.EmailConfig{Host: "smtp.example.com", Port: "587", Username: "test", Password: "test", From: "no-reply@example.com", OTPSecret: strings.Repeat("test", 8)}}
+	for _, status := range []models.InternshipApplicationStatus{models.ApplicationStatusSubmitted, models.ApplicationStatusWithdrawn, "unknown"} {
+		if err := sender.SendApplicationStatus(context.Background(), "student@example.com", "Intern", status, ""); err == nil {
+			t.Fatalf("invalid decision %q accepted", status)
+		}
+	}
+	for _, to := range []string{"invalid", "student@example.com\r\nBcc: other@example.com"} {
+		if err := sender.SendApplicationStatus(context.Background(), to, "Intern", models.ApplicationStatusAccepted, ""); err == nil {
+			t.Fatalf("invalid recipient %q accepted", to)
+		}
+	}
+}

# internal/services/email_verification_service.go

diff --git a/internal/services/email_verification_service.go b/internal/services/email_verification_service.go
new file mode 100644
index 0000000..58ee345
--- /dev/null
+++ b/internal/services/email_verification_service.go
@@ -0,0 +1,99 @@
+package services
+
+import (
+	"context"
+	"crypto/hmac"
+	"crypto/rand"
+	"crypto/sha256"
+	"encoding/hex"
+	"errors"
+	"fmt"
+	"math/big"
+	"strings"
+	"time"
+
+	"github.com/google/uuid"
+	"github.com/prasanna/student-job-portal/backend/internal/models"
+	"github.com/prasanna/student-job-portal/backend/internal/repositories"
+	"gorm.io/gorm"
+)
+
+var (
+	ErrEmailNotVerified = errors.New("confirm your email before signing in")
+	ErrEmailExists      = errors.New("an account with this email already exists; sign in to continue or confirm your email")
+	ErrEmailUnavailable = errors.New("email delivery is not configured")
+)
+
+func (s *AuthService) Register(ctx context.Context, email, password, name string, role models.Role) (*models.User, bool, error) {
+	if len(s.Cfg.Email.OTPSecret) < 32 {
+		return nil, false, ErrEmailUnavailable
+	}
+	email = strings.ToLower(strings.TrimSpace(email))
+	if _, err := s.Repo.GetByEmail(email); err == nil {
+		return nil, false, ErrEmailExists
+	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
+		return nil, false, err
+	}
+	hash, err := s.HashPassword(password)
+	if err != nil {
+		return nil, false, err
+	}
+	user := &models.User{Email: email, PasswordHash: hash, FullName: strings.TrimSpace(name), Role: role}
+	if err := s.Repo.CreatePendingUser(user); err != nil {
+		// A concurrent signup can win the unique email constraint.
+		if _, lookupErr := s.Repo.GetByEmail(email); lookupErr == nil {
+			return nil, false, ErrEmailExists
+		}
+		return nil, false, err
+	}
+	err = s.SendVerification(ctx, user)
+	return user, err == nil, nil
+}
+
+func (s *AuthService) verificationHash(userID uuid.UUID, code string) string {
+	mac := hmac.New(sha256.New, []byte(s.Cfg.Email.OTPSecret))
+	_, _ = mac.Write([]byte("email-verification:" + userID.String() + ":" + code))
+	return hex.EncodeToString(mac.Sum(nil))
+}
+
+func (s *AuthService) SendVerification(ctx context.Context, user *models.User) error {
+	if len(s.Cfg.Email.OTPSecret) < 32 {
+		return ErrEmailUnavailable
+	}
+	value, err := rand.Int(rand.Reader, big.NewInt(1000000))
+	if err != nil {
+		return err
+	}
+	code := fmt.Sprintf("%06d", value.Int64())
+	hash := s.verificationHash(user.ID, code)
+	if err := s.Repo.ReserveVerificationCode(user.ID, hash, time.Now().UTC()); err != nil {
+		return err
+	}
+	if err := s.Sender.SendVerification(ctx, user.Email, code); err != nil {
+		// A failed/ambiguous send must not leave a usable code, or clear a newer resend.
+		if clearErr := s.Repo.DB.Model(&models.EmailVerification{}).Where("user_id = ? AND code_hash = ?", user.ID, hash).Update("code_hash", "").Error; clearErr != nil {
+			s.Logger.Error("failed to invalidate email verification for user %s", user.ID)
+		}
+		// SMTP errors can contain addresses or message content; do not log them.
+		s.Logger.Error("verification email delivery failed for user %s", user.ID)
+		return ErrEmailUnavailable
+	}
+	return nil
+}
+
+func (s *AuthService) VerifyEmail(email, code string) error {
+	if len(s.Cfg.Email.OTPSecret) < 32 {
+		return ErrEmailUnavailable
+	}
+	if len(code) != 6 || strings.IndexFunc(code, func(r rune) bool { return r < '0' || r > '9' }) != -1 {
+		return repositories.ErrInvalidVerificationCode
+	}
+	user, err := s.Repo.GetByEmail(email)
+	if errors.Is(err, gorm.ErrRecordNotFound) {
+		return repositories.ErrInvalidVerificationCode
+	}
+	if err != nil {
+		return err
+	}
+	return s.Repo.VerifyEmailCode(user.ID, s.verificationHash(user.ID, code), time.Now().UTC())
+}

# internal/services/email_verification_test.go

diff --git a/internal/services/email_verification_test.go b/internal/services/email_verification_test.go
new file mode 100644
index 0000000..9cd836c
--- /dev/null
+++ b/internal/services/email_verification_test.go
@@ -0,0 +1,289 @@
+package services
+
+import (
+	"context"
+	"errors"
+	"strings"
+	"sync"
+	"testing"
+	"time"
+
+	"github.com/google/uuid"
+	"github.com/prasanna/student-job-portal/backend/internal/config"
+	"github.com/prasanna/student-job-portal/backend/internal/database"
+	"github.com/prasanna/student-job-portal/backend/internal/logger"
+	"github.com/prasanna/student-job-portal/backend/internal/models"
+	"github.com/prasanna/student-job-portal/backend/internal/repositories"
+	"gorm.io/driver/postgres"
+	"gorm.io/driver/sqlite"
+	"gorm.io/gorm"
+	gormlog "gorm.io/gorm/logger"
+	"os"
+)
+
+type capturedVerification struct {
+	mu    sync.Mutex
+	code  string
+	fail  bool
+	sends int
+}
+
+func (s *capturedVerification) SendVerification(_ context.Context, _, code string) error {
+	s.mu.Lock()
+	defer s.mu.Unlock()
+	s.code, s.sends = code, s.sends+1
+	if s.fail {
+		return errors.New("SMTP unavailable")
+	}
+	return nil
+}
+
+func verificationTestService(t *testing.T, dialector gorm.Dialector) (*AuthService, *capturedVerification) {
+	t.Helper()
+	db, err := gorm.Open(dialector, &gorm.Config{Logger: gormlog.Default.LogMode(gormlog.Silent)})
+	if err != nil {
+		t.Fatal(err)
+	}
+	sqlDB, _ := db.DB()
+	if db.Dialector.Name() == "sqlite" {
+		sqlDB.SetMaxOpenConns(1)
+	}
+	t.Cleanup(func() { _ = sqlDB.Close() })
+	if err := database.AutoMigrate(db); err != nil {
+		t.Fatal(err)
+	}
+	cfg := &config.Config{JWT: config.JWTConfig{AccessSecret: "test-jwt", AccessExpiry: time.Hour, RefreshExpiry: time.Hour}, Email: config.EmailConfig{OTPSecret: strings.Repeat("test-otp-", 4)}}
+	svc := NewAuthService(repositories.NewUserRepository(db), cfg, logger.New("test"))
+	sender := &capturedVerification{}
+	svc.Sender = sender
+	return svc, sender
+}
+
+func sqliteVerificationService(t *testing.T) (*AuthService, *capturedVerification) {
+	return verificationTestService(t, sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared&_foreign_keys=on"))
+}
+
+func TestAccountEmailVerification(t *testing.T) {
+	for _, role := range []models.Role{models.RoleStudent, models.RoleEmployer} {
+		t.Run(string(role), func(t *testing.T) {
+			svc, sender := sqliteVerificationService(t)
+			user, sent, err := svc.Register(context.Background(), "New.User@EXAMPLE.COM", "Password123!", " New User ", role)
+			if err != nil || !sent || user.Email != "new.user@example.com" || user.FullName != "New User" || user.EmailVerifiedAt != nil {
+				t.Fatalf("registration: user=%+v sent=%v err=%v", user, sent, err)
+			}
+			if len(sender.code) != 6 || strings.Trim(sender.code, "0123456789") != "" {
+				t.Fatal("code must have exactly six digits")
+			}
+			var verification models.EmailVerification
+			if err := svc.Repo.DB.First(&verification, "user_id = ?", user.ID).Error; err != nil {
+				t.Fatal(err)
+			}
+			if verification.CodeHash == sender.code || len(verification.CodeHash) != 64 {
+				t.Fatal("code must only be stored as HMAC")
+			}
+			if _, err := svc.GenerateAccessToken(user); !errors.Is(err, ErrEmailNotVerified) {
+				t.Fatalf("pending access token: %v", err)
+			}
+			if _, err := svc.GenerateRefreshToken(user); !errors.Is(err, ErrEmailNotVerified) {
+				t.Fatalf("pending refresh token: %v", err)
+			}
+			if _, _, err := svc.Register(context.Background(), user.Email, "Different123!", "Changed", models.RoleEmployer); !errors.Is(err, ErrEmailExists) {
+				t.Fatalf("duplicate registration: %v", err)
+			}
+			unchanged, _ := svc.Repo.GetByEmail(user.Email)
+			if unchanged.Role != role || unchanged.FullName != "New User" || svc.ComparePassword(unchanged.PasswordHash, "Password123!") != nil {
+				t.Fatal("duplicate changed existing account")
+			}
+			if err := svc.VerifyEmail("NEW.USER@example.com", sender.code); err != nil {
+				t.Fatal(err)
+			}
+			confirmed, _ := svc.Repo.GetByID(user.ID)
+			if confirmed.EmailVerifiedAt == nil {
+				t.Fatal("account not confirmed")
+			}
+			if _, err := svc.GenerateAccessToken(confirmed); err != nil {
+				t.Fatal(err)
+			}
+			if _, err := svc.GenerateRefreshToken(confirmed); err != nil {
+				t.Fatal(err)
+			}
+			if err := svc.VerifyEmail(user.Email, sender.code); !errors.Is(err, repositories.ErrInvalidVerificationCode) {
+				t.Fatalf("code replay: %v", err)
+			}
+		})
+	}
+}
+
+func TestVerificationReplacementAndLimits(t *testing.T) {
+	svc, sender := sqliteVerificationService(t)
+	user, _, err := svc.Register(context.Background(), "limits@example.com", "Password123!", "User", models.RoleStudent)
+	if err != nil {
+		t.Fatal(err)
+	}
+	now := time.Now().UTC()
+	if err := svc.SendVerification(context.Background(), user); !errors.Is(err, repositories.ErrVerificationCooldown) {
+		t.Fatalf("cooldown: %v", err)
+	}
+	// A deterministic leading-zero code exercises the string contract.
+	if err := svc.Repo.ReserveVerificationCode(user.ID, svc.verificationHash(user.ID, "000123"), now.Add(time.Minute)); err != nil {
+		t.Fatal(err)
+	}
+	wrong := "111111"
+	if err := svc.VerifyEmail(user.Email, wrong); !errors.Is(err, repositories.ErrInvalidVerificationCode) {
+		t.Fatalf("wrong code: %v", err)
+	}
+	if err := svc.VerifyEmail(user.Email, "000123"); err != nil {
+		t.Fatalf("leading zero: %v", err)
+	}
+
+	user, _, err = svc.Register(context.Background(), "attempts@example.com", "Password123!", "User", models.RoleStudent)
+	if err != nil {
+		t.Fatal(err)
+	}
+	for i := 0; i < 5; i++ {
+		err := svc.Repo.VerifyEmailCode(user.ID, "wrong-hash", now.Add(time.Duration(i)*time.Second))
+		want := repositories.ErrInvalidVerificationCode
+		if i == 4 {
+			want = repositories.ErrVerificationAttemptLimit
+		}
+		if !errors.Is(err, want) {
+			t.Fatalf("attempt %d: %v", i+1, err)
+		}
+	}
+	if err := svc.VerifyEmail(user.Email, sender.code); !errors.Is(err, repositories.ErrVerificationAttemptLimit) {
+		t.Fatalf("correct code after lockout: %v", err)
+	}
+	if err := svc.Repo.ReserveVerificationCode(user.ID, "replacement", now.Add(2*time.Minute)); !errors.Is(err, repositories.ErrVerificationAttemptLimit) {
+		t.Fatalf("resend bypassed attempts: %v", err)
+	}
+	if err := svc.Repo.ReserveVerificationCode(user.ID, "replacement", now.Add(time.Hour+time.Minute)); err != nil {
+		t.Fatalf("limit did not expire: %v", err)
+	}
+
+	user, _, err = svc.Register(context.Background(), "sends@example.com", "Password123!", "User", models.RoleEmployer)
+	if err != nil {
+		t.Fatal(err)
+	}
+	oldCode := sender.code
+	for i := 1; i < 5; i++ {
+		if err := svc.Repo.ReserveVerificationCode(user.ID, svc.verificationHash(user.ID, "000456"), now.Add(time.Duration(i+1)*time.Minute)); err != nil {
+			t.Fatal(err)
+		}
+	}
+	if err := svc.VerifyEmail(user.Email, oldCode); !errors.Is(err, repositories.ErrInvalidVerificationCode) {
+		t.Fatalf("replaced code: %v", err)
+	}
+	if err := svc.Repo.ReserveVerificationCode(user.ID, "sixth", now.Add(6*time.Minute)); !errors.Is(err, repositories.ErrVerificationSendLimit) {
+		t.Fatalf("send cap: %v", err)
+	}
+	if err := svc.Repo.VerifyEmailCode(user.ID, svc.verificationHash(user.ID, "000456"), now.Add(15*time.Minute)); !errors.Is(err, repositories.ErrInvalidVerificationCode) {
+		t.Fatalf("expired code: %v", err)
+	}
+}
+
+func TestFailedDeliveryIsRecoverable(t *testing.T) {
+	svc, sender := sqliteVerificationService(t)
+	sender.fail = true
+	user, sent, err := svc.Register(context.Background(), "failure@example.com", "Password123!", "User", models.RoleStudent)
+	if err != nil || sent || user == nil {
+		t.Fatalf("registration must remain recoverable: %v", err)
+	}
+	if err := svc.VerifyEmail(user.Email, sender.code); !errors.Is(err, repositories.ErrInvalidVerificationCode) {
+		t.Fatalf("failed delivery code remains usable: %v", err)
+	}
+	sender.fail = false
+	past := time.Now().UTC().Add(-2 * time.Minute)
+	if err := svc.Repo.DB.Model(&models.EmailVerification{}).Where("user_id = ?", user.ID).Update("last_sent_at", past).Error; err != nil {
+		t.Fatal(err)
+	}
+	if err := svc.SendVerification(context.Background(), user); err != nil {
+		t.Fatal(err)
+	}
+	if err := svc.VerifyEmail(user.Email, sender.code); err != nil {
+		t.Fatal(err)
+	}
+}
+
+func testConcurrentVerification(t *testing.T, svc *AuthService, sender *capturedVerification) {
+	t.Helper()
+	user, _, err := svc.Register(context.Background(), uuid.NewString()+"@example.com", "Password123!", "User", models.RoleStudent)
+	if err != nil {
+		t.Fatal(err)
+	}
+	results := make(chan error, 8)
+	for i := 0; i < cap(results); i++ {
+		go func() { results <- svc.VerifyEmail(user.Email, sender.code) }()
+	}
+	successes := 0
+	for i := 0; i < cap(results); i++ {
+		err := <-results
+		if err == nil {
+			successes++
+		} else if !errors.Is(err, repositories.ErrInvalidVerificationCode) {
+			t.Fatal(err)
+		}
+	}
+	if successes != 1 {
+		t.Fatalf("code used %d times", successes)
+	}
+	user, _, err = svc.Register(context.Background(), uuid.NewString()+"@example.com", "Password123!", "User", models.RoleStudent)
+	if err != nil {
+		t.Fatal(err)
+	}
+	code := sender.code
+	for i := 0; i < cap(results); i++ {
+		go func() { results <- svc.Repo.VerifyEmailCode(user.ID, "incorrect", time.Now().UTC()) }()
+	}
+	for i := 0; i < cap(results); i++ {
+		if err := <-results; !errors.Is(err, repositories.ErrInvalidVerificationCode) && !errors.Is(err, repositories.ErrVerificationAttemptLimit) {
+			t.Fatal(err)
+		}
+	}
+	if err := svc.VerifyEmail(user.Email, code); !errors.Is(err, repositories.ErrVerificationAttemptLimit) {
+		t.Fatalf("parallel attempts bypassed limit: %v", err)
+	}
+	var verification models.EmailVerification
+	if err := svc.Repo.DB.First(&verification, "user_id = ?", user.ID).Error; err != nil || verification.FailedAttempts != 5 {
+		t.Fatalf("attempt count=%d: %v", verification.FailedAttempts, err)
+	}
+
+	user, _, err = svc.Register(context.Background(), uuid.NewString()+"@example.com", "Password123!", "User", models.RoleEmployer)
+	if err != nil {
+		t.Fatal(err)
+	}
+	for i := 0; i < cap(results); i++ {
+		go func() { results <- svc.SendVerification(context.Background(), user) }()
+	}
+	for i := 0; i < cap(results); i++ {
+		if err := <-results; !errors.Is(err, repositories.ErrVerificationCooldown) {
+			t.Fatalf("parallel resend: %v", err)
+		}
+	}
+	verification = models.EmailVerification{}
+	if err := svc.Repo.DB.First(&verification, "user_id = ?", user.ID).Error; err != nil || verification.SendCount != 1 {
+		t.Fatal(err)
+	}
+}
+
+func TestConcurrentVerification(t *testing.T) {
+	svc, sender := sqliteVerificationService(t)
+	testConcurrentVerification(t, svc, sender)
+}
+
+func TestPostgresConcurrentVerification(t *testing.T) {
+	dsn := os.Getenv("OTP_TEST_POSTGRES_DSN")
+	if dsn == "" {
+		t.Skip("set OTP_TEST_POSTGRES_DSN to a disposable database to test PostgreSQL row locks")
+	}
+	svc, sender := verificationTestService(t, postgres.Open(dsn))
+	testConcurrentVerification(t, svc, sender)
+	// Canonical uniqueness must also be enforced for direct/concurrent writes.
+	first := &models.User{Email: uuid.NewString() + "@example.com", PasswordHash: "hash", Role: models.RoleStudent}
+	if err := svc.Repo.Create(first); err != nil {
+		t.Fatal(err)
+	}
+	duplicate := &models.User{Email: strings.ToUpper(first.Email), PasswordHash: "hash", Role: models.RoleEmployer}
+	if err := svc.Repo.Create(duplicate); err == nil {
+		t.Fatal("case-insensitive duplicate accepted")
+	}
+}

# internal/services/profile_picture.go

diff --git a/internal/services/profile_picture.go b/internal/services/profile_picture.go
new file mode 100644
index 0000000..a5de57b
--- /dev/null
+++ b/internal/services/profile_picture.go
@@ -0,0 +1,43 @@
+package services
+
+import (
+	"errors"
+	"io"
+	"mime/multipart"
+	"net/http"
+)
+
+const maxProfilePictureSize = 5 << 20
+
+var ErrInvalidProfilePicture = errors.New("upload a JPEG, PNG or WebP image of at most 5 MiB")
+
+func readProfilePicture(file *multipart.FileHeader) ([]byte, string, string, error) {
+	if file == nil || file.Size <= 0 || file.Size > maxProfilePictureSize {
+		return nil, "", "", ErrInvalidProfilePicture
+	}
+	reader, err := file.Open()
+	if err != nil {
+		return nil, "", "", err
+	}
+	defer reader.Close()
+	data, err := io.ReadAll(io.LimitReader(reader, maxProfilePictureSize+1))
+	if err != nil {
+		return nil, "", "", err
+	}
+	if len(data) == 0 || len(data) > maxProfilePictureSize {
+		return nil, "", "", ErrInvalidProfilePicture
+	}
+	contentType := http.DetectContentType(data)
+	var extension string
+	switch contentType {
+	case "image/jpeg":
+		extension = ".jpg"
+	case "image/png":
+		extension = ".png"
+	case "image/webp":
+		extension = ".webp"
+	default:
+		return nil, "", "", ErrInvalidProfilePicture
+	}
+	return data, contentType, extension, nil
+}

# run.sh

diff --git a/run.sh b/run.sh
new file mode 100755
index 0000000..73d4c28
--- /dev/null
+++ b/run.sh
@@ -0,0 +1 @@
+go run ./cmd/api/main.go

