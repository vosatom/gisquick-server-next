package postgres

import "time"

type User struct {
	Username    string     `db:"username"`
	Email       string     `db:"email"`
	Password    []byte     `db:"password"`
	FirstName   string     `db:"first_name"`
	LastName    string     `db:"last_name"`
	IsSuperuser bool       `db:"is_superuser"`
	IsActive    bool       `db:"is_active"`
	DateJoined  *time.Time `db:"date_joined"`
	LastLogin   *time.Time `db:"last_login"`
}
