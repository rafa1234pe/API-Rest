package request

import (
	"ApiRestFinance/internal/model/entities/enums"
	"time"
)

type CreateInstallmentRequest struct {
	CreditAccountID uint                    `json:"credit_account_id" binding:"required"`
	DueDate         time.Time               `json:"due_date" binding:"required"`
	Amount          float64                 `json:"amount" binding:"required,gt=0"`
	Status          enums.InstallmentStatus `json:"status" binding:"required"`
}
