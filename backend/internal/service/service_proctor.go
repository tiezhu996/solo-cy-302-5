package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gbexam/online-exam/internal/constants"
	"github.com/gbexam/online-exam/internal/dto"
	"github.com/gbexam/online-exam/internal/model"
	"github.com/gbexam/online-exam/internal/repository"
)

// ErrAlreadyReviewed is returned when a handled proctor event is reviewed again.
var ErrAlreadyReviewed = errors.New("proctor event already reviewed")

// proctorEventDedupWindow is the interval in which repeated reports of the
// same attempt and type are merged into the first stored event.
const proctorEventDedupWindow = 30 * time.Second

// ProctorService is the review center for anti-cheating events: students
// report events during an exam, teachers handle events of their own exams,
// and admins can browse every handling record.
type ProctorService struct {
	baseService
	proctorRepo ProctorRepo
	attemptRepo AttemptRepo
	examRepo    ExamRepo
}

// NewProctorService constructs ProctorService.
func NewProctorService(proctorRepo ProctorRepo, attemptRepo AttemptRepo, examRepo ExamRepo, logger *slog.Logger) *ProctorService {
	return &ProctorService{baseService: NewBaseService(logger), proctorRepo: proctorRepo, attemptRepo: attemptRepo, examRepo: examRepo}
}

// Report stores one event for an in-progress attempt of the calling student.
// Reports of the same attempt and type within the dedup window keep only the
// first event and are answered with Deduplicated=true.
func (s *ProctorService) Report(ctx context.Context, studentID uint, req dto.ProctorEventReportRequest) (*dto.ProctorEventReportResponse, error) {
	attempt, err := s.attemptRepo.FindAttemptByID(ctx, req.AttemptID)
	if err != nil {
		return nil, err
	}
	if attempt.StudentID != studentID {
		return nil, fmt.Errorf("%w: 只能上报本人考试场次的事件", ErrForbidden)
	}
	if attempt.Status != constants.AttemptInProgress {
		return nil, fmt.Errorf("%w: 考试已交卷，无法上报监考事件", ErrBadRequest)
	}

	now := time.Now()
	existing, err := s.proctorRepo.FindRecentProctorEvent(ctx, attempt.ID, req.Type, now.Add(-proctorEventDedupWindow))
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("check recent proctor event: %w", err)
	}
	if err == nil {
		return s.reportResponse(ctx, existing.ID, true)
	}

	event := &model.ProctorEvent{
		AttemptID:  attempt.ID,
		ExamID:     attempt.ExamID,
		StudentID:  studentID,
		Type:       req.Type,
		Detail:     req.Detail,
		Status:     constants.ProctorStatusPending,
		OccurredAt: now,
	}
	if err := s.proctorRepo.CreateProctorEvent(ctx, event); err != nil {
		return nil, fmt.Errorf("create proctor event: %w", err)
	}
	return s.reportResponse(ctx, event.ID, false)
}

func (s *ProctorService) reportResponse(ctx context.Context, id uint, deduplicated bool) (*dto.ProctorEventReportResponse, error) {
	row, err := s.proctorRepo.FindProctorEventByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &dto.ProctorEventReportResponse{Event: proctorRowToResponse(row), Deduplicated: deduplicated}, nil
}

// List returns a page of events: teachers only see events of their own exams,
// admins see all handling records.
func (s *ProctorService) List(ctx context.Context, role string, userID uint, query dto.ProctorEventListQuery) (dto.PageResult, error) {
	filter := repository.ProctorEventFilter{Status: query.Status, Type: query.Type, ExamID: query.ExamID}
	switch role {
	case constants.RoleAdmin:
		// admin sees every event
	case constants.RoleTeacher:
		filter.CreatedBy = userID
	default:
		return dto.PageResult{}, ErrForbidden
	}

	rows, total, err := s.proctorRepo.ListProctorEvents(ctx, filter, query.Page, query.PageSize)
	if err != nil {
		return dto.PageResult{}, fmt.Errorf("list proctor events: %w", err)
	}
	page, pageSize := normalizePage(query.Page, query.PageSize)
	items := make([]dto.ProctorEventResponse, 0, len(rows))
	for i := range rows {
		items = append(items, proctorRowToResponse(&rows[i]))
	}
	return dto.PageResult{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// Get returns one event after checking the caller may see its exam.
func (s *ProctorService) Get(ctx context.Context, role string, userID, id uint) (*dto.ProctorEventResponse, error) {
	row, err := s.proctorRepo.FindProctorEventByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.checkExamScope(ctx, role, userID, row.ExamID); err != nil {
		return nil, err
	}
	resp := proctorRowToResponse(row)
	return &resp, nil
}

// Review stores the handling conclusion for a pending event. The conclusion
// is final: reviewing an already handled event fails with ErrAlreadyReviewed.
func (s *ProctorService) Review(ctx context.Context, role string, userID, id uint, req dto.ProctorEventReviewRequest) (*dto.ProctorEventResponse, error) {
	row, err := s.proctorRepo.FindProctorEventByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.checkExamScope(ctx, role, userID, row.ExamID); err != nil {
		return nil, err
	}
	if row.Status != constants.ProctorStatusPending {
		return nil, ErrAlreadyReviewed
	}

	if err := s.proctorRepo.ReviewProctorEvent(ctx, id, req.Status, req.Note, userID, time.Now()); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, ErrAlreadyReviewed
		}
		return nil, fmt.Errorf("review proctor event: %w", err)
	}

	updated, err := s.proctorRepo.FindProctorEventByID(ctx, id)
	if err != nil {
		return nil, err
	}
	resp := proctorRowToResponse(updated)
	return &resp, nil
}

// checkExamScope allows admins everything and restricts teachers to events of
// exams they created.
func (s *ProctorService) checkExamScope(ctx context.Context, role string, userID, examID uint) error {
	switch role {
	case constants.RoleAdmin:
		return nil
	case constants.RoleTeacher:
		exam, err := s.examRepo.FindExamByID(ctx, examID)
		if err != nil {
			return err
		}
		if exam.CreatedBy != userID {
			return fmt.Errorf("%w: 只能处理本人创建考试的监考事件", ErrForbidden)
		}
		return nil
	default:
		return ErrForbidden
	}
}

func proctorRowToResponse(row *repository.ProctorEventRow) dto.ProctorEventResponse {
	return dto.ProctorEventResponse{
		ID:           row.ID,
		AttemptID:    row.AttemptID,
		ExamID:       row.ExamID,
		ExamTitle:    row.ExamTitle,
		StudentID:    row.StudentID,
		StudentName:  row.StudentName,
		Type:         row.Type,
		Detail:       row.Detail,
		Status:       row.Status,
		OccurredAt:   row.OccurredAt,
		ReviewNote:   row.ReviewNote,
		ReviewedBy:   row.ReviewedBy,
		ReviewerName: row.ReviewerName,
		ReviewedAt:   row.ReviewedAt,
		CreatedAt:    row.CreatedAt,
	}
}
