package collector

import (
	"context"
	"testing"
)

func TestRunRejectsMissingExtractor(t *testing.T) {
	if err := Run(context.Background(), t.TempDir(), nil, nil, nil); err == nil {
		t.Fatal("未拒绝缺少模型实例")
	}
}
