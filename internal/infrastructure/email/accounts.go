package email

import (
	"bytes"
	"fmt"
	htmltemplate "html/template"
	"net/url"
	texttemplate "text/template"

	"github.com/gisquick/gisquick-server/internal/domain"
	"github.com/gisquick/gisquick-server/internal/infrastructure/maps"
	mail "github.com/xhit/go-simple-mail/v2"
)

type AccountsEmailSender struct {
	client               EmailService
	sender               string
	siteURL              string
	activationSubject    string
	passwordResetSubject string
	templates            map[string]EmailTemplate
}

type EmailTemplate struct {
	HTML *htmltemplate.Template
	Text *texttemplate.Template
}

func parseEmailTemplate(name string) EmailTemplate {
	funcs := map[string]any{
		"query_escape": url.QueryEscape,
	}
	htmlFuncs := htmltemplate.FuncMap(funcs)
	textFuncs := texttemplate.FuncMap(funcs)
	html := htmltemplate.Must(htmltemplate.New("email").Funcs(htmlFuncs).ParseFiles("./templates/email_base.html", fmt.Sprintf("%s.html", name)))
	text := texttemplate.Must(texttemplate.New("email").Funcs(textFuncs).ParseFiles("./templates/email_base.txt", fmt.Sprintf("%s.txt", name)))
	return EmailTemplate{HTML: html, Text: text}
}

func NewAccountsEmailSender(client EmailService, sender, siteURL, activationSubject, passwordResetSubject string) *AccountsEmailSender {
	templates := make(map[string]EmailTemplate, 3)
	templates["activation_email"] = parseEmailTemplate("./templates/activation_email")
	templates["invitation_email"] = parseEmailTemplate("./templates/invitation_email")
	templates["password_reset_email"] = parseEmailTemplate("./templates/reset_password_email")
	return &AccountsEmailSender{
		client:               client,
		sender:               sender,
		siteURL:              siteURL,
		activationSubject:    activationSubject,
		passwordResetSubject: passwordResetSubject,
		templates:            templates,
	}
}

func (s *AccountsEmailSender) SendActivationEmail(account domain.Account, uid, token string, data map[string]interface{}) error {
	activationUrl, _ := url.Parse(s.siteURL)
	activationUrl.Path = "/accounts/activate/"
	params := activationUrl.Query()
	params.Set("uid", uid)
	params.Set("token", token)
	activationUrl.RawQuery = params.Encode()
	data = maps.NewMap(data)
	data["User"] = &account
	data["SiteURL"] = s.siteURL
	data["ActivationLink"] = activationUrl.String()
	data["uid"] = uid
	data["token"] = token
	template := "activation_email"
	if len(account.Password) == 0 {
		template = "invitation_email"
	}
	var htmlMsg, textMsg bytes.Buffer
	if err := s.templates[template].HTML.ExecuteTemplate(&htmlMsg, "email", data); err != nil {
		return err
	}
	if err := s.templates[template].Text.ExecuteTemplate(&textMsg, "email", data); err != nil {
		return err
	}
	email := mail.NewMSG()
	email.SetFrom(s.sender)
	email.AddTo(account.Email)
	email.SetSubject(s.activationSubject)
	email.SetBody(mail.TextPlain, textMsg.String())
	email.AddAlternative(mail.TextHTML, htmlMsg.String())
	// email.SetBody(mail.TextHTML, htmlMsg.String())
	// email.AddAlternative(mail.TextPlain, textMsg.String())
	if email.Error != nil {
		return email.Error
	}
	return s.client.SendEmail(email)
}

func (s *AccountsEmailSender) SendPasswordResetEmail(account domain.Account, uid, token string) error {
	activationUrl, _ := url.Parse(s.siteURL)
	activationUrl.Path = "/accounts/new-password/"
	params := activationUrl.Query()
	params.Set("uid", uid)
	params.Set("token", token)
	activationUrl.RawQuery = params.Encode()
	data := map[string]interface{}{
		"User":            &account,
		"SiteURL":         s.siteURL,
		"SetPasswordLink": activationUrl.String(),
	}
	var htmlMsg, textMsg bytes.Buffer
	if err := s.templates["password_reset_email"].HTML.ExecuteTemplate(&htmlMsg, "email", data); err != nil {
		return err
	}
	if err := s.templates["password_reset_email"].Text.ExecuteTemplate(&textMsg, "email", data); err != nil {
		return err
	}
	email := mail.NewMSG()
	email.SetFrom(s.sender)
	email.AddTo(account.Email)
	email.SetSubject(s.passwordResetSubject)
	email.SetBody(mail.TextPlain, textMsg.String())
	email.AddAlternative(mail.TextHTML, htmlMsg.String())

	if email.Error != nil {
		return email.Error
	}
	return s.client.SendEmail(email)
}

func (s *AccountsEmailSender) SendBulkEmail(accounts []domain.Account, subject string, htmlTemplate *htmltemplate.Template, textTemplate *texttemplate.Template, data map[string]interface{}) error {
	validAccounts := make([]domain.Account, 0, len(accounts))
	for _, a := range accounts {
		if a.Email != "" {
			validAccounts = append(validAccounts, a)
		}
	}
	index := 0
	generator := func() (*mail.Email, error) {
		if index >= len(validAccounts) {
			return nil, EndOfQue
		}
		account := validAccounts[index]
		index += 1

		templateData := maps.NewMap(data)
		templateData["User"] = &account
		templateData["SiteURL"] = s.siteURL
		email := mail.NewMSG()
		email.SetFrom(s.sender)
		email.AddTo(account.Email)
		email.SetSubject(subject)

		var htmlMsg, textMsg bytes.Buffer
		if textTemplate != nil {
			if err := textTemplate.Execute(&textMsg, templateData); err != nil {
				return email, fmt.Errorf("building text tempalte: %w", err)
			}
			email.SetBody(mail.TextPlain, textMsg.String())
		}
		if htmlTemplate != nil {
			// if err := htmlTemplate.ExecuteTemplate(&htmlMsg, "email", templateData); err != nil {
			if err := htmlTemplate.Execute(&htmlMsg, templateData); err != nil {
				return email, fmt.Errorf("building html tempalte: %w", err)
			}
			if textTemplate == nil {
				email.SetBody(mail.TextHTML, htmlMsg.String())
			} else {
				email.AddAlternative(mail.TextHTML, htmlMsg.String())
			}
		}
		return email, email.Error
	}

	/*
		email, err := generator()
		var failed []*mail.Email
		for err != EndOfQue {
			if err != nil {
				// failed to send email
				failed = append(failed, email)
				if email != nil {
					// log email.Error
				}
			} else {
				if err := email.Send(client); err != nil {
					failed = append(failed, email)
				}
			}
			email, err = generator()
		}
	*/
	err := s.client.SendMultiple(generator)
	if err != nil {
		return fmt.Errorf("sending bulk email: %w", err)
	}
	return nil
}
