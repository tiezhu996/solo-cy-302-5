// Package e2etest exercises the proctoring review center through the real
// HTTP stack (router → handler → service → repository) backed by an
// in-memory SQLite database, so the main scenarios run in `go test ./...`
// without a MySQL server. Every test gets its own isolated database, which
// keeps repeated runs (-count=N) deterministic.
package e2etest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/gbexam/online-exam/internal/config"
	"github.com/gbexam/online-exam/internal/handler"
	"github.com/gbexam/online-exam/internal/middleware"
	"github.com/gbexam/online-exam/internal/repository"
	"github.com/gbexam/online-exam/internal/router"
	"github.com/gbexam/online-exam/internal/service"
)

// dbSeq makes every test database unique: in-memory SQLite databases keyed
// by name would otherwise leak state across tests and across -count=N
// repetitions inside one process.
var dbSeq atomic.Int64

// openTestDB returns a fresh, isolated in-memory database.
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:e2e_%d?mode=memory&cache=shared", dbSeq.Add(1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	return db
}

// newTestServer assembles the full HTTP stack exactly like cmd/server does.
func newTestServer(t *testing.T, db *gorm.DB) *httptest.Server {
	t.Helper()
	repo := repository.NewRepository(db)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{JWTSecret: "e2e-secret", JWTExpireHours: 1, AdminUsername: "admin", AdminPassword: "admin123"}
	authService := service.NewAuthService(repo, cfg, logger)
	server := handler.NewServer(
		logger,
		authService,
		service.NewUserService(repo, logger),
		service.NewQuestionService(repo, logger),
		service.NewExamService(repo, repo, logger),
		service.NewAttemptService(repo, repo, repo, repo, repo, logger),
		service.NewStatsService(repo, logger),
		service.NewWrongQuestionService(repo, repo, logger),
		service.NewProctorService(repo, repo, repo, logger),
	)
	if err := authService.SeedAdmin(context.Background()); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	engine := router.New(server, middleware.Auth(authService))
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	return srv
}

// migrateFresh runs the same startup migration the server runs on boot.
func migrateFresh(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := repository.NewRepository(db).AutoMigrate(); err != nil {
		t.Fatalf("startup AutoMigrate: %v", err)
	}
}

// envelope is the unified API response body.
type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// client is a test HTTP client bound to one server and (optionally) one token.
type client struct {
	t     *testing.T
	srv   *httptest.Server
	token string
}

func newClient(t *testing.T, srv *httptest.Server) *client {
	t.Helper()
	return &client{t: t, srv: srv}
}

// do performs one request and asserts the HTTP status.
func (c *client) do(method, path string, body any, wantStatus int) envelope {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.srv.URL+"/api/v1"+path, reader)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		c.t.Fatalf("%s %s: status = %d, want %d, body: %s", method, path, resp.StatusCode, wantStatus, raw)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		c.t.Fatalf("unmarshal envelope: %v, body: %s", err, raw)
	}
	return env
}

// register creates an account and logs in, leaving the token on the client.
func (c *client) register(username, password, name, role string) {
	c.t.Helper()
	c.do("POST", "/auth/register", map[string]any{
		"username": username, "password": password, "name": name, "role": role,
	}, http.StatusCreated)
	c.login(username, password)
}

func (c *client) login(username, password string) {
	c.t.Helper()
	env := c.do("POST", "/auth/login", map[string]any{"username": username, "password": password}, http.StatusOK)
	var data struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		c.t.Fatalf("unmarshal login: %v", err)
	}
	c.token = data.Token
}

// seedPublishedAttempt drives the real exam flow: teacher creates a question
// and an exam, publishes it, and the student starts an attempt.
func seedPublishedAttempt(t *testing.T, teacher, student *client) (examID, attemptID uint) {
	t.Helper()
	teacher.do("POST", "/questions", map[string]any{
		"type": "single", "content": "1+1=?", "difficulty": "easy", "knowledge_point": "数学", "score": 5,
		"options": []map[string]string{{"key": "A", "text": "2"}, {"key": "B", "text": "3"}},
		"answer":  "A",
	}, http.StatusCreated)
	examEnv := teacher.do("POST", "/exams", map[string]any{
		"title": "数学测验", "duration_minutes": 60,
		"question_config": []map[string]any{{"type": "single", "count": 1, "score": 5, "difficulty": "easy"}},
	}, http.StatusCreated)
	var exam struct {
		ID uint `json:"id"`
	}
	if err := json.Unmarshal(examEnv.Data, &exam); err != nil {
		t.Fatalf("unmarshal exam: %v", err)
	}
	teacher.do("POST", fmt.Sprintf("/exams/%d/publish", exam.ID), nil, http.StatusOK)
	attemptEnv := student.do("POST", fmt.Sprintf("/exams/%d/attempts", exam.ID), nil, http.StatusCreated)
	var attempt struct {
		AttemptID uint `json:"attempt_id"`
	}
	if err := json.Unmarshal(attemptEnv.Data, &attempt); err != nil {
		t.Fatalf("unmarshal attempt: %v", err)
	}
	return exam.ID, attempt.AttemptID
}

// reportResult captures one concurrent report response.
type reportResult struct {
	status       int
	deduplicated bool
	eventID      uint
	body         string
}

// fireConcurrentReports sends workers simultaneous reports of one attempt and
// event type, returning every response for assertions.
func fireConcurrentReports(t *testing.T, srv *httptest.Server, token string, attemptID uint, eventType string, workers int) []reportResult {
	t.Helper()
	results := make([]reportResult, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			raw, _ := json.Marshal(map[string]any{"attempt_id": attemptID, "type": eventType, "detail": "并发上报"})
			req, err := http.NewRequest("POST", srv.URL+"/api/v1/proctor-events", bytes.NewReader(raw))
			if err != nil {
				t.Errorf("report %d: new request: %v", i, err)
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Errorf("report %d: do request: %v", i, err)
				return
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			r := reportResult{status: resp.StatusCode, body: string(body)}
			if resp.StatusCode == http.StatusOK {
				var env envelope
				if err := json.Unmarshal(body, &env); err == nil {
					var data struct {
						Event struct {
							ID uint `json:"id"`
						} `json:"event"`
						Deduplicated bool `json:"deduplicated"`
					}
					if err := json.Unmarshal(env.Data, &data); err == nil {
						r.deduplicated = data.Deduplicated
						r.eventID = data.Event.ID
					}
				}
			}
			results[i] = r
		}(i)
	}
	close(start)
	wg.Wait()
	return results
}

// assertSingleStoredEvent checks the outcome of a concurrent dedup scenario:
// exactly one fresh response, every response carries the same event ID, and
// exactly one row was stored.
func assertSingleStoredEvent(t *testing.T, results []reportResult, stored int64) {
	t.Helper()
	fresh := 0
	var wantID uint
	for i, r := range results {
		if r.status != http.StatusOK {
			t.Fatalf("并发上报第 %d 个请求: status = %d, want 200, body: %s", i, r.status, r.body)
		}
		if !r.deduplicated {
			fresh++
		}
		if wantID == 0 {
			wantID = r.eventID
		} else if r.eventID != wantID {
			t.Fatalf("并发上报第 %d 个请求看到事件 %d, 期望共享事件 %d", i, r.eventID, wantID)
		}
	}
	if fresh != 1 {
		t.Fatalf("非去重响应数 = %d, 期望恰好 1 个", fresh)
	}
	if stored != 1 {
		t.Fatalf("入库事件数 = %d, 期望 1 条", stored)
	}
}
