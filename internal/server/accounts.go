package server

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gisquick/gisquick-server/internal/application"
	"github.com/gisquick/gisquick-server/internal/domain"
	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

func (s *Server) handleSignUp() func(echo.Context) error {
	type SignUpForm struct {
		Username        string `json:"username" form:"username" validate:"required"`
		Password        string `json:"password1" form:"password1" validate:"required"`
		PasswordConfirm string `json:"password2" form:"password2" validate:"required"`
		Email           string `json:"email" form:"email" validate:"required,email"`
		FirstName       string `json:"first_name" form:"first_name"`
		LastName        string `json:"last_name" form:"last_name"`
	}
	var validate = validator.New()

	return func(c echo.Context) error {
		form := new(SignUpForm)
		if err := c.Bind(form); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		if err := validate.Struct(form); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		if form.Password != form.PasswordConfirm {
			return echo.NewHTTPError(http.StatusBadRequest, "Password doesn't match")
		}
		err := s.accountsService.NewAccount(form.Username, form.Email, form.FirstName, form.LastName, form.Password)
		if err != nil {
			if errors.Is(err, domain.ErrAccountExists) {
				return echo.NewHTTPError(http.StatusBadRequest, "Account already exists")
			}
			// todo handle specific error types
			return err
		}
		return c.NoContent(http.StatusOK)
	}
}

func (s *Server) handleActivateAccount() func(echo.Context) error {
	return func(c echo.Context) error {
		uid := c.QueryParam("uid")
		token := c.QueryParam("token")

		err := s.accountsService.Activate(uid, token)
		if err != nil {
			s.log.Errorw("activate account", "uid", uid, zap.Error(err))
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid activation link")
		}
		return c.NoContent(http.StatusOK)
	}
}

func (s *Server) handleCheckAvailability() func(echo.Context) error {
	type Resp struct {
		Available bool `json:"available"`
	}
	return func(c echo.Context) error {
		field := c.QueryParam("field")
		value := c.QueryParam("value")

		if field == "" || value == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid parameters")
		}
		var exists bool
		var err error
		switch field {
		case "username":
			exists, err = s.accountsService.Repository.UsernameExists(value)
		case "email":
			exists, err = s.accountsService.Repository.EmailExists(value) // strings.ToLower()?
		default:
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid value of 'field' parameter")
		}
		if err != nil {
			return fmt.Errorf("check account availability: %v", err)
		}
		return c.JSON(http.StatusOK, Resp{Available: !exists})
	}
}

func (s *Server) handlePasswordReset() func(echo.Context) error {
	type PasswordResetForm struct {
		Email string `json:"email" form:"email" validate:"required,email"`
	}
	var validate = validator.New()
	return func(c echo.Context) error {
		form := new(PasswordResetForm)
		if err := c.Bind(form); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		if err := validate.Struct(form); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		if err := s.accountsService.RequestPasswordReset(form.Email); err != nil {
			if errors.Is(err, domain.ErrAccountNotFound) {
				return echo.NewHTTPError(http.StatusBadRequest, "Account with given email doesn't exist")
			} else if errors.Is(err, application.ErrNotActiveAccount) {
				return echo.NewHTTPError(http.StatusBadRequest, err.Error())
			}
			return err
		}
		return c.NoContent(http.StatusOK)
	}
}

func (s *Server) handleNewPassword() func(echo.Context) error {
	type NewPasswordForm struct {
		UID             string `query:"uid" validate:"required"`
		Token           string `query:"token" validate:"required"`
		Password        string `json:"new_password1" form:"new_password1" validate:"required"`
		PasswordConfirm string `json:"new_password2" form:"new_password2" validate:"required"`
	}
	var validate = validator.New()
	return func(c echo.Context) error {
		form := new(NewPasswordForm)
		if err := (&echo.DefaultBinder{}).BindQueryParams(c, form); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid query parameters")
		}
		if err := (&echo.DefaultBinder{}).BindBody(c, form); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid query parameters")
		}
		if err := validate.Struct(form); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		if form.Password != form.PasswordConfirm {
			return echo.NewHTTPError(http.StatusBadRequest, "Passwords doesn't match")
		}
		err := s.accountsService.SetNewPassword(form.UID, form.Token, form.Password)
		if err != nil {
			if errors.Is(err, application.ErrInvalidToken) {
				return echo.NewHTTPError(http.StatusBadRequest, "Invalid link")
			}
		}
		return err
	}
}

func (s *Server) handleChangePassword() func(echo.Context) error {
	type ChangePasswordForm struct {
		OldPassword        string `json:"old_password" form:"old_password" validate:"required,email"`
		NewPassword        string `json:"new_password1" form:"new_password1" validate:"required"`
		NewPasswordConfirm string `json:"new_password2" form:"new_password2" validate:"required"`
	}
	var validate = validator.New()
	return func(c echo.Context) error {
		form := new(ChangePasswordForm)
		if err := c.Bind(form); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		if err := validate.Struct(form); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		if form.NewPassword != form.NewPasswordConfirm {
			return echo.NewHTTPError(http.StatusBadRequest, "New passwords doesn't match")
		}
		sessionInfo, err := s.auth.GetSessionInfo(c)
		if err != nil {
			return err
		}
		if sessionInfo == nil {
			return echo.NewHTTPError(http.StatusUnauthorized) // should be already handled by LoginRequired middleware
		}
		account, err := s.accountsService.Repository.GetByUsername(sessionInfo.Username)
		if err != nil {
			return err
		}
		if !account.CheckPassword(form.OldPassword) {
			return echo.NewHTTPError(http.StatusBadRequest, "Old password doesn't match")
		}
		if err := account.SetPassword(form.NewPassword); err != nil {
			return err
		}
		return s.accountsService.Repository.Update(account)
	}
}
