package server

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"io/fs"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	"github.com/gisquick/gisquick-server/internal/application"
	"github.com/gisquick/gisquick-server/internal/domain"
	"github.com/labstack/echo/v4"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/zap"
	_ "golang.org/x/image/webp"
	"golang.org/x/sync/singleflight"
)

const MB int64 = 1024 * 1024

var MaxJSONSize int64 = 1 * MB
var MaxScriptSize int64 = 5 * MB

func (s *Server) handleGetProjectFiles() func(echo.Context) error {
	type ProjectFiles struct {
		Files          []domain.ProjectFile `json:"files"`
		TemporaryFiles []domain.ProjectFile `json:"temporary"`
	}
	return func(c echo.Context) error {
		projectName := c.Get("project").(string)
		files, tmpFiles, err := s.projects.ListProjectFiles(projectName, true)
		if err != nil {
			if errors.Is(err, domain.ErrProjectNotExists) {
				return echo.NewHTTPError(http.StatusBadRequest, "Project does not exists")
			}
			return fmt.Errorf("handleGetProjectFiles: %w", err)
		}
		return c.JSON(http.StatusOK, ProjectFiles{files, tmpFiles})
	}
}

type UserDashboard struct {
	Projects []string `json:"projects"`
}

func (s *Server) handleGetProjects() func(echo.Context) error {
	type QueryParams struct {
		Projects string `query:"projects"`
		Filter   string `query:"filter"`
	}
	return func(c echo.Context) error {
		var user domain.User
		var projectsNames []string
		queryParams := new(QueryParams)
		if err := (&echo.DefaultBinder{}).BindQueryParams(c, queryParams); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid query parameters")
		}
		if queryParams.Projects != "" {
			projectsNames = strings.Split(queryParams.Projects, ",")
		} else {
			var err error
			user, err = s.auth.GetUser(c)
			if err != nil {
				return err
			}
			if !user.IsAuthenticated {
				return echo.ErrUnauthorized
			}
			dashboardPath := filepath.Join(s.Config.ProjectsRoot, user.Username, "dashboard.json")
			content, err := os.ReadFile(dashboardPath)
			if err == nil {
				var data UserDashboard
				if err = json.Unmarshal(content, &data); err != nil {
					s.log.Warnw("reading user dashboard file", "user", user.Username, zap.Error(err))
				} else {
					projectsNames = data.Projects
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				s.log.Warnw("reading user dashboard file", "user", user.Username, zap.Error(err))
			}
		}
		if len(projectsNames) > 0 {
			data := make([]domain.ProjectInfo, 0, len(projectsNames))
			for _, name := range projectsNames {
				p, err := s.projects.GetProjectInfo(strings.TrimSpace(name))
				if err == nil {
					data = append(data, p)
				}
			}
			return c.JSON(http.StatusOK, data)
		}
		if strings.EqualFold(queryParams.Filter, "accessible") {
			data, err := s.projects.AccessibleProjects(user.Username, true)
			if err != nil {
				return fmt.Errorf("getting list of user accessible projects: %w", err)
			}
			return c.JSON(http.StatusOK, data)
		}
		data, err := s.projects.GetUserProjects(user.Username)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, data)
	}
}

func (s *Server) handleGetUserProjects(c echo.Context) error {
	username := c.Param("user")
	data, err := s.projects.GetUserProjects(username)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, data)
}

func (s *Server) handleDeleteProject(c echo.Context) error {
	projectName := c.Get("project").(string)
	if err := s.projects.Delete(projectName); err != nil {
		if errors.Is(err, domain.ErrProjectNotExists) {
			return echo.NewHTTPError(http.StatusBadRequest, "Project does not exists")
		}
		return err
	}
	return c.NoContent(http.StatusOK)
}

// ProgressReader export
type ProgressReader struct {
	Reader   io.ReadCloser
	Callback func(int, int)
	Step     int
	Progress int
	lastVal  int
}

func (r *ProgressReader) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)
	r.Progress += n
	delta := r.Progress - r.lastVal
	if delta >= r.Step || err == io.EOF {
		r.Callback(r.Progress, delta)
		r.lastVal = r.Progress
	}
	return
}

func (r *ProgressReader) Close() error {
	return r.Reader.Close()
}

func percProgress(size, total int) int {
	if total == 0 {
		return 100
	}
	return int(100 * (float64(size) / float64(total)))
}

