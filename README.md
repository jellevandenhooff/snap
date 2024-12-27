# Snap: Source-based snapshot tests for Go
[![Go Reference](https:pkg.go.dev/badge/github.com/jellevandenhooff/snap.svg)](https://pkg.go.dev/github.com/jellevandenhooff/snap)

Snap is a package for automatically updating source-based snapshots tests.
A common pattern for Go tests is to store expected outputs in a "golden"
file in a testdata/ directory. That is quite convenient for storing and tracking
complicated values. But wouldn't it be nice if the expected value was
right there in the source code? That is what snap supports.

If you have a test like the following which computes and compares a sha256
hash, it can be annoying to write the correct expected value in the source:
```go
package example_test

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
    expected := snap.Source("")
	snap.CheckString(t, actual, expected)
}

func TestMain(m *testing.M) {
	snap.Run(m)
}
```

If you test this with "go test" you get an error saying the expected value is
wrong:
```
> go test
=== RUN   TestSnapshot
    example_test.go:19: snapshot different; expected "", got "2d5a34155bd3feb0728c3198c41250db"
--- FAIL: TestSnapshot (0.00s)
...
```

To fix the test we can either copy and paste this expected string... or we
can update the snapshots using snap:
```
> go test -update-snapshots
PASS
```

Now the test has been updated to include:
```go
	expected := snap.Source(`2d5a34155bd3feb0728c3198c41250db`)
```

And tests pass:
```
go test
PASS
```
