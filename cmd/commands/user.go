package commands

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"syscall"
	"time"

	"github.com/ardanlabs/conf/v2"
	"github.com/gisquick/gisquick-server/internal/domain"
	"github.com/gisquick/gisquick-server/internal/infrastructure/postgres"
	"github.com/gisquick/gisquick-server/internal/server"
	"github.com/jmoiron/sqlx"
	"golang.org/x/term"
)

var (
	ErrPasswordsMismatch = errors.New("Passwords does not match")
)

type Account struct {
	Username   string     `json:"username"`
	Email      string     `json:"email"`
	Password   string     `json:"password"`
	FirstName  string     `json:"first_name"`
	LastName   string     `json:"last_name"`
	Active     bool       `json:"is_active"`
	Superuser  bool       `json:"is_superuser"`
	DateJoined *time.Time `json:"date_joined"`
	LastLogin  *time.Time `json:"last_login"`
}

func runUserCommand(command func(dbConn *sqlx.DB, args conf.Args) error) error {
	cfg := struct {
		Postgres struct {
			User       string `conf:"default:postgres"`
			Password   string `conf:"default:postgres,mask"`
			Host       string `conf:"default:postgres"`
			Name       string `conf:"default:postgres,env:POSTGRES_DB"`
			DisableTLS bool   `conf:"default:true"`
		}
		Args conf.Args
	}{}

	help, err := conf.Parse("", &cfg)
	if err != nil {
		if errors.Is(err, conf.ErrHelpWanted) {
			fmt.Println(help)
			return nil
		}
		return fmt.Errorf("parsing config: %w", err)
	}
	// Database
	dbConn, err := server.OpenDB(server.DBConfig{
		User:         cfg.Postgres.User,
		Password:     cfg.Postgres.Password,
		Host:         cfg.Postgres.Host,
		Name:         cfg.Postgres.Name,
		MaxIdleConns: 1,
		MaxOpenConns: 1,
		DisableTLS:   cfg.Postgres.DisableTLS,
	})
	if err != nil {
		return fmt.Errorf("connecting to db: %w", err)
	}
	defer func() {
		// log.Infow("shutdown", "status", "stopping database support", "host", cfg.Postgres.Host)
		dbConn.Close()
	}()
	return command(dbConn, cfg.Args)
}

func createAccount() (domain.Account, error) {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Printf("Username: ")
	scanner.Scan()
	username := scanner.Text()
	fmt.Printf("Email: ")
	scanner.Scan()
	email := scanner.Text()
	fmt.Printf("First Name: ")
	scanner.Scan()
	firstName := scanner.Text()
	fmt.Printf("Last Name: ")
	scanner.Scan()
	lastName := scanner.Text()
	fmt.Printf("Password: ")
	password, _ := term.ReadPassword(int(syscall.Stdin))
	fmt.Printf("\nRepeat password: ")
	password2, _ := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if !bytes.Equal(password, password2) {
		return domain.Account{}, ErrPasswordsMismatch
	}
	// fmt.Println("you entered", username, email, firstName, lastName, string(password))
	account, err := domain.NewAccount(username, email, firstName, lastName, string(password))
	if err != nil {
		return domain.Account{}, err
	}
	account.Active = true
	now := time.Now() //.UTC()
	account.DateJoined = &now
	return account, nil
}

func addUser(dbConn *sqlx.DB, args conf.Args) error {
	account, err := createAccount()
	if err != nil {
		return fmt.Errorf("creating user account: %w", err)
	}
	accountsRepo := postgres.NewAccountsRepository(dbConn)
	return accountsRepo.Create(account)
}

func addSuperuser(dbConn *sqlx.DB, args conf.Args) error {
	account, err := createAccount()
	if err != nil {
		return fmt.Errorf("creating superuser account: %w", err)
	}
	account.IsSuperuser = true
	accountsRepo := postgres.NewAccountsRepository(dbConn)
	return accountsRepo.Create(account)
}

func utcTime(t *time.Time) *time.Time {
	if t == nil {
		return t
	}
	d := t.UTC()
	return &d
}

func dumpUsers(dbConn *sqlx.DB, args conf.Args) error {
	var dbUsers []postgres.User
	if err := dbConn.Select(&dbUsers, `SELECT * FROM app_user`); err != nil {
		return fmt.Errorf("querying users: %w", err)
	}
	accounts := make([]Account, len(dbUsers))
	for i, u := range dbUsers {
		accounts[i] = Account{
			Username:   u.Username,
			Email:      u.Email,
			Password:   string(u.Password),
			FirstName:  u.FirstName,
			LastName:   u.LastName,
			Active:     u.IsActive,
			Superuser:  u.IsSuperuser,
			DateJoined: utcTime(u.DateJoined),
			LastLogin:  utcTime(u.LastLogin),
		}
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(accounts)
}

func loadUsers(dbConn *sqlx.DB, args conf.Args) error {
	path := args.Num(0)
	if path == "" {
		return fmt.Errorf("missing file argument")
	}
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading project file: %w", err)
	}
	var users []Account
	if err := json.Unmarshal(content, &users); err != nil {
		return fmt.Errorf("parsing input file: %w", err)
	}
	accountsRepo := postgres.NewAccountsRepository(dbConn)
	for _, u := range users {
		a := domain.Account{
			Username:    u.Username,
			Email:       u.Email,
			Password:    []byte(u.Password),
			FirstName:   u.FirstName,
			LastName:    u.LastName,
			IsSuperuser: u.Superuser,
			Active:      u.Active,
			DateJoined:  u.DateJoined,
			LastLogin:   u.LastLogin,
		}
		if err := accountsRepo.Create(a); err != nil {
			fmt.Printf("failed to create account: %s (%s)\n", a.Username, err)
		}
	}
	return nil
}

func deleteUser(dbConn *sqlx.DB, args conf.Args) error {
	if len(args) != 1 {
		return fmt.Errorf("Invalid number of arguments")
	}
	username := args.Num(0)
	accountsRepo := postgres.NewAccountsRepository(dbConn)
	return accountsRepo.Delete(username)
}

func AddUser() error {
	return runUserCommand(addUser)
}

func AddSuperuser() error {
	return runUserCommand(addSuperuser)
}

func DumpUsers() error {
	return runUserCommand(dumpUsers)
}

func LoadUsers() error {
	return runUserCommand(loadUsers)
}

func DeleteUser() error {
	return runUserCommand(deleteUser)
}