func (s *Server) handleUpload() func(echo.Context) error {
	type fileUploadProgress struct {
		Files         map[string]int `json:"files"`
		TotalProgress int            `json:"total"`
	}
	type uploadInfo struct {
		Files []domain.ProjectFile `json:"files"`
	}

	return func(c echo.Context) error {
		req := c.Request()
		ctype, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
		if err != nil || ctype != "multipart/form-data" {
			return echo.NewHTTPError(http.StatusBadRequest, http.ErrNotMultipart.Error())
		}
		boundary, ok := params["boundary"]
		if !ok {
			return echo.NewHTTPError(http.StatusBadRequest, http.ErrMissingBoundary.Error())
		}
		user, err := s.auth.GetUser(c)
		if err != nil {
			return err
		}
		if s.Config.MaxProjectSize > 0 {
			req.Body = http.MaxBytesReader(c.Response(), req.Body, s.Config.MaxProjectSize)
		}
		reader := multipart.NewReader(req.Body, boundary)
		projectName := c.Get("project").(string)

		// first part should contain upload info
		var info uploadInfo
		part, err := reader.NextPart()
		if err != nil {
			s.log.Errorw("uploading files", "project", projectName, zap.Error(err))
			return err
		}
		err = json.NewDecoder(part).Decode(&info)
		if err != nil {
			s.log.Errorw("decoding upload metadata", "project", projectName, zap.Error(err))
			return err
		}

		totalSize := int64(0)
		uploadSizeMap := make(map[string]int, len(info.Files))
		for _, f := range info.Files {
			uploadSizeMap[f.Path] = int(f.Size)
			totalSize += f.Size
		}
		// Ver. 1
		uploadedSize := 0
		uploadProgress := make(map[string]int)
		lastNotification := time.Now()
		nextFile := func() (string, io.ReadCloser, error) { // or ReadCloser?
			part, err := reader.NextPart()
			if err != nil {
				return "", nil, err
			}
			var partReader io.ReadCloser = part
			if strings.HasSuffix(part.FileName(), ".gz") && !strings.HasSuffix(part.FormName(), ".gz") {
				partReader, _ = gzip.NewReader(part)
			}
			pr := &ProgressReader{Reader: partReader, Step: 32 * 1024, Callback: func(uploaded, last int) {
				uploadProgress[part.FormName()] = percProgress(uploaded, uploadSizeMap[part.FormName()])
				uploadedSize += last
				now := time.Now()
				if now.Sub(lastNotification).Seconds() > 0.5 {

					totalProgress := percProgress(uploadedSize, int(totalSize))
					s.log.Infow("upload progress", "file", part.FormName(), "uploaded", uploaded, "delta", last, "totalUploaded", uploadedSize, "totalSize", totalSize, "totalProgress", totalProgress)
					s.sws.AppChannel().Send(user.Username, "UploadProgress", fileUploadProgress{uploadProgress, totalProgress})

					lastNotification = now
					uploadProgress = make(map[string]int)
				}
			}}
			return part.FormName(), pr, nil
		}
		changes := domain.FilesChanges{Updates: info.Files}
		if _, err := s.projects.UpdateFiles(projectName, changes, nextFile); err != nil {
			// better check in future release https://github.com/golang/go/issues/30715
			if errors.Is(err, application.ErrAccountStorageLimit) {
				return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "Reached account storage limit")
			}
			if errors.Is(err, application.ErrProjectSizeLimit) || err.Error() == "http: request body too large" {
				// s.log.Warn("uploading files: max limit reached")
				return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "Reached project size limit.")
			}
			return err
		}
		// finish reading from stream
		if _, err := reader.NextPart(); err != io.EOF {
			s.log.Warnf("expected end of stream", "project", projectName)
		}
		s.sws.AppChannel().Send(user.Username, "UploadProgress", fileUploadProgress{uploadProgress, 100})

		// Ver. 2
		/*
			uploadProgress := make(map[string]int)
			lastNotification := time.Now()
			for {
				part, err := reader.NextPart()
				if err == io.EOF {
					s.sws.AppChannel().Send(user.Username, "UploadProgress", uploadProgress)
					// if appWs :=  appsWs.Get(username); appWs != nil {
					// 	s.sendJSONMessage(appWs, "UploadProgress", uploadProgress)
					// }
					break
				}
				var partReader io.ReadCloser = part
				if strings.HasSuffix(part.FileName(), ".gz") && !strings.HasSuffix(part.FormName(), ".gz") {
					partReader, _ = gzip.NewReader(part)
				}
				pr := &ProgressReader{Reader: partReader, Step: 32 * 1024, Callback: func(p int) {
					uploadProgress[part.FormName()] = p
					now := time.Now()
					if now.Sub(lastNotification).Seconds() > 0.5 {
						// if appWs := s.appsWs.Get(username); appWs != nil {
						// 	s.sendJSONMessage(appWs, "UploadProgress", uploadProgress)
						// }
						s.sws.AppChannel().Send(user.Username, "UploadProgress", uploadProgress)
						lastNotification = now
						uploadProgress = make(map[string]int)
					}
				}}
				s.projects.SaveFile(projectName, part.FormName(), pr)
				partReader.Close()
				if err != nil {
					return err
				}
			}
		*/
		return c.NoContent(http.StatusOK)
	}
}

