package server

import (
	"net/http"

	"github.com/gisquick/gisquick-server/internal/server/auth"
	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
)

func (s *Server) handleLogin() func(echo.Context) error {
	type LoginForm struct {
		Username string `json:"username" form:"username" validate:"required"`
		Password string `json:"password" form:"password" validate:"required"`
	}
	var validate = validator.New()
	return func(c echo.Context) error {
		form := new(LoginForm)
		if err := c.Bind(form); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		if err := validate.Struct(form); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		user, err := s.auth.Authenticate(form.Username, form.Password)
		if err != nil {
			// s.log.Errorw("authenticate", zap.Error(err)) // TODO: handle various types
			return echo.NewHTTPError(http.StatusUnauthorized, "Please provide valid credentials")
		}
		if err := s.auth.LoginUser(c, user); err != nil {
			return err
		}
		return c.JSON(http.StatusOK, auth.AccountToUser(user))
	}
}

func (s *Server) handleLogout(c echo.Context) error {
	s.auth.LogoutUser(c)
	return c.NoContent(http.StatusOK)
}
