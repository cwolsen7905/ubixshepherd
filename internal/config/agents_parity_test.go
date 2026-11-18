package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoHardCodedAgentList fails when a non-test Go file spells out the agent
// names as a list again: Agents is the one source.
func TestNoHardCodedAgentList(t *testing.T) {
	root := filepath.Join("..", "..")
	re := regexp.MustCompile(`"` + strings.Join(Agents, `"\s*,\s*"`) + `"`)
	def := filepath.Join(root, "internal", "config", "config.go") // where Agents is defined
	for _, dir := range []string{"cmd", "internal"} {
		filepath.Walk(filepath.Join(root, dir), func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || p == def {
				return nil
			}
			b, _ := os.ReadFile(p)
			if re.Match(b) {
				t.Errorf("%s spells out the agent list; read config.Agents", p)
			}
			return nil
		})
	}
}