func (s *Server) handleDeleteProjectFiles() func(echo.Context) error {
	type FilesInfo struct {
		Files []string `json:"files"`
	}

	return func(c echo.Context) error {
		projectName := c.Get("project").(string)
		var data FilesInfo
		if err := (&echo.DefaultBinder{}).BindBody(c, &data); err != nil {
			return err
		}
		if len(data.Files) < 1 {
			return echo.NewHTTPError(http.StatusBadRequest, "No files specified")
		}
		changes := domain.FilesChanges{Removes: data.Files}
		// nextFile := func() (string, io.ReadCloser, error) {
		// 	return "", nil, io.EOF
		// }
		files, err := s.projects.UpdateFiles(projectName, changes, nil)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, files)
	}
}

func (s *Server) handleProjectOws() func(echo.Context) error {
	type RequestParams struct {
		Map string `query:"map"`
	}
	director := func(req *http.Request) {
		target, _ := url.Parse(s.Config.MapserverURL)
		// query := req.URL.Query()
		// project := req.URL.Query().Get("MAP")
		// req.URL.RawQuery = query.Encode()
		s.log.Infow("Map proxy", "query", req.URL.RawQuery)
		req.URL.Path = target.Path
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host

		if _, ok := req.Header["User-Agent"]; !ok {
			// explicitly disable User-Agent so it's not set to default value
			req.Header.Set("User-Agent", "")
		}
	}
	reverseProxy := &httputil.ReverseProxy{Director: director}
	reverseProxy.ErrorHandler = func(rw http.ResponseWriter, r *http.Request, e error) {
		s.log.Errorw("mapserver proxy error", zap.Error(e))
	}
	// reverseProxy.ErrorLog.SetOutput(os.Stdout)
	return func(c echo.Context) error {
		// params := new(RequestParams)
		// if err := (&echo.DefaultBinder{}).BindQueryParams(c, params); err != nil {
		// 	return echo.NewHTTPError(http.StatusBadRequest, "Invalid query parameters")
		// }

		// project := params.Map
		// user, err := s.auth.GetUser(c)
		// if err != nil {
		// 	return err
		// }
		// c.Request().URL.Query()
		projectName := c.Get("project").(string)

		p, err := s.projects.GetProjectInfo(projectName)
		if err != nil {
			if errors.Is(err, domain.ErrProjectNotExists) {
				return echo.NewHTTPError(http.StatusBadRequest, "Project does not exists")
			}
			return err
		}
		// TODO: hardcoded /publish/ directory!
		owsProject := filepath.Join("/publish/", projectName, p.QgisFile)
		s.log.Infow("GetMap", "ows_project", owsProject)
		query := c.Request().URL.Query()
		query.Set("MAP", owsProject)
		c.Request().URL.RawQuery = query.Encode()

		reverseProxy.ServeHTTP(c.Response(), c.Request())
		return nil
	}
}

func (s *Server) handleCreateProject() func(echo.Context) error {
	return func(c echo.Context) error {
		// TODO: check project folder/index file doesn't exists
		req := c.Request()
		req.Body = http.MaxBytesReader(c.Response(), req.Body, MaxJSONSize)
		defer req.Body.Close()

		var data json.RawMessage
		d := json.NewDecoder(req.Body)
		if err := d.Decode(&data); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid request data")
		}
		username := c.Param("user")
		name := c.Param("name")
		projName := filepath.Join(username, name)
		info, err := s.projects.Create(projName, data)
		if err != nil {
			if errors.Is(err, domain.ErrProjectAlreadyExists) {
				return echo.NewHTTPError(http.StatusConflict, "Project already exists")
			}
			if errors.Is(err, application.ErrAccountProjectsLimit) {
				return echo.NewHTTPError(http.StatusConflict, "Projects limit was reached")
			}
			return err
		}
		s.log.Infow("Created project", "info", info)
		return c.JSON(http.StatusOK, info)
	}
}

