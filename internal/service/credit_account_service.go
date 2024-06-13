package service

import (
	"errors"
	"fmt"
	"math"
	"time"

	"ApiRestFinance/internal/model/dto/request"
	"ApiRestFinance/internal/model/dto/response"
	"ApiRestFinance/internal/model/entities"
	"ApiRestFinance/internal/model/entities/enums"
	"ApiRestFinance/internal/repository"
	"gorm.io/gorm"
)

// CreditAccountService defines methods for managing credit accounts.
type CreditAccountService interface {
	CreateCreditAccount(req request.CreateCreditAccountRequest) (*response.CreditAccountResponse, error)
	GetCreditAccountByID(id uint) (*response.CreditAccountResponse, error)
	UpdateCreditAccount(id uint, req request.UpdateCreditAccountRequest) (*response.CreditAccountResponse, error)
	DeleteCreditAccount(id uint) error
	GetCreditAccountsByEstablishmentID(establishmentID uint) ([]response.CreditAccountResponse, error) // Return type corrected
	GetCreditAccountsByClientID(clientID uint) ([]response.CreditAccountResponse, error)
	ApplyInterestToAllAccounts(establishmentID uint) error
	ApplyLateFeesToAllAccounts(establishmentID uint) error
	GetAdminDebtSummary(establishmentID uint) ([]response.AdminDebtSummary, error)
	ProcessPurchase(creditAccountID uint, amount float64, description string) error
	ProcessPayment(creditAccountID uint, amount float64, description string) error

	CreateCreditRequest(req request.CreateCreditRequest) (*response.CreditRequestResponse, error)
	GetCreditRequestByID(id uint) (*response.CreditRequestResponse, error)
	ApproveCreditRequest(creditRequestID uint, adminID uint) (*response.CreditAccountResponse, error)
	RejectCreditRequest(creditRequestID uint, adminID uint) error
	GetPendingCreditRequests(establishmentID uint) ([]response.CreditRequestResponse, error)
	AssignCreditAccountToClient(creditAccountID, clientID uint) (*response.CreditAccountResponse, error)
	CalculateDueDate(account entities.CreditAccount) time.Time
	GetNumberOfDues(account entities.CreditAccount) int
	CalculateInterest(creditAccount entities.CreditAccount) float64
}

type creditAccountService struct {
	creditAccountRepo repository.CreditAccountRepository
	transactionRepo   repository.TransactionRepository
	clientRepo        repository.ClientRepository
	establishmentRepo repository.EstablishmentRepository
	installmentRepo   repository.InstallmentRepository
}

// NewCreditAccountService creates a new instance of CreditAccountService.
func NewCreditAccountService(creditAccountRepo repository.CreditAccountRepository, transactionRepo repository.TransactionRepository, clientRepo repository.ClientRepository, establishmentRepo repository.EstablishmentRepository, installmentRepo repository.InstallmentRepository) CreditAccountService {
	return &creditAccountService{
		creditAccountRepo: creditAccountRepo,
		transactionRepo:   transactionRepo,
		clientRepo:        clientRepo,
		establishmentRepo: establishmentRepo,
		installmentRepo:   installmentRepo,
	}
}

func (s *creditAccountService) CreateCreditAccount(req request.CreateCreditAccountRequest) (*response.CreditAccountResponse, error) {
	// 1. Check if the client exists
	clientResponse, err := s.clientRepo.GetClientByID(req.ClientID)
	if err != nil {
		return nil, fmt.Errorf("error retrieving client: %w", err)
	}
	if clientResponse == nil {
		return nil, fmt.Errorf("client with ID %d not found", req.ClientID)
	}

	// 2. Check if the establishment exists
	establishmentResponse, err := s.establishmentRepo.GetByID(req.EstablishmentID)
	if err != nil {
		return nil, fmt.Errorf("error retrieving establishment: %w", err)
	}
	if establishmentResponse == nil {
		return nil, fmt.Errorf("establishment with ID %d not found", req.EstablishmentID)
	}

	// 3. Check if a credit account already exists for this client and establishment
	exists, err := s.creditAccountRepo.ExistsByClientAndEstablishment(req.ClientID, req.EstablishmentID)
	if err != nil {
		return nil, fmt.Errorf("error checking for existing credit account: %w", err)
	}
	if exists {
		return nil, errors.New("a credit account already exists for this client and establishment")
	}

	// 4. Create the credit account (assuming the establishment has permission)
	creditAccount := entities.CreditAccount{
		EstablishmentID:         req.EstablishmentID,
		ClientID:                req.ClientID,
		CreditLimit:             req.CreditLimit,
		MonthlyDueDate:          req.MonthlyDueDate,
		InterestRate:            req.InterestRate,
		InterestType:            req.InterestType,
		CreditType:              req.CreditType,
		GracePeriod:             req.GracePeriod,
		IsBlocked:               false, // Initially not blocked
		LastInterestAccrualDate: time.Now(),
		CurrentBalance:          0.0, // Initial balance is zero
		LateFeeRuleID:           req.LateFeeRuleID,
	}

	// 5. Create the credit account using the repository
	if err := s.creditAccountRepo.Create(&creditAccount); err != nil {
		return nil, fmt.Errorf("error creating credit account: %w", err)
	}

	return creditAccountToResponse(&creditAccount), nil
}

