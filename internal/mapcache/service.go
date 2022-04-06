package mapcache

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

	"github.com/gisquick/gisquick-server/internal/domain"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

var (
	ErrMapServer = errors.New("mapserver error")
)

type metrics struct {
	counter prometheus.Counter
}

func cacheMetrics() *metrics {
	// myCounter := prometheus.NewGauge(prometheus.GaugeOpts{
	// 	Name:        "my_handler_executions",
	// 	Help:        "Counts executions of my handler function.",
	// 	ConstLabels: prometheus.Labels{"version": "1234"},
	// })
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "mapcache_metatile_rendering_count",
		Help: "Counts executions of metatile rendering.",
		// ConstLabels: prometheus.Labels{"version": "1234"},
	})
	if err := prometheus.Register(counter); err != nil {
		log.Fatal(err)
	}
	return &metrics{counter: counter}
}

type Cache struct {
	Root      string
	ServerURL string
	log       *zap.SugaredLogger
	client    *http.Client
	tileLock  singleflight.Group
	metrics   *metrics
}

func NewMapcache(log *zap.SugaredLogger, root string, mapserverURL string) *Cache {
	return &Cache{
		Root:      root,
		ServerURL: mapserverURL,
		log:       log,
		client:    &http.Client{},
		tileLock:  singleflight.Group{},
		metrics:   cacheMetrics(),
	}
}

func (c *Cache) Clear(project *domain.Project) error {
	projectHash := fmt.Sprintf("%x", md5.Sum([]byte(project.Info.FullName)))
	dir := filepath.Join(c.Root, projectHash)
	c.log.Infof("clearing project mapcache: %s", project.Info.FullName)
	return os.RemoveAll(dir)
}

func (c *Cache) GetLayer(p *domain.Project, layers string) Layer {
	projectHash := fmt.Sprintf("%x", md5.Sum([]byte(p.Info.FullName)))
	layersHash := fmt.Sprintf("%x", md5.Sum([]byte(layers)))

	return Layer{
		Map:         filepath.Join("/publish", p.Info.Map),
		Project:     projectHash,
		Publish:     "",
		Name:        layersHash,
		ServerURL:   c.ServerURL,
		WMSLayer:    layers,
		Extent:      p.Settings.Extent,
		Resolutions: p.Settings.TileResolutions,
		Projection:  p.ProjectionCode(),
		ImageFormat: "png",
		TileSize:    256,
		MetaSize:    []int{5, 5},
		MetaBuffer:  []int{50, 50},
	}
}

func (c *Cache) ProcessMetaTile(layer Layer, metatile MetaTile, data io.Reader, dir string) error {
	img, format, err := image.Decode(data)
	if err != nil {
		return fmt.Errorf("decoding metatile: %v", err)
	}
	simg, ok := img.(subImager)
	if !ok {
		return fmt.Errorf("Image does not support cropping: %s", format)
	}
	var encodeImage func(io.Writer, image.Image) error
	if format == "png" {
		encodeImage = png.Encode
	} else if format == "jpeg" {
		encodeImage = func(out io.Writer, i image.Image) error {
			return jpeg.Encode(out, i, nil)
		}
	}

	metaCols, metaRows := layer.GetMetaSize(metatile.Z)
	metaHeight := metaRows*layer.TileSize + 2*layer.MetaBuffer[1]
	for i := 0; i < metaCols; i++ {
		for j := 0; j < metaRows; j++ {
			minx := i*layer.TileSize + layer.MetaBuffer[0]
			maxx := minx + layer.TileSize
			// this next calculation is because image origin is (top,left)
			maxy := metaHeight - (j*layer.TileSize + layer.MetaBuffer[1])
			miny := maxy - layer.TileSize

			x := metatile.X*layer.MetaSize[0] + i
			y := metatile.Y*layer.MetaSize[1] + j
			tile := Tile{layer, x, y, metatile.Z}

			tileImg := simg.SubImage(image.Rect(minx, miny, maxx, maxy))
			tilePath := filepath.Join(dir, layer.Path(tile))
			// log.Println("saving tile to:", tilePath)
			if err := os.MkdirAll(filepath.Dir(tilePath), os.ModePerm); err != nil {
				return err
			}
			f, err := os.Create(tilePath)
			if err != nil {
				return fmt.Errorf("creating tile file: %v", err)
			}
			if err := encodeImage(f, tileImg); err != nil {
				return fmt.Errorf("encoding tile image: %v", err)
			}
		}
	}
	return nil
}

func (c *Cache) GetTileFile(p *domain.Project, tile Tile) (string, error) {
	layer := tile.Layer
	// tile := mapcache.Tile{Layer: layer, X: params.X, Y: params.Y, Z: params.Z}
	tilePath := filepath.Join(c.Root, layer.Path(tile))
	_, err := os.Stat(tilePath)
	if err == nil {
		return tilePath, nil
	}

	metatile := layer.GetMetaTile(tile)
	metatileKey := layer.Path(metatile.Tile)
	var metatileUrl *url.URL
	_, err, _ = c.tileLock.Do(metatileKey, func() (interface{}, error) {
		c.metrics.counter.Inc()
		metatileUrl = layer.GetMetaTileURL(metatile)
		q := metatileUrl.Query()
		q.Set("MAP", filepath.Join("/publish", p.Info.Map))
		metatileUrl.RawQuery = q.Encode()
		c.log.Infow("fetching metatile", "service", "mapcache", "url", metatileUrl.String())

		req, _ := http.NewRequest(http.MethodGet, metatileUrl.String(), nil)
		// req = req.WithContext(c.Request().Context())
		resp, err := c.client.Do(req)
		if err != nil {
			// gateway error
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			msg, _ := ioutil.ReadAll(resp.Body)
			// mapserver error
			// http.newer
			// resp.Header.Get("Content-Type")
			// return nil, echo.NewHTTPError(resp.StatusCode, )
			return nil, fmt.Errorf(string(msg))
		}
		if err := c.ProcessMetaTile(layer, metatile, resp.Body, c.Root); err != nil {
			return nil, fmt.Errorf("processing metatile: %w", err)
		}
		return nil, nil
	})
	if err != nil {
		c.log.Errorw("mapcache metatile request", "project", p.Info.FullName, "url", metatileUrl, zap.Error(err))
		return "", ErrMapServer
	}
	return tilePath, nil
}
