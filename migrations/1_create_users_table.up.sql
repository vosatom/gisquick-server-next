-- create user gisquick;
-- alter user  gisquick with superuser createdb;
-- CREATE SCHEMA IF NOT EXISTS gisquick AUTHORIZATION gisquick;

CREATE TABLE users (
	"username" varchar(30) PRIMARY KEY,
	"email" varchar(255) NOT NULL,
	"first_name" varchar(50) NOT NULL,
	"last_name" varchar(50) NOT NULL,
	"password" varchar(255) NOT NULL,
	"is_active" bool NOT NULL,
	"is_superuser" bool NOT NULL,
	"created_at" timestamptz NULL,
	"confirmed_at" timestamptz NULL,
	"last_login_at" timestamptz NULL
);

CREATE INDEX users_email_idx ON users USING btree (email);