func (s *creditAccountService) GetCreditAccountByID(id uint) (*response.CreditAccountResponse, error) {
	return s.creditAccountRepo.GetByID(id)
}

func (s *creditAccountService) UpdateCreditAccount(id uint, req request.UpdateCreditAccountRequest) (*response.CreditAccountResponse, error) {
	// You can add additional business logic here before updating the account
	// For example, validation, permissions, etc.
	return s.creditAccountRepo.Update(id, req)
}

func (s *creditAccountService) DeleteCreditAccount(id uint) error {
	return s.creditAccountRepo.Delete(id)
}

func (s *creditAccountService) GetCreditAccountsByEstablishmentID(establishmentID uint) ([]response.CreditAccountResponse, error) {
	creditAccounts, err := s.creditAccountRepo.GetByEstablishmentID(establishmentID)
	if err != nil {
		return nil, fmt.Errorf("error retrieving credit accounts: %w", err)
	}

	creditAccountResponses := make([]response.CreditAccountResponse, len(creditAccounts))
	for i, account := range creditAccounts {
		creditAccountResponses[i] = account
	}
	return creditAccountResponses, nil
}

func (s *creditAccountService) GetCreditAccountsByClientID(clientID uint) ([]response.CreditAccountResponse, error) {
	return s.creditAccountRepo.GetByClientID(clientID)
}

// ApplyInterestToAllAccounts applies interest to all eligible credit accounts within an establishment.
func (s *creditAccountService) ApplyInterestToAllAccounts(establishmentID uint) error {
	creditAccounts, err := s.creditAccountRepo.GetByEstablishmentID(establishmentID)
	if err != nil {
		return fmt.Errorf("error retrieving credit accounts: %w", err)
	}

	for _, account := range creditAccounts {
		if err := s.creditAccountRepo.ApplyInterest(account.ID); err != nil {
			return fmt.Errorf("error applying interest to account %d: %w", account.ID, err)
		}
	}

	return nil
}

// ApplyLateFeesToAllAccounts applies late fees to all eligible credit accounts within an establishment.
func (s *creditAccountService) ApplyLateFeesToAllAccounts(establishmentID uint) error {
	overdueAccounts, err := s.creditAccountRepo.GetOverdueAccounts(establishmentID)
	if err != nil {
		return fmt.Errorf("error retrieving overdue accounts: %w", err)
	}

	for _, account := range overdueAccounts {
		if err := s.creditAccountRepo.ApplyLateFee(account.ID); err != nil {
			return fmt.Errorf("error applying late fee to account %d: %w", account.ID, err)
		}
	}

	return nil
}

// GetAdminDebtSummary retrieves a summary of debts owed to an establishment.
func (s *creditAccountService) GetAdminDebtSummary(establishmentID uint) ([]response.AdminDebtSummary, error) {
	creditAccounts, err := s.creditAccountRepo.GetByEstablishmentID(establishmentID)
	if err != nil {
		return nil, fmt.Errorf("error retrieving credit accounts: %w", err)
	}

	summary := make([]response.AdminDebtSummary, 0, len(creditAccounts))
	for _, account := range creditAccounts {
		client, err := s.clientRepo.GetClientByID(account.ClientID)
		if err != nil {
			return nil, fmt.Errorf("error retrieving client: %w", err)
		}

		dueDate := s.CalculateDueDate(*responseToCreditAccount(&account)) // Implement due date calculation

		summaryItem := response.AdminDebtSummary{
			ClientID:       account.ClientID,
			ClientName:     client.User.Name,
			CreditType:     string(account.CreditType),
			InterestRate:   account.InterestRate,
			NumberOfDues:   s.GetNumberOfDues(*responseToCreditAccount(&account)), // Calculate if LongTerm
			CurrentBalance: account.CurrentBalance,
			DueDate:        dueDate,
		}

		summary = append(summary, summaryItem)
	}

	return summary, nil
}

