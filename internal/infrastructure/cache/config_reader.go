package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"go.uber.org/zap"
)

var (
	ErrConfigNotExists = errors.New("config file not exists")
)

type FilesConfigReader[V any] struct {
	sync.RWMutex
	log *zap.SugaredLogger
	// lock       singleflight.Group
	configPath    string
	cache         *DataCache[string, V]
	DefaultConfig V
}

func NewFilesConfigReader[V any](log *zap.SugaredLogger, configPath string, defaultConfig V) *FilesConfigReader[V] {
	cache := NewDataCache(func(filename string) (V, error) {

		config := defaultConfig
		// filename := filepath.Join(configPath, fmt.Sprintf("%s.json", id))
		content, err := ioutil.ReadFile(filename)
		log.Infow("NewFilesConfigReader: parsing file", "path", filename)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return defaultConfig, ErrConfigNotExists
			}
			return defaultConfig, fmt.Errorf("reading project file: %w", err)
		}
		err = json.Unmarshal(content, &config)
		if err != nil {
			log.Errorw("parsing project file", zap.Error(err))
			return defaultConfig, fmt.Errorf("reading project file: %w", err)
		}
		return config, nil
	})
	return &FilesConfigReader[V]{
		log:           log,
		configPath:    configPath,
		cache:         cache,
		DefaultConfig: defaultConfig,
	}
}

func (c *FilesConfigReader[V]) GetConfig(id string) (V, error) {
	filename := filepath.Join(c.configPath, fmt.Sprintf("%s.json", id))
	fStat, err := os.Stat(filename)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c.DefaultConfig, nil
		}
		return c.DefaultConfig, err
	}
	updated := fStat.ModTime()
	timestamp := updated.Unix()

	config, err := c.cache.Get(filename, timestamp)
	if err != nil {
		return c.DefaultConfig, err
	}
	return config, nil
}
