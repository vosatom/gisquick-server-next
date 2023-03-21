package cache

import (
	"fmt"
	"sync"

	"golang.org/x/sync/singleflight"
)

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

func (c *DataCache[K, V]) Remove(key K) {
	c.Lock()
	defer c.Unlock()
	delete(c.items, key)
}
