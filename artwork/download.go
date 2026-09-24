package artwork

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"fengqi/kodi-metadata-tmdb-cli/utils"
)

// Download 以 URL 记录图片来源，只有同来源的完整文件才能跳过下载。
func Download(ctx context.Context, client *http.Client, url, dest string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if url == "" {
		return nil
	}
	marker := filepath.Join(filepath.Dir(dest), ".metadata", "artwork", filepath.Base(dest)+".url")
	if info, err := os.Stat(dest); err == nil && !info.IsDir() && info.Size() > 0 {
		if data, err := os.ReadFile(marker); err == nil && string(data) == url {
			return nil
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "kodi-metadata-cli/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("图片下载状态码 %d", resp.StatusCode)
	}
	const maxSize = 32 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return err
	}
	if len(data) == 0 || len(data) > maxSize {
		return errors.New("图片内容为空或超过 32 MiB")
	}
	if err := utils.WriteFileAtomic(dest, bytes.NewReader(data), 0644); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0755); err != nil {
		return err
	}
	return utils.WriteFileAtomic(marker, bytes.NewBufferString(url), 0644)
}

// Downloader 接收每个来源的客户端，输出流程只消费已解析的完整图片地址。
type Downloader struct{ Clients map[string]*http.Client }

func (d *Downloader) Save(ctx context.Context, source string, items []Item) error {
	if len(items) == 0 {
		return nil
	}
	if d == nil {
		return errors.New("图片下载器未初始化")
	}
	client := d.Clients[source]
	if client == nil {
		return errors.New("图片来源客户端未配置")
	}
	selected := make(map[string]bool)
	for _, item := range items {
		if item.URL == "" || selected[item.Path] {
			continue
		}
		selected[item.Path] = true
		if err := Download(ctx, client, item.URL, item.Path); err != nil {
			return fmt.Errorf("保存图片 %s: %w", item.Path, err)
		}
	}
	return nil
}

type Item struct {
	URL  string
	Path string
}
