package constants

// Roles
const (
	RoleAdmin   = "admin"
	RoleTeacher = "teacher"
	RoleStudent = "student"
)

// Question types
const (
	QuestionSingle     = "single"
	QuestionMultiple   = "multiple"
	QuestionTrueFalse  = "true_false"
	QuestionFillBlank  = "fill_blank"
	QuestionShortAnswer = "short_answer"
)

// Difficulties
const (
	DifficultyEasy   = "easy"
	DifficultyMedium = "medium"
	DifficultyHard   = "hard"
)

// Exam statuses
const (
	ExamDraft     = "draft"
	ExamPublished = "published"
	ExamClosed    = "closed"
)

// Attempt statuses
const (
	AttemptInProgress = "in_progress"
	AttemptSubmitted  = "submitted"
)

// Wrong question statuses
const (
	WrongUnresolved = "unresolved"
	WrongResolved   = "resolved"
)

// User statuses
const (
	UserActive   = "active"
	UserDisabled = "disabled"
)

// Proctoring event types reported from the exam page.
const (
	ProctorEventTabSwitch      = "tab_switch"
	ProctorEventFullscreenExit = "fullscreen_exit"
	ProctorEventPageLeave      = "page_leave"
)

// Proctoring event review statuses.
const (
	ProctorStatusPending   = "pending"
	ProctorStatusConfirmed = "confirmed"
	ProctorStatusIgnored   = "ignored"
)

// ProctorDedupWindowSeconds is the dedup window (in seconds) for proctor
// event reports. It sizes the window_bucket column of the uk_proctor_dedup
// unique index, so the service layer and the legacy-data migration must
// share this single source of truth.
const ProctorDedupWindowSeconds int64 = 30
