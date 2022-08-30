package email

import (
	"bytes"
	"net/url"
	"text/template"

	"github.com/gisquick/gisquick-server/internal/domain"
	mail "github.com/xhit/go-simple-mail/v2"
)

type AccountsEmailSender struct {
	client    EmailService
	sender    string
	siteURL   string
	templates map[string]*template.Template
}

func NewAccountsEmailSender(client EmailService, sender, siteURL string) *AccountsEmailSender {
	templates := make(map[string]*template.Template, 2)
	templates["activation_email_html"] = template.Must(template.ParseFiles("./templates/activation_email.html", "./templates/email_base.html"))
	templates["activation_email_text"] = template.Must(template.ParseFiles("./templates/activation_email.txt", "./templates/email_base.txt"))
	templates["password_reset_email_html"] = template.Must(template.ParseFiles("./templates/reset_password_email.html", "./templates/email_base.html"))
	templates["password_reset_email_text"] = template.Must(template.ParseFiles("./templates/reset_password_email.txt", "./templates/email_base.txt"))
	return &AccountsEmailSender{
		client:    client,
		sender:    sender,
		siteURL:   siteURL,
		templates: templates,
	}
}

func (s *AccountsEmailSender) SendRegistrationEmail(account domain.Account, uid, token string) error {
	activationUrl, _ := url.Parse(s.siteURL)
	activationUrl.Path = "/accounts/activate"
	params := activationUrl.Query()
	params.Set("uid", uid)
	params.Set("token", token)
	activationUrl.RawQuery = params.Encode()
	data := map[string]interface{}{
		"User":           &account,
		"SiteURL":        s.siteURL,
		"ActivationLink": activationUrl.String(),
	}
	var htmlMsg, textMsg bytes.Buffer
	if err := s.templates["activation_email_html"].ExecuteTemplate(&htmlMsg, "email", data); err != nil {
		return err
	}
	if err := s.templates["activation_email_text"].ExecuteTemplate(&textMsg, "email", data); err != nil {
		return err
	}
	email := mail.NewMSG()
	email.SetFrom(s.sender)
	email.AddTo(account.Email)
	email.SetSubject("Gisquick Registration")
	email.SetBody(mail.TextPlain, textMsg.String())
	email.AddAlternative(mail.TextHTML, htmlMsg.String())
	if email.Error != nil {
		return email.Error
	}
	return s.client.SendEmail(email)
}

func (s *AccountsEmailSender) SendPasswordResetEmail(account domain.Account, uid, token string) error {
	activationUrl, _ := url.Parse(s.siteURL)
	activationUrl.Path = "/accounts/new-password"
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
	if err := s.templates["password_reset_email_html"].ExecuteTemplate(&htmlMsg, "email", data); err != nil {
		return err
	}
	if err := s.templates["password_reset_email_text"].ExecuteTemplate(&textMsg, "email", data); err != nil {
		return err
	}
	email := mail.NewMSG()
	email.SetFrom(s.sender)
	email.AddTo(account.Email)
	email.SetSubject("Gisquick Password Reset")
	email.SetBody(mail.TextPlain, textMsg.String())
	email.AddAlternative(mail.TextHTML, htmlMsg.String())

	if email.Error != nil {
		return email.Error
	}
	return s.client.SendEmail(email)
}
