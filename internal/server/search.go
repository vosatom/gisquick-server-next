package server

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"

	"github.com/labstack/echo/v4"
)

func (s *Server) handleSearch() func(c echo.Context) error {
	director := func(req *http.Request) {
		if _, ok := req.Header["User-Agent"]; !ok {
			// explicitly disable User-Agent so it's not set to default value
			req.Header.Set("User-Agent", "")
		}
		req.Header.Del("Cookie")
	}
	reverseProxy := &httputil.ReverseProxy{Director: director}

	return func(c echo.Context) error {
		projectName := getProjectName(c)
		settings, err := s.projects.GetSettings(projectName)
		if err != nil {
			return fmt.Errorf("getting project settings: %w", err)
		}
		if settings.Geocoding.URL == "" {
			return echo.ErrForbidden
		}
		searchUrl, err := url.Parse(settings.Geocoding.URL)
		searchUrl.Path = path.Join(searchUrl.Path, c.Param("*"))
		query := c.Request().URL.Query()
		for _, p := range settings.Geocoding.QueryParams {
			if p.Path == "" || p.Path == c.Param("*") {
				query.Set(p.Name, p.Value)
			}
		}
		searchUrl.RawQuery = query.Encode()
		req, err := http.NewRequest(http.MethodGet, searchUrl.String(), nil)
		if err != nil {
			return fmt.Errorf("search error: %w", err)
		}
		req.Header = c.Request().Header.Clone()
		reverseProxy.ServeHTTP(c.Response(), req)
		return nil
	}
}
