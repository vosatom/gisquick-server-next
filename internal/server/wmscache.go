// Partly based on mapcache/mapcache.go
package server

import (
	"crypto/md5"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gisquick/gisquick-server/internal/domain"
	"github.com/labstack/echo/v4"
)

func (s *Server) checkAccess(c echo.Context) error {
	return nil
}

type Tile struct {
	Project domain.ProjectInfo

	// "user/project"
	ProjectFullName string
	Layers          string

	BoundingBox string

	// Mime type
	Format     string
	Projection string

	// File extension
	ImageFormat string

	Version string
	Width   int
	Height  int
}

func (s *Server) InvalidateMapCache(ProjectFullName string) error {
	baseDir := s.Config.MapCacheRoot
	projectHash := fmt.Sprintf("%x", md5.Sum([]byte(ProjectFullName)))

	dir := filepath.Join(baseDir, projectHash)
	s.log.Infof("clearing project mapcache: %s", ProjectFullName)
	return os.RemoveAll(dir)
}

func (s *Server) removeMapCache(c echo.Context) error {
	projectName := getProjectName(c)
	return s.InvalidateMapCache(projectName)
}

func (s *Server) getTilePath(tile Tile) string {
	baseDir := s.Config.MapCacheRoot

	projectHash := fmt.Sprintf("%x", md5.Sum([]byte(tile.ProjectFullName)))
	layersHash := fmt.Sprintf("%x", md5.Sum([]byte(tile.Layers)))
	bboxHash := fmt.Sprintf("%x", md5.Sum([]byte(tile.BoundingBox)))

	return filepath.Join(baseDir, projectHash, layersHash, bboxHash+"."+tile.ImageFormat)
}

func (s *Server) GetTileCache(c echo.Context, tilePath string) (io.ReadCloser, error) {
	file, err := os.Open(tilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return file, nil
}

func (s *Server) GetTileUrl(tile Tile, projectInfo domain.ProjectInfo) *url.URL {
	owsProject := filepath.Join("/publish", tile.ProjectFullName, projectInfo.QgisFile)

	params := map[string]string{
		"VERSION":     tile.Version,
		"SERVICE":     "WMS",
		"REQUEST":     "GetMap",
		"MAP":         owsProject,
		"BBOX":        tile.BoundingBox,
		"WIDTH":       strconv.Itoa(tile.Width),
		"HEIGHT":      strconv.Itoa(tile.Height),
		"SRS":         tile.Projection,
		"FORMAT":      tile.Format,
		"LAYERS":      tile.Layers,
		"TRANSPARENT": "true",
		"TILED":       "true",
	}
	u, _ := url.Parse(s.Config.MapserverURL)
	urlParams := u.Query()
	for name, val := range params {
		urlParams.Set(name, val)
	}
	u.RawQuery = urlParams.Encode()
	return u
}

func (s *Server) SaveTile(tilePath string, data io.Reader) error {

	img, format, err := image.Decode(data)
	if err != nil {
		return err
	}

	var encodeImage func(io.Writer, image.Image) error
	if format == "png" {
		encodeImage = png.Encode
	} else if format == "jpeg" {
		encodeImage = func(out io.Writer, i image.Image) error {
			return jpeg.Encode(out, i, nil)
		}
	}

	log.Println("saving tile to:", tilePath)
	if err := os.MkdirAll(filepath.Dir(tilePath), os.ModePerm); err != nil {
		return err
	}
	f, err := os.Create(tilePath)
	if err != nil {
		return fmt.Errorf("creating tile file: %v", err)
	}
	if err := encodeImage(f, img); err != nil {
		return fmt.Errorf("encoding tile image: %v", err)
	}
	return nil
}

// i32::from_str("9").unwrap_or(0);
func ParseIntOr(input string, fallback int) int {
	output, err := strconv.Atoi(input)
	if err != nil {
		output = fallback
	}
	return output
}

func closeIfNotNil(closer io.Closer) {
	if closer != nil {
		closer.Close()
	}
}

func (s *Server) handleMapCachedOws() func(c echo.Context) error {
	client := &http.Client{}

	return func(c echo.Context) error {
		// Check access to service resource (project, layers, user, etc.)
		if err := s.checkAccess(c); err != nil {
			return err
		}

		projectName := getProjectName(c)
		pInfo, err := s.projects.GetProjectInfo(projectName)
		if err != nil {
			if errors.Is(err, domain.ErrProjectNotExists) {
				return echo.ErrNotFound
			}
			return fmt.Errorf("reading project info: %w", err)
		}

		tile := Tile{
			Project:         pInfo,
			ProjectFullName: projectName,
			Projection:      pInfo.Projection,
			BoundingBox:     c.QueryParam("BBOX"),
			Layers:          c.QueryParam("LAYERS"),
			Width:           ParseIntOr(c.QueryParam("WIDTH"), 256),
			Height:          ParseIntOr(c.QueryParam("HEIGHT"), 256),
			Version:         c.QueryParam("VERSION"),
			Format:          c.QueryParam("FORMAT"),
			ImageFormat:     "png",
		}

		// Find out if the requested tileFile is cached
		var finalTileFile io.ReadCloser

		tilePath := s.getTilePath(tile)

		finalTileFile, err = s.GetTileCache(c, tilePath)
		if err != nil {
			closeIfNotNil(finalTileFile)
			return err
		}

		if finalTileFile == nil {
			// If not, request it from the WMS and save it to the cache
			tileUrl := s.GetTileUrl(tile, pInfo)
			req, _ := http.NewRequest(http.MethodGet, tileUrl.String(), nil)
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			if resp.StatusCode != 200 {
				closeIfNotNil(finalTileFile)
				msg, _ := ioutil.ReadAll(resp.Body)
				return fmt.Errorf(string(msg))
			}

			if err := s.SaveTile(tilePath, resp.Body); err != nil {
				closeIfNotNil(finalTileFile)
				return err
			}
		}

		finalTileFile, err = s.GetTileCache(c, tilePath)
		if finalTileFile == nil || err != nil {
			// err
			closeIfNotNil(finalTileFile)
			return fmt.Errorf(string("Error"))
		}

		result := c.Stream(http.StatusOK, tile.Format, finalTileFile)
		closeIfNotNil(finalTileFile)
		return result
	}
}
