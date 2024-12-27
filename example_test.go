package snap_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/jellevandenhooff/snap"
)

func complicated(s string) string {
	b := sha256.Sum256([]byte(s))
	return hex.EncodeToString(b[:16])
}

func TestSnapshot(t *testing.T) {
	actual := complicated("complicated value")
	expected := snap.Source(`2d5a34155bd3feb0728c3198c41250db`)
	snap.CheckString(t, actual, expected)
}

func TestMain(m *testing.M) {
	snap.Run(m)
}
