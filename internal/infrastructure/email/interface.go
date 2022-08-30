package email

import mail "github.com/xhit/go-simple-mail/v2"

type EmailService interface {
	SendEmail(email *mail.Email) error
}
