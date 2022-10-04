package application

import (
	"encoding/base64"
	"errors"
	"fmt"
	htmltemplate "html/template"
	texttemplate "text/template"

	"github.com/gisquick/gisquick-server/internal/domain"
)

var (
	ErrInvalidToken     = errors.New("Invalid token")
	ErrNotActiveAccount = errors.New("Account is not active")
	ErrEmailNotSet      = errors.New("Account does not have email address")
	ErrPasswordNotSet   = errors.New("Password is not set")
)

type TokenGenerator interface {
	GenerateToken(claims string) (string, error)
	CheckToken(token, claims string) error
}

type EmailService interface {
	SendActivationEmail(account domain.Account, uid, token string, data map[string]interface{}) error
	SendPasswordResetEmail(account domain.Account, uid, token string) error
	SendBulkEmail(accounts []domain.Account, subject string, htmlTemplate *htmltemplate.Template, textTemplate *texttemplate.Template, data map[string]interface{}) error
}

type AccountsService struct {
	Repository domain.AccountsRepository
	Email      EmailService
	tokenGen   TokenGenerator
}

func NewAccountsService(email EmailService, accountsRepo domain.AccountsRepository, tokenGen TokenGenerator) *AccountsService {
	return &AccountsService{
		Repository: accountsRepo,
		Email:      email,
		tokenGen:   tokenGen,
	}
}

// func signupClaims(account domain.Account) string {
// 	claims := []string{account.Username, account.Email, string(account.Password)}
// 	return strings.Join(claims, ":")
// }

// func passwordResetClaims(account domain.Account) string {
// 	return fmt.Sprintf("%s:%s:%s:%s", account.Username, account.Email, string(account.Password), account.LastLogin)
// }

func accountClaims(account domain.Account) string {
	return fmt.Sprintf("%s:%s:%s:%s", account.Username, account.Email, string(account.Password), account.LastLogin)
}

func (s *AccountsService) NewAccount(username, email, firstName, lastName, password string) error {
	account, err := domain.NewAccount(username, email, firstName, lastName, password)
	if err != nil {
		return err
	}
	if err := s.Repository.Create(account); err != nil {
		return err
	}
	if account.Email != "" && !account.Active {
		uid := base64.URLEncoding.EncodeToString([]byte(account.Username))
		token, err := s.tokenGen.GenerateToken(accountClaims(account))
		if err != nil {
			return err
		}
		if err := s.Email.SendActivationEmail(account, uid, token, nil); err != nil {
			// TODO: should we delete account, or implement re-sending?
			return fmt.Errorf("sending registration email [%s]: %w", email, err)
		}
	}
	return nil
}

func (s *AccountsService) SendActivationEmail(account domain.Account, data map[string]interface{}) error {
	if account.Email == "" {
		return ErrEmailNotSet
	}
	uid := base64.URLEncoding.EncodeToString([]byte(account.Username))
	token, err := s.tokenGen.GenerateToken(accountClaims(account))

	if err != nil {
		return fmt.Errorf("generating activation token: %w", err)
	}
	if err := s.Email.SendActivationEmail(account, uid, token, data); err != nil {
		return fmt.Errorf("sending activation email [%s]: %w", account.Email, err)
	}
	return nil
}

func (s *AccountsService) Activate(uid, token string) error {
	username, err := base64.URLEncoding.DecodeString(uid)
	if err != nil {
		return ErrInvalidToken
	}
	account, err := s.Repository.GetByUsername(string(username))
	if err != nil {
		return fmt.Errorf("activate user %s: %w", username, err)
	}
	// if errors.Is(err, ErrNotFound)
	if err := s.tokenGen.CheckToken(token, accountClaims(account)); err != nil {
		return ErrInvalidToken
	}
	if len(account.Password) == 0 {
		return ErrPasswordNotSet
	}
	if err := account.Activate(); err != nil {
		return err
	}
	return s.Repository.Update(account)
}

func (s *AccountsService) RequestPasswordReset(email string) error {
	account, err := s.Repository.GetByEmail(email)
	if err != nil {
		return err
	}
	if !account.Active {
		return ErrNotActiveAccount
	}
	uid := base64.URLEncoding.EncodeToString([]byte(account.Username))
	token, err := s.tokenGen.GenerateToken(accountClaims(account))
	if err != nil {
		return fmt.Errorf("generating token: %w", err)
	}
	if err := s.Email.SendPasswordResetEmail(account, uid, token); err != nil {
		return fmt.Errorf("sending email: %w", err)
	}
	return nil
}

func (s *AccountsService) SetNewPassword(uid, token, newPassword string) error {
	username, err := base64.URLEncoding.DecodeString(uid)
	if err != nil {
		return ErrInvalidToken
	}
	account, err := s.Repository.GetByUsername(string(username))
	if err != nil {
		return fmt.Errorf("set new password %s: %w", username, err)
	}
	// if errors.Is(err, ErrNotFound)
	if err := s.tokenGen.CheckToken(token, accountClaims(account)); err != nil {
		return ErrInvalidToken
	}
	if err := account.SetPassword(newPassword); err != nil {
		return fmt.Errorf("set new password: %w", err)
	}
	if !account.Active {
		if err := account.Activate(); err != nil {
			return fmt.Errorf("activating account: %w", err)
		}
	}
	return s.Repository.Update(account)
}

func (s *AccountsService) GetActiveAccounts() ([]domain.Account, error) {
	return s.Repository.GetActiveAccounts()
}

func (s *AccountsService) GetAllAccounts() ([]domain.Account, error) {
	return s.Repository.GetAllAccounts()
}

func (s *AccountsService) SupportEmails() bool {
	return s.Email != nil
}
