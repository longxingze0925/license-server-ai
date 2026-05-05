package service

import (
	"errors"

	"license-server/internal/model"

	"gorm.io/gorm"
)

// GenerationTaskService 生成任务的查询接口（写入由 ProxyService / AsyncRunner 负责）。
type GenerationTaskService struct{}

func NewGenerationTaskService() *GenerationTaskService { return &GenerationTaskService{} }

var ErrTaskNotFound = errors.New("任务不存在")
var ErrTaskForbidden = errors.New("无权访问该任务")

// GetForUser 取任务并校验所有权。
func (s *GenerationTaskService) GetForUser(taskID, userID string) (*model.GenerationTask, []model.GenerationFile, error) {
	var t model.GenerationTask
	err := model.DB.First(&t, "id = ?", taskID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrTaskNotFound
		}
		return nil, nil, err
	}
	if t.UserID != userID {
		return nil, nil, ErrTaskForbidden
	}
	var files []model.GenerationFile
	_ = model.DB.Where("task_id = ?", taskID).Order("created_at ASC").Find(&files).Error
	return &t, files, nil
}

// ListForUser 用户分页查看自己的任务。
type TaskListFilter struct {
	UserID   string
	Status   *model.GenerationStatus
	Provider string
	Page     int
	PageSize int
}

func (s *GenerationTaskService) ListForUser(f TaskListFilter) ([]model.GenerationTask, int64, error) {
	q := model.DB.Where("user_id = ?", f.UserID)
	if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	}
	if f.Provider != "" {
		q = q.Where("provider = ?", f.Provider)
	}
	var total int64
	if err := q.Model(&model.GenerationTask{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 || f.PageSize > 200 {
		f.PageSize = 50
	}
	var rows []model.GenerationTask
	if err := q.Order("created_at DESC").
		Offset((f.Page - 1) * f.PageSize).
		Limit(f.PageSize).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// AdminList 后台总览（不限用户）。
type AdminTaskFilter struct {
	TenantID string
	UserID   string
	Status   *model.GenerationStatus
	Provider string
	Page     int
	PageSize int
}

func (s *GenerationTaskService) AdminList(f AdminTaskFilter) ([]model.GenerationTask, int64, error) {
	q := model.DB.Model(&model.GenerationTask{})
	if f.TenantID != "" {
		q = q.Where(
			"(tenant_id = ? OR ((tenant_id IS NULL OR tenant_id = '') AND (app_id = ? OR app_id IN (SELECT id FROM applications WHERE tenant_id = ?))))",
			f.TenantID,
			f.TenantID,
			f.TenantID,
		)
	}
	if f.UserID != "" {
		q = q.Where("user_id = ?", f.UserID)
	}
	if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	}
	if f.Provider != "" {
		q = q.Where("provider = ?", f.Provider)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 || f.PageSize > 200 {
		f.PageSize = 50
	}
	var rows []model.GenerationTask
	if err := q.Order("created_at DESC").
		Offset((f.Page - 1) * f.PageSize).
		Limit(f.PageSize).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}
