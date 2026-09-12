package repository

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/gbexam/online-exam/internal/constants"
	"github.com/gbexam/online-exam/internal/model"
)

// proctorDedupIndex is the unique index guarding concurrent reports.
const proctorDedupIndex = "uk_proctor_dedup"

// migrateLegacyProctorEvents upgrades pre-dedup-index databases so that
// AutoMigrate can create uk_proctor_dedup without failing on duplicate key
// errors. It is a no-op for fresh installs and already-migrated databases,
// and is idempotent so a previously interrupted upgrade can finish on the
// next start.
func (r *Repository) migrateLegacyProctorEvents() error {
	migrator := r.db.Migrator()
	if !migrator.HasTable("proctor_events") {
		return nil // 新库：由 AutoMigrate 直接建表和索引
	}
	if migrator.HasIndex(&model.ProctorEvent{}, proctorDedupIndex) {
		return nil // 已迁移过
	}

	// 1. 补齐 window_bucket 列（旧表没有该列，或上次启动在建索引前中断）。
	if !migrator.HasColumn(&model.ProctorEvent{}, "WindowBucket") {
		if err := migrator.AddColumn(&model.ProctorEvent{}, "WindowBucket"); err != nil {
			return fmt.Errorf("add window_bucket column: %w", err)
		}
	}

	// 2. 按发生时间回填去重窗口桶（新增列默认全 0）。
	var legacy []model.ProctorEvent
	if err := r.db.Select("id", "occurred_at").Where("window_bucket = 0").Find(&legacy).Error; err != nil {
		return fmt.Errorf("list legacy proctor events: %w", err)
	}
	for _, event := range legacy {
		bucket := event.OccurredAt.Unix() / constants.ProctorDedupWindowSeconds
		if err := r.db.Model(&model.ProctorEvent{}).Where("id = ?", event.ID).Update("window_bucket", bucket).Error; err != nil {
			return fmt.Errorf("backfill window bucket for proctor event %d: %w", event.ID, err)
		}
	}

	// 3. 合并同场次同类型同窗口的重复记录，否则唯一索引无法创建。
	type dupGroup struct {
		AttemptID    uint
		Type         string
		WindowBucket int64
		Cnt          int64
	}
	var groups []dupGroup
	if err := r.db.Model(&model.ProctorEvent{}).
		Select("attempt_id, type, window_bucket, COUNT(*) AS cnt").
		Group("attempt_id, type, window_bucket").
		Having("cnt > 1").
		Scan(&groups).Error; err != nil {
		return fmt.Errorf("find duplicate proctor events: %w", err)
	}
	for _, group := range groups {
		var rows []model.ProctorEvent
		if err := r.db.
			Select("id", "status").
			Where("attempt_id = ? AND type = ? AND window_bucket = ?", group.AttemptID, group.Type, group.WindowBucket).
			Order("id ASC").
			Find(&rows).Error; err != nil {
			return fmt.Errorf("list duplicate proctor events: %w", err)
		}
		keepID := chooseProctorEventToKeep(rows)
		ids := make([]uint, 0, len(rows)-1)
		for _, row := range rows {
			if row.ID != keepID {
				ids = append(ids, row.ID)
			}
		}
		if len(ids) > 0 {
			if err := r.db.Where("id IN ?", ids).Delete(&model.ProctorEvent{}).Error; err != nil {
				return fmt.Errorf("delete duplicate proctor events: %w", err)
			}
		}
	}
	return nil
}

// chooseProctorEventToKeep picks the surviving row of a duplicate group:
// a reviewed event (its conclusion must not be lost) wins over pending ones,
// ties are broken by the earliest report. rows must be ordered by id ASC.
func chooseProctorEventToKeep(rows []model.ProctorEvent) uint {
	keep := rows[0]
	for _, row := range rows[1:] {
		if keep.Status == constants.ProctorStatusPending && row.Status != constants.ProctorStatusPending {
			keep = row
		}
	}
	return keep.ID
}

// ProctorEventFilter holds optional filters for proctor event list queries.
type ProctorEventFilter struct {
	Status    string
	Type      string
	ExamID    uint
	CreatedBy uint // scope to events of exams owned by this teacher
}

// ProctorEventRow is a proctor event joined with exam/student/reviewer display names.
type ProctorEventRow struct {
	model.ProctorEvent
	ExamTitle    string `gorm:"column:exam_title"`
	StudentName  string `gorm:"column:student_name"`
	ReviewerName string `gorm:"column:reviewer_name"`
}

