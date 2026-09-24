package metadata

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func ReadNumericOverride(file string, allowZero bool) (string, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(data))
	number, err := strconv.Atoi(value)
	if err != nil || number < 0 || (!allowZero && number == 0) {
		return "", fmt.Errorf("指定文件 %s 中的编号或季号无效", file)
	}
	return strconv.Itoa(number), nil
}
func CombineReferences(a, b Ref) (Ref, error) {
	if a.Provider == "" {
		return b, nil
	}
	if b.Provider == "" {
		return a, nil
	}
	if a.Provider != b.Provider || a.Kind != b.Kind || a.ID != "" && b.ID != "" && a.ID != b.ID || a.Slug != "" && b.Slug != "" && a.Slug != b.Slug {
		return Ref{}, errors.New("多个用户来源指定相互冲突")
	}
	if a.ID == "" {
		a.ID = b.ID
	}
	if a.Slug == "" {
		a.Slug = b.Slug
	}
	return a, nil
}