// ProcessPurchase processes a purchase on a credit account.
func (s *creditAccountService) ProcessPurchase(creditAccountID uint, amount float64, description string) error {
	// The service method now simply calls the repository method
	return s.creditAccountRepo.ProcessPurchase(creditAccountID, amount, description)
}

// ProcessPayment processes a payment towards a credit account.
func (s *creditAccountService) ProcessPayment(creditAccountID uint, amount float64, description string) error {
	// The service method now simply calls the repository method
	return s.creditAccountRepo.ProcessPayment(creditAccountID, amount, description)
}

// Helper functions for calculations

func (s *creditAccountService) CalculateDueDate(account entities.CreditAccount) time.Time {
	today := time.Now()

	if account.CreditType == enums.ShortTerm {
		// If short-term, due date is the next month's due date
		return time.Date(today.Year(), today.Month()+1, account.MonthlyDueDate, 0, 0, 0, 0, time.UTC) // Use +1 to add a month
	} else { // LongTerm
		// 1. Retrieve installments for the credit account
		installments, err := s.installmentRepo.GetByCreditAccountID(account.ID)
		if err != nil {
			// Handle error - you might want to log the error and return a zero time or today's date
			return time.Time{} // Or time.Now()
		}

		// 2. Find the next pending installment
		var nextDueDate time.Time
		for _, installment := range installments {
			if installment.Status == enums.Pending && installment.DueDate.After(today) {
				nextDueDate = installment.DueDate
				break
			}
		}

		// 3. If no pending installments, calculate next due date based on MonthlyDueDate
		if nextDueDate.IsZero() {
			// Calculate the next due date based on the MonthlyDueDate
			nextMonth := today.Month() + 1
			nextYear := today.Year()
			if nextMonth > time.December {
				nextMonth = time.January
				nextYear++
			}
			nextDueDate = time.Date(nextYear, nextMonth, account.MonthlyDueDate, 0, 0, 0, 0, time.UTC)
		}

		return nextDueDate
	}
}

func (s *creditAccountService) GetNumberOfDues(account entities.CreditAccount) int {
	if account.CreditType != enums.LongTerm {
		return 0 // Number of dues is not applicable for ShortTerm credit
	}

	// 1. Retrieve installments from the database
	installments, err := s.installmentRepo.GetByCreditAccountID(account.ID)
	if err != nil {
		// Handle error appropriately (e.g., log the error and return 0)
		return 0
	}

	// 2. Count the number of installments
	numberOfDues := len(installments)

	return numberOfDues
}

func (s *creditAccountService) CalculateInterest(creditAccount entities.CreditAccount) float64 {
	var interest float64
	today := time.Now()

	if creditAccount.CreditType == enums.ShortTerm {
		// Short-Term Interest Calculation

		// Calculate the number of days since the last interest accrual
		daysSinceLastAccrual := today.Sub(creditAccount.LastInterestAccrualDate).Hours() / 24

		// Check if it's time to accrue interest (at least a month has passed)
		if daysSinceLastAccrual >= daysInMonth(creditAccount.LastInterestAccrualDate.Month(), creditAccount.LastInterestAccrualDate.Year()) {
			if creditAccount.InterestType == enums.Nominal {
				// Nominal Interest: Interest = Principal * (Rate/100) * (Time in years)
				interest = creditAccount.CurrentBalance * (creditAccount.InterestRate / 100) * (daysSinceLastAccrual / 365.0)
			} else if creditAccount.InterestType == enums.Effective {
				// Effective Interest: Interest = Principal * ((1 + Rate/100)^(Time in years) - 1)
				interest = creditAccount.CurrentBalance * (math.Pow(1+(creditAccount.InterestRate/100), daysSinceLastAccrual/365.0) - 1)
			}
		}

	} else { // LongTerm
		// Long-Term (Installment) Interest Calculation
		installments, err := s.installmentRepo.GetByCreditAccountID(creditAccount.ID)
		if err != nil {
			// Handle error appropriately (e.g., log the error and return 0)
			return 0
		}

		for _, installment := range installments {
			// Only calculate interest on pending installments
			if installment.Status == enums.Pending {
				// Calculate days until the installment's due date
				daysUntilDueDate := time.Until(installment.DueDate).Hours() / 24

				// Check if the due date is in the future
				if daysUntilDueDate > 0 {
					if creditAccount.InterestType == enums.Nominal {
						// Nominal Interest for Installment
						interest += installment.Amount * (creditAccount.InterestRate / 100) * (daysUntilDueDate / 365.0)
					} else if creditAccount.InterestType == enums.Effective {
						// Effective Interest for Installment
						interest += installment.Amount * (math.Pow(1+(creditAccount.InterestRate/100), daysUntilDueDate/365.0) - 1)
					}
				}
			}
		}
	}

	return interest
}