func (s *Server) handleGetProjectFullInfo() func(echo.Context) error {
	type FullInfo struct {
		Auth       string          `json:"authentication"`
		Name       string          `json:"name"`
		Title      string          `json:"title"`
		Created    time.Time       `json:"created"`
		LastUpdate time.Time       `json:"last_update"`
		State      string          `json:"state"`
		Size       int64           `json:"size"`
		Thumbnail  bool            `json:"thumbnail"`
		Meta       domain.QgisMeta `json:"meta"`
		// Meta     json.RawMessage         `json:"meta"`
		Settings *domain.ProjectSettings `json:"settings"`
		Scripts  domain.Scripts          `json:"scripts"`
	}
	return func(c echo.Context) error {
		projectName := c.Get("project").(string)
		info, err := s.projects.GetProjectInfo(projectName)
		if err != nil {
			if errors.Is(err, domain.ErrProjectNotExists) {
				return echo.NewHTTPError(http.StatusBadRequest, "Project does not exists")
			}
			return fmt.Errorf("[handleGetProjectInfo] loading project info: %w", err)
		}
		// var meta json.RawMessage
		var meta domain.QgisMeta
		if err := s.projects.GetQgisMetadata(projectName, &meta); err != nil {
			return fmt.Errorf("[handleGetProjectInfo] loading qgis meta: %w", err)
		}
		// meta, err := s.projects.GetQgisMetadata(projectName)
		// if err != nil {
		// 	return fmt.Errorf("[handleGetProjectInfo] loading qgis meta: %w", err)
		// }
		data := FullInfo{
			Auth:       info.Authentication,
			Name:       projectName,
			Title:      info.Title,
			Created:    info.Created,
			LastUpdate: info.LastUpdate,
			State:      info.State,
			Size:       info.Size,
			Thumbnail:  info.Thumbnail,
			Meta:       meta,
		}
		if info.State != "empty" {
			settings, err := s.projects.GetSettings(projectName)
			if err == nil {
				data.Settings = &settings
			} else {
				s.log.Warnw("[handleGetProjectInfo] settings not found", "project", projectName, zap.Error(err))
			}
		}
		scripts, err := s.projects.GetScripts(projectName)
		if err != nil {
			s.log.Errorw("[handleGetProjectInfo] loading scripts", "project", projectName)
		} else {
			data.Scripts = scripts
		}
		return c.JSON(http.StatusOK, data)
	}
}

func (s *Server) handleGetProjectInfo(c echo.Context) error {
	projectName := c.Get("project").(string)
	info, err := s.projects.GetProjectInfo(projectName)
	if err != nil {
		if errors.Is(err, domain.ErrProjectNotExists) {
			return echo.NewHTTPError(http.StatusBadRequest, "Project does not exists")
		}
		return fmt.Errorf("handleGetProjectInfo: %w", err)
	}
	return c.JSON(http.StatusOK, info)
}

func (s *Server) handleUpdateProjectMeta() func(echo.Context) error {
	return func(c echo.Context) error {
		projectName := c.Get("project").(string)
		req := c.Request()
		req.Body = http.MaxBytesReader(c.Response(), req.Body, MaxJSONSize)
		defer req.Body.Close()

		var data json.RawMessage
		d := json.NewDecoder(req.Body)
		if err := d.Decode(&data); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid request data")
		}

		err := s.projects.UpdateMeta(projectName, data)
		if err != nil {
			if errors.Is(err, domain.ErrProjectNotExists) {
				return echo.NewHTTPError(http.StatusConflict, "Project does not exists")
			}
			return err
		}
		s.InvalidateMapCache(projectName)
		return c.NoContent(http.StatusOK)
	}
}

/* Settings Handlers */

func (s *Server) handleSaveProjectSettings(c echo.Context) error {
	projectName := c.Get("project").(string)
	req := c.Request()
	req.Body = http.MaxBytesReader(c.Response(), req.Body, MaxJSONSize)
	defer req.Body.Close()

	var data json.RawMessage
	d := json.NewDecoder(req.Body)
	if err := d.Decode(&data); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request data")
	}
	return s.projects.UpdateSettings(projectName, data)
}

func (s *Server) handleUploadThumbnail(c echo.Context) error {
	if err := c.Request().ParseForm(); err != nil {
		return err
	}
	f, h, err := c.Request().FormFile("image")
	if err != nil {
		return err
	}
	defer f.Close()
	projectName := c.Get("project").(string)
	s.log.Infow("thumbnail", "project", projectName, "image", h.Filename)
	if err := s.projects.SaveThumbnail(projectName, f); err != nil {
		return err
	}
	return c.NoContent(http.StatusOK)
}

