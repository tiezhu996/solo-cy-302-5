package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/gbexam/online-exam/internal/constants"
	"github.com/gbexam/online-exam/internal/dto"
	"github.com/gbexam/online-exam/internal/model"
	"github.com/gbexam/online-exam/internal/repository"
)

// fakeProctorRepo is an in-memory ProctorRepo for service tests. It enforces
// the (attempt_id, type, window_bucket) unique key atomically, mirroring the
// database unique index that guards concurrent reports.
type fakeProctorRepo struct {
	mu      sync.Mutex
	events  []model.ProctorEvent
	exams   *fakeExamRepo
	nextID  uint
	reviews int
}

func newFakeProctorRepo(exams *fakeExamRepo) *fakeProctorRepo {
	return &fakeProctorRepo{exams: exams, nextID: 1}
}

func (f *fakeProctorRepo) CreateProctorEvent(_ context.Context, event *model.ProctorEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.events {
		if e.AttemptID == event.AttemptID && e.Type == event.Type && e.WindowBucket == event.WindowBucket {
			return repository.ErrConflict
		}
	}
	event.ID = f.nextID
	f.nextID++
	f.events = append(f.events, *event)
	return nil
}

func (f *fakeProctorRepo) FindRecentProctorEvent(_ context.Context, attemptID uint, eventType string, since time.Time) (*model.ProctorEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.events) - 1; i >= 0; i-- {
		e := f.events[i]
		if e.AttemptID == attemptID && e.Type == eventType && !e.OccurredAt.Before(since) {
			return &e, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakeProctorRepo) FindProctorEventByDedupKey(_ context.Context, attemptID uint, eventType string, bucket int64) (*repository.ProctorEventRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.events {
		if e.AttemptID == attemptID && e.Type == eventType && e.WindowBucket == bucket {
			return &repository.ProctorEventRow{ProctorEvent: e}, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakeProctorRepo) FindProctorEventByID(_ context.Context, id uint) (*repository.ProctorEventRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.events {
		if e.ID == id {
			return &repository.ProctorEventRow{ProctorEvent: e}, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakeProctorRepo) ListProctorEvents(_ context.Context, filter repository.ProctorEventFilter, _, _ int) ([]repository.ProctorEventRow, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := make([]repository.ProctorEventRow, 0, len(f.events))
	for _, e := range f.events {
		if filter.Status != "" && e.Status != filter.Status {
			continue
		}
		if filter.Type != "" && e.Type != filter.Type {
			continue
		}
		if filter.ExamID != 0 && e.ExamID != filter.ExamID {
			continue
		}
		if filter.CreatedBy != 0 {
			exam, ok := f.exams.exams[e.ExamID]
			if !ok || exam.CreatedBy != filter.CreatedBy {
				continue
			}
		}
		rows = append(rows, repository.ProctorEventRow{ProctorEvent: e})
	}
	return rows, int64(len(rows)), nil
}

func (f *fakeProctorRepo) ReviewProctorEvent(_ context.Context, id uint, status, note string, reviewerID uint, reviewedAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.events {
		if f.events[i].ID != id {
			continue
		}
		if f.events[i].Status != constants.ProctorStatusPending {
			return repository.ErrConflict
		}
		f.events[i].Status = status
		f.events[i].ReviewNote = note
		f.events[i].ReviewedBy = reviewerID
		f.events[i].ReviewedAt = &reviewedAt
		f.reviews++
		return nil
	}
	return repository.ErrNotFound
}

func (f *fakeProctorRepo) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

// fakeAttemptRepo serves a fixed set of attempts.
type fakeAttemptRepo struct {
	attempts map[uint]model.ExamAttempt
}

func (f *fakeAttemptRepo) CreateAttempt(context.Context, *model.ExamAttempt) error {
	panic("not implemented")
}

func (f *fakeAttemptRepo) FindAttemptByID(_ context.Context, id uint) (*model.ExamAttempt, error) {
	if a, ok := f.attempts[id]; ok {
		return &a, nil
	}
	return nil, repository.ErrNotFound
}

func (f *fakeAttemptRepo) UpdateAttempt(context.Context, *model.ExamAttempt) error {
	panic("not implemented")
}

func (f *fakeAttemptRepo) FindInProgressAttempt(context.Context, uint, uint) (*model.ExamAttempt, error) {
	panic("not implemented")
}

func (f *fakeAttemptRepo) ListAttemptsByStudent(context.Context, uint, uint, int, int) ([]model.ExamAttempt, int64, error) {
	panic("not implemented")
}

func (f *fakeAttemptRepo) ListAttemptsByExam(context.Context, uint) ([]model.ExamAttempt, error) {
	panic("not implemented")
}

// fakeExamRepo serves a fixed set of exams.
type fakeExamRepo struct {
	exams map[uint]model.Exam
}

func (f *fakeExamRepo) CreateExam(context.Context, *model.Exam) error { panic("not implemented") }

func (f *fakeExamRepo) FindExamByID(_ context.Context, id uint) (*model.Exam, error) {
	if e, ok := f.exams[id]; ok {
		return &e, nil
	}
	return nil, repository.ErrNotFound
}

func (f *fakeExamRepo) UpdateExam(context.Context, *model.Exam) error { panic("not implemented") }
func (f *fakeExamRepo) DeleteExam(context.Context, uint) error        { panic("not implemented") }
func (f *fakeExamRepo) ListExams(context.Context, repository.ExamFilter, int, int) ([]model.Exam, int64, error) {
	panic("not implemented")
}
func (f *fakeExamRepo) ReplaceExamQuestions(context.Context, uint, []model.ExamQuestion) error {
	panic("not implemented")
}
func (f *fakeExamRepo) ListExamQuestions(context.Context, uint) ([]model.ExamQuestion, error) {
	panic("not implemented")
}
func (f *fakeExamRepo) CountExamQuestions(context.Context, uint) (int64, error) {
	panic("not implemented")
}

func newProctorTestService() (*ProctorService, *fakeProctorRepo) {
	examRepo := &fakeExamRepo{exams: map[uint]model.Exam{
		10: {ID: 10, Title: "期中考试", CreatedBy: 7},
		20: {ID: 20, Title: "期末考试", CreatedBy: 8},
	}}
	proctorRepo := newFakeProctorRepo(examRepo)
	attemptRepo := &fakeAttemptRepo{attempts: map[uint]model.ExamAttempt{
		1: {ID: 1, ExamID: 10, StudentID: 100, Status: constants.AttemptInProgress},
		2: {ID: 2, ExamID: 10, StudentID: 100, Status: constants.AttemptSubmitted},
		3: {ID: 3, ExamID: 20, StudentID: 100, Status: constants.AttemptInProgress},
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewProctorService(proctorRepo, attemptRepo, examRepo, logger), proctorRepo
}

func reportReq(attemptID uint, eventType string) dto.ProctorEventReportRequest {
	return dto.ProctorEventReportRequest{AttemptID: attemptID, Type: eventType, Detail: "test"}
}

func TestProctorReportCreatesPendingEvent(t *testing.T) {
	svc, repo := newProctorTestService()
	resp, err := svc.Report(context.Background(), 100, reportReq(1, constants.ProctorEventTabSwitch))
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if resp.Deduplicated {
		t.Fatal("first report must not be deduplicated")
	}
	if resp.Event.Status != constants.ProctorStatusPending {
		t.Fatalf("status = %q, want pending", resp.Event.Status)
	}
	if resp.Event.ExamID != 10 || resp.Event.StudentID != 100 {
		t.Fatalf("unexpected event scope: %+v", resp.Event)
	}
	if len(repo.events) != 1 {
		t.Fatalf("stored events = %d, want 1", len(repo.events))
	}
}

func TestProctorReportDeduplicatesWithinWindow(t *testing.T) {
	svc, repo := newProctorTestService()
	first, err := svc.Report(context.Background(), 100, reportReq(1, constants.ProctorEventTabSwitch))
	if err != nil {
		t.Fatalf("first Report: %v", err)
	}
	second, err := svc.Report(context.Background(), 100, reportReq(1, constants.ProctorEventTabSwitch))
	if err != nil {
		t.Fatalf("second Report: %v", err)
	}
	if !second.Deduplicated {
		t.Fatal("repeated report within window must be deduplicated")
	}
	if second.Event.ID != first.Event.ID {
		t.Fatalf("deduplicated id = %d, want %d", second.Event.ID, first.Event.ID)
	}
	if len(repo.events) != 1 {
		t.Fatalf("stored events = %d, want 1", len(repo.events))
	}

	// A different type in the same window is a new event.
	third, err := svc.Report(context.Background(), 100, reportReq(1, constants.ProctorEventFullscreenExit))
	if err != nil {
		t.Fatalf("third Report: %v", err)
	}
	if third.Deduplicated || third.Event.ID == first.Event.ID {
		t.Fatalf("different type must create a new event: %+v", third)
	}
}

func TestProctorReportAfterWindowCreatesNewEvent(t *testing.T) {
	svc, repo := newProctorTestService()
	if _, err := svc.Report(context.Background(), 100, reportReq(1, constants.ProctorEventTabSwitch)); err != nil {
		t.Fatalf("first Report: %v", err)
	}
	// Age the stored event beyond the dedup window (occurrence time and bucket).
	aged := time.Now().Add(-2 * proctorEventDedupWindow)
	repo.events[0].OccurredAt = aged
	repo.events[0].WindowBucket = aged.Unix() / constants.ProctorDedupWindowSeconds

	resp, err := svc.Report(context.Background(), 100, reportReq(1, constants.ProctorEventTabSwitch))
	if err != nil {
		t.Fatalf("second Report: %v", err)
	}
	if resp.Deduplicated {
		t.Fatal("report after the dedup window must create a new event")
	}
	if len(repo.events) != 2 {
		t.Fatalf("stored events = %d, want 2", len(repo.events))
	}
}

func TestProctorReportGuards(t *testing.T) {
	svc, _ := newProctorTestService()

	if _, err := svc.Report(context.Background(), 999, reportReq(1, constants.ProctorEventTabSwitch)); !errors.Is(err, ErrForbidden) {
		t.Fatalf("report for others attempt: err = %v, want ErrForbidden", err)
	}
	if _, err := svc.Report(context.Background(), 100, reportReq(2, constants.ProctorEventTabSwitch)); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("report for submitted attempt: err = %v, want ErrBadRequest", err)
	}
	if _, err := svc.Report(context.Background(), 100, reportReq(404, constants.ProctorEventTabSwitch)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("report for missing attempt: err = %v, want ErrNotFound", err)
	}
}

// TestProctorReportConcurrentDedup fires many simultaneous reports of the
// same attempt and type: exactly one event may be stored, exactly one caller
// gets Deduplicated=false, and every caller sees the same event ID.
func TestProctorReportConcurrentDedup(t *testing.T) {
	svc, repo := newProctorTestService()
	const workers = 32

	var wg sync.WaitGroup
	responses := make([]*dto.ProctorEventReportResponse, workers)
	errs := make([]error, workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			responses[i], errs[i] = svc.Report(context.Background(), 100, reportReq(1, constants.ProctorEventTabSwitch))
		}(i)
	}
	close(start)
	wg.Wait()

	fresh := 0
	var wantID uint
	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			t.Fatalf("concurrent Report %d: %v", i, errs[i])
		}
		if responses[i] == nil {
			t.Fatalf("concurrent Report %d: nil response", i)
		}
		if !responses[i].Deduplicated {
			fresh++
		}
		if wantID == 0 {
			wantID = responses[i].Event.ID
		} else if responses[i].Event.ID != wantID {
			t.Fatalf("response %d sees event %d, want shared event %d", i, responses[i].Event.ID, wantID)
		}
	}
	if fresh != 1 {
		t.Fatalf("non-deduplicated responses = %d, want exactly 1", fresh)
	}
	if got := repo.count(); got != 1 {
		t.Fatalf("stored events = %d, want 1", got)
	}
}

func TestProctorListScopesByRole(t *testing.T) {
	svc, _ := newProctorTestService()
	ctx := context.Background()
	if _, err := svc.Report(ctx, 100, reportReq(1, constants.ProctorEventTabSwitch)); err != nil {
		t.Fatalf("Report exam 10: %v", err)
	}
	if _, err := svc.Report(ctx, 100, reportReq(3, constants.ProctorEventPageLeave)); err != nil {
		t.Fatalf("Report exam 20: %v", err)
	}

	admin, err := svc.List(ctx, constants.RoleAdmin, 1, dto.ProctorEventListQuery{})
	if err != nil {
		t.Fatalf("admin List: %v", err)
	}
	if admin.Total != 2 {
		t.Fatalf("admin total = %d, want 2", admin.Total)
	}

	owner, err := svc.List(ctx, constants.RoleTeacher, 7, dto.ProctorEventListQuery{})
	if err != nil {
		t.Fatalf("owner List: %v", err)
	}
	if owner.Total != 1 {
		t.Fatalf("owner total = %d, want 1 (only own exam)", owner.Total)
	}

	other, err := svc.List(ctx, constants.RoleTeacher, 8, dto.ProctorEventListQuery{Status: constants.ProctorStatusPending})
	if err != nil {
		t.Fatalf("other List: %v", err)
	}
	if other.Total != 1 {
		t.Fatalf("other teacher total = %d, want 1 (only own exam)", other.Total)
	}

	if _, err := svc.List(ctx, constants.RoleStudent, 100, dto.ProctorEventListQuery{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("student List: err = %v, want ErrForbidden", err)
	}
}

func TestProctorGetScopesByRole(t *testing.T) {
	svc, _ := newProctorTestService()
	ctx := context.Background()
	resp, err := svc.Report(ctx, 100, reportReq(1, constants.ProctorEventTabSwitch))
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	id := resp.Event.ID

	if _, err := svc.Get(ctx, constants.RoleTeacher, 7, id); err != nil {
		t.Fatalf("owner Get: %v", err)
	}
	if _, err := svc.Get(ctx, constants.RoleAdmin, 1, id); err != nil {
		t.Fatalf("admin Get: %v", err)
	}
	if _, err := svc.Get(ctx, constants.RoleTeacher, 8, id); !errors.Is(err, ErrForbidden) {
		t.Fatalf("other teacher Get: err = %v, want ErrForbidden", err)
	}
	if _, err := svc.Get(ctx, constants.RoleAdmin, 1, 404); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing Get: err = %v, want ErrNotFound", err)
	}
}

func TestProctorReviewIsFinal(t *testing.T) {
	svc, _ := newProctorTestService()
	ctx := context.Background()
	resp, err := svc.Report(ctx, 100, reportReq(1, constants.ProctorEventTabSwitch))
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	id := resp.Event.ID
	review := dto.ProctorEventReviewRequest{Status: constants.ProctorStatusConfirmed, Note: "确认存在切屏行为"}

	updated, err := svc.Review(ctx, constants.RoleTeacher, 7, id, review)
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if updated.Status != constants.ProctorStatusConfirmed || updated.ReviewNote != review.Note {
		t.Fatalf("unexpected reviewed event: %+v", updated)
	}
	if updated.ReviewedBy != 7 || updated.ReviewedAt == nil {
		t.Fatalf("review metadata missing: %+v", updated)
	}

	// A submitted conclusion cannot be changed, by anyone.
	if _, err := svc.Review(ctx, constants.RoleTeacher, 7, id, review); !errors.Is(err, ErrAlreadyReviewed) {
		t.Fatalf("re-review by owner: err = %v, want ErrAlreadyReviewed", err)
	}
	if _, err := svc.Review(ctx, constants.RoleAdmin, 1, id, dto.ProctorEventReviewRequest{Status: constants.ProctorStatusIgnored, Note: "改判"}); !errors.Is(err, ErrAlreadyReviewed) {
		t.Fatalf("re-review by admin: err = %v, want ErrAlreadyReviewed", err)
	}
}

func TestProctorReviewGuards(t *testing.T) {
	svc, _ := newProctorTestService()
	ctx := context.Background()
	resp, err := svc.Report(ctx, 100, reportReq(1, constants.ProctorEventTabSwitch))
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	id := resp.Event.ID
	review := dto.ProctorEventReviewRequest{Status: constants.ProctorStatusIgnored, Note: "误报"}

	if _, err := svc.Review(ctx, constants.RoleTeacher, 8, id, review); !errors.Is(err, ErrForbidden) {
		t.Fatalf("other teacher Review: err = %v, want ErrForbidden", err)
	}
	if _, err := svc.Review(ctx, constants.RoleStudent, 100, id, review); !errors.Is(err, ErrForbidden) {
		t.Fatalf("student Review: err = %v, want ErrForbidden", err)
	}
	if _, err := svc.Review(ctx, constants.RoleAdmin, 1, 404, review); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing Review: err = %v, want ErrNotFound", err)
	}
	if _, err := svc.Review(ctx, constants.RoleAdmin, 1, id, review); err != nil {
		t.Fatalf("admin Review own-scope-free: %v", err)
	}
}
