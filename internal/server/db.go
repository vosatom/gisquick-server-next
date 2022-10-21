package server

// Code from https://github.com/ardanlabs/service/blob/master/business/sys/database/database.go

import (
	"net/url"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq" // Calls init function.
)

// Config is the required properties to use the database.
type DBConfig struct {
	User         string
	Password     string
	Host         string
	Name         string
	MaxIdleConns int
	MaxOpenConns int
	SSLMode      string
}

func OpenDB(cfg DBConfig) (*sqlx.DB, error) {
	q := make(url.Values)
	q.Set("sslmode", cfg.SSLMode)
	q.Set("timezone", "utc")

	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(cfg.User, cfg.Password),
		Host:     cfg.Host,
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
