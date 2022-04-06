package mapcache

import (
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sync/singleflight"
)

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Tile
type Tile struct {
	Layer Layer
	X     int
	Y     int
	Z     int
}

func (t Tile) Size() int {
	return t.Layer.TileSize
}

func (t Tile) Bounds() ([]float64, error) {
	if t.Z >= len(t.Layer.Resolutions) {
		return nil, fmt.Errorf("Tile zoom level %d is out of layer resolutions", t.Z)
	}
	res := t.Layer.Resolutions[t.Z]
	extent := t.Layer.Extent
	tileSize := t.Layer.TileSize
	minx := extent[0] + (res * float64(t.X) * float64(tileSize))
	miny := extent[1] + (res * float64(t.Y) * float64(tileSize))
	maxx := extent[0] + (res * float64(t.X+1) * float64(tileSize))
	maxy := extent[1] + (res * float64(t.Y+1) * float64(tileSize))
	return []float64{minx, miny, maxx, maxy}, nil
}

// MetaTile
type MetaTile struct {
	Tile
}

func (mt MetaTile) ActualSize() (int, int) {
	metaCols, metaRows := mt.Layer.GetMetaSize(mt.Z)
	return mt.Layer.TileSize * metaCols, mt.Layer.TileSize * metaRows
}

func (mt MetaTile) Size() (int, int) {
	width, height := mt.ActualSize()
	return width + mt.Layer.MetaBuffer[0]*2, height + mt.Layer.MetaBuffer[1]*2

}

func (mt MetaTile) Bounds() []float64 {
	width, height := mt.ActualSize()
	res := mt.Layer.Resolutions[mt.Z]
	bufferX := res * float64(mt.Layer.MetaBuffer[0])
	bufferY := res * float64(mt.Layer.MetaBuffer[1])
	metaWidth := res * float64(width)
	metaHeight := res * float64(height)
	minx := mt.Layer.Extent[0] + float64(mt.X)*metaWidth - bufferX
	miny := mt.Layer.Extent[1] + float64(mt.Y)*metaHeight - bufferY
	maxx := minx + metaWidth + 2*bufferX
	maxy := miny + metaHeight + 2*bufferY
	return []float64{minx, miny, maxx, maxy}
}

// func (mt MetaTile) Extent() string {
// 	bounds := mt.Bounds()
// 	return fmt.Sprintf("%f,%f,%f,%f", bounds[0], bounds[1], bounds[2], bounds[3])
// }

func FormatExtent(extent []float64) string {
	return fmt.Sprintf("%f,%f,%f,%f", extent[0], extent[1], extent[2], extent[3])
}

// Layer
type Layer struct {
	Map         string
	Project     string
	Publish     string
	ServerURL   string
	Name        string
	WMSLayer    string
	Extent      []float64
	Projection  string
	TileSize    int
	MetaSize    []int
	MetaBuffer  []int
	ImageFormat string
	Resolutions []float64
}

func (l Layer) Grid(z int) ([]float64, error) {
	if z >= len(l.Resolutions) {
		return nil, fmt.Errorf("Requested zoom level %d does not exist", z)
	}
	res := l.Resolutions[z]
	width := (l.Extent[2] - l.Extent[0]) / (res * float64(l.TileSize))
	height := (l.Extent[3] - l.Extent[1]) / (res * float64(l.TileSize))
	return []float64{width, height}, nil
}

func (l Layer) Format() string {
	format := strings.ToLower(l.ImageFormat)
	if format == "jpg" {
		format = "jpeg"
	}
	return "image/" + format
}

func (l Layer) GetMetaSize(z int) (int, int) {
	grid, _ := l.Grid(z)
	return minInt(l.MetaSize[0], int(grid[0])+1), minInt(l.MetaSize[1], int(grid[1]+1))
}

func (l Layer) GetMetaTile(tile Tile) MetaTile {
	x := int(tile.X / l.MetaSize[0])
	y := int(tile.Y / l.MetaSize[1])
	return MetaTile{Tile{l, x, y, tile.Z}}
}

func (l Layer) Path(tile Tile) string {
	parts := []string{
		l.Project,
		// l.Publish,
		"tile",
		l.Name,
		strconv.Itoa(tile.Z),
		strconv.Itoa(tile.X),
		strconv.Itoa(tile.Y),
		// fmt.Sprintf("%d.%s", tile.Y, l.ImageFormat),
	}
	return filepath.Join(parts...)
}

func (l Layer) GetMetaTileURL(metatile MetaTile) *url.URL {
	width, height := metatile.Size()
	params := map[string]string{
		"SERVICE":     "WMS",
		"REQUEST":     "GetMap",
		"MAP":         l.Map,
		"BBOX":        FormatExtent(metatile.Bounds()),
		"WIDTH":       strconv.Itoa(width),
		"HEIGHT":      strconv.Itoa(height),
		"SRS":         l.Projection,
		"FORMAT":      l.Format(),
		"TRANSPARENT": "true",
		"LAYERS":      l.WMSLayer,
		// "LAYERS":      strings.Join(l.WMSLayers, ","),
	}
	u, _ := url.Parse(l.ServerURL)
	urlParams := u.Query()
	for name, val := range params {
		urlParams.Set(name, val)
	}
	u.RawQuery = urlParams.Encode()
	return u
}

type subImager interface {
	SubImage(r image.Rectangle) image.Image
}

type CacheService struct {
	Root      string
	client    *http.Client
	tilesLock *singleflight.Group
}

func NewCacheService(root string) *CacheService {
	client := &http.Client{}
	return &CacheService{
		Root:      root,
		client:    client,
		tilesLock: &singleflight.Group{},
	}
}

func (s *CacheService) RenderTile(tile Tile) error {
	layer := tile.Layer
	metatile := layer.GetMetaTile(tile)
	metatileKey := layer.Path(metatile.Tile)
	_, err, _ := s.tilesLock.Do(metatileKey, func() (interface{}, error) {
		// TODO: metrics
		metatileUrl := layer.GetMetaTileURL(metatile)
		req, _ := http.NewRequest(http.MethodGet, metatileUrl.String(), nil)
		resp, err := s.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			msg, _ := ioutil.ReadAll(resp.Body)
			return nil, fmt.Errorf(string(msg))
		}
		if err := s.saveMetaTile(metatile, resp.Body); err != nil {
			return nil, err
		}
		return nil, nil
	})
	if err != nil {
		return err
	}
	return nil
}

func (s *CacheService) saveMetaTile(metatile MetaTile, data io.Reader) error {
	layer := metatile.Layer
	img, format, err := image.Decode(data)
	if err != nil {
		return err
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
			tilePath := filepath.Join(s.Root, layer.Path(tile))
			// log.Println("saving tile to:", tilePath)
			if err := os.MkdirAll(filepath.Dir(tilePath), os.ModePerm); err != nil {
				return err
			}
			f, err := os.Create(tilePath)
			if err != nil {
				return err
			}
			if err := encodeImage(f, tileImg); err != nil {
				return err
			}
		}
	}
	return nil
}