// Helper function to get the number of days in a month
func daysInMonth(month time.Month, year int) float64 {
	return float64(time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day())
}

func (s *creditAccountService) CreateCreditRequest(req request.CreateCreditRequest) (*response.CreditRequestResponse, error) {
	// 1. Check if a credit account already exists for the client and establishment
	exists, err := s.creditAccountRepo.ExistsByClientAndEstablishment(req.ClientID, req.EstablishmentID)
	if err != nil {
		return nil, fmt.Errorf("error checking for existing credit account: %w", err)
	}
	if exists {
		return nil, errors.New("a credit account already exists for this client and establishment")
	}

	// 2. Create the credit request
	creditRequest := entities.CreditRequest{
		ClientID:             req.ClientID,
		EstablishmentID:      req.EstablishmentID,
		RequestedCreditLimit: req.RequestedCreditLimit,
		MonthlyDueDate:       req.MonthlyDueDate,
		InterestType:         req.InterestType,
		CreditType:           req.CreditType,
		GracePeriod:          req.GracePeriod,
		Status:               entities.Pending,
	}

	if err := s.creditAccountRepo.CreateCreditRequest(&creditRequest); err != nil { // Use the repository method
		return nil, fmt.Errorf("error creating credit request: %w", err)
	}

	// 3. Map creditRequest to CreditRequestResponse and return
	return getCreditRequestResponse(&creditRequest), nil
}

func (s *creditAccountService) GetCreditRequestByID(id uint) (*response.CreditRequestResponse, error) {
	creditRequest, err := s.creditAccountRepo.GetCreditRequestByID(id) // Use the repository method
	if err != nil {
		return nil, fmt.Errorf("error retrieving credit request: %w", err)
	}

	return getCreditRequestResponse(creditRequest), nil
}

func (s *creditAccountService) ApproveCreditRequest(creditRequestID uint, adminID uint) (*response.CreditAccountResponse, error) {
	// 1. Retrieve the credit request using the repository (now preloads data)
	creditRequest, err := s.creditAccountRepo.GetCreditRequestByID(creditRequestID)
	if err != nil {
		return nil, fmt.Errorf("error retrieving credit request: %w", err)
	}

	// 2. Check if the admin belongs to the establishment
	if creditRequest.Establishment.Admin.UserID != adminID {
		return nil, errors.New("admin does not have permission to approve this request")
	}

	// 3. Check if the request is already approved or rejected
	if creditRequest.Status != entities.Pending {
		return nil, fmt.Errorf("credit request is already %s", creditRequest.Status)
	}

	// 4. Approve the credit request in the repository (no more preloading needed here)
	creditAccountResponse, err := s.creditAccountRepo.ApproveCreditRequest(creditRequest)
	if err != nil {
		return nil, fmt.Errorf("error approving credit request: %w", err)
	}

	return creditAccountResponse, nil
}

func (s *creditAccountService) RejectCreditRequest(creditRequestID uint, adminID uint) error {
	// 1. Retrieve the credit request
	creditRequest, err := s.creditAccountRepo.GetCreditRequestByID(creditRequestID)
	if err != nil {
		return fmt.Errorf("error retrieving credit request: %w", err)
	}

	// 2. Check if the admin belongs to the establishment
	if creditRequest.Establishment.Admin.UserID != adminID {
		return errors.New("admin does not have permission to reject this request")
	}

	// 3. Check if the request is already approved or rejected
	if creditRequest.Status != entities.Pending {
		return fmt.Errorf("credit request is already %s", creditRequest.Status)
	}

	// 4. Update the credit request status
	now := time.Now()
	creditRequest.Status = entities.Rejected
	creditRequest.RejectedAt = &now

	// 5. Use the repository method to update the request
	if err := s.creditAccountRepo.UpdateCreditRequest(creditRequest); err != nil {
		return fmt.Errorf("error updating credit request status: %w", err)
	}

	return nil
}

