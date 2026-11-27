package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/prasanna/student-job-portal/backend/internal/config"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestReadinessChecksDatabaseAndBuckets(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	available := true
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !available {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(200)
	}))
	defer storage.Close()
	client, err := minio.New(strings.TrimPrefix(storage.URL, "http://"), &minio.Options{Region: "us-east-1", Creds: credentials.NewStaticV4("key", "secret", "")})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{MinIO: config.MinIOConfig{ProfileBucket: "profiles", StudentDocBucket: "documents"}}
	router := gin.New()
	router.GET("/ready", readinessCheck(db, client, cfg))
	router.GET("/health", healthCheck)
	check := func(path string, want int) {
		t.Helper()
		res := httptest.NewRecorder()
		router.ServeHTTP(res, httptest.NewRequest("GET", path, nil))
		if res.Code != want {
			t.Fatalf("%s: got %d want %d", path, res.Code, want)
		}
	}
	check("/ready", 200)
	available = false
	check("/ready", 503)
	check("/health", 200)
	available = true
	_ = sqlDB.Close()
	check("/ready", 503)
}
