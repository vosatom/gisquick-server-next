package server

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/jellydator/ttlcache/v3"
	"github.com/labstack/echo/v4"
	"golang.org/x/sync/singleflight"
)

/*
<plugins>
</pyqgis_plugin>
<pyqgis_plugin name="TestTest" version="1.0.2" plugin_id="1605">
<description><![CDATA[This plugin makes Local Dominance visualisations of raster images]]></description>
<about><![CDATA[This plugin makes Local Dominance visualisations of raster images which enchances e.g. very subtle features in digital elevation models.]]></about>
<version>1.0.2</version>
<trusted>False</trusted>
<qgis_minimum_version>3.0.0</qgis_minimum_version>
<qgis_maximum_version>3.99.0</qgis_maximum_version>
<homepage><![CDATA[https://github.com/ThomasLjungberg/LocalDominance-visualisation]]></homepage>
<file_name>local_dominance.1.0.2.zip</file_name>
<icon>/media/packages/2022/icon_Dnhjs7n.png</icon>
<author_name><![CDATA[Thomas Ljungberg]]></author_name>
<download_url>https://plugins.qgis.org/plugins/local_dominance/version/1.0.2/download/</download_url>
<uploaded_by><![CDATA[thomasljungberg]]></uploaded_by>
<create_date>2019-01-16T08:35:17.180211</create_date>
<update_date>2022-02-25T14:45:23.136206</update_date>
<experimental>True</experimental>
<deprecated>False</deprecated>
<tracker><![CDATA[https://github.com/ThomasLjungberg/LocalDominance-visualisation/issues]]></tracker>
<repository><![CDATA[https://github.com/ThomasLjungberg/LocalDominance-visualisation]]></repository>
<tags><![CDATA[dem,landscape,archaeology,lidar,morphology,visualisation]]></tags>
<downloads>1883</downloads>
<average_vote>3.2495938007749032</average_vote>
<rating_votes>8</rating_votes>
<external_dependencies></external_dependencies>
<server>False</server>
</pyqgis_plugin>
</plugins>
*/

type Plugins struct {
	XMLName xml.Name       `xml:"plugins"`
	Plugins []PyQgisPlugin `xml:"plugins"`
}

type PyQgisPlugin struct {
	XMLName        xml.Name `xml:"pyqgis_plugin"`
	Name           string   `xml:"name,attr" json:"name"`
	QgisMinVersion string   `xml:"qgis_minimum_version" json:"qgisMinimumVersion"`
	QgisMaxVersion string   `xml:"qgis_maximum_version,omitempty" json:"qgisMaximumVersion"`
	Description    CDATA    `xml:"description" json:"description"`
	About          CDATA    `xml:"about" json:"about"`
	Version        string   `xml:"version,attr" json:"version"`
	Author         string   `xml:"author_name" json:"author"`
	// Email
	Changelog    CDATA  `xml:"changelog" json:"changelog"`
	Experimental string `xml:"experimental" json:"experimental"`
	Deprecated   string `xml:"deprecated" json:"deprecated"`
	Tags         string `xml:"tags" json:"tags"`
	Homepage     CDATA  `xml:"homepage" json:"homepage"`
	Repository   CDATA  `xml:"repository" json:"repository"`
	Tracker      CDATA  `xml:"tracker" json:"tracker"`
	Icon         string `xml:"icon,omitempty" json:"icon,omitempty"`
	Server       string `xml:"server" json:"server"`
	// Category string `json:"category"` // one of Raster, Vector, Database and Web

	// not part of qgis metadata
	Updated time.Time `xml:"update_date" json:"updated"`

	// generated data
	DownloadURL string `xml:"download_url"`
	FileName    string `xml:"file_name" json:"filename"`
}

// type CDATA struct {
// 	Text string `xml:",cdata"`
// }

type CDATA string

func (c CDATA) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	return e.EncodeElement(struct {
		string `xml:",cdata"`
	}{string(c)}, start)
}

type Item[V any] struct {
	value     V
	timestamp int64
}

type DataCache[K comparable, V any] struct {
	sync.RWMutex
	items      map[K]*Item[V]
	loaderLock *singleflight.Group
	loader     func(K) (V, error)
}

// type LoaderFunc[K comparable, V any] func(*DataCache[K, V], K) *Item[K, V]
// type LoaderFunc[K comparable, V any] func(K) (V, error)

func NewDataCache[K comparable, V any](loader func(K) (V, error)) *DataCache[K, V] {
	items := make(map[K]*Item[V])
	loaderLock := &singleflight.Group{}
	return &DataCache[K, V]{items: items, loaderLock: loaderLock, loader: loader}
}

func (c *DataCache[K, V]) get(key K) (*Item[V], bool) {
	c.RLock()
	defer c.RUnlock()
	item, ok := c.items[key]
	return item, ok
}

