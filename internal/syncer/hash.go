package syncer

import (
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/zeebo/blake3"
)

const hashPrefix = "b3:"

// HashFile считает blake3-хеш и возвращает строку "b3:<hex>".
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	h := blake3.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hashPrefix + hex.EncodeToString(h.Sum(nil)), nil
}
