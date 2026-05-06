package service

import (
	"encoding/json"
	"errors"
	"time"

	"license-server/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CreditService 用户额度服务。
//
// 三个核心操作（Adjust / Reserve / Refund）每个都在 DB 事务里做：
//  1. 锁定用户余额行（SELECT ... FOR UPDATE）
//  2. 校验 / 计算新余额
//  3. UPDATE user_credits + INSERT credit_transactions（同一事务）
//
// 流水表是审计源，永不删；balance == sum(transactions.amount) 必须恒成立。
type CreditService struct{}

func NewCreditService() *CreditService { return &CreditService{} }

// 错误。
var (
	ErrInsufficientBalance   = errors.New("余额不足")
	ErrUserCreditNotFound    = errors.New("用户额度记录不存在")
	ErrTaskAlreadyRefunded   = errors.New("该任务已经退过款")
	ErrConsumeRecordNotFound = errors.New("找不到对应的扣款流水")
	ErrUserNotInTenant       = errors.New("用户不属于当前租户")
	ErrConcurrentLimit       = errors.New("超过并发上限")
)

// ReserveTaskInput 预扣并创建任务的入参。
type ReserveTaskInput struct {
	UserID      string
	Cost        int64
	Task        *model.GenerationTask
	RuleID      int64
	LimitStatus []model.GenerationStatus
}

type FailTaskRefundResult struct {
	Refunded     bool
	Amount       int64
	RefundStatus model.GenerationRefundStatus
	RefundedAt   *time.Time
}

// EnsureUser 确保用户额度行存在；不存在则按默认值创建（balance=0, concurrent_limit=1）。
func (s *CreditService) EnsureUser(userID string) (*model.UserCredit, error) {
	var row model.UserCredit
	err := model.DB.First(&row, "user_id = ?", userID).Error
	if err == nil {
		return &row, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	row = model.UserCredit{UserID: userID, Balance: 0, ConcurrentLimit: 1}
	if err := model.DB.Create(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// Get 取用户额度（不存在自动创建）。
func (s *CreditService) Get(userID string) (*model.UserCredit, error) {
	return s.EnsureUser(userID)
}

func (s *CreditService) ensureUserInTenant(userID, tenantID string) error {
	if userID == "" || tenantID == "" {
		return ErrUserNotInTenant
	}
	var count int64
	if err := model.DB.Model(&model.TeamMember{}).
		Where("id = ? AND tenant_id = ?", userID, tenantID).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		if err := model.DB.Model(&model.Customer{}).
			Where("id = ? AND tenant_id = ?", userID, tenantID).
			Count(&count).Error; err != nil {
			return err
		}
	}
	if count == 0 {
		return ErrUserNotInTenant
	}
	return nil
}

// GetForTenant 取当前租户内用户额度（不存在自动创建）。
func (s *CreditService) GetForTenant(userID, tenantID string) (*model.UserCredit, error) {
	if err := s.ensureUserInTenant(userID, tenantID); err != nil {
		return nil, err
	}
	return s.EnsureUser(userID)
}

// Adjust 后台手动调整余额。amount 可正可负。
// 写流水 type=adjust。
func (s *CreditService) Adjust(userID string, amount int64, operatorID, note string) (*model.UserCredit, *model.CreditTransaction, error) {
	if amount == 0 {
		return nil, nil, errors.New("调整金额不能为 0")
	}
	if _, err := s.EnsureUser(userID); err != nil {
		return nil, nil, err
	}

	var (
		updated model.UserCredit
		tx      model.CreditTransaction
	)
	err := model.DB.Transaction(func(db *gorm.DB) error {
		var row model.UserCredit
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&row, "user_id = ?", userID).Error; err != nil {
			return err
		}
		newBalance := row.Balance + amount
		if newBalance < 0 {
			return ErrInsufficientBalance
		}
		row.Balance = newBalance
		if amount > 0 {
			row.TotalTopup += amount
		}
		if err := db.Save(&row).Error; err != nil {
			return err
		}
		tx = model.CreditTransaction{
			UserID:       userID,
			Amount:       amount,
			Type:         model.TransactionAdjust,
			BalanceAfter: newBalance,
			OperatorID:   operatorID,
			Note:         note,
		}
		if err := db.Create(&tx).Error; err != nil {
			return err
		}
		updated = row
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &updated, &tx, nil
}

// AdjustForTenant 后台在当前租户内手动调整余额。
func (s *CreditService) AdjustForTenant(userID, tenantID string, amount int64, operatorID, note string) (*model.UserCredit, *model.CreditTransaction, error) {
	if err := s.ensureUserInTenant(userID, tenantID); err != nil {
		return nil, nil, err
	}
	return s.Adjust(userID, amount, operatorID, note)
}

// Reserve 转发任务执行前预扣点数。
// taskID 必填，便于失败时退款关联。ruleID 可选（来自计价匹配）。
// 返回 ErrInsufficientBalance 表示余额不足。
func (s *CreditService) Reserve(userID string, cost int64, taskID string, ruleID int64) (*model.UserCredit, *model.CreditTransaction, error) {
	if cost <= 0 {
		return nil, nil, errors.New("扣点数必须为正")
	}
	if taskID == "" {
		return nil, nil, errors.New("taskID 不能为空")
	}
	if _, err := s.EnsureUser(userID); err != nil {
		return nil, nil, err
	}

	var (
		updated model.UserCredit
		tx      model.CreditTransaction
	)
	err := model.DB.Transaction(func(db *gorm.DB) error {
		var row model.UserCredit
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&row, "user_id = ?", userID).Error; err != nil {
			return err
		}
		if row.Balance < cost {
			return ErrInsufficientBalance
		}
		row.Balance -= cost
		row.TotalConsumed += cost
		if err := db.Save(&row).Error; err != nil {
			return err
		}
		tx = model.CreditTransaction{
			UserID:       userID,
			Amount:       -cost,
			Type:         model.TransactionConsume,
			TaskID:       taskID,
			RuleID:       ruleID,
			BalanceAfter: row.Balance,
		}
		if err := db.Create(&tx).Error; err != nil {
			return err
		}
		updated = row
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &updated, &tx, nil
}

// ReserveAndCreateTask 在同一事务里锁定用户额度、检查并发上限、扣点、写流水、创建任务。
func (s *CreditService) ReserveAndCreateTask(in ReserveTaskInput) (*model.UserCredit, *model.CreditTransaction, error) {
	if in.Cost <= 0 {
		return nil, nil, errors.New("扣点数必须为正")
	}
	if in.Task == nil || in.Task.ID == "" {
		return nil, nil, errors.New("taskID 不能为空")
	}
	if in.UserID == "" {
		in.UserID = in.Task.UserID
	}
	if in.UserID == "" {
		return nil, nil, errors.New("user_id 不能为空")
	}
	if in.Task.UserID == "" {
		in.Task.UserID = in.UserID
	}
	if in.Task.UserID != in.UserID {
		return nil, nil, errors.New("任务用户与扣款用户不一致")
	}
	if in.RuleID == 0 {
		in.RuleID = in.Task.RuleID
	}
	if _, err := s.EnsureUser(in.UserID); err != nil {
		return nil, nil, err
	}
	if len(in.LimitStatus) == 0 {
		in.LimitStatus = []model.GenerationStatus{model.GenerationPending, model.GenerationRunning}
	}

	var (
		updated model.UserCredit
		tx      model.CreditTransaction
	)
	err := model.DB.Transaction(func(db *gorm.DB) error {
		var row model.UserCredit
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&row, "user_id = ?", in.UserID).Error; err != nil {
			return err
		}
		if row.ConcurrentLimit > 0 {
			var running int64
			if err := db.Model(&model.GenerationTask{}).
				Where("user_id = ? AND status IN ?", in.UserID, in.LimitStatus).
				Count(&running).Error; err != nil {
				return err
			}
			if running >= int64(row.ConcurrentLimit) {
				return ErrConcurrentLimit
			}
		}
		if row.Balance < in.Cost {
			return ErrInsufficientBalance
		}
		row.Balance -= in.Cost
		row.TotalConsumed += in.Cost
		if err := db.Save(&row).Error; err != nil {
			return err
		}
		tx = model.CreditTransaction{
			UserID:       in.UserID,
			Amount:       -in.Cost,
			Type:         model.TransactionConsume,
			TaskID:       in.Task.ID,
			RuleID:       in.RuleID,
			BalanceAfter: row.Balance,
		}
		if err := db.Create(&tx).Error; err != nil {
			return err
		}
		if err := db.Create(in.Task).Error; err != nil {
			return err
		}
		updated = row
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &updated, &tx, nil
}

// Refund 任务失败时按 taskID 退回原 consume 的全额。
// 幂等保护：若已存在 type=refund 且同 task_id 的记录，直接返回 ErrTaskAlreadyRefunded。
// 找不到 consume 记录 → ErrConsumeRecordNotFound。
func (s *CreditService) Refund(taskID, note string) (*model.UserCredit, *model.CreditTransaction, error) {
	if taskID == "" {
		return nil, nil, errors.New("taskID 不能为空")
	}

	var (
		updated model.UserCredit
		tx      model.CreditTransaction
	)
	err := model.DB.Transaction(func(db *gorm.DB) error {
		// 检查是否已退款过（幂等）
		var existing int64
		if err := db.Model(&model.CreditTransaction{}).
			Where("task_id = ? AND type = ?", taskID, model.TransactionRefund).
			Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return ErrTaskAlreadyRefunded
		}

		// 找原扣款流水
		var consume model.CreditTransaction
		if err := db.Where("task_id = ? AND type = ?", taskID, model.TransactionConsume).
			Order("id ASC").
			First(&consume).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrConsumeRecordNotFound
			}
			return err
		}
		amountToRefund := -consume.Amount // consume 是负数，退回正数
		userID := consume.UserID

		// 锁余额行 + 加回去
		var row model.UserCredit
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&row, "user_id = ?", userID).Error; err != nil {
			return err
		}
		row.Balance += amountToRefund
		row.TotalConsumed -= amountToRefund
		if err := db.Save(&row).Error; err != nil {
			return err
		}

		tx = model.CreditTransaction{
			UserID:       userID,
			Amount:       amountToRefund,
			Type:         model.TransactionRefund,
			TaskID:       taskID,
			RuleID:       consume.RuleID,
			BalanceAfter: row.Balance,
			Note:         note,
		}
		if err := db.Create(&tx).Error; err != nil {
			return err
		}
		updated = row
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &updated, &tx, nil
}

func (s *CreditService) FailTaskAndRefund(taskID, reason, refundNote string) (*FailTaskRefundResult, error) {
	if taskID == "" {
		return nil, errors.New("taskID 不能为空")
	}
	if refundNote == "" {
		refundNote = reason
	}

	var result FailTaskRefundResult
	err := model.DB.Transaction(func(db *gorm.DB) error {
		var task model.GenerationTask
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&task, "id = ?", taskID).Error; err != nil {
			return err
		}

		now := time.Now()
		if task.RefundStatus == "" {
			task.RefundStatus = model.GenerationRefundNone
		}

		refundStatus := task.RefundStatus
		refundAmount := task.RefundAmount
		var refundedAt *time.Time
		if task.RefundedAt != nil {
			refundedAt = task.RefundedAt
		}

		if refundStatus != model.GenerationRefunded && refundStatus != model.GenerationRefundSkipped {
			var existing int64
			if err := db.Model(&model.CreditTransaction{}).
				Where("task_id = ? AND type = ?", taskID, model.TransactionRefund).
				Count(&existing).Error; err != nil {
				return err
			}

			if existing > 0 {
				refundStatus = model.GenerationRefunded
				if refundedAt == nil {
					refundedAt = &now
				}
				if refundAmount == 0 {
					var refund model.CreditTransaction
					if err := db.Where("task_id = ? AND type = ?", taskID, model.TransactionRefund).
						Order("id ASC").
						First(&refund).Error; err == nil {
						refundAmount = refund.Amount
					}
				}
			} else {
				var consume model.CreditTransaction
				if err := db.Where("task_id = ? AND type = ?", taskID, model.TransactionConsume).
					Order("id ASC").
					First(&consume).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						refundStatus = model.GenerationRefundSkipped
					} else {
						return err
					}
				} else {
					amountToRefund := -consume.Amount
					var row model.UserCredit
					if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
						First(&row, "user_id = ?", consume.UserID).Error; err != nil {
						return err
					}
					row.Balance += amountToRefund
					row.TotalConsumed -= amountToRefund
					if err := db.Save(&row).Error; err != nil {
						return err
					}

					tx := model.CreditTransaction{
						UserID:       consume.UserID,
						Amount:       amountToRefund,
						Type:         model.TransactionRefund,
						TaskID:       taskID,
						RuleID:       consume.RuleID,
						BalanceAfter: row.Balance,
						Note:         refundNote,
					}
					if err := db.Create(&tx).Error; err != nil {
						return err
					}
					refundStatus = model.GenerationRefunded
					refundAmount = amountToRefund
					refundedAt = &now
				}
			}
		}

		errJSON, _ := json.Marshal(map[string]string{"reason": reason})
		if err := db.Model(&model.GenerationTask{}).
			Where("id = ?", taskID).
			Updates(map[string]any{
				"status":         model.GenerationFailed,
				"error_json":     string(errJSON),
				"upstream_error": reason,
				"refund_status":  refundStatus,
				"refund_amount":  refundAmount,
				"refunded_at":    refundedAt,
				"next_poll_at":   nil,
				"completed_at":   &now,
			}).Error; err != nil {
			return err
		}

		result = FailTaskRefundResult{
			Refunded:     refundStatus == model.GenerationRefunded,
			Amount:       refundAmount,
			RefundStatus: refundStatus,
			RefundedAt:   refundedAt,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// SetConcurrentLimit 修改并发上限。
func (s *CreditService) SetConcurrentLimit(userID string, limit int) error {
	if limit < 0 {
		return errors.New("并发上限不能为负")
	}
	if _, err := s.EnsureUser(userID); err != nil {
		return err
	}
	return model.DB.Model(&model.UserCredit{}).
		Where("user_id = ?", userID).
		Update("concurrent_limit", limit).Error
}

// SetConcurrentLimitForTenant 修改当前租户内用户并发上限。
func (s *CreditService) SetConcurrentLimitForTenant(userID, tenantID string, limit int) error {
	if err := s.ensureUserInTenant(userID, tenantID); err != nil {
		return err
	}
	return s.SetConcurrentLimit(userID, limit)
}

// CheckIntegrity 启动时跑一次：校验 balance == sum(transactions.amount) 对所有用户成立。
// 返回不一致用户列表。返回值为 nil 表示全部一致。
type IntegrityIssue struct {
	UserID        string
	StoredBalance int64
	SumOfTxAmount int64
}

func (s *CreditService) CheckIntegrity() ([]IntegrityIssue, error) {
	rows, err := model.DB.Raw(`
		SELECT uc.user_id,
		       uc.balance AS stored,
		       COALESCE(SUM(ct.amount), 0) AS sum_amount
		FROM user_credits uc
		LEFT JOIN credit_transactions ct ON ct.user_id = uc.user_id
		GROUP BY uc.user_id, uc.balance
		HAVING uc.balance <> COALESCE(SUM(ct.amount), 0)
	`).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var issues []IntegrityIssue
	for rows.Next() {
		var iss IntegrityIssue
		if err := rows.Scan(&iss.UserID, &iss.StoredBalance, &iss.SumOfTxAmount); err != nil {
			return nil, err
		}
		issues = append(issues, iss)
	}
	return issues, nil
}

// ListUsers 后台分页列出已开通额度的用户。
type UserCreditWithEmail struct {
	UserID          string    `gorm:"column:user_id" json:"user_id"`
	Balance         int64     `gorm:"column:balance" json:"balance"`
	TotalTopup      int64     `gorm:"column:total_topup" json:"total_topup"`
	TotalConsumed   int64     `gorm:"column:total_consumed" json:"total_consumed"`
	ConcurrentLimit int       `gorm:"column:concurrent_limit" json:"concurrent_limit"`
	UpdatedAt       time.Time `gorm:"column:updated_at" json:"updated_at"`
	CreatedAt       time.Time `gorm:"column:created_at" json:"created_at"`
	Email           string    `gorm:"column:email" json:"email"`
	Name            string    `gorm:"column:name" json:"name"`
	UserType        string    `gorm:"column:user_type" json:"user_type"`
}

func (s *CreditService) ListUsers(tenantID, keyword string, page, pageSize int) ([]UserCreditWithEmail, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}

	base := model.DB.Table("user_credits AS uc").
		Joins("LEFT JOIN team_members AS tm ON tm.id = uc.user_id AND tm.tenant_id = ?", tenantID).
		Joins("LEFT JOIN customers AS c ON c.id = uc.user_id AND c.tenant_id = ?", tenantID).
		Where("tm.id IS NOT NULL OR c.id IS NOT NULL")
	if keyword != "" {
		like := "%" + keyword + "%"
		base = base.Where("tm.email LIKE ? OR tm.name LIKE ? OR c.email LIKE ? OR c.name LIKE ?", like, like, like, like)
	}

	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []UserCreditWithEmail
	if err := base.Session(&gorm.Session{}).
		Select(`
			uc.user_id,
			uc.balance,
			uc.total_topup,
			uc.total_consumed,
			uc.concurrent_limit,
			uc.updated_at,
			uc.created_at,
			COALESCE(NULLIF(tm.email, ''), c.email) AS email,
			COALESCE(NULLIF(tm.name, ''), c.name) AS name,
			CASE WHEN c.id IS NOT NULL THEN 'customer' ELSE 'team_member' END AS user_type
		`).
		Order("uc.updated_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// EnableForTenant 在当前租户内开通用户额度。
func (s *CreditService) EnableForTenant(userID, tenantID string) (*model.UserCredit, error) {
	if err := s.ensureUserInTenant(userID, tenantID); err != nil {
		return nil, err
	}
	return s.EnsureUser(userID)
}

// ListTransactions 取某用户的流水（倒序）。
func (s *CreditService) ListTransactions(userID string, page, pageSize int) ([]model.CreditTransaction, int64, error) {
	q := model.DB.Where("user_id = ?", userID)
	var total int64
	if err := q.Model(&model.CreditTransaction{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}
	var rows []model.CreditTransaction
	if err := q.Order("id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ListTransactionsForTenant 取当前租户内某用户流水（倒序）。
func (s *CreditService) ListTransactionsForTenant(userID, tenantID string, page, pageSize int) ([]model.CreditTransaction, int64, error) {
	if err := s.ensureUserInTenant(userID, tenantID); err != nil {
		return nil, 0, err
	}
	return s.ListTransactions(userID, page, pageSize)
}
