package server

import (
	"fmt"
	"net/http"

	"github.com/gisquick/gisquick-server/internal/domain"
	"github.com/labstack/echo/v4"
)

type App struct {
	Language         string `json:"lang"`
	LandingProject   string `json:"landing_project,omitempty"`
	PasswordResetUrl string `json:"reset_password_url,omitempty"`
}

type UserInfo struct {
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	FullName  string `json:"full_name"`
	Active    bool   `json:"active"`
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
		Language:       s.Config.Language,
		LandingProject: s.Config.LandingProject,
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

func (s *Server) handleGetSessionUser(c echo.Context) error {
	user, err := s.auth.GetUser(c)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, UserData{user})
}

func (s *Server) handleGetUsers(c echo.Context) error {
	accounts, err := s.accountsService.GetAllAccounts()
	if err != nil {
		return err
	}
	res := []UserInfo{}
	for _, u := range accounts {
		res = append(res, UserInfo{
			Username:  u.Username,
			FirstName: u.FirstName,
			LastName:  u.LastName,
			FullName:  u.FullName(),
			Active:    u.Active,
		})
	}
	return c.JSON(http.StatusOK, res)
}
