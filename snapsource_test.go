package snapsource_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/jellevandenhooff/snapsource"
)

func complicated(s string) string {
	b := sha256.Sum256([]byte(s))
	return hex.EncodeToString(b[:16])
}

func TestHello(t *testing.T) {
	// comment
	snapsource.CheckString(t, "ok", snapsource.S(`ok`)) /* comment */

	snapsource.CheckString(t, fmt.Sprintf("%02d", 1), snapsource.S(`01`))

	snapsource.CheckJSON(t, []string{"huh", "ok"}, snapsource.S(`[
  "huh",
  "ok"
]`))

	snapsource.CheckString(t,
		complicated("hash me please"),
		snapsource.S(`8ba5880e5fa878582c9211302e309f37`),
	)

	cases := []struct {
		a string
		b *snapsource.Snapshot
	}{
		{
			a: "hello",
			b: snapsource.S(`2cf24dba5fb0a30e26e83b2ac5b9e29e`),
		},
		{
			a: "goodbye",
			b: snapsource.S(`82e35a63ceba37e9646434c5dd412ea5`),
		},
	}

	for _, c := range cases {
		snapsource.CheckString(t, complicated(c.a), c.b)
	}
}

func TestMain(m *testing.M) {
	snapsource.Run(m)
}
