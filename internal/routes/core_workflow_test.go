package routes_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/prasanna/student-job-portal/backend/internal/config"
	"github.com/prasanna/student-job-portal/backend/internal/database"
	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
	"github.com/prasanna/student-job-portal/backend/internal/routes"
	"github.com/prasanna/student-job-portal/backend/internal/seeder"
	"github.com/prasanna/student-job-portal/backend/internal/services"
	portalstorage "github.com/prasanna/student-job-portal/backend/internal/storage"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type storageTransport func(*http.Request) (*http.Response, error)

func (f storageTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCoreRecruitmentWorkflow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared&_foreign_keys=on"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if _, err := seeder.Run(db, ""); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.JWT.AccessSecret, cfg.JWT.AccessExpiry = "test-secret", time.Hour
	cfg.Email.OTPSecret = strings.Repeat("test-secret-", 4)
	cfg.MinIO.StudentDocBucket = "student-documents"
	cfg.MinIO.ProfileBucket = "profiles"
	cfg.MinIO.Endpoint, cfg.MinIO.PublicURL = "minio:9000", "https://files.test"
	cfg.MinIO.AccessKey, cfg.MinIO.SecretKey = "key", "secret"
	log := logger.New("test")
	puts, deletes := 0, 0
	failUploads := false
	storage, err := minio.New("storage.test", &minio.Options{Secure: true, Region: "us-east-1", Creds: credentials.NewStaticV4("key", "secret", ""), Transport: storageTransport(func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case http.MethodPut:
			if failUploads {
				return &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
			}
			puts++
			_, _ = io.Copy(io.Discard, req.Body)
		case http.MethodDelete:
			deletes++
		default:
			return nil, fmt.Errorf("unexpected storage request: %s", req.Method)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Etag": {"test"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	signer, err := portalstorage.NewURLSigner(&cfg.MinIO)
	if err != nil {
		t.Fatal(err)
	}
	routes.Register(router, db, cfg, log, storage, signer)
	auth := services.NewAuthService(repositories.NewUserRepository(db), cfg, log)
	token := func(email string) string {
		t.Helper()
		user, err := repositories.NewUserRepository(db).GetByEmail(email)
		if err != nil {
			t.Fatal(err)
		}
		value, err := auth.GenerateAccessToken(user)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	employer, student, other := token(seeder.EmployerEmail), token(seeder.StudentEmail), token(seeder.StudentTwoEmail)
	request := func(method, path, token string, payload interface{}, want int) json.RawMessage {
		t.Helper()
		var body io.Reader
		contentType := "application/json"
		if data, ok := payload.([]byte); ok {
			var buf bytes.Buffer
			writer := multipart.NewWriter(&buf)
			field := "file"
			if strings.HasSuffix(path, "/logo") {
				field = "logo"
			}
			part, _ := writer.CreateFormFile(field, "upload.png")
			_, _ = part.Write(data)
			_ = writer.Close()
			body = &buf
			contentType = writer.FormDataContentType()
		} else if payload != nil {
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			body = bytes.NewReader(data)
		}
		req := httptest.NewRequest(method, "/api/v1"+path, body)
		req.Header.Set("Content-Type", contentType)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, response.Code, want, response.Body)
		}
		var result struct {
			Data json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(response.Body.Bytes(), &result)
		return result.Data
	}
	// Server-rendered pages validate sessions on every request without consuming login attempts.
	for i := 0; i < 12; i++ {
		request("GET", "/auth/me", student, nil, 200)
	}
	request("GET", "/auth/me", "", nil, 401)
	request("GET", "/auth/me", student+"invalid", nil, 401)
	cfg.JWT.AccessExpiry = -time.Minute
	expired := token(seeder.StudentEmail)
	cfg.JWT.AccessExpiry = time.Hour
	request("GET", "/auth/me", expired, nil, 401)

	legacyAdmin, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": uuid.NewString(), "role": "admin", "exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte(cfg.JWT.AccessSecret))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/auth/me", "/recruiters/me/profile", "/students/me/profile"} {
		request("GET", path, legacyAdmin, nil, 403)
	}
	// Fresh employers must save a company profile before posting an internship.
	request("POST", "/auth/register/employer", "", map[string]string{"email": "new.employer@example.com", "password": "new.employer@example.com", "full_name": "New Employer"}, 201)
	// Email confirmation itself is covered by the auth tests; this fixture exercises hiring.
	if err := db.Model(&models.User{}).Where("email = ?", "new.employer@example.com").Update("email_verified_at", time.Now().UTC()).Error; err != nil {
		t.Fatal(err)
	}
	newEmployer := token("new.employer@example.com")
	posting := map[string]string{"title": "First internship", "description": "A useful internship for a student project.", "work_mode": "remote", "status": "published"}
	request("POST", "/recruiters/me/internships", newEmployer, posting, 400)
	request("PUT", "/recruiters/me/profile", newEmployer, map[string]string{"organization_name": "New Company"}, 200)
	var firstPosting models.Internship
	if err := json.Unmarshal(request("POST", "/recruiters/me/internships", newEmployer, posting, 201), &firstPosting); err != nil || firstPosting.Status != "published" {
		t.Fatalf("first posting=%s: %v", firstPosting.Status, err)
	}
	posting["status"] = "private"
	var draft models.Internship
	if err := json.Unmarshal(request("POST", "/recruiters/me/internships", newEmployer, posting, 201), &draft); err != nil {
		t.Fatal(err)
	}
	request("GET", "/internships/"+draft.ID.String(), "", nil, 404)
	request("GET", "/internships/"+draft.ID.String(), newEmployer, nil, 404)
	request("GET", "/recruiters/me/internships/"+draft.ID.String(), "", nil, 401)
	request("GET", "/recruiters/me/internships/"+draft.ID.String(), student, nil, 403)
	request("GET", "/recruiters/me/internships/"+draft.ID.String(), employer, nil, 404)
	request("GET", "/recruiters/me/internships/"+draft.ID.String(), newEmployer, nil, 200)
	posting["status"] = "published"

	request("POST", "/students/me/profile", student, map[string]interface{}{"full_name": "Student", "college_name": "Any college", "degree": "BCA"}, 200)
	var internship models.Internship
	if err := db.Where("title = ?", "Product Design Intern").First(&internship).Error; err != nil {
		t.Fatal(err)
	}
	applyPath := "/internships/" + internship.ID.String() + "/apply"
	request("POST", applyPath, student, nil, 400) // A CV is required.
	request("PUT", "/students/me/resume", student, []byte("this is not a PDF"), 400)
	pdf := []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\n%%EOF")
	request("PUT", "/students/me/resume", student, pdf, 200)
	request("PUT", "/students/me/resume", student, pdf, 200)
	var count int64
	db.Model(&models.StudentDocument{}).Count(&count)
	if count != 1 || puts != 2 || deletes != 1 {
		t.Fatalf("CV replacement: rows=%d puts=%d deletes=%d", count, puts, deletes)
	}
	request("GET", "/students/me/resume", student, nil, 200)
	var app models.InternshipApplication
	if err := json.Unmarshal(request("POST", applyPath, student, nil, 201), &app); err != nil {
		t.Fatal(err)
	}
	if app.Status != models.ApplicationStatusSubmitted {
		t.Fatalf("application status=%s", app.Status)
	}
	request("POST", applyPath, student, nil, 409)
	decisionPath := "/recruiters/applications/" + app.ID.String() + "/status"
	request("PUT", decisionPath, token("seed.employer.pending@jobportal.test"), map[string]string{"status": "accepted"}, 403)
	request("PATCH", "/students/me/applications/"+app.ID.String()+"/withdraw", other, nil, 403)
	request("PUT", decisionPath, employer, map[string]string{"status": "reviewing"}, 400)
	request("PUT", decisionPath, employer, map[string]string{"status": "accepted"}, 200)
	request("PUT", decisionPath, employer, map[string]string{"status": "rejected"}, 400)
	request("PATCH", "/students/me/applications/"+app.ID.String()+"/withdraw", student, nil, 400)
	request("GET", "/recruiters/applications/"+app.ID.String(), employer, nil, 200)
	request("GET", "/students/me/applications?status=submitted", student, nil, 200)
	request("GET", "/students/me/applications?status=pending", student, nil, 400)
	var submitted models.InternshipApplication
	if err := db.Where("student_id = ? AND status = ?", app.StudentID, "submitted").First(&submitted).Error; err != nil {
		t.Fatal(err)
	}
	request("PATCH", "/students/me/applications/"+submitted.ID.String()+"/withdraw", student, nil, 200)
	// Employers can publish and edit immediately, without an approval step.
	secondEmployer := token("seed.employer.pending@jobportal.test")
	var published models.Internship
	data := request("POST", "/recruiters/me/internships", secondEmployer, posting, 201)
	if err := json.Unmarshal(data, &published); err != nil || published.Status != "published" {
		t.Fatalf("employer posting=%s: %v", published.Status, err)
	}
	postingPath := "/recruiters/me/internships/" + published.ID.String()
	request("PUT", postingPath, employer, map[string]string{"status": "private"}, 403)
	request("PUT", postingPath, secondEmployer, map[string]string{"status": "private"}, 200)
	request("PUT", postingPath, secondEmployer, map[string]string{"status": "published"}, 200)
	request("PUT", "/recruiters/me/profile", secondEmployer, map[string]string{"organization_name": "Updated company"}, 200)
	if err := db.First(&published, "id = ?", published.ID).Error; err != nil || published.Status != "published" {
		t.Fatalf("updated posting=%s: %v", published.Status, err)
	}
	request("GET", "/internships/"+published.ID.String(), "", nil, 200)
	if data := request("GET", "/internships?q=First", "", nil, 200); !strings.Contains(string(data), published.ID.String()) {
		t.Fatal("new employer's published internship is missing from search")
	}
	for _, authToken := range []string{"", student, employer} {
		request("GET", "/admin/users", authToken, nil, 404)
		request("GET", "/admin/employers", authToken, nil, 404)
		request("PUT", "/admin/employers/"+published.IssuedBy.String()+"/approval", authToken, map[string]string{"status": "approved"}, 404)
	}
	request("DELETE", "/students/me/resume", "", nil, 401)
	request("DELETE", "/students/me/resume", employer, nil, 403)
	request("DELETE", "/students/me/resume", other, nil, 200)
	if data := request("GET", "/students/me/resume", student, nil, 200); string(data) == "null" {
		t.Fatal("deleting another student's CV removed the current student's CV")
	}
	request("DELETE", "/students/me/resume", student, nil, 200)
	if data := request("GET", "/students/me/resume", student, nil, 200); string(data) != "null" {
		t.Fatalf("CV remains after deletion: %s", data)
	}
	if deletes != 2 {
		t.Fatalf("CV was not removed from storage: deletes=%d", deletes)
	}
	request("DELETE", "/students/me/resume", student, nil, 200)
	if deletes != 2 {
		t.Fatalf("repeat deletion removed unexpected storage objects: deletes=%d", deletes)
	}
	request("PUT", "/students/me/resume", student, pdf, 200)
	if data := request("GET", "/students/me/resume", student, nil, 200); string(data) == "null" {
		t.Fatal("CV upload after deletion did not restore the CV")
	}

	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	for _, upload := range []struct{ path, profilePath, token, wrongRole, method, field string }{
		{"/students/me/profile/picture", "/students/me/profile", student, employer, "POST", "profile_picture"},
		{"/recruiters/me/profile/logo", "/recruiters/me/profile", employer, student, "PUT", "organization_logo"},
	} {
		t.Run(upload.field, func(t *testing.T) {
			initialPuts, initialDeletes := puts, deletes
			request("POST", upload.path, "", picture.Bytes(), 401)
			request("POST", upload.path, upload.wrongRole, picture.Bytes(), 403)
			request("POST", upload.path, upload.token, []byte("<svg>not an allowed image</svg>"), 400)
			request("POST", upload.path, upload.token, []byte{}, 400)
			request("POST", upload.path, upload.token, bytes.Repeat([]byte("x"), (5<<20)+1), 400)
			request("POST", upload.path, upload.token, picture.Bytes(), 200)
			var result map[string]interface{}
			if err := json.Unmarshal(request("GET", upload.profilePath, upload.token, nil, 200), &result); err != nil {
				t.Fatal(err)
			}
			firstURL, _ := result[upload.field].(string)
			if !strings.Contains(firstURL, "X-Amz-Signature=") || !strings.Contains(firstURL, "/profiles/") {
				t.Fatalf("missing signed picture URL: %s", firstURL)
			}
			request("POST", upload.path, upload.token, picture.Bytes(), 200)
			if puts != initialPuts+2 || deletes != initialDeletes+1 {
				t.Fatalf("picture replacement: puts=%d deletes=%d", puts-initialPuts, deletes-initialDeletes)
			}
			if err := json.Unmarshal(request("GET", upload.profilePath, upload.token, nil, 200), &result); err != nil {
				t.Fatal(err)
			}
			url := result[upload.field]
			if url == firstURL {
				t.Fatal("replacement did not change the picture")
			}
			failUploads = true
			request("POST", upload.path, upload.token, picture.Bytes(), 500)
			failUploads = false
			result["full_name"], result["organization_name"] = "Updated name", "Updated company"
			result[upload.field] = "https://untrusted.test/image.png"
			result["profile_picture_key"] = "another-user/image.png"
			if err := json.Unmarshal(request(upload.method, upload.profilePath, upload.token, result, 200), &result); err != nil {
				t.Fatal(err)
			}
			if result[upload.field] != url {
				t.Fatal("profile edits or failed uploads replaced the saved image")
			}
			if puts != initialPuts+2 || deletes != initialDeletes+1 {
				t.Fatal("failed upload changed stored images")
			}
		})
	}

	// Internship cards, details, and student applications share the uploaded logo.
	request("PUT", "/recruiters/me/internships/"+internship.ID.String(), employer, map[string]string{"status": "published"}, 200)
	for _, path := range []string{"/internships", "/internships/" + internship.ID.String(), "/students/me/applications"} {
		data := request("GET", path, student, nil, 200)
		if !strings.Contains(string(data), `"organization_logo":"https://files.test/profiles/organization-logos/`) || !strings.Contains(string(data), "X-Amz-Signature=") {
			t.Fatalf("missing signed company logo in %s", path)
		}
	}

	for _, path := range []string{"/colleges", "/internship-records", "/recruiters/me/verification", "/recruiters/interviews", "/students/me/documents", "/admin/organization-verifications"} {
		request("GET", path, employer, nil, 404)
	}
}
