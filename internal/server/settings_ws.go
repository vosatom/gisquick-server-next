package server

import (
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

func (s *Server) handleWebAppWS(c echo.Context) error {
	user, err := s.auth.GetUser(c)
	if err != nil {
		return err
	}
	err = s.sws.WebAppHandler(user.Username, c.Response(), c.Request())
	if err != nil {
		s.log.Errorw("websocket handler", "channel", "webapp", "user", user.Username, zap.Error(err))
	}
	return nil
}

func (s *Server) handlePluginWS(c echo.Context) error {
	user, err := s.auth.GetUser(c)
	if err != nil {
		return err
	}
	err = s.sws.PluginHandler(user.Username, c.Response(), c.Request())
	if err != nil {
		s.log.Errorw("websocket handler", "channel", "plugin", "user", user.Username, zap.Error(err))
	}
	return nil
}
