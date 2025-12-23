package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Cache handles keeping track of processed files
type Cache struct {
	mu    sync.RWMutex
	Items map[string]int64 `json:"items"` // Path -> ModTime
	path  string
}

// NewCache loads or creates a new cache at the specific path
func NewCache(path string) (*Cache, error) {
	c := &Cache{
		Items: make(map[string]int64),
		path:  path,
	}

	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &c.Items); err != nil {
			// If corrupted, just return empty cache, or maybe error?
			// Ignoring error is safer to recover.
		}
	} else if os.IsNotExist(err) {
		// Fine, new cache
	} else {
		return nil, err
	}

	return c, nil
}

// Check returns true if the file is cached and the modtime matches.
func (c *Cache) Check(cacheKey, filePath string) (bool, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return false, err
	}

	stat, err := os.Stat(absPath)
	if err != nil {
		return false, err
	}

	c.mu.RLock()
	cachedTime, ok := c.Items[cacheKey]
	c.mu.RUnlock()

	if !ok {
		return false, nil
	}

	if stat.ModTime().Unix() == cachedTime {
		return true, nil
	}

	return false, nil
}

// Update adds or updates a file in the cache.
func (c *Cache) Update(cacheKey, filePath string) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return err
	}

	stat, err := os.Stat(absPath)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.Items[cacheKey] = stat.ModTime().Unix()
	c.mu.Unlock()
	return nil
}

// Save writes the cache to disk
func (c *Cache) Save() error {
	c.mu.RLock()
	data, err := json.MarshalIndent(c.Items, "", "  ")
	c.mu.RUnlock()
	if err != nil {
		return err
	}

	return os.WriteFile(c.path, data, 0644)
}
