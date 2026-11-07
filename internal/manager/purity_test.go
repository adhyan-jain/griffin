package manager

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Managers are brains: they route, interpret, plan and verify. They never do labor. This test enforces that
// structurally: the manager package may not import anything that can run a provider, start a process, reach
// the network or touch the file system. If it ever needs one, the work belongs in a worker instead.
func TestManagersAreBrainsNotWorkers(t *testing.T) {
	forbidden := []string{
		"github.com/griffin/core/internal/worker",
		"github.com/griffin/core/internal/provider",
		"github.com/griffin/core/internal/runner",
		"github.com/griffin/core/internal/storage",
		"os/exec",
		"net/http",
		"net",
		"os",
		"io/ioutil",
		"syscall",
	}

	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no source files found: %v", err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		checked++
		for _, imp := range parsed.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			for _, bad := range forbidden {
				if path == bad || strings.HasPrefix(path, bad+"/") {
					t.Errorf("%s imports %q: managers must not do labor; put that work in a worker", f, path)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no non-test files were checked")
	}
}
