package mock

import (
	"log"

	mail "github.com/xhit/go-simple-mail/v2"
)

type dummyService struct{}

func (s *dummyService) SendEmail(email *mail.Email) error {
	email.Encoding = mail.EncodingNone
	log.Println(email.GetMessage())
	return nil
}

func NewDummyEmailService() *dummyService {
	return &dummyService{}
}
