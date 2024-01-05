package server_tests

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/ardanlabs/conf/v2"
	"github.com/gisquick/gisquick-server/cmd/commands"
	"github.com/gisquick/gisquick-server/internal/domain"
	gisquickPostgres "github.com/gisquick/gisquick-server/internal/infrastructure/postgres"
	"github.com/gisquick/gisquick-server/internal/server"
	"github.com/golang-migrate/migrate/v4"

	"github.com/golang-migrate/migrate/v4/database/postgres"
)

func GetBasePath() string {
	path := os.Getenv("PROJECT_BASEPATH")
	return path
}

func prepareDB() error {
	projectPath := GetBasePath()
	cfg := commands.AppConfig{}
	const prefix = ""
	_, err := conf.Parse(prefix, &cfg)
	if err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	q := make(url.Values)
	q.Set("sslmode", cfg.Postgres.SSLMode)
	q.Set("timezone", "utc")
	q.Set("statement_cache_mode", cfg.Postgres.StatementCacheMode)
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(cfg.Postgres.User, cfg.Postgres.Password),
		Host:     cfg.Postgres.Host + ":" + strconv.Itoa(cfg.Postgres.Port),
		Path:     cfg.Postgres.Name,
		RawQuery: q.Encode(),
	}

	db, err := sql.Open("pgx", u.String())
	if err != nil {
		return fmt.Errorf("connecting to db: %w", err)
	}
	defer db.Close()
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("connecting to db: %w", err)
	}

	db.Exec("CREATE DATABASE gisquick IF NOT EXISTS")

	m, err := migrate.NewWithDatabaseInstance("file:///"+projectPath+"/migrations", "postgres", driver)
	if err != nil {
		return fmt.Errorf("migration: %w", err)
	}
	defer m.Close()

	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migration: %w", err)
	}

	dbConn, err := server.OpenDB(server.DBConfig{
		User:               cfg.Postgres.User,
		Password:           cfg.Postgres.Password,
		Host:               cfg.Postgres.Host,
		Port:               cfg.Postgres.Port,
		Name:               cfg.Postgres.Name,
		MaxIdleConns:       1,
		MaxOpenConns:       1,
		SSLMode:            cfg.Postgres.SSLMode,
		StatementCacheMode: cfg.Postgres.StatementCacheMode,
	})
	if err != nil {
		return fmt.Errorf("connecting to db: %w", err)
	}
	defer func() {
		dbConn.Close()
	}()

	account, err := domain.NewAccount("admin", "admin@localhost", "admin", "admin", "admin")
	if err != nil {
		return fmt.Errorf("new account: %w", err)
	}
	account.Active = true
	account.Superuser = true

	accountsRepo := gisquickPostgres.NewAccountsRepository(dbConn)

	err = accountsRepo.Create(account)
	if err != nil && err != domain.ErrAccountExists {
		return fmt.Errorf("new account: %w", err)
	}

	return nil
}

func CreateFormFile(body *bytes.Buffer, path string, fileName string) (*multipart.Writer, error) {
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", fileName)

	file, err := os.Open(path)
	if err != nil {
		return writer, err
	}
	fileContents, err := io.ReadAll(file)
	if err != nil {
		return writer, err
	}

	part.Write(fileContents)
	writer.Close()
	return writer, nil
}

func LoginUser(server *server.Server, username string, password string) (string, error) {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"`+username+`","password":"`+password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	setCookie := ""
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "gq_session" {
			setCookie = cookie.Value
		}
	}

	return setCookie, nil
}

func authenticateRequest(server *server.Server, req *http.Request, username string, password string) error {
	userCookie, err := LoginUser(server, username, password)
	if err != nil {
		return err
	}
	req.AddCookie(&http.Cookie{Name: "gq_session", Value: userCookie})
	return nil
}
