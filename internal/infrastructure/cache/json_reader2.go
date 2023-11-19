package cache

import (
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/jellydator/ttlcache/v3"
)

type value[T any] struct {
	Val       T
	Err       error
	Timestamp int64
}

type key struct {
	Filename  string
	Timestamp int64
}

type JSONFileReader2[T any] struct {
	cache *ttlcache.Cache[string, value[T]]
}

func NewJSONFileReader2[V any](ttl time.Duration) *JSONFileReader2[V] {
	loader := ttlcache.LoaderFunc[string, value[V]](
		func(c *ttlcache.Cache[string, value[V]], filename string) *ttlcache.Item[string, value[V]] {
			value := value[V]{}
			var data V
			content, err := os.ReadFile(filename)
			if err != nil {
				value.Err = err
			} else {
				err = json.Unmarshal(content, &data)
				if err != nil {
					value.Err = err
				} else {
					value.Val = data
					fStat, err := os.Stat(filename)
					if err != nil {
						if errors.Is(err, os.ErrNotExist) {
							return nil
						}
						value.Err = err
					} else {
						updated := fStat.ModTime()
						timestamp := updated.Unix()
						value.Timestamp = timestamp
						return c.Set(filename, value, ttlcache.DefaultTTL)
					}
				}
			}
			return c.Set(filename, value, 10*time.Second)
		},
	)
	cache := ttlcache.New(
		ttlcache.WithTTL[string, value[V]](ttl),
		ttlcache.WithLoader[string, value[V]](loader),
	)
	go cache.Start()
	return &JSONFileReader2[V]{cache: cache}
}

func (c *JSONFileReader2[V]) Get(filename string) (V, error) {
	var value V
	fStat, err := os.Stat(filename)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.cache.Delete(filename)
		}
		return value, err
	}
	updated := fStat.ModTime()
	timestamp := updated.Unix()

	item := c.cache.Get(filename)
	if item.Value().Err != nil {
		return value, err
	}
	if item.Value().Timestamp != timestamp {
		c.cache.Delete(filename)
		item = c.cache.Get(filename)
		if item.Value().Err != nil {
			return value, err
		}
	}
	v := item.Value().Val
	return v, nil
}

func (c *JSONFileReader2[V]) Close() {
	c.cache.Stop()
	c.cache.DeleteAll()
}
