package server

import (
	"context"
	"fmt"
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
				return echo.ErrUnauthorized
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

func ProjectAccessMiddleware(a *auth.AuthService, ps application.ProjectService) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			username := c.Param("user")
			name := c.Param("name")
			projectName := filepath.Join(username, name)

			settings, err := ps.GetSettings(projectName)
			if err != nil {
				return fmt.Errorf("[ProjectAccessMiddleware] reading project settings: %w", err)
			}
			access := false

			if settings.Auth.Type == "public" {
				access = true
			} else {
				user, err := a.GetUser(c)
				if err != nil {
					return fmt.Errorf("[ProjectAccessMiddleware] get user: %w", err)
				}
				switch t := settings.Auth.Type; t {
				case "authenticated":
					access = user.IsAuthenticated
				case "private":
					access = user.Username == username
				case "users":
					access = domain.Flags(settings.Auth.Users).Has(user.Username)
				}
			}

			c.Set("project", projectName)
			if !access {
				return echo.ErrForbidden
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
		cookie, err := c.Request().Cookie("sessionid")
		if err == nil {
			sessionid = cookie.Value
		}
	}
	return sessionid
}
