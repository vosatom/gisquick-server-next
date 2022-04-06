package domain

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrAccountExists   = errors.New("Account already exists")
	ErrAccountActive   = errors.New("Account was already activated")
	ErrAccountNotFound = errors.New("Account not found")
)

var isValidUsername = regexp.MustCompile(`^[0-9A-Za-z_\-\.]+$`).MatchString

func validateUsername(v string) bool {
	return len(v) < 24 && isValidUsername(v)
}

func validateEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}

// Account entity
type Account struct {
	Username    string
	Email       string
	Password    []byte
	FirstName   string
	LastName    string
	IsSuperuser bool
	Active      bool
	DateJoined  *time.Time
	LastLogin   *time.Time
}

func (a *Account) IsActive() bool {
	return a.Active && a.DateJoined != nil
}

func (a *Account) SetPassword(password string) error {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	a.Password = hashedPassword
	return nil
}

func (a *Account) CheckPassword(password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(a.Password), []byte(password)) == nil
}

func (a *Account) FullName() string {
	name := strings.TrimSpace(fmt.Sprintf("%s %s", a.FirstName, a.LastName))
	if name == "" {
		name = a.Username
	}
	return name
}

// type AccountOpts struct {
// 	IsSuperuser bool
// }

func NewAccount(username, email, firstName, lastName, password string) (Account, error) {
	username = strings.TrimSpace(username)
	if !validateUsername(username) {
		return Account{}, fmt.Errorf("invalid username: '%s'", username)
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if !validateEmail(email) {
		return Account{}, fmt.Errorf("invalid email: '%s'", email)
	}
	account := Account{
		Username:  username,
		Email:     email,
		FirstName: strings.TrimSpace(firstName),
		LastName:  strings.TrimSpace(lastName),
	}
	if password != "" {
		if err := account.SetPassword(password); err != nil {
			return account, err
		}
	}
	return account, nil
}

// AccountsRepository repository interface
type AccountsRepository interface {
	Create(account Account) error
	Update(account Account) error
	GetByUsername(username string) (Account, error)
	GetByEmail(email string) (Account, error)
	EmailExists(email string) (bool, error)
	UsernameExists(username string) (bool, error)
	GetActiveAccounts() ([]Account, error)
}
