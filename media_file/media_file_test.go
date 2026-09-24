package media_file

import "testing"

func TestNewMediaFileKeepsPhysicalTypeOnly(t *testing.T) {
	for _, name := range []string{"Movie.sample.mkv", "movie-trailer.mp4", "clip.mp4", "VTS_01_1.VOB"} {
		path := "/library/" + name
		if name == "clip.mp4" {
			path = "/library/extras/clip.mp4"
		}
		mf := NewMediaFile(path, name)
		if mf == nil || mf.MediaType != VIDEO {
			t.Fatalf("视频命名不应决定内容角色: %q %+v", name, mf)
		}
	}
	if mf := NewMediaFile("/library/.hidden", ".hidden"); mf != nil {
		t.Fatalf("隐藏文件应排除: %+v", mf)
	}
	if mf := NewMediaFile("/library/movie.nfo", "movie.nfo"); mf == nil || !mf.IsNFO() || mf.PathWithoutSuffix() != "/library/movie" {
		t.Fatalf("NFO 物理类型或路径异常: %+v", mf)
	}
}

func TestDiscDirectoriesRemainPhysicalUnits(t *testing.T) {
	for _, name := range []string{"BDMV", "VIDEO_TS", "HDVD_TS", "DVD"} {
		mf := NewMediaFile("/library/Film/"+name, name)
		if mf == nil || !mf.IsDisc() {
			t.Fatalf("原盘目录未识别: %s %+v", name, mf)
		}
	}
	if !NewMediaFile("/library/movie.mkv", "movie.mkv").IsVideo() {
		t.Fatal("普通视频未识别")
	}
}
