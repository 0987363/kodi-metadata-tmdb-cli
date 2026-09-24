package utils

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func SaveNfo(file string, value any) error {
	if file == "" {
		return errors.New("NFO 输出路径为空")
	}
	data, err := xml.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append([]byte(xml.Header), data...)
	if existing, err := os.ReadFile(file); err == nil && bytes.Equal(existing, data) {
		return nil
	}
	return WriteFileAtomic(file, bytes.NewReader(data), 0644)
}

// WriteFileAtomic 仅在内容全部写入并关闭成功后替换目标，失败保留原文件。
func WriteFileAtomic(file string, content io.Reader, mode fs.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(file), ".metadata-write-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := io.Copy(f, content); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, file)
}
