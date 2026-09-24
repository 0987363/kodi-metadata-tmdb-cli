package metadata

import "testing"

func TestLocalRefStableAndIsolatesObjectKindsAndPaths(t *testing.T) {
	a := LocalRef(Movie, "/library/event.mkv")
	if a.Provider != "local" || a.ID == "" || a.Kind != Movie {
		t.Fatalf("本地身份错误: %+v", a)
	}
	if a != LocalRef(Movie, "/library/event.mkv") {
		t.Fatal("重复运行身份不稳定")
	}
	if a.ID == LocalRef(Show, "/library/event.mkv").ID || a.ID == LocalRef(Movie, "/other/event.mkv").ID {
		t.Fatal("不同本地对象共用了标识")
	}
}
