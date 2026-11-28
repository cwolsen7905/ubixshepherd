package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// seedHome runs the container entrypoint's seeding against an empty home, with the seed files
// from deploy/docker, and returns the config.yaml it writes.
func seedHome(t *testing.T, env ...string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the entrypoint is a POSIX shell script")
	}
	seeds, err := filepath.Abs(filepath.Join("..", "..", "deploy", "docker"))
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	cmd := exec.Command("sh", filepath.Join(seeds, "entrypoint.sh"), "true")
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "SHEPHERD_HOME=" + home, "SHEPHERD_SEED_DIR=" + seeds}, env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("entrypoint: %v\n%s", err, out)
	}
	return filepath.Join(home, "config.yaml")
}

// loadSeed loads a seeded home with the real loader: a seed that names a key the loader does not
// know fails here rather than at a pod's first start.
func loadSeed(t *testing.T, env ...string) Config {
	t.Helper()
	cfg, err := Load(seedHome(t, env...))
	if err != nil {
		t.Fatalf("the seeded config does not load: %v", err)
	}
	return cfg
}

func TestWorkerSeedLoads(t *testing.T) {
	url := "https://controller.example.com"
	cfg := loadSeed(t, "SHEPHERD_CONTROLLER_URL="+url)
	if cfg.Cluster.Controller != url {
		t.Fatalf("cluster.controller = %q, want %q", cfg.Cluster.Controller, url)
	}
}

func TestControllerSeedLoads(t *testing.T) {
	cfg := loadSeed(t, "SHEPHERD_PUBLIC_URL=https://shepherd.example.com", "SHEPHERD_TRUSTED_PROXIES=10.0.0.0/8")
	p := cfg.Daemon.Public
	if p == nil || p.URL != "https://shepherd.example.com" || len(p.TrustedProxies) != 1 || p.TrustedProxies[0] != "10.0.0.0/8" {
		t.Fatalf("daemon.public = %+v", p)
	}
}

func TestSeedValuesAreCopiedAsWritten(t *testing.T) {
	// | & and \ end or change a sed replacement unless the entrypoint escapes them
	odd := `https://a.example.com/x|y&z\w`
	for _, env := range [][]string{
		{"SHEPHERD_CONTROLLER_URL=" + odd},
		{"SHEPHERD_PUBLIC_URL=" + odd, "SHEPHERD_TRUSTED_PROXIES=" + odd},
	} {
		b, err := os.ReadFile(seedHome(t, env...))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), " "+odd+"\n") && !strings.Contains(string(b), "["+odd+"]") {
			t.Errorf("%s was not copied as written:\n%s", env[0], b)
		}
	}
}
