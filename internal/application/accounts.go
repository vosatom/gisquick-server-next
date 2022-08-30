package application

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/gisquick/gisquick-server/internal/domain"
)

var (
	ErrInvalidToken     = errors.New("Invalid token")
	ErrNotActiveAccount = errors.New("Account is not active")
)

type TokenGenerator interface {
	GenerateToken(claims string) (string, error)
	CheckToken(token, claims string) error
}

type EmailService interface {
	SendRegistrationEmail(account domain.Account, uid, token string) error
	SendPasswordResetEmail(account domain.Account, uid, token string) error
}

type AccountsService struct {
	Repository domain.AccountsRepository
	email      EmailService
	tokenGen   TokenGenerator
}

func NewAccountsService(email EmailService, accountsRepo domain.AccountsRepository, tokenGen TokenGenerator) *AccountsService {
	return &AccountsService{
		Repository: accountsRepo,
		email:      email,
		tokenGen:   tokenGen,
	}
}

func (s *AccountsService) signupClaims(account domain.Account) string {
	claims := []string{account.Username, account.Email, string(account.Password)}
	return strings.Join(claims, ":")
}

func (s *AccountsService) passwordResetClaims(account domain.Account) string {
	return fmt.Sprintf("%s:%s:%s:%s", account.Username, account.Email, account.Password, account.LastLogin)
}

func (s *AccountsService) NewAccount(username, email, firstName, lastName, password string) error {
	account, err := domain.NewAccount(username, email, firstName, lastName, password)
	if err != nil {
		return err
	}
	uid := base64.URLEncoding.EncodeToString([]byte(account.Username))
	token, err := s.tokenGen.GenerateToken(s.signupClaims(account))
	if err != nil {
		return err
	}
	if err := s.Repository.Create(account); err != nil {
		return err
	}
	if err := s.email.SendRegistrationEmail(account, uid, token); err != nil {
		// TODO: should we delete account, or implement re-sending?
		return fmt.Errorf("sending registration email [%s]: %w", email, err)
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
	if err := s.tokenGen.CheckToken(token, s.signupClaims(account)); err != nil {
		return ErrInvalidToken
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
	token, err := s.tokenGen.GenerateToken(s.passwordResetClaims(account))
	if err != nil {
		return fmt.Errorf("generating token: %w", err)
	}
	if err := s.email.SendPasswordResetEmail(account, uid, token); err != nil {
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
	if err := s.tokenGen.CheckToken(token, s.passwordResetClaims(account)); err != nil {
		return ErrInvalidToken
	}
	if err := account.SetPassword(newPassword); err != nil {
		return fmt.Errorf("set new password: %w", err)
	}
	return s.Repository.Update(account)
}

func (s *AccountsService) GetActiveAccounts() ([]domain.Account, error) {
	return s.Repository.GetActiveAccounts()
}

func (s *AccountsService) SupportEmails() bool {
	return s.email != nil
}
