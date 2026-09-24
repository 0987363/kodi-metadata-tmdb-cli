package metadata

import (
	"crypto/sha256"
	"encoding/hex"
)

// LocalRef 标识本地对象而非网站记录，identity 由调用方的规范路径及明确坐标组成。
func LocalRef(kind Kind, identity string) Ref {
	digest := sha256.Sum256([]byte(string(kind) + "\x00" + identity))
	return Ref{Provider: "local", Kind: kind, ID: hex.EncodeToString(digest[:])}
}
