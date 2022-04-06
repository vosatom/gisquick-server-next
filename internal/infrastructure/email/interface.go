package email

import mail "github.com/xhit/go-simple-mail/v2"

type EmailClient interface {
	SendEmail(email *mail.Email) error
}