func (s *Server) handleGetThumbnail(c echo.Context) error {
	username := c.Param("user")
	name := c.Param("name")
	projectName := filepath.Join(username, name)
	return c.File(s.projects.GetThumbnailPath(projectName))
}

func (s *Server) handleScriptUpload() func(echo.Context) error {
	type Data struct {
		domain.ScriptModule
		Module string `json:"module"`
	}
	return func(c echo.Context) error {
		projectName := c.Get("project").(string)

		req := c.Request()
		req.Body = http.MaxBytesReader(c.Response(), req.Body, MaxJSONSize)
		if err := req.ParseMultipartForm(2 * MB); err != nil {
			return err
		}
		var info Data
		jsonInfo := c.FormValue("info")
		if err := json.Unmarshal([]byte(jsonInfo), &info); err != nil {
			s.log.Errorw("[handleScriptUpload] parsing metadata", zap.Error(err))
			return echo.ErrBadRequest
		}
		if info.Module == "" || len(info.Components) < 1 { // TODO: better name validation with regex
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid script module data")
		}
		var filesMeta []domain.ProjectFile = []domain.ProjectFile{}
		var files []*multipart.FileHeader = []*multipart.FileHeader{}

		form, err := c.MultipartForm()
		if err != nil {
			return err
		}
		for n, f := range form.File {
			s.log.Infow("[handleScriptUpload]", "file", n, "len", len(f))
			for _, fh := range f {
				path := filepath.Join("web", "components", fh.Filename)
				filesMeta = append(filesMeta, domain.ProjectFile{Path: path, Size: fh.Size})
				files = append(files, fh)
			}
		}
		changes := domain.FilesChanges{Updates: filesMeta}
		s.log.Infow("[handleScriptUpload]", "info", info, "changes", changes)

		findex := 0
		nextFile := func() (string, io.ReadCloser, error) { // or ReadCloser?
			if findex >= len(files) || findex >= len(filesMeta) {
				return "", nil, io.EOF
			}
			f := files[findex]
			path := filesMeta[findex].Path
			file, err := f.Open()
			if err != nil {
				return "", nil, err
			}
			findex += 1
			return path, file, nil
		}
		if _, err := s.projects.UpdateFiles(projectName, changes, nextFile); err != nil {
			if errors.Is(err, application.ErrProjectSizeLimit) {
				return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "Reached project size limit.")
			}
			return fmt.Errorf("[handleScriptUpload] saving script file: %w", err)
		}
		scripts, err := s.projects.GetScripts(projectName)
		if err != nil {
			return fmt.Errorf("[handleScriptUpload] getting scripts metadata: %w", err)
		}
		if scripts == nil {
			scripts = make(domain.Scripts, 1)
		}
		info.Path = filepath.Join("web", "components", info.Path)
		scripts[info.Module] = info.ScriptModule
		// s.log.Infow("[handleScriptUpload]", "scripts", scripts)

		if err := s.projects.UpdateScripts(projectName, scripts); err != nil {
			return fmt.Errorf("[handleScriptUpload] updating scripts metadata: %w", err)
		}
		return c.JSON(http.StatusOK, scripts)
	}
}

func (s *Server) handleDeleteScript() func(echo.Context) error {
	type Params struct {
		Modules []string `json:"modules"`
	}
	return func(c echo.Context) error {
		projectName := c.Get("project").(string)
		// var params Params
		var modules []string
		if err := (&echo.DefaultBinder{}).BindBody(c, &modules); err != nil {
			return err
		}
		scripts, err := s.projects.RemoveScripts(projectName, modules...)
		if err != nil {
			return fmt.Errorf("[handleDeleteScript] removing scripts: %w", err)
		}
		return c.JSON(http.StatusOK, scripts)
	}
}

func (s *Server) handleProjectFile(c echo.Context) error {
	projectName := c.Get("project").(string)
	filePath := c.Param("*")
	return c.File(filepath.Join(s.Config.ProjectsRoot, projectName, filePath))
}

func CopyFile(dest io.Writer, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(dest, file)
	return err
}

