package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/gbexam/online-exam/internal/constants"
	"github.com/gbexam/online-exam/internal/dto"
	"github.com/gbexam/online-exam/internal/middleware"
	"github.com/gbexam/online-exam/pkg/httpx"
)

// ReportProctorEvent handles POST /proctor-events (student, during an exam).
func (s *Server) ReportProctorEvent(c *gin.Context) {
	var req dto.ProctorEventReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, http.StatusUnprocessableEntity, constants.CodeValidation, "请求参数不合法")
		return
	}
	result, err := s.proctor.Report(c.Request.Context(), middleware.UserID(c), req)
	if err != nil {
		s.respondError(c, err)
		return
	}
	httpx.OK(c, result)
}

// ListProctorEvents handles GET /proctor-events (teacher: own exams, admin: all).
func (s *Server) ListProctorEvents(c *gin.Context) {
	var query dto.ProctorEventListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		httpx.Fail(c, http.StatusUnprocessableEntity, constants.CodeValidation, "请求参数不合法")
		return
	}
	result, err := s.proctor.List(c.Request.Context(), middleware.Role(c), middleware.UserID(c), query)
	if err != nil {
		s.respondError(c, err)
		return
	}
	httpx.OK(c, result)
}

// GetProctorEvent handles GET /proctor-events/:id.
func (s *Server) GetProctorEvent(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpx.Fail(c, http.StatusUnprocessableEntity, constants.CodeValidation, "事件 ID 不合法")
		return
	}
	event, err := s.proctor.Get(c.Request.Context(), middleware.Role(c), middleware.UserID(c), uint(id))
	if err != nil {
		s.respondError(c, err)
		return
	}
	httpx.OK(c, event)
}

// ReviewProctorEvent handles POST /proctor-events/:id/review.
func (s *Server) ReviewProctorEvent(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpx.Fail(c, http.StatusUnprocessableEntity, constants.CodeValidation, "事件 ID 不合法")
		return
	}
	var req dto.ProctorEventReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, http.StatusUnprocessableEntity, constants.CodeValidation, "请求参数不合法")
		return
	}
	event, err := s.proctor.Review(c.Request.Context(), middleware.Role(c), middleware.UserID(c), uint(id), req)
	if err != nil {
		s.respondError(c, err)
		return
	}
	httpx.OK(c, event)
}
