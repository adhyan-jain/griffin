package tests

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/griffin/core/internal/config"
	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

const (
	TestDBURL     = "postgres://postgres:postgres@localhost:54322/griffin_test?sslmode=disable"
	TestJWTSecret = "test-secret-key-32chars-minimum-length-for-hmac!"
	TestWorkerKey = "test-worker-api-key-secret-12345"
)

type TestEnv struct {
	DB           *database.DB
	TaskRepo     repository.TaskRepository
	ApprovalRepo repository.ApprovalRepository
	AgentRepo    repository.AgentRepository
	ArtifactRepo repository.ArtifactRepository
	Engine       *service.TaskEngine
	Config       *config.Config
}

func SetupTestEnv(t *testing.T) *TestEnv {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := database.Connect(ctx, TestDBURL)
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}

	// Resolve path to migrations directory
	migDir := filepath.Join("..", "migrations")
	if _, err := os.Stat(migDir); os.IsNotExist(err) {
		migDir = "migrations"
	}

	if err := database.RunMigrations(ctx, db, migDir); err != nil {
		t.Fatalf("failed to run migrations on test db: %v", err)
	}

	// Clean tables before running test
	cleanTables(t, db)

	cfg := &config.Config{
		Port:                     8081,
		DatabaseURL:              TestDBURL,
		SupabaseJWTSecret:        TestJWTSecret,
		WorkerAPIKey:             TestWorkerKey,
		Environment:              "test",
		LogLevel:                 "warn",
		LeaseDurationSeconds:     5,
		HeartbeatIntervalSeconds: 1,
		ReaperIntervalSeconds:    2,
		MaxConcurrentWorkers:     2,
	}

	taskRepo := repository.NewPostgresTaskRepository(db.Pool)
	approvalRepo := repository.NewPostgresApprovalRepository(db.Pool)
	agentRepo := repository.NewPostgresAgentRepository(db.Pool)
	artifactRepo := repository.NewPostgresArtifactRepository(db.Pool)

	engine := service.NewTaskEngine(taskRepo, approvalRepo, nil)

	return &TestEnv{
		DB:           db,
		TaskRepo:     taskRepo,
		ApprovalRepo: approvalRepo,
		AgentRepo:    agentRepo,
		ArtifactRepo: artifactRepo,
		Engine:       engine,
		Config:       cfg,
	}
}

func (e *TestEnv) Teardown() {
	if e.DB != nil {
		e.DB.Close()
	}
}

func cleanTables(t *testing.T, db *database.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `
		TRUNCATE TABLE 
			notifications,
			artifacts,
			schedules,
			approvals,
			task_events,
			task_attempts,
			task_dependencies,
			tasks,
			agents,
			workspaces,
			profiles,
			auth.users
		CASCADE;
	`
	if _, err := db.Pool.Exec(ctx, query); err != nil {
		t.Fatalf("failed to truncate test tables: %v", err)
	}
}

func CreateTestUser(t *testing.T, db *database.DB, email string) uuid.UUID {
	t.Helper()
	userID := uuid.New()
	ctx := context.Background()

	q1 := `INSERT INTO auth.users (id, email) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING;`
	if _, err := db.Pool.Exec(ctx, q1, userID, email); err != nil {
		t.Fatalf("failed to insert auth user: %v", err)
	}

	q2 := `INSERT INTO profiles (id, email, full_name, role) VALUES ($1, $2, 'Test User', 'OWNER') ON CONFLICT (id) DO NOTHING;`
	if _, err := db.Pool.Exec(ctx, q2, userID, email); err != nil {
		t.Fatalf("failed to insert profile: %v", err)
	}

	return userID
}

func GenerateTestJWT(userID uuid.UUID, email string) string {
	claims := jwt.MapClaims{
		"sub":   userID.String(),
		"email": email,
		"role":  "authenticated",
		"exp":   time.Now().Add(1 * time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte(TestJWTSecret))
	return fmt.Sprintf("Bearer %s", tokenString)
}
