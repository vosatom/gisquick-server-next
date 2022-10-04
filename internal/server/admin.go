package server

import (
	"bytes"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"net/http"
	"strings"
	texttemplate "text/template"
	"time"

	"github.com/gisquick/gisquick-server/internal/domain"
	"github.com/gisquick/gisquick-server/internal/infrastructure/email"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

type Account struct {
	Username   string     `json:"username"`
	Email      string     `json:"email"`
	FirstName  string     `json:"first_name"`
	LastName   string     `json:"last_name"`
	Superuser  bool       `json:"superuser"`
	Active     bool       `json:"active"`
	DateJoined *time.Time `json:"date_joined"`
	LastLogin  *time.Time `json:"last_login"`
}

func toAccountInfo(a domain.Account) Account {
	return Account{
		Username:   a.Username,
		Email:      a.Email,
		FirstName:  a.FirstName,
		LastName:   a.LastName,
		Active:     a.Active,
		Superuser:  a.IsSuperuser,
		DateJoined: a.DateJoined,
		LastLogin:  a.LastLogin,
	}
}

func (s *Server) handleAdminConfig(c echo.Context) error {
	return c.File("/etc/gisquick/admin.json")
}

func (s *Server) handleGetAllUsers(c echo.Context) error {
	accounts, err := s.accountsService.GetAllAccounts()
	if err != nil {
		return err
	}
	data := []Account{}
	for _, a := range accounts {
		data = append(data, toAccountInfo(a))
	}
	return c.JSON(http.StatusOK, data)
}

func (s *Server) handleGetUser(c echo.Context) error {
	username := c.Param("user")
	account, err := s.accountsService.Repository.GetByUsername(username)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toAccountInfo(account))
}

