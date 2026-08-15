package idempotency

import (
	"crypto/sha256"
	"encoding/hex"
)

func Key(taskID, toolName, callKey string) string {
	h := sha256.New()
	h.Write([]byte(taskID))
	h.Write([]byte{0})
	h.Write([]byte(toolName))
	h.Write([]byte{0})
	h.Write([]byte(callKey))
	return hex.EncodeToString(h.Sum(nil))
}
