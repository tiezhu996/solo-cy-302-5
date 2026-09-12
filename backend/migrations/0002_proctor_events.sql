-- Migration 0002: proctoring event review center.
-- Anti-cheating events reported during exams; review conclusions are immutable
-- once the status leaves 'pending'.

CREATE TABLE IF NOT EXISTS proctor_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    attempt_id BIGINT UNSIGNED NOT NULL,
    exam_id BIGINT UNSIGNED NOT NULL,
    student_id BIGINT UNSIGNED NOT NULL,
    type VARCHAR(32) NOT NULL,
    detail VARCHAR(512) DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    occurred_at DATETIME(3) NOT NULL,
    review_note VARCHAR(512) DEFAULT '',
    reviewed_by BIGINT UNSIGNED DEFAULT 0,
    reviewed_at DATETIME(3) NULL,
    created_at DATETIME(3) NULL,
    updated_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    KEY idx_proctor_events_attempt_id (attempt_id),
    KEY idx_proctor_events_exam_id (exam_id),
    KEY idx_proctor_events_student_id (student_id),
    KEY idx_proctor_events_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