func (s *Server) handleDownloadProjectFiles(c echo.Context) error {
	projectName := c.Get("project").(string)
	filePath := c.Param("*")
	fullPath := filepath.Join(s.Config.ProjectsRoot, projectName, filePath)

	name := filepath.Base(fullPath)

	info, err := os.Lstat(fullPath)
	if err != nil {
		return fmt.Errorf("getting file info: %w", err)
	}
	if info.IsDir() {
		c.Response().Header().Set("Content-Type", "application/octet-stream")
		c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.zip", name))
		writer := zip.NewWriter(c.Response())
		defer writer.Close()
		rootPath := filepath.Dir(fullPath)
		err := filepath.WalkDir(fullPath, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() {
				// relPath2 := path[len(rootPath)+1:]
				relPath, _ := filepath.Rel(rootPath, path)
				part, err := writer.Create(relPath)
				if err != nil {
					return err
				}
				return CopyFile(part, path)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("downloading project directory: %w", err)
		}
		return nil
	}
	return c.Attachment(fullPath, name)
}

func (s *Server) handleInlineProjectFile(c echo.Context) error {
	projectName := c.Get("project").(string)
	filePath := c.Param("*")
	name := filepath.Base(filePath)
	return c.Inline(filepath.Join(s.Config.ProjectsRoot, projectName, filePath), name)
}

func (s *Server) handleProjectReload(c echo.Context) error {
	client := &http.Client{}
	projectName := c.Get("project").(string)
	p, err := s.projects.GetProjectInfo(projectName)
	if err != nil {
		if errors.Is(err, domain.ErrProjectNotExists) {
			return echo.NewHTTPError(http.StatusBadRequest, "Project does not exists")
		}
		return err
	}
	// TODO: hardcoded /publish/ directory!
	owsProject := filepath.Join("/publish/", projectName, p.QgisFile)
	params := url.Values{"MAP": {owsProject}}

	req, err := http.NewRequest(http.MethodPost, s.Config.MapserverURL, nil)
	if err != nil {
		return fmt.Errorf("[handleProjectReload] building request: %w", err)
	}
	req.URL.Path = filepath.Join(req.URL.Path, "/reload")
	req.URL.RawQuery = params.Encode()
	// s.log.Infow("[handleProjectReload]", "project", projectName, "url", req.URL.String())

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("mapserver request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		msg, _ := ioutil.ReadAll(resp.Body)
		s.log.Errorw("[handleProjectReload]", "project", projectName, "status", resp.StatusCode, "msg", string(msg))
		return fmt.Errorf("reloading project on qgis server: %s", string(msg))
	}
	s.InvalidateMapCache(projectName)
	return c.NoContent(http.StatusOK)
}

/*
func (s *Server) handleMediaFileUpload(c echo.Context) error {
	projectName := c.Get("project").(string)
	directory := c.Param("*")
	// directory := c.QueryParam("directory")

	if err := c.Request().ParseMultipartForm(2 * MB); err != nil {
		return err
	}
	var filesMeta []domain.ProjectFile = []domain.ProjectFile{}
	var files []*multipart.FileHeader = []*multipart.FileHeader{}

	form, err := c.MultipartForm()
	if err != nil {
		return fmt.Errorf("parsing MultipartForm data: %w", err)
	}
	for n, f := range form.File {
		s.log.Infow("[handleMediaFileUpload]", "file", n, "len", len(f))
		for _, fh := range f {
			path := filepath.Join(directory, fh.Filename)
			filesMeta = append(filesMeta, domain.ProjectFile{Path: path, Size: fh.Size})
			files = append(files, fh)
		}
	}
	changes := domain.FilesChanges{Updates: filesMeta}
	s.log.Infow("[handleMediaFileUpload]", "directory", directory, "changes", changes)
	if true {
		return nil
	}

	findex := 0
	nextFile := func() (string, io.ReadCloser, error) { // or ReadCloser?
		if findex >= len(files) || findex >= len(filesMeta) {
			return "", nil, io.EOF
		}
		f := files[findex]
		path := filesMeta[findex].Path
		file, err := f.Open()
		if err != nil {
			return "", nil, err
		}
		findex += 1
		return path, file, nil
	}
	if _, err := s.projects.UpdateFiles(projectName, changes, nextFile); err != nil {
		return fmt.Errorf("[handleMediaFileUpload] saving script file: %w", err)
	}
	return nil
}
*/

func (s *Server) mediaFileHandler(cacheDir string) func(echo.Context) error {
	var lock singleflight.Group
	return func(c echo.Context) error {
		projectName := c.Get("project").(string)
		filePath := c.Param("*")
		folder := filepath.Dir(filePath)
		if folder != "web" && !strings.HasPrefix(folder, "web/") {
			return echo.ErrNotFound
		}

		absPath := filepath.Join(s.Config.ProjectsRoot, projectName, filePath)
		if cacheDir != "" && strings.EqualFold(c.Request().URL.Query().Get("thumbnail"), "true") {
			key := filepath.Join(projectName, filePath)
			val, err, _ := lock.Do(key, func() (interface{}, error) {
				srcFinfo, err := os.Stat(absPath)
				if err != nil {
					return "", err
				}
				thumbAbsPath := filepath.Join(cacheDir, key)
				finfo, err := os.Stat(thumbAbsPath)
				if err == nil {
					if finfo.ModTime().Unix() > srcFinfo.ModTime().Unix() {
						// valid thumbnail image
						return thumbAbsPath, nil
					}
				}

				err = os.MkdirAll(filepath.Dir(thumbAbsPath), 0777)
				if err != nil {
					return "", err
				}

				srcImage, err := imaging.Open(absPath, imaging.AutoOrientation(true))
				if err != nil {
					return "", fmt.Errorf("reading media image file: %w", err)
				}

				dstImageFit := imaging.Fit(srcImage, 500, 500, imaging.Lanczos)
				format, err := imaging.FormatFromFilename(absPath)
				if err != nil {
					format = imaging.JPEG
				}

				f, err := os.Create(thumbAbsPath)
				if err != nil {
					return "", err
				}
				defer f.Close()
				err = imaging.Encode(f, dstImageFit, format, imaging.JPEGQuality(75))
				return thumbAbsPath, err
			})
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return echo.NewHTTPError(http.StatusNotFound, "Image not found")
				}
				return err
			}
			absPath = val.(string)
		}
		// maybe when media folders permissions will be implemented
		// c.Response().Header().Set("Cache-Control", "private, must-revalidate")
		return c.File(absPath)
	}
}

