package email

import (
	"crypto/tls"
	"fmt"
	"time"

	mail "github.com/xhit/go-simple-mail/v2"
)

type SmtpEmailService struct {
	Host     string
	Port     int
	SSL      bool
	Username string
	Password string
}

func (s *SmtpEmailService) SendEmail(email *mail.Email) error {
	smtp := mail.NewSMTPClient()
	smtp.Host = s.Host
	smtp.Port = s.Port
	smtp.Username = s.Username
	smtp.Password = s.Password
	if s.SSL {
		smtp.Encryption = mail.EncryptionSSLTLS
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
