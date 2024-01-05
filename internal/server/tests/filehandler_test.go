package server_tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"testing"

	"github.com/gisquick/gisquick-server/cmd/commands"
	"github.com/gisquick/gisquick-server/internal/server"
	"github.com/labstack/echo"
	"github.com/stretchr/testify/assert"
)

func setupTest(tb testing.TB) commands.ServerHandle {
	if !assert.NoError(tb, prepareDB()) {
		return commands.ServerHandle{}
	}

	handle, err := commands.CreateServer()
	if !assert.NoError(tb, err) {
		return commands.ServerHandle{}
	}
	return handle
}

func TestRequestUploadS3File(t *testing.T) {
	handle := setupTest(t)
	defer handle.Close()
	basePath := GetBasePath()

	mockPath := path.Join(basePath, "/mocks/files/gisquick_logo.png")
	body := new(bytes.Buffer)
	writer, err := CreateFormFile(body, mockPath, "gisquick_logo.png")
	if !assert.NoError(t, err) {
		return
	}

	parsedUrl, _ := url.Parse("/api/project/media_file/test/project")
	q := parsedUrl.Query()
	q.Set("directory", "web/photos")
	q.Set("provider_id", "minio")
	parsedUrl.RawQuery = q.Encode()

	req := httptest.NewRequest(http.MethodPost, parsedUrl.String(), body)
	err = authenticateRequest(handle.Server, req, "admin", "admin")
	if !assert.NoError(t, err) {
		return
	}

	req.Header.Set(echo.HeaderContentType, writer.FormDataContentType())
	rec := httptest.NewRecorder()

	handle.Server.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var fileResult server.MediaFileResult
	json.Unmarshal(rec.Body.Bytes(), &fileResult)
	assert.Equal(t, "gisquick/web/photos/gisquick_logo.png", fileResult.ProjectFile.Path)
	assert.Equal(t, int64(24150), fileResult.ProjectFile.Size)
	assert.Greater(t, int64(0), fileResult.ProjectFile.Mtime)
	assert.Equal(t, "gisquick_logo.png", fileResult.Filename)
}

func TestRequestUploadFileWithoutPermission(t *testing.T) {
	basePath := GetBasePath()

	handle := setupTest(t)
	defer handle.Close()

	mockPath := path.Join(basePath, "/mocks/files/gisquick_logo.png")
	body := new(bytes.Buffer)
	writer, err := CreateFormFile(body, mockPath, "gisquick_logo.png")
	if !assert.NoError(t, err) {
		return
	}

	parsedUrl, _ := url.Parse("/api/project/media_file/test/project")
	q := parsedUrl.Query()
	q.Set("directory", "web/photos")
	q.Set("provider_id", "minio")
	parsedUrl.RawQuery = q.Encode()

	req := httptest.NewRequest(http.MethodPost, parsedUrl.String(), body)

	req.Header.Set(echo.HeaderContentType, writer.FormDataContentType())
	rec := httptest.NewRecorder()

	handle.Server.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}
func TestRequestUploadDifferentFileWithExistingName(t *testing.T) {
	basePath := GetBasePath()

	handle := setupTest(t)
	defer handle.Close()

	mockPath := path.Join(basePath, "/mocks/files/empty.png")
	body := new(bytes.Buffer)
	writer, err := CreateFormFile(body, mockPath, "gisquick_logo.png")
	if !assert.NoError(t, err) {
		return
	}

	parsedUrl, _ := url.Parse("/api/project/media_file/test/project")
	q := parsedUrl.Query()
	q.Set("directory", "web/photos")
	q.Set("provider_id", "minio")
	parsedUrl.RawQuery = q.Encode()

	req := httptest.NewRequest(http.MethodPost, parsedUrl.String(), body)
	err = authenticateRequest(handle.Server, req, "admin", "admin")
	if !assert.NoError(t, err) {
		return
	}

	req.Header.Set(echo.HeaderContentType, writer.FormDataContentType())
	rec := httptest.NewRecorder()

	handle.Server.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var fileResult server.MediaFileResult
	json.Unmarshal(rec.Body.Bytes(), &fileResult)
	assert.Equal(t, "gisquick/web/photos/gisquick_logo_c2ed3e2419022dbfe94ed72ef0f87beb.png", fileResult.ProjectFile.Path)
	assert.Equal(t, int64(82), fileResult.ProjectFile.Size)
	assert.Greater(t, int64(0), fileResult.ProjectFile.Mtime)
	assert.Equal(t, "gisquick_logo_c2ed3e2419022dbfe94ed72ef0f87beb.png", fileResult.Filename)
}

func TestRequestUploadLocalFile(t *testing.T) {
	basePath := GetBasePath()

	handle := setupTest(t)
	defer handle.Close()

	mockPath := path.Join(basePath, "/mocks/files/gisquick_logo.png")
	body := new(bytes.Buffer)
	writer, err := CreateFormFile(body, mockPath, "gisquick_logo.png")
	if !assert.NoError(t, err) {
		return
	}

	parsedUrl, _ := url.Parse("/api/project/media_file/test/project")
	q := parsedUrl.Query()
	q.Set("directory", "web/photos")
	q.Set("provider_id", "local")
	parsedUrl.RawQuery = q.Encode()

	req := httptest.NewRequest(http.MethodPost, parsedUrl.String(), body)
	err = authenticateRequest(handle.Server, req, "admin", "admin")
	if !assert.NoError(t, err) {
		return
	}

	req.Header.Set(echo.HeaderContentType, writer.FormDataContentType())
	rec := httptest.NewRecorder()

	handle.Server.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var fileResult server.MediaFileResult
	json.Unmarshal(rec.Body.Bytes(), &fileResult)
	assert.Equal(t, "web/photos/gisquick_logo.png", fileResult.ProjectFile.Path)
	assert.Equal(t, "9761244b42a88c941c209132c76b224e22664880", fileResult.ProjectFile.Hash)
	assert.Equal(t, int64(24150), fileResult.ProjectFile.Size)
	assert.Greater(t, fileResult.ProjectFile.Mtime, int64(0))
	assert.Equal(t, "gisquick_logo.png", fileResult.Filename)
}

func TestRequestGetFileScreenshot(t *testing.T) {
	handle := setupTest(t)
	defer handle.Close()

	body := new(bytes.Buffer)

	parsedUrl, _ := url.Parse("/api/project/media_file/test/project")
	q := parsedUrl.Query()
	q.Set("thumbnail", "true")
	q.Set("provider_id", "minio")
	q.Set("src", "http://localhost:9000/gisquick/web/photos/gisquick_logo.png")
	parsedUrl.RawQuery = q.Encode()

	req := httptest.NewRequest(http.MethodGet, parsedUrl.String(), body)
	rec := httptest.NewRecorder()

	handle.Server.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusPermanentRedirect, rec.Code)
	assert.Equal(t, "http://localhost:9000/gisquick/gisquick/web/photos/thumbs/gisquick_logo.png", rec.Header().Get(echo.HeaderLocation))
}
