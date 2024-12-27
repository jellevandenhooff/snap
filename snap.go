// Package snap supports source-based snapshot testing. Package snap maintains
// expected or "golden" values in _test.go files and can update the expected
// value by rewriting the source.
//
// To make a snapshot test, create a snapshot with [Source] and test the
// snapshot with a Check function such as [CheckString]. Then, call [Run] in a
// TestMain function and run tests with "go test -update-snapshots" to update
// snapshots.
//
// As an example, this is a complete test file with [Source], [CheckString],
// and [Run]. The snapshot is out of date: It should mention "complicated
// value" but instead it is "old".
//
//	package example_test
//
//	import (
//	   "testing"
//	   "github.com/jellevandenhooff/snap"
//	)
//
//	func TestSnapshot(t *testing.T) {
//		magic := "complicated value"
//		snap.CheckString(t, magic, snap.Source("old"))
//	}
//
//	func TestMain(m *testing.M) {
//		snap.Run(m)
//	}
//
// Running "go test" will fail because the snapshot is out of date.
// Running "go test -update-snapshots" will update the snapshot in the file to
// "snap.Source(`complicated value`)" and afterwards tests will pass.
package snap

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/scanner"
	"go/token"
	"io"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

var updateSnapshots bool

// Run should be called from TestMain (see [testing.M]) to run tests and
// optionally update snapshots with the "go test -update-snapshots" flag.
//
// Any snapshots in skipped tests will be kept as-is.
func Run(m *testing.M) {
	flag.BoolVar(&updateSnapshots, "update-snapshots", false, "update snapshots")
	flag.Parse()
	code := m.Run()
	if updateSnapshots {
		if err := globalShots.update(); err != nil {
			log.Fatalf("updating snapshots: %s", err)
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
	mu         sync.Mutex
	byLocation map[location]*Snapshot
}

var globalShots = &shots{
	byLocation: make(map[location]*Snapshot),
}

func format(s string, indent int) string {
	replaced := strings.ReplaceAll(s, "\n", "")
	if strconv.CanBackquote(replaced) {
		if indent > 0 {
			var builder strings.Builder
			builder.WriteString("`")
			prefix := strings.Repeat("\t", indent)
			lines := strings.Split(s, "\n")
			for i, line := range lines {
				if i > 0 {
					builder.WriteString("\n")
					builder.WriteString(prefix)
				}
				builder.WriteString(line)
			}
			builder.WriteString("`")
			return builder.String()
		}
		return "`" + s + "`"
	}
	return strconv.Quote(s)
}

type tok struct {
	token   token.Token
	literal string
	line    int
	offset  int
}

func (s *shots) updateFile(file string) error {
	// update a file to include new snapshot values. We scan the file using the
	// built-in go/scanner and looks for tokens indicating a call to
	// "snap.Source(". Between calls to snap.Source( we copy all bytes
	// verbatim to not mess up any existing comments.

	// read the file and a create a scanner
	source, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("could not read file %s: %s", file, err)
	}
	fs := token.NewFileSet()
	f := fs.AddFile(file, -1, len(source))
	var sc scanner.Scanner
	var parseError error
	sc.Init(f, source, func(pos token.Position, msg string) {
		parseError = fmt.Errorf("failed to parse %s: %s", pos, msg)
	}, scanner.ScanComments)

	nextToken := func() (tok, error) {
		pos, kind, lit := sc.Scan()
		if parseError != nil {
			return tok{}, parseError
		}
		return tok{
			token:   kind,
			literal: lit,
			line:    f.Line(pos),
			offset:  f.Offset(pos),
		}, nil
	}

	// processing state:

	// modified source
	var out bytes.Buffer
	// last offset copied
	prevOffset := 0

	// tokens we are looking for
	match := [4]tok{
		{token: token.IDENT, literal: "snap"},
		{token: token.PERIOD},
		{token: token.IDENT, literal: "Source"},
		{token: token.LPAREN},
	}
	// recently read tokens
	var recent [len(match)]tok

	for {
		next, err := nextToken()
		if err != nil {
			return err
		}
		if next.token == token.EOF {
			break
		}

		copy(recent[0:], recent[1:])
		recent[len(recent)-1] = tok{token: next.token, literal: next.literal} // drop source info

		if recent != match {
			continue
		}

		// found snapshot; only update if we have a single new value from a test
		line := next.line
		shot, ok := s.byLocation[location{file: file, line: line}]
		if !ok || !shot.hasActual || shot.calledMultiple {
			continue
		}

		// mark the shot as updated
		shot.updated = true

		// maybe figure out indentation to add
		indent := 0
		if shot.indentOk {
			// scan backwards until we find a newline; then count tabs
			offset := next.offset
			for offset > 0 {
				r, n := utf8.DecodeLastRune(source[:offset])
				if r == '\n' {
					break
				}
				offset -= n
			}
			if offset+indent < len(source) && source[offset+indent] == '\t' {
				indent++
			}
		}

		// format value for source
		formatted := format(shot.actual, indent)

		// copy all non-snapshot code verbatim
		out.Write(source[prevOffset:next.offset])

		// write the new value
		out.WriteString("(")
		out.WriteString(formatted)
		out.WriteString(")")

		// consume everything inside the parenthesis
		depth := 1
		for depth != 0 {
			next, err = nextToken()
			if err != nil {
				return err
			}
			if next.token == token.EOF {
				return io.ErrUnexpectedEOF
			}
			switch next.token {
			case token.LPAREN:
				depth++
			case token.RPAREN:
				depth--
			}
		}
		prevOffset = next.offset + 1
	}

	// copy all non-snapshot code verbatim
	out.Write(source[prevOffset:])

	// update file if necessary
	if !bytes.Equal(out.Bytes(), source) {
		if err := os.WriteFile(file, out.Bytes(), 0644); err != nil {
			return fmt.Errorf("error writing file %s: %s", file, err)
		}
	}

	return nil
}

func (s *shots) update() error {
	files := make(map[string]struct{})
	for _, shot := range s.byLocation {
		files[shot.location.file] = struct{}{}
	}

	// update snapshots file-by-file
	for file := range files {
		if err := s.updateFile(file); err != nil {
			return err
		}
	}

	for _, shot := range s.byLocation {
		if shot.hasActual && !shot.updated {
			return fmt.Errorf("failed to update snapshot at location %s; somehow failed to find it", shot.location.String())
		}
	}

	return nil
}

func (s *shots) register(shot *Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.byLocation[shot.location]; ok {
		fmt.Printf("bad test: two calls to snap.Source on the same line %s; only one call per line is supported\n", shot.location.String())
		os.Exit(1)
	}

	s.byLocation[shot.location] = shot
}

// A Snapshot is an expected value in a test created by calling [Source].
// See [Source] on how to use Snapshots.
type Snapshot struct {
	location location
	expected string

	hasActual      bool
	actual         string
	indentOk       bool
	updated        bool
	calledMultiple bool
}

func trimLines(a string) string {
	lines := strings.Split(a, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return strings.Join(lines, "")
}

// CheckString compares a string with a snapshot.
func CheckString(t *testing.T, actual string, expected *Snapshot) {
	t.Helper()

	if !updateSnapshots {
		var ok bool
		if !expected.indentOk {
			ok = expected.expected == actual
		} else {
			ok = trimLines(expected.expected) == trimLines(actual)
		}
		if !ok {
			t.Errorf("snapshot different; expected %q, got %q", expected.expected, actual)
		}
	}
	if expected.hasActual {
		t.Errorf("snapshot compare invoked more than once")
		expected.calledMultiple = true
	}
	expected.hasActual = true
	expected.actual = actual
}

// CompareString compares a JSON-marshaled with a snapshot.
func CheckJSON(t *testing.T, actual any, expected *Snapshot) {
	t.Helper()

	b, err := json.MarshalIndent(actual, "", "\t")
	if err != nil {
		t.Errorf("could not marshal json: %s", err)
	}
	expected.indentOk = true
	CheckString(t, string(b), expected)
}

// Source creates a automatically-updated [Snapshot].
//
// A [Snapshot] must be passed to a Check function like [CheckJSON] to be
// tested. Each [Snapshot] can be used only once.
//
// The argument to [Source] can be automatically updated against actual values
// using the "go test -update-snapshots" flag; see [Run].
//
// Only one call to [Source] per line is supported.
func Source(input string) *Snapshot {
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
