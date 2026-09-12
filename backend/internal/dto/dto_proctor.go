package dto

import "time"

// ProctorEventReportRequest is sent by the exam page when an anti-cheating
// trigger fires (tab switch, fullscreen exit, page leave).
type ProctorEventReportRequest struct {
	AttemptID uint   `json:"attempt_id" binding:"required,min=1"`
	Type      string `json:"type" binding:"required,oneof=tab_switch fullscreen_exit page_leave"`
	Detail    string `json:"detail" binding:"max=512"`
}

// ProctorEventListQuery filters the review-center event list.
type ProctorEventListQuery struct {
	PageQuery
	Status string `form:"status" binding:"omitempty,oneof=pending confirmed ignored"`
	Type   string `form:"type" binding:"omitempty,oneof=tab_switch fullscreen_exit page_leave"`
	ExamID uint   `form:"exam_id" binding:"omitempty,min=1"`
}

// ProctorEventReviewRequest submits the handling conclusion for one event.
// The conclusion is final: once stored it cannot be changed.
type ProctorEventReviewRequest struct {
	Status string `json:"status" binding:"required,oneof=confirmed ignored"`
	Note   string `json:"note" binding:"required,min=1,max=512"`
}

// ProctorEventResponse is the review-center view of one event.
type ProctorEventResponse struct {
	ID           uint       `json:"id"`
	AttemptID    uint       `json:"attempt_id"`
	ExamID       uint       `json:"exam_id"`
	ExamTitle    string     `json:"exam_title"`
	StudentID    uint       `json:"student_id"`
	StudentName  string     `json:"student_name"`
	Type         string     `json:"type"`
	Detail       string     `json:"detail"`
	Status       string     `json:"status"`
	OccurredAt   time.Time  `json:"occurred_at"`
	ReviewNote   string     `json:"review_note"`
	ReviewedBy   uint       `json:"reviewed_by"`
	ReviewerName string     `json:"reviewer_name"`
	ReviewedAt   *time.Time `json:"reviewed_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

// ProctorEventReportResponse tells the caller whether the report was stored
// as a new event or merged into a recent identical one.
type ProctorEventReportResponse struct {
	Event        ProctorEventResponse `json:"event"`
	Deduplicated bool                 `json:"deduplicated"`
}
