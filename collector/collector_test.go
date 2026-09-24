package collector

import (
	"context"
	"strings"
	"testing"
)

func TestRunRejectsMissingExtractor(t *testing.T) {
	if err := Run(context.Background(), nil, nil, nil); err == nil || !strings.Contains(err.Error(), "提取器") {
		t.Fatalf("未返回缺失提取器错误：%v", err)
	}
}
