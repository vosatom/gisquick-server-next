package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gisquick/gisquick-server/internal/infrastructure/project"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

func (s *Server) handleSaveNotification(c echo.Context) error {
	req := c.Request()
	req.Body = http.MaxBytesReader(c.Response(), req.Body, MaxJSONSize)
	defer req.Body.Close()

	var notification project.Notification
	d := json.NewDecoder(req.Body)
	if err := d.Decode(&notification); err != nil {
		s.log.Error("saving notification", zap.Error(err))
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request data")
	}
	if notification.ID == "" {
		notification.ID = strconv.Itoa(int(time.Now().Unix()))
	}
	s.log.Infow("handleSaveNotification", "data", notification)
	if err := s.notifications.SaveNotification(c.Request().Context(), notification); err != nil {
		if errors.Is(err, project.ErrInvalidDuration) {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid expiration")
		}
		return fmt.Errorf("saving notification: %w", err)
	}
	return c.JSON(http.StatusOK, notification)
}

func (s *Server) handleGetNotifications(c echo.Context) error {
	notifications, err := s.notifications.GetNotifications()
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, notifications)
}

func (s *Server) handleDeleteNotification(c echo.Context) error {
	id := c.Param("id")
	if err := s.notifications.DeleteNotification(c.Request().Context(), id); err != nil {
		return err
	}
	return c.NoContent(http.StatusOK)
}
