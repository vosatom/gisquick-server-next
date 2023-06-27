package server

// Code from https://github.com/ardanlabs/service/blob/master/business/sys/database/database.go

import (
	"strconv"
	"net/url"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq" // Calls init function.
)

// Config is the required properties to use the database.
type DBConfig struct {
	User               string
	Password           string
	Host               string
	Name               string
	Port               int
	MaxIdleConns       int
	MaxOpenConns       int
	SSLMode            string
	StatementCacheMode string
}

func OpenDB(cfg DBConfig) (*sqlx.DB, error) {
	q := make(url.Values)
	q.Set("sslmode", cfg.SSLMode)
	q.Set("timezone", "utc")
	q.Set("statement_cache_mode", cfg.StatementCacheMode)

	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(cfg.User, cfg.Password),
		Host:     cfg.Host + ":" + strconv.Itoa(cfg.Port),
		Path:     cfg.Name,
		RawQuery: q.Encode(),
	}

	// db, err := sqlx.Open("postgres", u.String())
	db, err := sqlx.Connect("pgx", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	return db, nil
}
