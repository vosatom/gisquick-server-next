package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"path/filepath"
	"strings"

	"github.com/gisquick/gisquick-server/internal/domain"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

func getProjectName(c echo.Context) string {
	user := c.Param("user")
	name := c.Param("name")
	return filepath.Join(user, name)
}

func (s *Server) handleGetProject() func(c echo.Context) error {
	type Notification struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Message string `json:"msg"`
	}
	return func(c echo.Context) error {
		projectName := getProjectName(c)
		info, err := s.projects.GetProjectInfo(projectName)
		if err != nil {
			if errors.Is(err, domain.ErrProjectNotExists) {
				s.log.Errorw(err.Error(), "handler", "handleGetProject")
				s.log.Errorw("handleGetProject", zap.Error(err))
				return echo.ErrNotFound
			}
			return err
		}
		if info.State != "published" {
			return echo.NewHTTPError(http.StatusBadRequest, "Project not valid")
		}

		// if !s.checkProjectAccess(info, c) {
		// 	return echo.ErrForbidden
		// }

		user, err := s.auth.GetUser(c)
		data, err := s.projects.GetMapConfig(projectName, user)
		if err != nil {
			return err
		}
		if s.Config.ProjectCustomization {
			cfg, err := s.projects.GetProjectCustomizations(projectName)
			if err != nil {
				s.log.Errorw("reading project customization config", zap.Error(err))
			} else if cfg != nil {
				data["app"] = cfg
			}
		}
		notifications, err := s.notifications.GetMapProjectNotifications(projectName, user)
		if err != nil {
			s.log.Errorw("getting app notifications", zap.Error(err))
		} else if len(notifications) > 0 {
			messages := make([]Notification, len(notifications))
			for i, n := range notifications {
				messages[i] = Notification{
					ID:      n.ID,
					Title:   n.Title,
					Message: n.Message,
				}
				data["notifications"] = messages
			}
		}
		data["status"] = 200
		// delete(data, "layers")
		// return c.JSON(http.StatusOK, data["layers"])
		return c.JSON(http.StatusOK, data)
	}
}

func (s *Server) handleGetLayerCapabilities() func(c echo.Context) error {
	director := func(req *http.Request) {}
	reverseProxy := &httputil.ReverseProxy{Director: director}

	return func(c echo.Context) error {
		projectName := c.Get("project").(string)
		// projectName := getProjectName(c)
		type LayersMetadata struct {
			Layers map[string]domain.LayerMeta `json:"layers"`
		}
		var meta LayersMetadata
		err := s.projects.GetQgisMetadata(projectName, &meta)
		if err != nil {
			if errors.Is(err, domain.ErrProjectNotExists) {
				return echo.ErrNotFound
			}
			return err
		}
		layername := c.QueryParam("LAYER")
		if layername == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "Missing LAYER parameter")
		}
		var lmeta domain.LayerMeta

		for _, layer := range meta.Layers {
			if layer.Name == layername {
				lmeta = layer
				sourceURL := lmeta.SourceParams.String("url")
				req, err := http.NewRequest(http.MethodGet, sourceURL, nil)
				if err != nil {
					return fmt.Errorf("handleGetLayerCapabilities error: %w", err)
				}
				for k, vv := range c.Request().Header {
					if strings.HasPrefix(k, "Accept") {
						for _, v := range vv {
							req.Header.Add(k, v)
						}
					}
				}
				reverseProxy.ServeHTTP(c.Response(), req)
				return nil
			}
		}
		return echo.NewHTTPError(http.StatusBadRequest, "Unknown LAYER name")
	}
}