// proctorEventBaseQuery applies the shared joins and filters used by list/count/get.
func (r *Repository) proctorEventBaseQuery(ctx context.Context, filter ProctorEventFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Model(&model.ProctorEvent{}).
		Joins("JOIN exams ON exams.id = proctor_events.exam_id").
		Joins("JOIN users ON users.id = proctor_events.student_id").
		Joins("LEFT JOIN users AS reviewers ON reviewers.id = proctor_events.reviewed_by")
	if filter.Status != "" {
		q = q.Where("proctor_events.status = ?", filter.Status)
	}
	if filter.Type != "" {
		q = q.Where("proctor_events.type = ?", filter.Type)
	}
	if filter.ExamID != 0 {
		q = q.Where("proctor_events.exam_id = ?", filter.ExamID)
	}
	if filter.CreatedBy != 0 {
		q = q.Where("exams.created_by = ?", filter.CreatedBy)
	}
	return q
}

const proctorEventSelect = "proctor_events.*, exams.title AS exam_title, users.name AS student_name, reviewers.name AS reviewer_name"

// CreateProctorEvent inserts a proctor event. A duplicate
// (attempt_id, type, window_bucket) is reported as ErrConflict so the
// service can answer concurrent repeated reports with the stored event.
func (r *Repository) CreateProctorEvent(ctx context.Context, event *model.ProctorEvent) error {
	if err := r.db.WithContext(ctx).Create(event).Error; err != nil {
		return wrapQuery("create proctor event", err)
	}
	return nil
}

// FindProctorEventByDedupKey returns the event stored for one dedup bucket,
// used after a unique-index conflict to answer the concurrent loser.
func (r *Repository) FindProctorEventByDedupKey(ctx context.Context, attemptID uint, eventType string, bucket int64) (*ProctorEventRow, error) {
	var row ProctorEventRow
	err := r.proctorEventBaseQuery(ctx, ProctorEventFilter{}).
		Select(proctorEventSelect).
		Where("proctor_events.attempt_id = ? AND proctor_events.type = ? AND proctor_events.window_bucket = ?", attemptID, eventType, bucket).
		Order("proctor_events.id ASC").
		First(&row).Error
	if err != nil {
		return nil, wrapQuery("find proctor event by dedup key", err)
	}
	return &row, nil
}

// FindRecentProctorEvent returns the newest event of the same attempt and type
// occurred at or after since, used to deduplicate rapid repeated reports.
func (r *Repository) FindRecentProctorEvent(ctx context.Context, attemptID uint, eventType string, since time.Time) (*model.ProctorEvent, error) {
	var event model.ProctorEvent
	err := r.db.WithContext(ctx).
		Where("attempt_id = ? AND type = ? AND occurred_at >= ?", attemptID, eventType, since).
		Order("occurred_at DESC").
		First(&event).Error
	if err != nil {
		return nil, wrapQuery("find recent proctor event", err)
	}
	return &event, nil
}

// FindProctorEventByID returns one event with joined display names.
func (r *Repository) FindProctorEventByID(ctx context.Context, id uint) (*ProctorEventRow, error) {
	var row ProctorEventRow
	err := r.proctorEventBaseQuery(ctx, ProctorEventFilter{}).
		Select(proctorEventSelect).
		Where("proctor_events.id = ?", id).
		First(&row).Error
	if err != nil {
		return nil, wrapQuery("find proctor event by id", err)
	}
	return &row, nil
}

// ListProctorEvents returns a filtered page of events with display names.
func (r *Repository) ListProctorEvents(ctx context.Context, filter ProctorEventFilter, page, pageSize int) ([]ProctorEventRow, int64, error) {
	base := r.proctorEventBaseQuery(ctx, filter)

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count proctor events: %w", err)
	}

	var rows []ProctorEventRow
	p, ps := NormalizePage(page, pageSize)
	err := base.Select(proctorEventSelect).
		Order("proctor_events.id DESC").
		Limit(ps).Offset((p - 1) * ps).
		Find(&rows).Error
	if err != nil {
		return nil, 0, fmt.Errorf("list proctor events: %w", err)
	}
	return rows, total, nil
}

// ReviewProctorEvent writes the review conclusion only while the event is still
// pending, so a submitted conclusion can never be overwritten. Returns
// ErrNotFound when the event does not exist and ErrConflict when it was
// already reviewed.
func (r *Repository) ReviewProctorEvent(ctx context.Context, id uint, status, note string, reviewerID uint, reviewedAt time.Time) error {
	res := r.db.WithContext(ctx).Model(&model.ProctorEvent{}).
		Where("id = ? AND status = ?", id, "pending").
		Updates(map[string]any{
			"status":      status,
			"review_note": note,
			"reviewed_by": reviewerID,
			"reviewed_at": reviewedAt,
		})
	if res.Error != nil {
		return fmt.Errorf("review proctor event: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		var count int64
		if err := r.db.WithContext(ctx).Model(&model.ProctorEvent{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return fmt.Errorf("check proctor event: %w", err)
		}
		if count == 0 {
			return ErrNotFound
		}
		return ErrConflict
	}
	return nil
}
