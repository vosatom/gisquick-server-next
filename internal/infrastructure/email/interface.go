package email

import (
	"errors"
	"fmt"

	mail "github.com/xhit/go-simple-mail/v2"
)

var EndOfQue = errors.New("End of emails queue")

type EmailError struct {
	Recepients []string
	Err        error
}

type BulkEmailError struct {
	Errors []EmailError
}

func (e *BulkEmailError) Error() string {
	return fmt.Sprintf("failed to send %d emails", len(e.Errors))
}

type EmailService interface {
	SendEmail(email *mail.Email) error
	SendMultiple(next func() (*mail.Email, error)) error
}