func (c *DataCache[K, V]) Get(key K, t int64) (V, error) {
	item, ok := c.get(key)
	if !ok || item.timestamp != t {
		strKey := fmt.Sprintf("%v", key)
		res, err, _ := c.loaderLock.Do(strKey, func() (interface{}, error) {
			value, err := c.loader(key)
			if err != nil {
				return value, err
				// return value, fmt.Errorf("loader error: %w", err)
			}
			c.Lock()
			defer c.Unlock()
			c.items[key] = &Item[V]{value: value, timestamp: t}
			return value, nil
		})
		var v V
		if err != nil {
			return v, err
		}
		v = res.(V)
		return v, nil
	}
	return item.value, nil
}

func (s *Server) handleDownloadPlugin(rootDir string) func(echo.Context) error {
	return func(c echo.Context) error {
		filename := c.Param("*")
		fpath := filepath.Join(rootDir, filename)
		return c.File(fpath)
	}
}

func (s *Server) platformPluginRepoHandler1(rootDir string) func(echo.Context) error {
	type PluginKey struct {
		Filename string
		Mtime    int64
	}
	type PluginEntry struct {
		Data  PyQgisPlugin
		Mtime int64
	}
	loader := ttlcache.LoaderFunc[PluginKey, PluginEntry](
		func(c *ttlcache.Cache[PluginKey, PluginEntry], key PluginKey) *ttlcache.Item[PluginKey, PluginEntry] {
			s.log.Infof("platformPluginRepoHandler.LoaderFunc: %s", key)
			// var plugin PyQgisPlugin2
			// f, err := os.Open(filename)
			// if err != nil {
			// 	s.log.Errorw("reading qgis plugin metadata", zap.Error(err))
			// 	return nil
			// }
			// if err := json.NewDecoder(f).Decode(&plugin); err != nil {
			// 	s.log.Errorw("parsing qgis plugin metadata", zap.Error(err))
			// 	return nil
			// }
			// entry = PluginEntry{plugin}
			// item := c.Set(filename, plugin, ttlcache.NoTTL)
			// return item
			return nil
		},
	)
	cache := ttlcache.New(
		ttlcache.WithLoader[PluginKey, PluginEntry](loader),
	)
	cache.Metrics()
	return func(c echo.Context) error {
		return nil
	}
}

func (s *Server) platformPluginRepoHandler(rootDir string) func(echo.Context) error {
	cache := NewDataCache(func(filename string) (PyQgisPlugin, error) {
		var plugin PyQgisPlugin
		s.log.Infow("loading qgis plugin metadata", "file", filename)
		f, err := os.Open(filename)
		if err != nil {
			// s.log.Errorw("reading qgis plugin metadata", zap.Error(err))
			return plugin, fmt.Errorf("reading qgis plugin metadata: %w", err)
		}
		if err := json.NewDecoder(f).Decode(&plugin); err != nil {
			// s.log.Errorw("parsing qgis plugin metadata", zap.Error(err))
			return plugin, fmt.Errorf("parsing qgis plugin metadata: %w", err)
		}
		return plugin, nil
	})

	return func(c echo.Context) error {
		platform := c.Param("platform")
		// siteURL, _ := url.Parse(s.Config.SiteURL)

		files, err := filepath.Glob(filepath.Join(rootDir, platform, "*/*.json"))
		if err != nil {
			return fmt.Errorf("listing qgis plugins repo: %w", err)
		}
		plugins := make([]PyQgisPlugin, 0, len(files))
		for _, filename := range files {
			fStat, err := os.Stat(filename)
			if err != nil {
				return fmt.Errorf("listing qgis plugins repo: %w", err)
				// plugin.Updated = fStat.ModTime()
			}
			updated := fStat.ModTime()
			timestamp := updated.Unix()

			plugin, err := cache.Get(filename, timestamp)
			if err != nil {
				return fmt.Errorf("getting qgis plugin metadata: %w", err)
			}
			pluginName := filepath.Base(filepath.Dir(filename))
			// plugin.Updated = updated
			if plugin.Icon != "" {
				// plugin.Icon = fmt.Sprintf("/api/plugins/download/%s/%s/%s", platform, pluginName, plugin.Icon)
				// plugin.Icon = fmt.Sprintf("/plugins/download/%s/%s/%s", platform, pluginName, plugin.Icon)
				plugin.Icon = path.Join("/download", platform, pluginName, plugin.Icon)
			}

			relURL := path.Join("download", platform, pluginName, plugin.FileName)
			u, _ := url.Parse(s.Config.PluginsURL)
			u.Path = path.Join(u.Path, relURL)
			plugin.DownloadURL = u.String()

			// s.log.Infow("plugin metadata", "meta", plugin)
			plugins = append(plugins, plugin)
		}
		return c.XML(http.StatusOK, Plugins{Plugins: plugins})
	}
}

/*
https://dev.gisquick.org/api/plugins/lin64

or

https://plugins.gisquick.org/platform/linux
https://plugins.gisquick.org/platform/windows
https://plugins.gisquick.org/platform/macos

https://plugins.gisquick.org/platform/lin64
https://plugins.gisquick.org/platform/win64
https://plugins.gisquick.org/platform/mac64

https://plugins.gisquick.org/download/lin64/gisquick/
*/
