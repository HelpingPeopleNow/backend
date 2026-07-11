package repository

import (
	"github.com/HelpingPeopleNow/backend/internal/core"
	"log/slog"

	"gorm.io/gorm"
)

// GormFeedbackRepository implements ports.FeedbackRepository via GORM.
type GormFeedbackRepository struct {
	db *gorm.DB
}

func NewGormFeedbackRepository(db *gorm.DB) *GormFeedbackRepository {
	return &GormFeedbackRepository{db: db}
}

func (r *GormFeedbackRepository) Create(fb *core.Feedback) error {
	if err := r.db.Create(fb).Error; err != nil {
		slog.Error("feedback_repo: create failed", "error", err)
		return err
	}
	return nil
}

func (r *GormFeedbackRepository) List(status string, limit, offset int) ([]core.Feedback, int64, error) {
	var fbs []core.Feedback
	var total int64

	q := r.db.Model(&core.Feedback{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Count(&total).Error; err != nil {
		slog.Error("feedback_repo: list count failed", "error", err)
		return nil, 0, err
	}

	err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&fbs).Error
	if err != nil {
		slog.Error("feedback_repo: list find failed", "error", err)
	}
	return fbs, total, err
}

func (r *GormFeedbackRepository) UpdateStatus(id, status, adminNote string) error {
	updates := map[string]interface{}{"status": status}
	if adminNote != "" {
		updates["admin_note"] = adminNote
	}
	if err := r.db.Model(&core.Feedback{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		slog.Error("feedback_repo: update status failed", "error", err, "id", id, "status", status)
		return err
	}
	return nil
}

func (r *GormFeedbackRepository) CountByStatus() (map[string]int64, error) {
	type row struct {
		Status string
		Count  int64
	}
	var rows []row
	err := r.db.Model(&core.Feedback{}).
		Select("status, count(*) as count").
		Group("status").
		Scan(&rows).Error
	if err != nil {
		slog.Error("feedback_repo: count by status failed", "error", err)
		return nil, err
	}
	result := make(map[string]int64, len(rows))
	for _, r := range rows {
		result[r.Status] = r.Count
	}
	return result, nil
}
