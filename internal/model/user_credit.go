package model

import "time"

// UserCredit 用户额度
//
// 余额变动铁律：任何对 Balance 的修改必须同时插入一条 CreditTransaction，
// 且必须放在同一 DB 事务内。启动时校验 balance == sum(transactions.amount)。
type UserCredit struct {
	UserID           string    `gorm:"type:varchar(36);primaryKey" json:"user_id"`
	Balance          int64     `gorm:"not null;default:0" json:"balance"`
	TotalTopup       int64     `gorm:"not null;default:0" json:"total_topup"`
	TotalConsumed    int64     `gorm:"not null;default:0" json:"total_consumed"`
	ConcurrentLimit  int       `gorm:"not null;default:1" json:"concurrent_limit"` // 0 = 不限
	UpdatedAt        time.Time `gorm:"autoUpdateTime" json:"updated_at"`
	CreatedAt        time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (UserCredit) TableName() string {
	return "user_credits"
}

// CreditTransaction 额度流水（账本）
//
// 永不删除。所有余额变更都要追加一条。
type CreditTransaction struct {
	ID            int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID        string         `gorm:"type:varchar(36);not null;index:idx_user_created" json:"user_id"`
	Amount        int64          `gorm:"not null" json:"amount"` // 正=入账，负=扣减
	Type          TransactionTyp `gorm:"type:varchar(16);not null" json:"type"`
	TaskID        string         `gorm:"type:varchar(36);index" json:"task_id,omitempty"`
	RuleID        int64          `gorm:"index" json:"rule_id,omitempty"`
	BalanceAfter  int64          `gorm:"not null" json:"balance_after"`
	OperatorID    string         `gorm:"type:varchar(36)" json:"operator_id,omitempty"` // adjust 时记后台操作员
	Note          string         `gorm:"type:varchar(256)" json:"note"`
	CreatedAt     time.Time      `gorm:"autoCreateTime;index:idx_user_created" json:"created_at"`
}

// TransactionTyp 流水类型
type TransactionTyp string

const (
	TransactionAdjust  TransactionTyp = "adjust"  // 后台手动调整
	TransactionConsume TransactionTyp = "consume" // 任务消费
	TransactionRefund  TransactionTyp = "refund"  // 任务失败退款
)

func (CreditTransaction) TableName() string {
	return "credit_transactions"
}
