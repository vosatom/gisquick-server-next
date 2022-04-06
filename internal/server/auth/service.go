package auth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/ReneKroon/ttlcache/v2"
	"github.com/gisquick/gisquick-server/internal/domain"
	"github.com/go-redis/redis/v8"
	"github.com/gofrs/uuid"
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
	cache      *ttlcache.Cache
}

func NewAuthService(logger *zap.SugaredLogger, domain string, expiration time.Duration, accounts domain.AccountsRepository, store SessionStore) *AuthService {
	cache := ttlcache.NewCache()
	cache.SkipTTLExtensionOnHit(true)
	return &AuthService{
		logger:     logger,
		domain:     domain,
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

func (s *AuthService) loadUserAccount(username string) (interface{}, time.Duration, error) {
	log.Println("loadUserAccount", username)
	s.logger.Infow("loadUserAccount", "name", username)
	account, err := s.accounts.GetByUsername(username)
	ttl := time.Second * 45
	return account, ttl, err
}

func (s *AuthService) getUserAccount(username string) (domain.Account, error) {
	// loadFn := func(key string) (interface{}, time.Duration, error) {
	// 	log.Println("getUserAccount", username)
	// 	account, err := s.accounts.GetByUsername(username)
	// 	return account, 5 * time.Minute, err
	// }
	// a, err := s.cache.GetByLoader(username, loadFn)
	a, err := s.cache.GetByLoader(username, s.loadUserAccount)
	// if err != nil {
	// 	return domain.Account{}, err
	// }
	return a.(domain.Account), err
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
	// account, err := s.accounts.GetByUsername(session.Username)
	account, err := s.getUserAccount(session.Username)
	if err != nil {
		return domain.User{}, fmt.Errorf("auth: get session user: %w", err)
	}
	u = AccountToUser(account)
	c.Set("user", u)
	return u, nil
}

func (s *AuthService) Authenticate(username, password string) (domain.Account, error) {
	account, err := s.accounts.GetByUsername(username)
	if errors.Is(err, domain.ErrAccountNotFound) {
		return domain.Account{}, err // TODO: wrap
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
