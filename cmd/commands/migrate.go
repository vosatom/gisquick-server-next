package commands

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/ardanlabs/conf/v2"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func runMigrateCommand() error {
	cfg := struct {
		Postgres struct {
			User     string `conf:"default:postgres"`
			Password string `conf:"default:postgres,mask"`
			Host     string `conf:"default:postgres"`
			Name     string `conf:"default:postgres,env:POSTGRES_DB"`
			SSLMode  string `conf:"default:prefer"`
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

	q := make(url.Values)
	q.Set("sslmode", cfg.Postgres.SSLMode)
	q.Set("timezone", "utc")
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(cfg.Postgres.User, cfg.Postgres.Password),
		Host:     cfg.Postgres.Host,
		Path:     cfg.Postgres.Name,
		RawQuery: q.Encode(),
	}
	// m, err := migrate.New("file:///app/migrations", u.String())
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer db.Close()
	driver, err := postgres.WithInstance(db, &postgres.Config{})

	// dbConn, err := server.OpenDB(server.DBConfig{
	// 	User:         cfg.Postgres.User,
	// 	Password:     cfg.Postgres.Password,
	// 	Host:         cfg.Postgres.Host,
	// 	Name:         cfg.Postgres.Name,
	// 	SSLMode:      cfg.Postgres.SSLMode,
	// 	MaxIdleConns: 1,
	// 	MaxOpenConns: 1,
	// })
	// if err != nil {
	// 	return fmt.Errorf("connecting to database: %w", err)
	// }
	// defer dbConn.Close()
	// driver, err := postgres.WithInstance(dbConn.DB, &postgres.Config{})

	m, err := migrate.NewWithDatabaseInstance("file:///app/migrations", "postgres", driver)
	if err != nil {
		return err
	}
	defer m.Close()

	subcmd := cfg.Args.Num(0)

	// up/down command with specified number of steps
	if len(cfg.Args) > 1 && (subcmd == "up" || subcmd == "down") {
		steps, err := strconv.Atoi(cfg.Args.Num(1))
		if err != nil {
			return fmt.Errorf("invalid steps parameter: %s", cfg.Args.Num(1))
		}
		if subcmd == "down" {
			steps = -steps
		}
		return m.Steps(steps)
	}

	switch subcmd {
	case "up":
		return m.Up()
	case "down":
		return m.Down()
	case "force":
		val, err := strconv.Atoi(cfg.Args.Num(1))
		if err != nil {
			return fmt.Errorf("invalid or missing version parameter: %s", cfg.Args.Num(1))
		}
		return m.Force(val)
	case "version":
		ver, dirty, err := m.Version()
		if err == nil {
			if dirty {
				fmt.Printf(": %d, dirty: %v\n", ver, dirty)
			} else {
				fmt.Println(ver)
			}
		}
		return err
	case "drop":
		return m.Drop()
	default:
		return fmt.Errorf("Unknown or missing migrate command")
	}
}

func Migrate() error {
	if err := runMigrateCommand(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return nil
}
