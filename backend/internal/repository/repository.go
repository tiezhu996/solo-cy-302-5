package repository

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/gbexam/online-exam/internal/model"
)

// Sentinel errors returned by repositories.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

// Repository provides data access for all aggregates.
type Repository struct {
	db *gorm.DB
}

// NewRepository builds a Repository with an injected GORM handle.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// AutoMigrate creates/updates database tables for the given models. Legacy
// proctor_events data is migrated first so the dedup unique index can be
// created on upgraded databases without a duplicate-key startup failure.
func (r *Repository) AutoMigrate() error {
	if err := r.migrateLegacyProctorEvents(); err != nil {
		return fmt.Errorf("migrate legacy proctor events: %w", err)
	}
	return r.db.AutoMigrate(
		&model.User{},
		&model.Question{},
		&model.Exam{},
		&model.ExamQuestion{},
		&model.ExamAttempt{},
		&model.Answer{},
		&model.WrongQuestion{},
		&model.ProctorEvent{},
	)
}

// DB exposes the underlying handle for setup tasks such as seeding.
func (r *Repository) DB() *gorm.DB {
	return r.db
}

// NormalizePage applies sane defaults to pagination parameters.
func NormalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

// Offset converts page/pageSize to a SQL offset.
func Offset(page, pageSize int) int {
	p, ps := NormalizePage(page, pageSize)
	return (p - 1) * ps
}

func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// MySQL: "duplicate entry ..."; SQLite (tests): "unique constraint failed ..."
	return strings.Contains(msg, "duplicate entry") || strings.Contains(msg, "unique constraint failed")
}

func isRecordNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

func wrapQuery(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	if isDuplicateKey(err) {
		return ErrConflict
	}
	return fmt.Errorf("%s: %w", operation, err)
}
