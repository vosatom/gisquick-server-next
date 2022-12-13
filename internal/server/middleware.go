package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/gisquick/gisquick-server/internal/application"
	"github.com/gisquick/gisquick-server/internal/domain"
	"github.com/gisquick/gisquick-server/internal/server/auth"
	"github.com/go-redis/redis/v8"
	"github.com/labstack/echo/v4"
)

func LoginRequiredMiddlewareWithConfig(a *auth.AuthService) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			si, err := a.GetSessionInfo(c)
			if err != nil {
				return fmt.Errorf("login required middleware: %w", err)
			}
			if si == nil {
				// add support to basic auth here? (with a.GetUser())
				return echo.ErrUnauthorized
			}
			return next(c)
		}
	}
}

func SuperuserAccessMiddleware(a *auth.AuthService) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user, err := a.GetUser(c)
			if err != nil {
				return fmt.Errorf("SuperuserAccessMiddleware: %w", err)
			}
			if user.IsGuest {
				return echo.ErrUnauthorized
			}
			if !user.IsSuperuser {
				return echo.ErrForbidden
			}
			return next(c)
		}
	}
}

func ProjectAdminAccessMiddleware(a *auth.AuthService) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			username := c.Param("user")
			projectName := c.Param("name")

			user, err := a.GetUser(c)
			if err != nil {
				return fmt.Errorf("ProjectAdminAccessMiddleware: %w", err)
			}
			if username != user.Username && !user.IsSuperuser {
				return echo.ErrUnauthorized
			}
			c.Set("project", filepath.Join(username, projectName))
			return next(c)
		}
	}
}

func ProjectAccessMiddleware(a *auth.AuthService, ps application.ProjectService, basicAuthRealm string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			username := c.Param("user")
			name := c.Param("name")
			projectName := filepath.Join(username, name)

			pInfo, err := ps.GetProjectInfo(projectName)
			if err != nil {
				if errors.Is(err, domain.ErrProjectNotExists) {
					return echo.NewHTTPError(http.StatusBadRequest, "Project does not exists")
				}
				return fmt.Errorf("[ProjectAccessMiddleware] reading project info: %w", err)
			}
			access := false
			if pInfo.Authentication == "public" {
				access = true
			} else {
				user, err := a.GetUser(c)
				if err != nil {
					return fmt.Errorf("[ProjectAccessMiddleware] getting user: %w", err)
				}
				if user.IsAuthenticated {
					if pInfo.Authentication == "authenticated" {
						access = true
					} else {
						access = user.Username == username || user.IsSuperuser
						if !access && pInfo.Authentication == "users" {
							settings, err := ps.GetSettings(projectName)
							if err != nil {
								return fmt.Errorf("[ProjectAccessMiddleware] reading project settings: %w", err)
							}
							access = domain.StringArray(settings.Auth.Users).Has(user.Username)
						}
					}
				}
			}
			c.Set("project", projectName)
			if !access {
				if basicAuthRealm != "" {
					c.Response().Header().Set(echo.HeaderWWWAuthenticate, basicAuthRealm)
				}
				return echo.ErrUnauthorized
			}
			return next(c)
		}
	}
}

type SessionStore interface {
	Get(ctx context.Context, sessionid string) (string, error)
}

func SessionMiddlewareWithConfig(store SessionStore) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			sessionid := getSessionId(c)
			if sessionid != "" {
				sd, err := store.Get(context.Background(), sessionid)
				if err == nil {
					c.Set("session", sd)
				} else if err != redis.Nil {
					c.Logger().Errorf("session error: %v", err)
				}
			}
			return next(c)
		}
	}
}

func getSessionId(c echo.Context) string {
	sessionid := c.Request().Header.Get("Authorization")
	if sessionid == "" {
		cookie, err := c.Request().Cookie("gq_session")
		if err == nil {
			sessionid = cookie.Value
		}
	}
	return sessionid
}
