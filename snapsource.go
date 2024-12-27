package snapsource

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/scanner"
	"go/token"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

var rewriteSnapsource bool

// Run should be called from TestMain (see [testing.M]) to run tests and
// optionally rewrite snapsource snapshots.
func Run(m *testing.M) {
	flag.BoolVar(&rewriteSnapsource, "rewrite-snapsource", false, "rewrite snapsource snapshots")
	flag.Parse()
	code := m.Run()
	if rewriteSnapsource {
		if err := globalShots.rewrite(); err != nil {
			log.Fatalf("rewriting snapsource: %s", err)
		}
	}
	os.Exit(code)
}

type location struct {
	file string
	line int
}

func (l *location) String() string {
	return fmt.Sprintf("%s:%d", l.file, l.line)
}

type shots struct {
	byLocation map[location]*Snapshot
}

var globalShots = &shots{
	byLocation: make(map[location]*Snapshot),
}

func format(s string) string {
	replaced := strings.ReplaceAll(s, "\n", "")
	if strconv.CanBackquote(replaced) {
		return "`" + s + "`"
	}
	return strconv.Quote(s)
}

func (s *shots) rewrite() error {
	byFile := make(map[string][]*Snapshot)
	for _, shot := range s.byLocation {
		byFile[shot.location.file] = append(byFile[shot.location.file], shot)
	}

	for path, shots := range byFile {
		source, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("could not read file %s: %s", path, err)
		}

		byLine := make(map[int]*Snapshot)
		for _, shot := range shots {
			byLine[shot.location.line] = shot
		}

		fs := token.NewFileSet()
		f := fs.AddFile(path, -1, len(source))

		var parseError error

		var sc scanner.Scanner
		sc.Init(f, source, func(pos token.Position, msg string) {
			parseError = fmt.Errorf("failed to parse %s: %s", pos, msg)
		}, scanner.ScanComments)

		var out bytes.Buffer

		step := 0
		prevEndOffset := 0

		for {
			pos, tok, lit := sc.Scan()
			if parseError != nil {
				return parseError
			}

			if tok == token.EOF {
				break
			}

			startOffset := f.Offset(pos)
			endOffset := startOffset
			if lit == "" {
				endOffset += len(tok.String())
			} else {
				endOffset += len(lit)
			}

			match := false
			switch {
			case tok == token.IDENT && lit == "snapsource":
				step = 1

			case step == 1 && tok == token.PERIOD:
				step = 2

			case step == 2 && tok == token.IDENT && lit == "S":
				step = 3

			case step == 3 && tok == token.LPAREN:
				step = 4

			case step == 4 && tok == token.STRING:
				match = true
				step = 0

			default:
				step = 0
			}

			line := f.Line(pos)

			if match {
				// we might not have a match if this test was skipped
				if shot, ok := byLine[line]; ok && shot.hasActual {
					lit = format(shot.actual)
					shot.rewritten = true
				}
			}

			// copy all skipped junk
			out.Write(source[prevEndOffset:startOffset])

			if lit != "" {
				out.WriteString(lit)
			} else {
				out.WriteString(tok.String())
			}

			prevEndOffset = endOffset
		}

		for _, shot := range shots {
			if shot.hasActual && !shot.rewritten {
				return fmt.Errorf("failed to rewrite shot at location %s; somehow failed to find it while scanning", shot.location.String())
			}
		}

		if !bytes.Equal(out.Bytes(), source) {
			if err := os.WriteFile(path, out.Bytes(), 0644); err != nil {
				return fmt.Errorf("error writing file %s: %s", path, err)
			}
		}
	}

	return nil
}

func (s *shots) register(shot *Snapshot) {
	if _, ok := s.byLocation[shot.location]; ok {
		panic(fmt.Sprintf("two snapshots with the same location: %v", shot.location))
	}

	s.byLocation[shot.location] = shot
}

// A Snapshot is an expected value in a test. Snapshots must be created by
// calling [S] and must be checked with a call to function like [CheckString].
// Each [Snapshot] can be used only once.
type Snapshot struct {
	location location
	expected string

	hasActual bool
	actual    string
	rewritten bool
}

// CheckString compares a string against a snapshot.
func CheckString(t *testing.T, actual string, s *Snapshot) {
	t.Helper()
	if !rewriteSnapsource && s.expected != actual {
		t.Errorf("snapshot different; expected %q, got %q", s.expected, actual)
	}
	if s.hasActual {
		t.Errorf("snapshot compare invoked more than once")
	}
	s.hasActual = true
	s.actual = actual
}

// CompareString compares a JSON-marshaled value against a snapshot.
func CheckJSON(t *testing.T, actual any, s *Snapshot) {
	t.Helper()

	b, err := json.MarshalIndent(actual, "", "  ")
	if err != nil {
		t.Errorf("could not marshal json: %s", err)
	}
	CheckString(t, string(b), s)
}

// S creates a [Snapshot]. The argument to [S] will be automatically
// updated in the source code.
func S(input string) *Snapshot {
	_, file, line, ok := runtime.Caller(1)
	if !ok {
		panic("could not find caller")
	}

	shot := &Snapshot{
		location: location{
			file: file,
			line: line,
		},
		expected: input,
	}
	globalShots.register(shot)

	return shot
}
