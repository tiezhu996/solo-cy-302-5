package e2etest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gbexam/online-exam/internal/model"
)

// TestFreshStartupConcurrentReport 场景：新库启动后，同一场次同一事件类型
// 被并发上报多次时，数据库只保留一条，且每个调用方都拿到明确结果。
func TestFreshStartupConcurrentReport(t *testing.T) {
	db := openTestDB(t)
	migrateFresh(t, db)

	t.Run("新库启动后去重唯一索引已建立", func(t *testing.T) {
		if !db.Migrator().HasIndex(&model.ProctorEvent{}, "uk_proctor_dedup") {
			t.Fatal("新库 AutoMigrate 后应存在 uk_proctor_dedup 唯一索引")
		}
	})

	srv := newTestServer(t, db)
	teacher := newClient(t, srv)
	student := newClient(t, srv)
	teacher.register("teacher1", "pass123", "老师", "teacher")
	student.register("student1", "pass123", "学生甲", "student")
	_, attemptID := seedPublishedAttempt(t, teacher, student)

	t.Run("同场次同类型并发上报只保留一条", func(t *testing.T) {
		results := fireConcurrentReports(t, srv, student.token, attemptID, "tab_switch", 32)
		var stored int64
		if err := db.Model(&model.ProctorEvent{}).Where("attempt_id = ? AND type = ?", attemptID, "tab_switch").Count(&stored).Error; err != nil {
			t.Fatalf("count events: %v", err)
		}
		assertSingleStoredEvent(t, results, stored)
	})

	t.Run("窗口内重复上报合并到已有事件", func(t *testing.T) {
		env := student.do("POST", "/proctor-events", map[string]any{"attempt_id": attemptID, "type": "tab_switch"}, http.StatusOK)
		var data struct {
			Event struct {
				ID uint `json:"id"`
			} `json:"event"`
			Deduplicated bool `json:"deduplicated"`
		}
		mustUnmarshal(t, env.Data, &data)
		if !data.Deduplicated {
			t.Fatal("窗口内重复上报应返回 deduplicated=true")
		}
		var stored int64
		if err := db.Model(&model.ProctorEvent{}).Where("attempt_id = ? AND type = ?", attemptID, "tab_switch").Count(&stored).Error; err != nil {
			t.Fatalf("count events: %v", err)
		}
		if stored != 1 {
			t.Fatalf("入库事件数 = %d, 期望仍为 1 条", stored)
		}
	})

	t.Run("不同类型事件互不影响", func(t *testing.T) {
		env := student.do("POST", "/proctor-events", map[string]any{"attempt_id": attemptID, "type": "fullscreen_exit"}, http.StatusOK)
		var data struct {
			Deduplicated bool `json:"deduplicated"`
		}
		mustUnmarshal(t, env.Data, &data)
		if data.Deduplicated {
			t.Fatal("不同类型事件不应被去重")
		}
	})
}

// TestReportGuards 场景：上报接口的边界结果明确——他人场次 403、已交卷 400。
func TestReportGuards(t *testing.T) {
	db := openTestDB(t)
	migrateFresh(t, db)
	srv := newTestServer(t, db)
	teacher := newClient(t, srv)
	student := newClient(t, srv)
	other := newClient(t, srv)
	teacher.register("teacher1", "pass123", "老师", "teacher")
	student.register("student1", "pass123", "学生甲", "student")
	other.register("student2", "pass123", "学生乙", "student")
	_, attemptID := seedPublishedAttempt(t, teacher, student)

	t.Run("不能上报他人考试场次", func(t *testing.T) {
		other.do("POST", "/proctor-events", map[string]any{"attempt_id": attemptID, "type": "tab_switch"}, http.StatusForbidden)
	})

	t.Run("未登录上报返回401", func(t *testing.T) {
		newClient(t, srv).do("POST", "/proctor-events", map[string]any{"attempt_id": attemptID, "type": "tab_switch"}, http.StatusUnauthorized)
	})

	t.Run("交卷后上报被拒绝", func(t *testing.T) {
		student.do("POST", fmt.Sprintf("/attempts/%d/submit", attemptID), nil, http.StatusOK)
		student.do("POST", "/proctor-events", map[string]any{"attempt_id": attemptID, "type": "page_leave"}, http.StatusBadRequest)
	})
}

func mustUnmarshal(t *testing.T, raw []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}
