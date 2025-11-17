package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_back/internal/models"
)

type ValuationReportRepository interface {
	Save(ctx context.Context, r *models.ValuationReport) error
	GetByID(ctx context.Context, id uuid.UUID) (*models.ValuationReport, error)
}

type valuationReportRepo struct {
	db *gorm.DB
}

func NewValuationReportRepository(db *gorm.DB) ValuationReportRepository {
	return &valuationReportRepo{db: db}
}

func (r *valuationReportRepo) Save(ctx context.Context, vr *models.ValuationReport) error {
	if vr.ID == uuid.Nil {
		vr.ID = uuid.New()
	}
	if err := r.db.WithContext(ctx).Create(vr).Error; err != nil {
		return fmt.Errorf("create valuation_report: %w", err)
	}
	return nil
}

func (r *valuationReportRepo) GetByID(ctx context.Context, id uuid.UUID) (*models.ValuationReport, error) {
	var vr models.ValuationReport
	if err := r.db.WithContext(ctx).First(&vr, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get valuation_report: %w", err)
	}
	return &vr, nil
}
