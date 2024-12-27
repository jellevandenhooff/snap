package snap_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/rogpeppe/go-internal/gotooltest"
	"github.com/rogpeppe/go-internal/testscript"
	"golang.org/x/mod/modfile"
)

var updateGolden = flag.Bool("update-golden", false, "update golden files in test scripts")

func TestScript(t *testing.T) {
	p := testscript.Params{
		Dir:           "testdata",
		UpdateScripts: *updateGolden,
	}
	gotooltest.Setup(&p)

	// make a go.mod for the test that points at the current directory:
	modDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %s", err)
	}
	modPath := filepath.Join(modDir, "go.mod")
	modBytes, err := os.ReadFile(modPath)
	if err != nil {
		t.Fatalf("os.ReadFile: %s", err)
	}
	mod, err := modfile.Parse(modPath, modBytes, nil)
	if err != nil {
		t.Fatalf("modfile.Parse: %s", err)
	}
	mod.AddModuleStmt("github.com/jellevandenhooff/snap/example")
	mod.AddRequire("github.com/jellevandenhooff/snap", "v0.0.0")
	mod.AddReplace("github.com/jellevandenhooff/snap", "", modDir, "")
	modBytes, err = mod.Format()
	if err != nil {
		t.Fatalf("mod.Format: %s", err)
	}

	origSetup := p.Setup
	p.Setup = func(e *testscript.Env) error {
		if err := origSetup(e); err != nil {
			return err
		}

		// put the fake go.mod in the work directory
		if err := os.WriteFile(filepath.Join(e.WorkDir, "go.mod"), modBytes, 0644); err != nil {
			t.Fatal(err)
		}

		return nil
	}

	// run tests
	testscript.Run(t, p)
}