func (s *Server) appMediaFileHandler(c echo.Context) error {
	username := c.Param("user")
	name := c.Param("name")
	projectName := filepath.Join(username, name)
	filePath := c.Param("*")
	absPath := filepath.Join(s.Config.ProjectsRoot, projectName, "web", "app", filePath)
	return c.File(absPath)
}

func (s *Server) mediaFileHandlerService(cacheDir string) func(echo.Context) error {
	var lock singleflight.Group
	return func(c echo.Context) error {
		projectName := c.Get("project").(string)
		thumbnail := c.QueryParam("thumbnail")
		providerId := c.QueryParam("provider_id")

		provider, err := getProvider(s, projectName, providerId)
		if err != nil {
			return err
		}

		filePath := c.QueryParam("path")
		// folder := filepath.Dir(filePath)

		url := provider.StoreUrl + filePath
		absPath := filepath.Join(s.Config.ProjectsRoot, projectName, filePath)
		if strings.EqualFold(thumbnail, "true") {
			key := filepath.Join(projectName, filePath)
			val, err, _ := lock.Do(key, func() (interface{}, error) {
				thumbAbsPath := filepath.Join(cacheDir, key)
				err = os.MkdirAll(filepath.Dir(thumbAbsPath), 0777)
				if err != nil {
					return "", err
				}

				// fetch input data
				response, err := http.Get(url)
				if err != nil {
					return "", err
				}
				// don't forget to close the response
				defer response.Body.Close()

				// decode input data to image
				srcImage, _, err := image.Decode(response.Body)
				if err != nil {
					return "", err
				}

				dstImageFit := imaging.Fit(srcImage, 500, 500, imaging.Lanczos)
				format, err := imaging.FormatFromFilename(absPath)
				if err != nil {
					format = imaging.JPEG
				}

				f, err := os.Create(thumbAbsPath)
				if err != nil {
					return "", err
				}
				defer f.Close()
				err = imaging.Encode(f, dstImageFit, format, imaging.JPEGQuality(75))
				return thumbAbsPath, err
			})
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return echo.NewHTTPError(http.StatusNotFound, "Image not found")
				}
				return err
			}
			absPath = val.(string)
		}
		// maybe when media folders permissions will be implemented
		// c.Response().Header().Set("Cache-Control", "private, must-revalidate")
		return c.Redirect(308, url)
	}
}

type MediaFile struct {
	domain.ProjectFile
	Filename string `json:"filename"`
}

// Original way of uploading
func (s *Server) handleUploadMediaFile(c echo.Context) error {
	directory := c.Param("*")
	return s.handleUploadMediaFileLocal(c, directory)
}

func (s *Server) handleUploadMediaFileLocal(c echo.Context, directory string) error {
	projectName := c.Get("project").(string)

	file, err := c.FormFile("file")
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}
	if !strings.HasPrefix(directory, "web/") {
		return echo.ErrForbidden
	}

	// user, err := s.auth.GetUser(c)
	// if err != nil {
	// 	return err
	// }

	// TODO: check directory access
	// s.projects.GetDirectoryPerms(user)
	src, err := file.Open()
	if err != nil {
		return fmt.Errorf("reading upload file: %w", err)
	}

	finfo, err := s.projects.SaveFile(projectName, directory, file.Filename, src, file.Size)
	if err != nil {
		if errors.Is(err, application.ErrProjectSizeLimit) {
			return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "Reached project size limit.")
		}
		return err
	}
	return c.JSON(http.StatusOK, MediaFile{finfo, filepath.Base(finfo.Path)})

}

