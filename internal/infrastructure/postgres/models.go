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
	Created     *time.Time `db:"created_at"`
	Confirmed   *time.Time `db:"confirmed_at"`
	LastLogin   *time.Time `db:"last_login_at"`
}
