package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
	"gorm.io/gorm"
)

type GormProfileRepository struct {
	db *gorm.DB
}

func NewGormProfileRepository(db *gorm.DB) ports.ProfileRepository {
	return &GormProfileRepository{db: db}
}

func (r *GormProfileRepository) GetWorkerProfile(ctx context.Context, userID string) (*core.WorkerProfile, error) {
	var wp core.WorkerProfile
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&wp).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &wp, nil
}

func (r *GormProfileRepository) UpsertWorkerProfile(ctx context.Context, userID string, fields map[string]interface{}) error {
	var existing core.WorkerProfile
	found := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&existing).Error == nil
	wp := existing
	if !found {
		wp = core.WorkerProfile{UserID: userID}
	}

	wp.MergeFields(fields)

	if found {
		if err := r.db.WithContext(ctx).Save(&wp).Error; err != nil {
			return fmt.Errorf("save worker profile: %w", err)
		}
		slog.Info("repository: worker profile saved", "user_id", userID, "profession", wp.Profession)
	} else {
		if err := r.db.WithContext(ctx).Create(&wp).Error; err != nil {
			return fmt.Errorf("create worker profile: %w", err)
		}
		slog.Info("repository: worker profile created", "user_id", userID, "profession", wp.Profession)
	}
	return nil
}

func (r *GormProfileRepository) DeleteWorkerProfile(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&core.WorkerProfile{}).Error
}

func (r *GormProfileRepository) GetClientProfile(ctx context.Context, userID string) (*core.ClientProfile, error) {
	var cp core.ClientProfile
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&cp).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &cp, nil
}

func (r *GormProfileRepository) UpsertClientProfile(ctx context.Context, userID string, fields map[string]interface{}) error {
	var existing core.ClientProfile
	found := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&existing).Error == nil
	cp := existing
	if !found {
		cp = core.ClientProfile{UserID: userID}
	}

	cp.MergeFields(fields)

	if found {
		if err := r.db.WithContext(ctx).Save(&cp).Error; err != nil {
			return fmt.Errorf("save client profile: %w", err)
		}
		slog.Info("repository: client profile saved", "user_id", userID, "full_name", cp.FullName)
	} else {
		if err := r.db.WithContext(ctx).Create(&cp).Error; err != nil {
			return fmt.Errorf("create client profile: %w", err)
		}
		slog.Info("repository: client profile created", "user_id", userID, "full_name", cp.FullName)
	}
	return nil
}

func (r *GormProfileRepository) DeleteClientProfile(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&core.ClientProfile{}).Error
}

func (r *GormProfileRepository) FindWorkers(ctx context.Context, filters core.WorkerSearchFilters) ([]core.WorkerProfile, error) {
	query := r.db.WithContext(ctx).Model(&core.WorkerProfile{})

	if filters.Profession != "" {
		query = query.Where("profession ILIKE ?", "%"+filters.Profession+"%")
	}
	if filters.City != "" {
		query = query.Where("city ILIKE ?", "%"+filters.City+"%")
	}
	if filters.EmergencyOnly {
		query = query.Where("emergency_service = true")
	}
	if filters.FreeEstimateOnly {
		query = query.Where("free_estimate = true")
	}
	if filters.InsuredOnly {
		query = query.Where("has_insurance = true")
	}

	if filters.City != "" {
		query = query.Order(gorm.Expr("CASE WHEN LOWER(city) = LOWER(?) THEN 0 ELSE 1 END, created_at DESC", filters.City))
	} else {
		query = query.Order("created_at DESC")
	}

	query = query.Limit(50)

	var workers []core.WorkerProfile
	if err := query.Find(&workers).Error; err != nil {
		return nil, err
	}
	return workers, nil
}
