package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"hash"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"
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

// https://github.com/meehow/go-django-hashers/blob/master/check.go
func checkPbkdf2(password, encoded string, keyLen int, h func() hash.Hash) (bool, error) {
	parts := strings.SplitN(encoded, "$", 4)
	if len(parts) != 4 {
		return false, errors.New("Hash must consist of 4 segments")
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil {
		return false, fmt.Errorf("Wrong number of iterations: %v", err)
	}
	salt := []byte(parts[2])
	k, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return false, fmt.Errorf("Wrong hash encoding: %v", err)
	}
	dk := pbkdf2.Key([]byte(password), salt, iter, keyLen, h)
	return bytes.Equal(k, dk), nil
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

func (a *Account) Activate() error {
	if a.IsActive() {
		return ErrAccountActive
	}
	now := time.Now()
	a.DateJoined = &now
	a.Active = true
	return nil
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
	hashedPassword := string(a.Password)
	if strings.HasPrefix(hashedPassword, "pbkdf2_sha256$") {
		// compatibility with Django's default hashes
		valid, _ := checkPbkdf2(password, hashedPassword, sha256.Size, sha256.New)
		return valid
	}
	return bcrypt.CompareHashAndPassword(a.Password, []byte(password)) == nil
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
	if email != "" && !validateEmail(email) {
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
	Delete(username string) error
	GetByUsername(username string) (Account, error)
	GetByEmail(email string) (Account, error)
	EmailExists(email string) (bool, error)
	UsernameExists(username string) (bool, error)
	GetActiveAccounts() ([]Account, error)
}
