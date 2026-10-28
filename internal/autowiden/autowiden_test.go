package autowiden

import (
	"strings"
	"testing"
)

func base() Input {
	return Input{
		Mode:    "auto",
		Files:   []string{"internal/foo/helper.go"},
		Scope:   []string{"internal/foo/foo.go"},
		Changed: []string{"internal/foo/foo.go"},
	}
}

func TestDecide(t *testing.T) {
	tests := []struct {
		name   string
		mut    func(*Input)
		ok     bool
		reason string
	}{
		{"same directory", func(in *Input) {}, true, ""},
		{"human mode", func(in *Input) { in.Mode = "human" }, false, "does not auto-widen"},
		{"unset mode", func(in *Input) { in.Mode = "" }, false, "widen: human"},
		{"no files", func(in *Input) { in.Files = nil }, false, "no files"},
		{"glob", func(in *Input) { in.Files = []string{"internal/foo/*.go"} }, false, "pattern"},
		{"double star", func(in *Input) { in.Files = []string{"internal/**"} }, false, "pattern"},
		{"directory", func(in *Input) { in.Files = []string{"internal/foo/"} }, false, "directory"},
		{"absolute", func(in *Input) { in.Files = []string{"/etc/passwd"} }, false, "not a path inside"},
		{"escapes", func(in *Input) { in.Files = []string{"../x.go"} }, false, "not a path inside"},
		{"unclean", func(in *Input) { in.Files = []string{"internal/foo/../foo/x.go"} }, false, "not a clean"},
		{"dot slash is fine", func(in *Input) { in.Files = []string{"./internal/foo/x.go"} }, true, ""},
		{"already in scope", func(in *Input) { in.Files = []string{"internal/foo/foo.go"} }, false, "already in scope"},
		{"three files", func(in *Input) {
			in.Files = []string{"internal/foo/a.go", "internal/foo/b.go", "internal/foo/c.go"}
		}, true, ""},
		{"four files", func(in *Input) {
			in.Files = []string{"internal/foo/a.go", "internal/foo/b.go", "internal/foo/c.go", "internal/foo/d.go"}
		}, false, "more than the 3"},
		{"max raised", func(in *Input) {
			in.MaxFiles = 4
			in.Files = []string{"internal/foo/a.go", "internal/foo/b.go", "internal/foo/c.go", "internal/foo/d.go"}
		}, true, ""},
		{"other directory", func(in *Input) { in.Files = []string{"internal/bar/x.go"} }, false, "not near"},
		{"test in tests directory", func(in *Input) { in.Files = []string{"internal/foo/__tests__/foo.test.go"} }, true, ""},
		{"go test file", func(in *Input) { in.Files = []string{"internal/foo/foo_test.go"} }, true, ""},
		{"tests directory ts", func(in *Input) {
			in.Scope, in.Changed = []string{"web/src/a.ts"}, []string{"web/src/a.ts"}
			in.Files = []string{"web/src/__tests__/a.test.ts"}
		}, true, ""},
		{"python test", func(in *Input) {
			in.Scope, in.Changed = []string{"pkg/a.py"}, []string{"pkg/a.py"}
			in.Files = []string{"pkg/test_a.py"}
		}, true, ""},
		{"glob scope with no changes anchors nothing", func(in *Input) {
			in.Scope, in.Changed = []string{"internal/foo/**"}, nil
			in.Files = []string{"internal/foo/deep/y.go"}
		}, false, "already in scope"},
		{"changed file outside scope is not an anchor", func(in *Input) {
			in.Changed = []string{"internal/bar/stray.go"}
			in.Files = []string{"internal/bar/x.go"}
		}, false, "not near"},
		{"protected ci", func(in *Input) {
			in.Scope, in.Changed = []string{"internal/foo/foo.go", ".gitlab/x"}, nil
			in.Files = []string{".gitlab-ci.yml"}
		}, false, "protected"},
		{"protected go.mod", func(in *Input) {
			in.Files = []string{"internal/foo/go.mod"}
		}, false, "protected"},
		{"protected root go.sum", func(in *Input) {
			in.Scope, in.Changed = []string{"go.work"}, nil
			in.Files = []string{"go.sum"}
		}, false, "protected"},
		{"protected github", func(in *Input) {
			in.Scope, in.Changed = []string{".github/x.md"}, nil
			in.Files = []string{".github/workflows/ci.yml"}
		}, false, "protected"},
		{"protected migration", func(in *Input) {
			in.Scope, in.Changed = []string{"db/migrations/001.sql"}, []string{"db/migrations/001.sql"}
			in.Files = []string{"db/migrations/002.sql"}
		}, false, "protected"},
		{"protected env", func(in *Input) {
			in.Scope, in.Changed = []string{"app/main.go"}, []string{"app/main.go"}
			in.Files = []string{"app/.env"}
		}, false, "protected"},
		{"protected lockfile", func(in *Input) {
			in.Scope, in.Changed = []string{"web/src/a.ts"}, []string{"web/src/a.ts"}
			in.Files = []string{"web/src/yarn.lock"}
		}, false, "protected"},
		{"protected case", func(in *Input) {
			in.Scope, in.Changed = []string{"docs/a.md"}, []string{"docs/a.md"}
			in.Files = []string{"docs/work.md"}
		}, false, "protected"},
		{"repo protected", func(in *Input) {
			in.Protected = []string{"internal/foo/helper.go"}
		}, false, "protected"},
		{"protected makefile", func(in *Input) { in.Files = []string{"internal/foo/Makefile"} }, false, "protected"},
		{"protected scripts", func(in *Input) {
			in.Scope, in.Changed = []string{"scripts/a.sh"}, []string{"scripts/a.sh"}
			in.Files = []string{"scripts/b.sh"}
		}, false, "protected"},
		{"protected bin", func(in *Input) {
			in.Scope, in.Changed = []string{"bin/a.sh"}, []string{"bin/a.sh"}
			in.Files = []string{"bin/promote.sh"}
		}, false, "protected"},
		{"new file under another lane's glob", func(in *Input) {
			in.Others = [][]string{{"internal/foo/**"}}
			in.RepoFiles = nil
		}, false, "overlaps"},
		{"cap reached", func(in *Input) { in.Used = 3 }, false, "used its 3"},
		{"below cap", func(in *Input) { in.Used = 2 }, true, ""},
		{"cap raised", func(in *Input) { in.Used, in.Cap = 3, 5 }, true, ""},
		{"overlap with other lane", func(in *Input) {
			in.Others = [][]string{{"internal/foo/helper.go"}}
		}, false, "overlaps"},
		{"overlap by glob", func(in *Input) {
			in.Others = [][]string{{"internal/foo/**"}}
			in.RepoFiles = []string{"internal/foo/helper.go"}
		}, false, "overlaps"},
		{"unrelated other lane", func(in *Input) {
			in.Others = [][]string{{"web/**"}}
		}, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := base()
			tc.mut(&in)
			v := Decide(in)
			if v.OK != tc.ok {
				t.Fatalf("OK = %v (%s), want %v", v.OK, v.Reason, tc.ok)
			}
			if !tc.ok && !strings.Contains(v.Reason, tc.reason) {
				t.Errorf("reason %q lacks %q", v.Reason, tc.reason)
			}
			if tc.ok && (len(v.Files) == 0 || len(v.Checks) == 0) {
				t.Errorf("ok verdict without files or checks: %+v", v)
			}
		})
	}
}

