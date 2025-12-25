package hashx

import (
	"crypto/sha256"
	"encoding/hex"
)

func Sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
