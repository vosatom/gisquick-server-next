package mock

import (
	"log"
	"net/url"

	"github.com/gisquick/gisquick-server/internal/domain"
)

type EmailService struct {
	siteURL string
}

func (s *EmailService) SendRegistrationEmail(account domain.Account, uid, token string) error {
	activationUrl, _ := url.Parse(s.siteURL)
	activationUrl.Path = "/api/accounts/activate"
	params := activationUrl.Query()
	params.Set("uid", uid)
	params.Set("token", token)
	log.Println("Activation link:", activationUrl.String())
	return nil
}

func (s *EmailService) SendPasswordResetEmail(account domain.Account, uid, token string) error {

	return nil
}
