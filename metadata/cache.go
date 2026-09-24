package metadata

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fengqi/kodi-metadata-tmdb-cli/utils"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const cacheVersion = 3

type cachedValue struct {
	Version int             `json:"version"`
	SavedAt time.Time       `json:"saved_at"`
	Value   json.RawMessage `json:"value"`
}

func cacheFile(root string, p Provider, operation string, req Request) string {
	key, _ := json.Marshal(struct {
		Version          int
		Scope, Operation string
		Request          Request
	}{cacheVersion, p.Scope(), operation, req})
	digest := sha256.Sum256(key)
	return filepath.Join(root, "cache", p.Name(), fmt.Sprintf("%x.json", digest))
}
func readCache[T any](file string, ttl time.Duration) (T, bool, error) {
	var value T
	if ttl <= 0 {
		return value, false, nil
	}
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return value, false, nil
	}
	if err != nil {
		return value, false, err
	}
	var stored cachedValue
	if err := json.Unmarshal(data, &stored); err != nil {
		return value, false, fmt.Errorf("缓存 %s 无法解析: %w", file, err)
	}
	if stored.Version != cacheVersion || time.Since(stored.SavedAt) >= ttl || stored.SavedAt.After(time.Now()) {
		return value, false, nil
	}
	if err := json.Unmarshal(stored.Value, &value); err != nil {
		return value, false, fmt.Errorf("缓存 %s 内容无效: %w", file, err)
	}
	return value, true, nil
}
func writeCache[T any](file string, ttl time.Duration, value T) error {
	if ttl <= 0 {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(cachedValue{Version: cacheVersion, SavedAt: time.Now(), Value: data})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		return err
	}
	return utils.WriteFileAtomic(file, bytes.NewReader(encoded), 0644)
}