func TestDecideAddsOnlyWhatIsOutside(t *testing.T) {
	in := base()
	in.Files = []string{"internal/foo/foo.go", "internal/foo/helper.go"}
	v := Decide(in)
	if !v.OK || len(v.Files) != 1 || v.Files[0] != "internal/foo/helper.go" {
		t.Errorf("verdict = %+v", v)
	}
}

func TestSecretGlobsAreNarrow(t *testing.T) {
	for file, want := range map[string]bool{
		"internal/secrets/key.go": true, "config/secret": true, "config/secret.yaml": true,
		"config/secrets.json": true, "internal/secretary.go": false, "lib/secretion.py": false,
	} {
		if got := protectedBy(Denylist, file) != nil; got != want {
			t.Errorf("%s protected = %v, want %v", file, got, want)
		}
	}
}

func TestDenylistCoversSensitivePaths(t *testing.T) {
	for _, f := range []string{
		".gitlab-ci.yml", ".github/workflows/ci.yml", ".gitlab/issue_templates/a.md", ".git/config", "sub/.git/hooks/pre-push",
		".githooks/pre-push", ".husky/pre-commit", ".env", "app/.env.local", "certs/server.pem", "certs/tls.key",
		"home/id_rsa", "home/id_rsa.pub", "config/app-secret.yaml", "config/db_credentials.json", "config/credential-helper.sh",
		".npmrc", "web/.npmrc", ".netrc", "deploy/kubeconfig", "kubeconfig", "go.sum", "web/package-lock.json",
	} {
		if protectedBy(Denylist, f) == nil {
			t.Errorf("%s is not protected", f)
		}
	}
	for _, f := range []string{
		"internal/secretary.go", "internal/keyboard.go", "docs/gitlab-notes.md", "internal/gitutil/git.go",
		"internal/foo/helper.go", "web/src/monkey.ts", "docs/github.md",
	} {
		if r := protectedBy(Denylist, f); r != nil {
			t.Errorf("%s is protected by %s", f, r.Glob)
		}
	}
}

func TestDenylistRulesHaveReasons(t *testing.T) {
	for _, r := range Denylist {
		if r.Glob == "" || r.Why == "" {
			t.Errorf("rule %+v lacks a glob or a reason", r)
		}
	}
	if len(DefaultProtected) != len(Denylist) {
		t.Errorf("DefaultProtected has %d globs, Denylist %d", len(DefaultProtected), len(Denylist))
	}
}

func TestProtectedFilesAreSkippedAndNamed(t *testing.T) {
	in := base()
	in.Files = []string{"internal/foo/helper.go", "internal/foo/.env", "internal/foo/server.pem"}
	v := Decide(in)
	if !v.OK || len(v.Files) != 1 || v.Files[0] != "internal/foo/helper.go" {
		t.Fatalf("verdict = %+v", v)
	}
	if len(v.Refused) != 2 {
		t.Fatalf("refused = %+v", v.Refused)
	}
	got := v.Describe()
	for _, want := range []string{"internal/foo/.env", "**/.env", "environment file", "internal/foo/server.pem", "**/*.pem", "key file"} {
		if !strings.Contains(got, want) {
			t.Errorf("description %q lacks %q", got, want)
		}
	}
}

func TestAllProtectedDeclinesAndNamesThem(t *testing.T) {
	in := base()
	in.Files = []string{"internal/foo/.npmrc"}
	v := Decide(in)
	if v.OK || len(v.Refused) != 1 || !strings.Contains(v.Reason, "explicit widen") || !strings.Contains(v.Reason, "**/.npmrc") {
		t.Errorf("verdict = %+v", v)
	}
}

func TestRepoProtectedReasonIsNamed(t *testing.T) {
	in := base()
	in.Protected = []string{"internal/foo/helper.go"}
	v := Decide(in)
	if v.OK || len(v.Refused) != 1 || !strings.Contains(v.Refused[0].Why, "widen_protected") {
		t.Errorf("verdict = %+v", v)
	}
}
