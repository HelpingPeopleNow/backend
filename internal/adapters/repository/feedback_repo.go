package repository

import (
	"github.com/HelpingPeopleNow/backend/internal/core"
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
	return r.db.Create(fb).Error
}

func (r *GormFeedbackRepository) List(status string, limit, offset int) ([]core.Feedback, int64, error) {
	var fbs []core.Feedback
	var total int64

	q := r.db.Model(&core.Feedback{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&fbs).Error
	return fbs, total, err
}

func (r *GormFeedbackRepository) UpdateStatus(id, status, adminNote string) error {
	updates := map[string]interface{}{"status": status}
	if adminNote != "" {
		updates["admin_note"] = adminNote
	}
	return r.db.Model(&core.Feedback{}).Where("id = ?", id).Updates(updates).Error
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
		return nil, err
	}
	result := make(map[string]int64, len(rows))
	for _, r := range rows {
		result[r.Status] = r.Count
	}
	return result, nil
}
