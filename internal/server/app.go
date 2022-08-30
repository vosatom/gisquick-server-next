package server

import (
	"fmt"
	"net/http"

	"github.com/gisquick/gisquick-server/internal/domain"
	"github.com/labstack/echo/v4"
)

type App struct {
	Language         string `json:"lang"`
	PasswordResetUrl string `json:"reset_password_url,omitempty"`
}

type UserInfo struct {
	Username  string `json:"username"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	FullName  string `json:"full_name"`
}

func dtoUser(u domain.User) UserInfo {
	return UserInfo{
		Username:  u.Username,
		Email:     u.Email,
		FirstName: u.FirstName,
		LastName:  u.LastName,
	}
}

type AppPayload struct {
	App  App          `json:"app"`
	User *domain.User `json:"user"`
}

func (s *Server) handleAppInit(c echo.Context) error {
	user, err := s.auth.GetUser(c)
	if err != nil {
		return fmt.Errorf("handleAppInit get user: %w", err)
	}
	// userdtoUser()
	app := App{
		Language: s.Config.Language,
	}
	if s.accountsService.SupportEmails() {
		app.PasswordResetUrl = "/api/accounts/password_reset"
	}
	data := AppPayload{App: app, User: &user}
	return c.JSON(http.StatusOK, data)
}

type UserData struct {
	User domain.User `json:"user"`
}

func (s *Server) handleGetUser(c echo.Context) error {
	user, err := s.auth.GetUser(c)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, UserData{user})
	// return echo.ErrUnauthorized
}

func (s *Server) handleGetUsers(c echo.Context) error {
	accounts, err := s.accountsService.GetActiveAccounts()
	if err != nil {
		return err
	}
	res := []UserInfo{}
	for _, u := range accounts {
		res = append(res, UserInfo{
			Username:  u.Username,
			Email:     u.Email,
			FirstName: u.FirstName,
			LastName:  u.LastName,
			FullName:  u.FullName(),
		})
	}
	return c.JSON(http.StatusOK, res)
}
