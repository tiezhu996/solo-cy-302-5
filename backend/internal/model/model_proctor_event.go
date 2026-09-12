package model

import "time"

// ProctorEvent is one anti-cheating event reported during an exam attempt.
// Review fields are immutable once the event leaves the pending status.
// (AttemptID, Type, WindowBucket) carries a unique index so that concurrent
// reports within the same dedup window bucket can never insert duplicates.
type ProctorEvent struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	AttemptID    uint       `gorm:"index;uniqueIndex:uk_proctor_dedup;not null" json:"attempt_id"`
	ExamID       uint       `gorm:"index;not null" json:"exam_id"`
	StudentID    uint       `gorm:"index;not null" json:"student_id"`
	Type         string     `gorm:"size:32;uniqueIndex:uk_proctor_dedup;not null" json:"type"`
	Detail       string     `gorm:"size:512" json:"detail"`
	Status       string     `gorm:"size:16;not null;default:pending;index" json:"status"`
	OccurredAt   time.Time  `gorm:"not null" json:"occurred_at"`
	WindowBucket int64      `gorm:"uniqueIndex:uk_proctor_dedup;not null;default:0" json:"-"`
	ReviewNote   string     `gorm:"size:512" json:"review_note"`
	ReviewedBy   uint       `json:"reviewed_by"`
	ReviewedAt   *time.Time `json:"reviewed_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}
