package email

import (
	"crypto/tls"
	"fmt"
	"time"

	mail "github.com/xhit/go-simple-mail/v2"
)

func newEmailError(email *mail.Email, err error) EmailError {
	e := err
	if email != nil && email.Error != nil {
		e = email.Error
	}
	return EmailError{
		Recepients: email.GetRecipients(),
		Err:        e,
	}
}

type SmtpEmailService struct {
	Host       string
	Port       int
	Encryption mail.Encryption
	Username   string
	Password   string
}

func (s *SmtpEmailService) SendEmail(email *mail.Email) error {
	smtp := mail.NewSMTPClient()
	smtp.Host = s.Host
	smtp.Port = s.Port
	smtp.Username = s.Username
	smtp.Password = s.Password
	smtp.Encryption = s.Encryption
	if s.Encryption == mail.EncryptionTLS || s.Encryption == mail.EncryptionSSLTLS || s.Encryption == mail.EncryptionSTARTTLS {
		smtp.TLSConfig = &tls.Config{
			ServerName: s.Host,
		}
	} else {
		smtp.TLSConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}
	smtp.KeepAlive = false
	// Timeout for connect to SMTP Server
	smtp.ConnectTimeout = 10 * time.Second
	// Timeout for send the data and wait respond
	smtp.SendTimeout = 10 * time.Second

	client, err := smtp.Connect()
	if err != nil {
		return fmt.Errorf("smtp connect: %w", err)
	}
	defer client.Close()
	err = email.Send(client)
	if err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}

func (s *SmtpEmailService) SendMultiple(next func() (*mail.Email, error)) error {
	smtp := mail.NewSMTPClient()
	smtp.Host = s.Host
	smtp.Port = s.Port
	smtp.Username = s.Username
	smtp.Password = s.Password
	smtp.Encryption = s.Encryption
	if s.Encryption == mail.EncryptionTLS || s.Encryption == mail.EncryptionSSLTLS || s.Encryption == mail.EncryptionSTARTTLS {
		smtp.TLSConfig = &tls.Config{
			ServerName: s.Host,
		}
	} else {
		smtp.TLSConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}
	smtp.KeepAlive = true
	// Timeout for connect to SMTP Server
	smtp.ConnectTimeout = 10 * time.Second
	// Timeout for send the data and wait respond
	smtp.SendTimeout = 10 * time.Second

	client, err := smtp.Connect()
	if err != nil {
		return fmt.Errorf("smtp connect: %w", err)
	}
	defer client.Close()
	email, err := next()
	var errs []EmailError
	for err != EndOfQue {
		if err != nil {
			if email != nil {
				errs = append(errs, newEmailError(email, err))
			} else {
				errs = append(errs, EmailError{Err: err})
			}
		} else {
			if err := email.Send(client); err != nil {
				errs = append(errs, newEmailError(email, err))
			}
		}
		email, err = next()
	}
	if len(errs) > 0 {
		return &BulkEmailError{Errors: errs}
	}
	return nil
}
