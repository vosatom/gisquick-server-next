package server

import (
	"github.com/labstack/echo/v4"
)

func (s *Server) AddRoutes(e *echo.Echo) {

	LoginRequired := LoginRequiredMiddlewareWithConfig(s.auth)
	SuperuserRequired := SuperuserAccessMiddleware(s.auth)
	ProjectAdminAccess := ProjectAdminAccessMiddleware(s.auth)
	ProjectAccess := ProjectAccessMiddleware(s.auth, s.projects)

	e.POST("/api/auth/login", s.handleLogin())
	e.POST("/api/auth/logout", s.handleLogout)
	e.GET("/api/auth/logout", s.handleLogout) // Just for compatibility!!!

	e.GET("/api/users", s.handleGetUsers, LoginRequired)

	if s.Config.SignupAPI {
		e.POST("/api/accounts/signup", s.handleSignUp())
		e.POST("/api/accounts/activate", s.handleActivateAccount())
		e.GET("/api/accounts/check", s.handleCheckAvailability())
	}
	e.POST("/api/accounts/password_reset", s.handlePasswordReset())
	e.POST("/api/accounts/new_password", s.handleNewPassword())
	e.POST("/api/accounts/change_password", s.handleChangePassword(), LoginRequired)
	e.GET("/api/auth/user", s.handleGetUser)

	// /api/
	e.GET("/api/app", s.handleAppInit)

	// e.POST("/api/map/project/*", s.handleUpdateProject)

	e.POST("/api/project/:user/:name", s.handleCreateProject(), LoginRequired)
	e.DELETE("/api/project/:user/:name", s.handleDeleteProject, ProjectAdminAccess)
	e.GET("/api/projects", s.handleGetProjects, LoginRequired)
	e.GET("/api/projects/:user", s.handleGetUserProjects, SuperuserRequired)
	e.POST("/api/project/upload/:user/:name", s.handleUpload(), ProjectAdminAccess)

	e.GET("/api/project/map/:user/:name", s.handleGetMap(), ProjectAdminAccess)
	e.POST("/api/project/map/:user/:name", s.handleGetMap(), ProjectAdminAccess)
	e.GET("/api/project/files/:user/:name", s.handleGetProjectFiles(), ProjectAdminAccess)
	e.DELETE("/api/project/files/:user/:name", s.handleDeleteProjectFiles(), ProjectAdminAccess)
	e.GET("/api/project/info/:user/:name", s.handleGetProjectInfo, ProjectAdminAccess)
	e.GET("/api/project/full-info/:user/:name", s.handleGetProjectFullInfo(), ProjectAdminAccess)

	e.GET("/api/project/media/:user/:name/*", s.handleGetMediaFile, ProjectAccess)
	e.POST("/api/project/media/:user/:name/*", s.handleUploadMediaFile, ProjectAccess)
	e.POST("/api/project/script/:user/:name", s.handleScriptUpload(), ProjectAdminAccess)
	e.DELETE("/api/project/script/:user/:name", s.handleDeleteScript(), ProjectAdminAccess)

	e.GET("/api/project/file/:user/:name/*", s.handleProjectFile, ProjectAdminAccess)
	e.GET("/api/project/download/:user/:name", s.handleDownloadProjectFiles, ProjectAdminAccess)
	e.GET("/api/project/download/:user/:name/*", s.handleDownloadProjectFiles, ProjectAdminAccess)
	e.GET("/api/project/inline/:user/:name/*", s.handleInlineProjectFile, ProjectAdminAccess)

	e.POST("/api/project/meta/:user/:name", s.handleUpdateProjectMeta(), ProjectAdminAccess)

	e.POST("/api/project/settings/:user/:name", s.handleSaveProjectSettings, ProjectAdminAccess)
	e.POST("/api/project/thumbnail/:user/:name", s.handleUploadThumbnail, ProjectAdminAccess)
	e.GET("/api/project/thumbnail/:user/:name", s.handleGetThumbnail)
	e.GET("/api/map/project/:user/:name", s.handleGetProject, ProjectAccess)
	owsHandler := s.handleMapOws()
	e.GET("/api/map/ows/:user/:name", owsHandler, ProjectAccess)
	e.POST("/api/map/ows/:user/:name", owsHandler, ProjectAccess)

	e.POST("/api/project/reload/:user/:name", s.handleProjectReload, ProjectAdminAccess)

	e.GET("/ws/app", s.handleWebAppWS, LoginRequired)
	e.GET("/ws/plugin", s.handlePluginWS, LoginRequired)

	if s.Config.PluginsURL != "" {
		// e.GET("/plugins/", s.pythonPluginRepoHandler("/qgis-plugins-repo"))
		e.GET("/plugins/platform/:platform", s.platformPluginRepoHandler("/qgis-plugins-repo"))
		e.GET("/plugins/download/*", s.handleDownloadPlugin("/qgis-plugins-repo"))
	}

	// owsHandler := s.owsHandler()
	// e.GET("/api/map/ows", owsHandler)
	// e.POST("/api/map/ows", owsHandler)

	// // Mapcache
	// e.GET("/api/map/tile/:project_hash/tile/:layers_hash/:z/:x/:y", s.handleMapcacheTile())
	// e.GET("/api/map/tile/:project_hash/legend/:layers_hash/:filename", s.handleMapcacheLegend())
}
