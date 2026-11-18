package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoQuotedAgentListInCode fails when a non-test Go file spells out the agent
// names as a quoted, in-order list again. It guards code that should read Agents;
// prose in help text and comments is not checked.
func TestNoQuotedAgentListInCode(t *testing.T) {
	root := filepath.Join("..", "..")
	re := regexp.MustCompile(`"` + strings.Join(Agents, `"\s*,\s*"`) + `"`)
	def := filepath.Join(root, "internal", "config", "config.go") // where Agents is defined
	visited := 0
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.Walk(filepath.Join(root, dir), func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || p == def {
				return nil
			}
			visited++
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if re.Match(b) {
				t.Errorf("%s spells out the agent list; read config.Agents", p)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if visited == 0 {
		t.Fatal("no Go files scanned")
	}
}