func (s *creditAccountService) GetPendingCreditRequests(establishmentID uint) ([]response.CreditRequestResponse, error) {
	// 1. Retrieve credit requests using the repository method
	creditRequests, err := s.creditAccountRepo.GetPendingCreditRequests(establishmentID)
	if err != nil {
		return nil, fmt.Errorf("error retrieving pending credit requests: %w", err)
	}

	// 2. Convert entities to responses
	var creditRequestResponses []response.CreditRequestResponse
	for _, creditRequest := range creditRequests {
		creditRequestResponses = append(creditRequestResponses, *getCreditRequestResponse(&creditRequest))
	}

	return creditRequestResponses, nil
}

func getCreditRequestResponse(creditRequest *entities.CreditRequest) *response.CreditRequestResponse {
	return &response.CreditRequestResponse{
		ID:                   creditRequest.ID,
		ClientID:             creditRequest.ClientID,
		EstablishmentID:      creditRequest.EstablishmentID,
		RequestedCreditLimit: creditRequest.RequestedCreditLimit,
		MonthlyDueDate:       creditRequest.MonthlyDueDate,
		InterestType:         creditRequest.InterestType,
		CreditType:           creditRequest.CreditType,
		GracePeriod:          creditRequest.GracePeriod,
		Status:               creditRequest.Status,
		ApprovedAt:           creditRequest.ApprovedAt,
		RejectedAt:           creditRequest.RejectedAt,
		CreatedAt:            creditRequest.CreatedAt,
		UpdatedAt:            creditRequest.UpdatedAt,
	}
}

// AssignCreditAccountToClient assigns an existing credit account to a client.
func (s *creditAccountService) AssignCreditAccountToClient(creditAccountID, clientID uint) (*response.CreditAccountResponse, error) {
	// Call the repository method to perform the assignment
	err := s.creditAccountRepo.AssignCreditAccountToClient(creditAccountID, clientID)
	if err != nil {
		return nil, fmt.Errorf("error assigning credit account to client: %w", err)
	}

	// If successful, retrieve the updated credit account and return the response
	updatedCreditAccount, err := s.creditAccountRepo.GetByID(creditAccountID)
	if err != nil {
		return nil, fmt.Errorf("error retrieving updated credit account: %w", err)
	}

	return updatedCreditAccount, nil
}

func creditAccountToResponse(creditAccount *entities.CreditAccount) *response.CreditAccountResponse {
	return &response.CreditAccountResponse{
		ID:                      creditAccount.ID,
		EstablishmentID:         creditAccount.EstablishmentID,
		ClientID:                creditAccount.ClientID,
		CreditLimit:             creditAccount.CreditLimit,
		CurrentBalance:          creditAccount.CurrentBalance,
		MonthlyDueDate:          creditAccount.MonthlyDueDate,
		InterestRate:            creditAccount.InterestRate,
		InterestType:            creditAccount.InterestType,
		CreditType:              creditAccount.CreditType,
		GracePeriod:             creditAccount.GracePeriod,
		IsBlocked:               creditAccount.IsBlocked,
		LastInterestAccrualDate: creditAccount.LastInterestAccrualDate,
		CreatedAt:               creditAccount.CreatedAt,
		UpdatedAt:               creditAccount.UpdatedAt,
		LateFeeRuleID:           creditAccount.LateFeeRuleID,
		// You can add Client and LateFeeRule data if needed
		// Client:          // ... (map client entity to response)
		// LateFeeRule:     // ... (map lateFeeRule entity to response)
	}
}

func responseToCreditAccount(res *response.CreditAccountResponse) *entities.CreditAccount {
	return &entities.CreditAccount{
		Model: gorm.Model{
			ID:        res.ID,
			CreatedAt: res.CreatedAt,
			UpdatedAt: res.UpdatedAt,
		},
		EstablishmentID:         res.EstablishmentID,
		ClientID:                res.ClientID,
		CreditLimit:             res.CreditLimit,
		MonthlyDueDate:          res.MonthlyDueDate,
		InterestRate:            res.InterestRate,
		InterestType:            res.InterestType,
		CreditType:              res.CreditType,
		GracePeriod:             res.GracePeriod,
		IsBlocked:               res.IsBlocked,
		LastInterestAccrualDate: res.LastInterestAccrualDate,
		CurrentBalance:          res.CurrentBalance,
		LateFeeRuleID:           res.LateFeeRuleID,
		// You can add Client and LateFeeRule data if needed
		// You'll need to convert ClientResponse and LateFeeRuleResponse to entities as well
		// Client:          // ... (map client response to entity)
		// LateFeeRule:     // ... (map lateFeeRule response to entity)
	}
}