func getProvider(s *Server, projectName string, providerId string) (*domain.StorageProvider, error) {
	info, err := s.projects.GetSettings(projectName)
	if err != nil {
		if errors.Is(err, domain.ErrProjectNotExists) {
			s.log.Errorw(err.Error(), "handler", "handleGetProject")
			s.log.Errorw("handleGetProject", zap.Error(err))
			return nil, echo.ErrNotFound
		}
		return nil, err
	}

	providersConfig := info.Storage
	provider := findObjectByID(providersConfig, providerId)
	if provider == nil {
		provider = &domain.StorageProvider{
			ID:   "local",
			Type: "local",
		}
	}
	return provider, nil
}


func (s *Server) handleUploadMediaFileService(c echo.Context) error {
	directory := c.QueryParam("path")
	projectName := c.Get("project").(string)
	providerId := c.QueryParam("provider_id")

	provider, err := getProvider(s, projectName, providerId)
	if err != nil {
		return err
	}
	providerType := provider.Type

	if providerType == "s3" {
		return s.handleUploadMediaFileS3(c, directory, provider)
	}
	return s.handleUploadMediaFileLocal(c, directory)
}

type AWSMediaFile struct {
	domain.ProjectFile
	Filename string `json:"filename"`
}

func findObjectByID(array []domain.StorageProvider, id string) *domain.StorageProvider {
	for _, obj := range array {
		if obj.ID == id {
			return &obj
		}
	}
	return nil
}

func (s *Server) handleUploadMediaFileS3(c echo.Context, directory string, provider *domain.StorageProvider) error {
	endpointUrl, err := url.Parse(provider.StoreUrl)
	if err != nil {
		return err
	}

	ctx := context.Background()
	minioClient, err := minio.New(endpointUrl.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(provider.AccessKey, provider.SecretKey, ""),
		Secure: true,
	})
	if err != nil {
		return fmt.Errorf("connecting to service: %w", err)
	}

	file, err := c.FormFile("file")
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	src, err := file.Open()
	if err != nil {
		return fmt.Errorf("reading upload file: %w", err)
	}

  defer src.Close()

	pattern, err := processFileName(file)
	if err != nil {
		return err
	}

	bucketName := provider.Bucket
	parsedURL, err := url.Parse(pattern)
	if err != nil {
		fmt.Println("Error parsing URL:", err)
		return nil
	}
	fileName := path.Clean(parsedURL.Path)
	objectName := path.Join(directory, fileName)

	miniinfo, err := minioClient.PutObject(ctx, bucketName, objectName, src, file.Size, minio.PutObjectOptions{})
	if err != nil {
		return err
	}

	filePath := path.Join(bucketName, miniinfo.Key)
	if err != nil {
			return fmt.Errorf("unable to upload: %w", err)
	}

	return c.JSON(http.StatusOK, AWSMediaFile{domain.ProjectFile{Path: filePath, Size: file.Size}, fileName})
}

func processFileName(file *multipart.FileHeader) (string, error) {
	pattern := file.Filename

	if strings.Contains(pattern, "<timestamp>") {
		pattern = strings.Replace(pattern, "<timestamp>", fmt.Sprint(time.Now().Unix()), 1)
	}

	if strings.Contains(pattern, "<random>") {
		// https://github.com/joncrlsn/fileutil/blob/5aac37a6ac963fd712b618b024d2eb14aa190958/fileutil.go#L98-L103
		randBytes := make([]byte, 16)
		rand.Read(randBytes)
		pattern = strings.Replace(pattern, "<random>", hex.EncodeToString(randBytes), 1)
	}

	if strings.Contains(pattern, "<hash>") {
		src, err := file.Open()
		if err != nil {
			return "", err
		}
		defer src.Close()
	
		h := sha1.New()
		if _, err := io.Copy(h, src); err != nil {
			return "", err
		}

		hash := fmt.Sprintf("%x", h.Sum(nil))
		pattern = strings.Replace(pattern, "<hash>", hash, 1)
	}
	return pattern, nil
}

func (s *Server) handleDeleteMediaFile(c echo.Context) error {
	projectName := c.Get("project").(string)
	path := c.Param("*")
	if !strings.HasPrefix(path, "web/") {
		return echo.ErrForbidden
	}
	return s.projects.DeleteFile(projectName, path)
}
