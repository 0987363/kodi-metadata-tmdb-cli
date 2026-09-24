package utils

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveNfoRejectsEmptyDestination(t *testing.T) {
	if err := SaveNfo("", struct {
		XMLName xml.Name `xml:"movie"`
	}{}); err == nil {
		t.Fatal("空输出路径必须报错")
	}
}
func TestSaveNfoPreservesExistingOnMarshalFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.nfo")
	if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SaveNfo(path, make(chan int)); err == nil {
		t.Fatal("无法序列化的对象必须报错")
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "original" {
		t.Fatalf("破坏了原文件：%s %v", b, err)
	}
}
