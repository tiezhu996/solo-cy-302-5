package e2etest

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/gbexam/online-exam/internal/constants"
	"github.com/gbexam/online-exam/internal/model"
	"github.com/gbexam/online-exam/internal/repository"
)

// legacyProctorEvent mirrors the proctor_events table as it existed before
// the dedup window bucket and unique index were introduced.
type legacyProctorEvent struct {
	ID         uint `gorm:"primaryKey"`
	AttemptID  uint `gorm:"index"`
	ExamID     uint `gorm:"index"`
	StudentID  uint `gorm:"index"`
	Type       string `gorm:"size:32"`
	Detail     string `gorm:"size:512"`
	Status     string `gorm:"size:16"`
	OccurredAt time.Time
	ReviewNote string `gorm:"size:512"`
	ReviewedBy uint
	ReviewedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (legacyProctorEvent) TableName() string { return "proctor_events" }

// legacyIDs of the seeded duplicate groups, for assertions.
type legacySeeds struct {
	teacherID   uint
	studentID   uint
	examID      uint
	attemptID   uint
	group1First uint // 组 1 最早一条（应保留）
	group1Dup   uint // 组 1 重复条（应删除）
	group2First uint // 组 2 待处理条（应删除）
	group2Kept  uint // 组 2 已确认条（应保留，结论不丢）
	group3Solo  uint // 组 3 单条（不动）
	group4OldA  uint // 组 4 不同窗口两条（都保留）
	group4OldB  uint
}

// seedLegacyDatabase builds a database as the pre-dedup-index version left
// it: current base tables, a legacy-shaped proctor_events table without
// window_bucket and without the unique index, and duplicate rows.
func seedLegacyDatabase(t *testing.T, db *gorm.DB) legacySeeds {
	t.Helper()
	if err := db.AutoMigrate(
		&model.User{}, &model.Question{}, &model.Exam{}, &model.ExamQuestion{},
		&model.ExamAttempt{}, &model.Answer{}, &model.WrongQuestion{},
	); err != nil {
		t.Fatalf("migrate base models: %v", err)
	}
	if err := db.AutoMigrate(&legacyProctorEvent{}); err != nil {
		t.Fatalf("migrate legacy proctor_events: %v", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("pass123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	teacher := model.User{Username: "teacher1", PasswordHash: string(hash), Name: "老师", Role: constants.RoleTeacher, Status: constants.UserActive}
	student := model.User{Username: "student1", PasswordHash: string(hash), Name: "学生甲", Role: constants.RoleStudent, Status: constants.UserActive}
	if err := db.Create(&teacher).Error; err != nil {
		t.Fatalf("create teacher: %v", err)
	}
	if err := db.Create(&student).Error; err != nil {
		t.Fatalf("create student: %v", err)
	}
	exam := model.Exam{Title: "数学测验", TotalScore: 100, DurationMinutes: 60, Status: constants.ExamPublished, CreatedBy: teacher.ID}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatalf("create exam: %v", err)
	}
	attempt := model.ExamAttempt{
		ExamID: exam.ID, StudentID: student.ID, Status: constants.AttemptInProgress,
		StartedAt: time.Now(), Deadline: time.Now().Add(time.Hour),
	}
	if err := db.Create(&attempt).Error; err != nil {
		t.Fatalf("create attempt: %v", err)
	}

	// 时间戳按去重窗口桶对齐构造，保证任意时刻运行分桶结果都确定：
	// 同组事件落在同一桶（应被合并），组 4 间隔 90 秒必跨桶（应保留）。
	aligned := time.Unix(time.Now().Unix()/constants.ProctorDedupWindowSeconds*constants.ProctorDedupWindowSeconds, 0).Add(-time.Hour)
	reviewedAt := aligned.Add(time.Minute)
	rows := []legacyProctorEvent{
		// 组 1：同场次同类型同窗口，两条均待处理 → 保留最早一条
		{AttemptID: attempt.ID, ExamID: exam.ID, StudentID: student.ID, Type: "tab_switch", Status: "pending", OccurredAt: aligned},
		{AttemptID: attempt.ID, ExamID: exam.ID, StudentID: student.ID, Type: "tab_switch", Status: "pending", OccurredAt: aligned.Add(3 * time.Second)},
		// 组 2：一条待处理 + 一条已确认 → 保留已确认，结论不丢
		{AttemptID: attempt.ID, ExamID: exam.ID, StudentID: student.ID, Type: "fullscreen_exit", Status: "pending", OccurredAt: aligned},
		{AttemptID: attempt.ID, ExamID: exam.ID, StudentID: student.ID, Type: "fullscreen_exit", Status: "confirmed", OccurredAt: aligned.Add(2 * time.Second), ReviewNote: "确认退出全屏", ReviewedBy: teacher.ID, ReviewedAt: &reviewedAt},
		// 组 3：单条记录 → 不动
		{AttemptID: attempt.ID, ExamID: exam.ID, StudentID: student.ID, Type: "page_leave", Status: "pending", OccurredAt: aligned.Add(10 * time.Second)},
		// 组 4：同类型但不同窗口（间隔 90 秒）→ 两条都保留
		{AttemptID: attempt.ID, ExamID: exam.ID, StudentID: student.ID, Type: "tab_switch", Status: "pending", OccurredAt: aligned.Add(90 * time.Second)},
		{AttemptID: attempt.ID, ExamID: exam.ID, StudentID: student.ID, Type: "tab_switch", Status: "pending", OccurredAt: aligned.Add(180 * time.Second)},
	}
	ids := make([]uint, 0, len(rows))
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatalf("insert legacy row %d: %v", i, err)
		}
		ids = append(ids, rows[i].ID)
	}
	return legacySeeds{
		teacherID: teacher.ID, studentID: student.ID, examID: exam.ID, attemptID: attempt.ID,
		group1First: ids[0], group1Dup: ids[1],
		group2First: ids[2], group2Kept: ids[3],
		group3Solo: ids[4], group4OldA: ids[5], group4OldB: ids[6],
	}
}

// TestLegacyDatabaseUpgrade 场景：旧库存在同场次同类型重复记录时，新版本
// 启动自动完成迁移（不报错退出），重复记录被合并、已处理结论不丢失，
// 升级后复核流程与并发去重仍然可用。
func TestLegacyDatabaseUpgrade(t *testing.T) {
	db := openTestDB(t)
	seeds := seedLegacyDatabase(t, db)

	t.Run("旧库启动自动迁移不报错", func(t *testing.T) {
		if err := repository.NewRepository(db).AutoMigrate(); err != nil {
			t.Fatalf("旧库升级启动应自动完成迁移, 实际报错: %v", err)
		}
	})

	t.Run("去重唯一索引与窗口桶就绪", func(t *testing.T) {
		if !db.Migrator().HasIndex(&model.ProctorEvent{}, "uk_proctor_dedup") {
			t.Fatal("旧库迁移后应建立 uk_proctor_dedup 唯一索引")
		}
		var events []model.ProctorEvent
		if err := db.Find(&events).Error; err != nil {
			t.Fatalf("list events: %v", err)
		}
		for _, e := range events {
			want := e.OccurredAt.Unix() / constants.ProctorDedupWindowSeconds
			if e.WindowBucket != want {
				t.Fatalf("事件 %d 的 window_bucket = %d, 期望按发生时间回填为 %d", e.ID, e.WindowBucket, want)
			}
		}
	})

	t.Run("重复记录已合并且已处理结论不丢", func(t *testing.T) {
		var survived []model.ProctorEvent
		if err := db.Order("id ASC").Find(&survived).Error; err != nil {
			t.Fatalf("list survived: %v", err)
		}
		if len(survived) != 5 {
			t.Fatalf("迁移后留存事件 = %d 条, 期望 5 条（7 条旧数据合并 2 条重复）", len(survived))
		}
		byID := make(map[uint]model.ProctorEvent, len(survived))
		for _, e := range survived {
			byID[e.ID] = e
		}
		if _, ok := byID[seeds.group1First]; !ok {
			t.Error("组 1: 最早的待处理事件应保留")
		}
		if _, ok := byID[seeds.group1Dup]; ok {
			t.Error("组 1: 重复事件应被删除")
		}
		kept, ok := byID[seeds.group2Kept]
		if !ok {
			t.Fatal("组 2: 已确认事件应保留（结论不丢失）")
		}
		if kept.Status != constants.ProctorStatusConfirmed || kept.ReviewNote != "确认退出全屏" || kept.ReviewedBy != seeds.teacherID {
			t.Errorf("组 2: 已处理结论丢失或被篡改: %+v", kept)
		}
		if _, ok := byID[seeds.group2First]; ok {
			t.Error("组 2: 与已确认事件重复的待处理事件应被删除")
		}
		for _, id := range []uint{seeds.group3Solo, seeds.group4OldA, seeds.group4OldB} {
			if _, ok := byID[id]; !ok {
				t.Errorf("事件 %d 不属于重复组, 应保留", id)
			}
		}
	})

	t.Run("迁移可重复执行", func(t *testing.T) {
		if err := repository.NewRepository(db).AutoMigrate(); err != nil {
			t.Fatalf("重复执行迁移应无副作用, 实际报错: %v", err)
		}
		var count int64
		if err := db.Model(&model.ProctorEvent{}).Count(&count).Error; err != nil {
			t.Fatalf("count events: %v", err)
		}
		if count != 5 {
			t.Fatalf("重复迁移后事件数 = %d, 期望仍为 5", count)
		}
	})

	t.Run("升级后复核流程可用", func(t *testing.T) {
		srv := newTestServer(t, db)
		teacher := newClient(t, srv)
		teacher.login("teacher1", "pass123")

		env := teacher.do("GET", "/proctor-events", nil, http.StatusOK)
		var list struct {
			Total int64 `json:"total"`
			Items []struct {
				ID     uint   `json:"id"`
				Status string `json:"status"`
			} `json:"items"`
		}
		mustUnmarshal(t, env.Data, &list)
		if list.Total != 5 {
			t.Fatalf("升级后教师应看到 5 条事件, 实际 %d", list.Total)
		}
		var pendingID uint
		for _, item := range list.Items {
			if item.Status == constants.ProctorStatusPending {
				pendingID = item.ID
				break
			}
		}
		if pendingID == 0 {
			t.Fatal("升级后应仍存在待处理事件")
		}
		teacher.do("POST", fmt.Sprintf("/proctor-events/%d/review", pendingID), map[string]any{"status": "ignored", "note": "升级后处理"}, http.StatusOK)
		teacher.do("POST", fmt.Sprintf("/proctor-events/%d/review", pendingID), map[string]any{"status": "confirmed", "note": "改判"}, http.StatusConflict)
	})

	t.Run("升级后并发上报仍只保留一条", func(t *testing.T) {
		srv := newTestServer(t, db)
		student := newClient(t, srv)
		student.login("student1", "pass123")
		results := fireConcurrentReports(t, srv, student.token, seeds.attemptID, "page_leave", 32)
		var stored int64
		if err := db.Model(&model.ProctorEvent{}).
			Where("attempt_id = ? AND type = ? AND window_bucket = ?", seeds.attemptID, "page_leave", time.Now().Unix()/constants.ProctorDedupWindowSeconds).
			Count(&stored).Error; err != nil {
			t.Fatalf("count events: %v", err)
		}
		assertSingleStoredEvent(t, results, stored)
	})
}
