package loop

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoopMustNotImportCmdOrUI is the M8 build-time architectural rule: the
// core (internal/*) must never depend on a frontend (cmd/*) nor on UI
// libraries (e.g. bubbletea). The M9 TUI lives entirely in cmd/ancientcode; a
// future web UI may add transports without touching this package.
func TestLoopMustNotImportCmdOrUI(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, e.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(p, "ancientcode/cmd/") {
				t.Errorf("%s imports frontend %q — core must never depend on a delivery layer", e.Name(), p)
			}
			if strings.Contains(p, "charmbracelet") {
				t.Errorf("%s imports UI library %q — bubbletea is delivery-only (D-36)", e.Name(), p)
			}
		}
	}
}

var _ = filepath.Join // keep filepath import stable for tooling
