package email

import (
	"crypto/tls"
	"time"

	mail "github.com/xhit/go-simple-mail/v2"
)

type EmailService struct {
	Host     string
	Port     int
	SSL      bool
	Username string
	Password string
}

func (s *EmailService) SendEmail(email *mail.Email) error {
	smtp := mail.NewSMTPClient()
	smtp.Host = s.Host
	smtp.Port = s.Port
	smtp.Username = s.Username
	smtp.Password = s.Password
	if s.SSL {
		smtp.Encryption = mail.EncryptionSSLTLS
	}

	smtp.KeepAlive = false
	// Timeout for connect to SMTP Server
	smtp.ConnectTimeout = 10 * time.Second
	// Timeout for send the data and wait respond
	smtp.SendTimeout = 10 * time.Second
	// Set TLSConfig to provide custom TLS configuration. For example,
	// to skip TLS verification (useful for testing):
	smtp.TLSConfig = &tls.Config{InsecureSkipVerify: true}

	client, err := smtp.Connect()
	if err != nil {
		return err
	}
	defer client.Close()
	err = email.Send(client)
	if err != nil {
		return err
	}
	return nil
}
