package auth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gisquick/gisquick-server/internal/domain"
	"github.com/go-redis/redis/v8"
	"github.com/gofrs/uuid"
	"github.com/jellydator/ttlcache/v3"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

var (
	ErrUserNotFound    = errors.New("User not found")
	ErrInvalidPassword = errors.New("Password doesn't match")
	ErrInvalidSession  = errors.New("Invalid session")
)

type SessionInfo struct {
	ID       string
	Username string
}

type SessionStore interface {
	Set(ctx context.Context, sessionID, data string, expiration time.Duration) error
	Get(ctx context.Context, sessionID string) (string, error)
}

type RedisSessionStore struct {
	rdb *redis.Client
}

func NewRedisStore(rdb *redis.Client) *RedisSessionStore {
	return &RedisSessionStore{rdb: rdb}
}

func (s *RedisSessionStore) Set(ctx context.Context, sessionID, data string, expiration time.Duration) error {
	if err := s.rdb.Set(ctx, sessionID, data, expiration).Err(); err != nil {
		return fmt.Errorf("redis save session: %v", err)
	}
	return nil
}

func (s *RedisSessionStore) Get(ctx context.Context, sessionID string) (string, error) {
	val, err := s.rdb.Get(context.Background(), sessionID).Result()
	if err != nil {
		if err == redis.Nil {
			return "", ErrInvalidSession
		}
		return "", fmt.Errorf("redis get session: %v", err)
	}
	return val, nil
}

type AuthService struct {
	logger     *zap.SugaredLogger
	domain     string
	expiration time.Duration
	accounts   domain.AccountsRepository
	store      SessionStore
	cache      *ttlcache.Cache[string, domain.Account]
}

func NewAuthService(logger *zap.SugaredLogger, serverDomain string, expiration time.Duration, accounts domain.AccountsRepository, store SessionStore) *AuthService {
	loader := ttlcache.LoaderFunc[string, domain.Account](
		func(c *ttlcache.Cache[string, domain.Account], username string) *ttlcache.Item[string, domain.Account] {
			logger.Infof("ttlcache.LoaderFunc: %s", username)
			account, err := accounts.GetByUsername(username)
			if err != nil {
				logger.Errorw("getting account", "username", username, zap.Error(err))
				return nil
			}
			item := c.Set(username, account, ttlcache.DefaultTTL)
			return item
		},
	)
	cache := ttlcache.New(
		ttlcache.WithTTL[string, domain.Account](45*time.Second),
		ttlcache.WithLoader[string, domain.Account](loader),
		ttlcache.WithDisableTouchOnHit[string, domain.Account](),
	)
	return &AuthService{
		logger:     logger,
		domain:     serverDomain,
		expiration: expiration,
		accounts:   accounts,
		store:      store,
		cache:      cache,
	}
}

func (s *AuthService) GetSessionInfo(c echo.Context) (*SessionInfo, error) {
	si, saved := c.Get("session").(SessionInfo)
	if saved {
		return &si, nil
	}
	sessionid := c.Request().Header.Get("Authorization")
	if sessionid == "" {
		cookie, err := c.Request().Cookie("sessionid")
		if err == nil {
			sessionid = cookie.Value
		}
	}
	if sessionid == "" {
		c.Set("session", nil)
		return nil, nil
	}
	data, err := s.store.Get(c.Request().Context(), sessionid)
	if err != nil {
		if errors.Is(err, ErrInvalidSession) {
			s.LogoutUser(c)
			c.Set("session", nil)
			return nil, nil
		}
		return nil, err
	}
	si = SessionInfo{ID: sessionid, Username: data}
	c.Set("session", si)
	return &si, nil
}

func (s *AuthService) GetUser(c echo.Context) (domain.User, error) {
	u, saved := c.Get("user").(domain.User)
	if saved {
		return u, nil
	}
	session, err := s.GetSessionInfo(c)
	if err != nil {
		return domain.User{}, err
	}
	if session == nil {
		return domain.User{IsGuest: true}, nil
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("auth: get session user: %w", err)
	}
	item := s.cache.Get(session.Username)
	if item == nil {
		return domain.User{IsGuest: true}, nil
	}
	u = AccountToUser(item.Value())
	c.Set("user", u)
	return u, nil
}

func (s *AuthService) Authenticate(login, password string) (domain.Account, error) {
	var account domain.Account
	var err error
	if strings.Contains(login, "@") {
		account, err = s.accounts.GetByEmail(login)
	} else {
		account, err = s.accounts.GetByUsername(login)
	}
	if err != nil {
		return domain.Account{}, err
	}
	if !account.Active {
		return domain.Account{}, ErrUserNotFound
	}
	if !account.CheckPassword(password) {
		return domain.Account{}, ErrInvalidPassword
	}
	return account, nil
}

func (s *AuthService) LoginUser(c echo.Context, userAccount domain.Account) error {
	token, err := uuid.NewV4()
	if err != nil {
		return err
	}
	sessionid := token.String()
	// sessionid := fmt.Sprintf("%s:%s", user.Username, token.String())
	if err := s.store.Set(c.Request().Context(), sessionid, userAccount.Username, s.expiration); err != nil {
		return fmt.Errorf("save session: %v", err)
	}

	now := time.Now().UTC()
	userAccount.LastLogin = &now
	if err := s.accounts.Update(userAccount); err != nil {
		// not a critical issue, just log error and continue
		log.Println("update user.last_login: %w", err) // TODO: proper logger
	}

	// serverUrl.Hostname()
	// c.Request().URL.Hostname()
	http.SetCookie(c.Response(), &http.Cookie{
		Path:     "/",
		Domain:   s.domain,
		SameSite: http.SameSiteLaxMode,
		Name:     "sessionid",
		Value:    sessionid,
		HttpOnly: true,
		Expires:  time.Now().Add(s.expiration),
	})
	return nil
}

func (s *AuthService) LogoutUser(c echo.Context) {
	http.SetCookie(c.Response(), &http.Cookie{
		Path:     "/",
		Domain:   s.domain,
		SameSite: http.SameSiteLaxMode,
		Name:     "sessionid",
		Value:    "",
		MaxAge:   -1,
		HttpOnly: true,
	})
}

func AccountToUser(account domain.Account) domain.User {
	return domain.User{
		Username:        account.Username,
		Email:           account.Email,
		FirstName:       account.FirstName,
		LastName:        account.LastName,
		IsSuperuser:     account.IsSuperuser,
		IsGuest:         false,
		IsAuthenticated: true,
	}
}
