package e2etest

import (
	"fmt"
	"net/http"
	"testing"
)

// TestReviewCenterPermissions 场景：复核中心的数据范围与状态机——教师只能
// 看到并处理自己考试的事件，管理员可查看全部处理记录，已处理事件再次
// 修改被明确拒绝。
func TestReviewCenterPermissions(t *testing.T) {
	db := openTestDB(t)
	migrateFresh(t, db)
	srv := newTestServer(t, db)

	admin := newClient(t, srv)
	owner := newClient(t, srv)
	otherTeacher := newClient(t, srv)
	student := newClient(t, srv)
	admin.login("admin", "admin123")
	owner.register("teacher1", "pass123", "老师甲", "teacher")
	otherTeacher.register("teacher2", "pass123", "老师乙", "teacher")
	student.register("student1", "pass123", "学生甲", "student")
	_, attemptID := seedPublishedAttempt(t, owner, student)

	reportEnv := student.do("POST", "/proctor-events", map[string]any{"attempt_id": attemptID, "type": "tab_switch", "detail": "切屏"}, http.StatusOK)
	var report struct {
		Event struct {
			ID uint `json:"id"`
		} `json:"event"`
	}
	mustUnmarshal(t, reportEnv.Data, &report)
	eventID := report.Event.ID
	reviewPath := fmt.Sprintf("/proctor-events/%d/review", eventID)
	detailPath := fmt.Sprintf("/proctor-events/%d", eventID)

	t.Run("学生禁止访问复核中心", func(t *testing.T) {
		student.do("GET", "/proctor-events", nil, http.StatusForbidden)
		student.do("GET", detailPath, nil, http.StatusForbidden)
		student.do("POST", reviewPath, map[string]any{"status": "ignored", "note": "越权"}, http.StatusForbidden)
	})

	t.Run("未登录访问返回401", func(t *testing.T) {
		anon := newClient(t, srv)
		anon.do("GET", "/proctor-events", nil, http.StatusUnauthorized)
		anon.do("GET", detailPath, nil, http.StatusUnauthorized)
		anon.do("POST", reviewPath, map[string]any{"status": "ignored", "note": "越权"}, http.StatusUnauthorized)
	})

	t.Run("其他教师越权访问被拒绝", func(t *testing.T) {
		env := otherTeacher.do("GET", "/proctor-events", nil, http.StatusOK)
		var list struct {
			Total int64 `json:"total"`
		}
		mustUnmarshal(t, env.Data, &list)
		if list.Total != 0 {
			t.Fatalf("其他教师的事件列表应只含本人考试的事件, 实际看到 %d 条", list.Total)
		}
		otherTeacher.do("GET", detailPath, nil, http.StatusForbidden)
		otherTeacher.do("POST", reviewPath, map[string]any{"status": "confirmed", "note": "越权处理"}, http.StatusForbidden)
	})

	t.Run("教师处理本人考试的事件", func(t *testing.T) {
		env := owner.do("POST", reviewPath, map[string]any{"status": "confirmed", "note": "确认切屏违规"}, http.StatusOK)
		var event struct {
			Status       string `json:"status"`
			ReviewNote   string `json:"review_note"`
			ReviewerName string `json:"reviewer_name"`
		}
		mustUnmarshal(t, env.Data, &event)
		if event.Status != "confirmed" || event.ReviewNote != "确认切屏违规" || event.ReviewerName != "老师甲" {
			t.Fatalf("处理结果不符合预期: %+v", event)
		}
	})

	t.Run("已处理事件再次修改被拒绝", func(t *testing.T) {
		owner.do("POST", reviewPath, map[string]any{"status": "ignored", "note": "改判"}, http.StatusConflict)
		admin.do("POST", reviewPath, map[string]any{"status": "ignored", "note": "管理员改判"}, http.StatusConflict)
	})

	t.Run("管理员可查看全部处理记录", func(t *testing.T) {
		env := admin.do("GET", "/proctor-events?status=confirmed", nil, http.StatusOK)
		var list struct {
			Total int64 `json:"total"`
			Items []struct {
				ID           uint   `json:"id"`
				ReviewNote   string `json:"review_note"`
				ReviewerName string `json:"reviewer_name"`
			} `json:"items"`
		}
		mustUnmarshal(t, env.Data, &list)
		if list.Total != 1 || len(list.Items) != 1 {
			t.Fatalf("管理员按已确认筛选应看到 1 条处理记录, 实际 %d", list.Total)
		}
		if list.Items[0].ID != eventID || list.Items[0].ReviewNote != "确认切屏违规" {
			t.Fatalf("处理记录内容不符合预期: %+v", list.Items[0])
		}
		// 结论未被后续的 409 请求篡改
		detail := admin.do("GET", detailPath, nil, http.StatusOK)
		var event struct {
			Status     string `json:"status"`
			ReviewNote string `json:"review_note"`
		}
		mustUnmarshal(t, detail.Data, &event)
		if event.Status != "confirmed" || event.ReviewNote != "确认切屏违规" {
			t.Fatalf("已提交结论被篡改: %+v", event)
		}
	})
}