func (s *Server) handleUpdateUser() func(echo.Context) error {
	type UserFields struct {
		Email     string `json:"email"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Superuser bool   `json:"superuser"`
		Active    bool   `json:"active"`
	}
	return func(c echo.Context) error {
		username := c.Param("user")
		form := new(UserFields)
		if err := (&echo.DefaultBinder{}).BindBody(c, &form); err != nil {
			return err
		}
		account, err := s.accountsService.Repository.GetByUsername(username)
		if err != nil {
			return err
		}
		account.Email = form.Email
		account.FirstName = form.FirstName
		account.LastName = form.LastName
		account.Active = form.Active
		account.IsSuperuser = form.Superuser
		if err := s.accountsService.Repository.Update(account); err != nil {
			return fmt.Errorf("updating account [%s]: %w", username, err)
		}
		return c.JSON(http.StatusOK, toAccountInfo(account))
	}
}

func (s *Server) handleCreateUser() func(echo.Context) error {
	type UserFields struct {
		Username  string                 `json:"username"`
		Email     string                 `json:"email"`
		Password  string                 `json:"password"`
		FirstName string                 `json:"first_name"`
		LastName  string                 `json:"last_name"`
		Superuser bool                   `json:"superuser"`
		Active    bool                   `json:"active"`
		Extra     map[string]interface{} `json:"extra"`
		SendEmail bool                   `json:"send_email"`
	}
	return func(c echo.Context) error {
		form := new(UserFields)
		if err := (&echo.DefaultBinder{}).BindBody(c, &form); err != nil {
			return err
		}
		if form.SendEmail && !s.accountsService.SupportEmails() {
			return echo.NewHTTPError(http.StatusPreconditionFailed, "Email service not supported")
		}
		account, err := domain.NewAccount(
			form.Username,
			form.Email,
			form.FirstName,
			form.LastName,
			form.Password,
		)
		if err != nil {
			return err
		}
		account.Active = form.Active
		account.IsSuperuser = form.Superuser
		if err := s.accountsService.Repository.Create(account); err != nil {
			s.log.Errorw("creating account", "username", form.Username, zap.Error(err))
			return fmt.Errorf("Failed to create user account")
		}
		if account.Email != "" && !account.Active && form.SendEmail {
			if err := s.accountsService.SendActivationEmail(account, form.Extra); err != nil {
				s.log.Errorw("sending activation email", "username", form.Username, "email", form.Email, zap.Error(err))
				return fmt.Errorf("Failed to send activation email")
			}
		}

		return c.JSON(http.StatusOK, toAccountInfo(account))
		// return c.NoContent(http.StatusOK)
	}
}

func (s *Server) handleDeleteUser(c echo.Context) error {
	username := c.Param("user")
	return s.accountsService.Repository.Delete(username)
}

func (s *Server) handleGetEmailPreview() func(echo.Context) error {
	type Params struct {
		HtmlTemplate string `json:"html_template"`
		TextTemplate string `json:"text_template"`
		Style        string `json:"style"`
	}
	type Preview struct {
		Html string `json:"html"`
		Text string `json:"text"`
	}
	return func(c echo.Context) error {
		params := new(Params)
		if err := (&echo.DefaultBinder{}).BindBody(c, &params); err != nil {
			return err
		}
		if params.HtmlTemplate == "" && params.TextTemplate == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "Template not specified")
		}

		resp := Preview{}
		var buffer bytes.Buffer
		user, err := s.auth.GetUser(c)
		if err != nil {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "Service currently unavailable")
		}
		data := map[string]interface{}{
			"User": &domain.Account{
				Username:  user.Username,
				FirstName: user.FirstName,
				LastName:  user.LastName,
				Email:     user.Email,
			},
			"SiteURL": s.Config.SiteURL,
		}
		if params.TextTemplate != "" {
			t := texttemplate.New("preview")
			t.Parse(params.TextTemplate)
			if strings.HasPrefix(params.TextTemplate, `{{template "email" .}}`) {
				t.ParseFiles("./templates/email_base.txt")
			}
			if err := t.Execute(&buffer, data); err != nil {
				return fmt.Errorf("processing text template: %w", err)
			}
			resp.Text = buffer.String()
		}

		if params.HtmlTemplate != "" {
			t := htmltemplate.New("preview")
			buffer.Reset()
			data["Style"] = htmltemplate.CSS(params.Style)
			t.Parse(params.HtmlTemplate)
			if strings.HasPrefix(params.HtmlTemplate, `{{template "email" .}}`) {
				t.ParseFiles("./templates/email_base.html")
			}
			// if err := t.ExecuteTemplate(&buffer, "email", data); err != nil {
			if err := t.Execute(&buffer, data); err != nil {
				return fmt.Errorf("processing html template: %w", err)
			}
			resp.Html = buffer.String()
		}
		return c.JSON(http.StatusOK, resp)
	}
}

func (s *Server) handleSendEmail() func(echo.Context) error {
	type Params struct {
		HtmlTemplate string   `json:"html_template"`
		TextTemplate string   `json:"text_template"`
		Style        string   `json:"style"`
		Subject      string   `json:"subject"`
		Users        []string `json:"users"`
		UsersFilter  string   `json:"users_filter"`
	}
	type EmailError struct {
		Recepient string `json:"recepient"`
		Message   string `json:"msg"`
	}
	type EmailErrors struct {
		Subject string       `json:"subject"`
		Errors  []EmailError `json:"errors"`
	}
	return func(c echo.Context) error {
		params := new(Params)
		if err := (&echo.DefaultBinder{}).BindBody(c, &params); err != nil {
			return err
		}
		if params.HtmlTemplate == "" && params.TextTemplate == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "Email template not specified")
		}
		var htmlTemplate *htmltemplate.Template
		var textTemplate *texttemplate.Template
		if params.TextTemplate != "" {
			textTemplate = texttemplate.New("new_text_email")
			textTemplate.Parse(params.TextTemplate)
			if strings.HasPrefix(params.TextTemplate, `{{template "email" .}}`) {
				textTemplate.ParseFiles("./templates/email_base.txt")
			}
		}
		if params.HtmlTemplate != "" {
			htmlTemplate = htmltemplate.New("new_html_email")
			htmlTemplate.Parse(params.HtmlTemplate)
			if strings.HasPrefix(params.HtmlTemplate, `{{template "email" .}}`) {
				htmlTemplate.ParseFiles("./templates/email_base.html")
			}
		}

		var accounts []domain.Account
		if params.UsersFilter == "active" {
			var err error
			accounts, err = s.accountsService.GetActiveAccounts()
			if err != nil {
				return fmt.Errorf("querying accounts: %w", err)
			}
		} else if len(params.Users) > 0 {
			accounts = make([]domain.Account, len(params.Users))
			var err error
			for i, username := range params.Users {
				accounts[i], err = s.accountsService.Repository.GetByUsername(username)
				if err != nil {
					return echo.NewHTTPError(http.StatusBadRequest, "Invalid recepients list")
				}
			}
		}
		if len(accounts) == 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "No email recepients")
		}
		data := map[string]interface{}{
			"Style": htmltemplate.CSS(params.Style),
		}
		if err := s.accountsService.Email.SendBulkEmail(accounts, params.Subject, htmlTemplate, textTemplate, data); err != nil {
			var bulkErr *email.BulkEmailError
			switch {
			case errors.As(err, &bulkErr):
				errs := make([]EmailError, len(bulkErr.Errors))
				for i, e := range bulkErr.Errors {
					errs[i] = EmailError{Recepient: strings.Join(e.Recepients, ","), Message: e.Err.Error()}
				}
				errData := EmailErrors{
					Subject: params.Subject,
					Errors:  errs,
				}
				s.log.Errorw("sending bulk email", "subject", params.Subject, "error", errData)
				return c.JSON(http.StatusInternalServerError, errData)
			default:
				s.log.Errorw("sending bulk email", "subject", params.Subject, zap.Error(err))
			}
			return err
			// return echo.NewHTTPError(http.StatusInternalServerError, "Failed to send email")
		}
		return c.NoContent(http.StatusOK)
	}
}

func (s *Server) handleSendActivationEmail() func(echo.Context) error {
	type Form struct {
		Email string `json:"email"`
	}
	return func(c echo.Context) error {
		form := new(Form)
		if err := (&echo.DefaultBinder{}).BindBody(c, &form); err != nil {
			// return echo.NewHTTPError(http.StatusBadRequest, "Invalid input parameters")
			return err
		}
		account, err := s.accountsService.Repository.GetByEmail(form.Email)
		if err != nil {
			return fmt.Errorf("getting user account: %w", err)
		}
		if account.Active {
			return echo.NewHTTPError(http.StatusBadRequest, "Account already activated")
		}
		if err := s.accountsService.SendActivationEmail(account, nil); err != nil {
			s.log.Errorw("sending activation email", "username", account.Username, "email", account.Email, zap.Error(err))
			return fmt.Errorf("Failed to send activation email")
		}
		return nil
	}
}
