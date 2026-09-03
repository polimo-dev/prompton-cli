package config_test

import (
	"encoding/json"
	"github.com/polimo-dev/prompton-cli/internal/meta"
	"os"
	"path/filepath"
	"testing"

	"github.com/polimo-dev/prompton-cli/internal/config"
)

// writeConfig drops a config file at a temp path and points PTN_CONFIG at
// it, which is how every test here isolates itself from the real home dir.
func writeConfig(t *testing.T, f config.File) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.WriteFile(path, f); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("PTN_CONFIG", path)
	return path
}

func TestPathHonoursExplicitOverride(t *testing.T) {
	t.Setenv("PTN_CONFIG", "/somewhere/else.json")
	got, err := config.Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if got != "/somewhere/else.json" {
		t.Fatalf("Path = %q, want the override", got)
	}
}

func TestPathFollowsXDGConfigHome(t *testing.T) {
	t.Setenv("PTN_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	got, err := config.Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if want := filepath.Join("/xdg", "prompton", "config.json"); got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

func TestLoadPrefersFlagsOverEnvOverFile(t *testing.T) {
	writeConfig(t, config.File{
		Host:    "https://file.example",
		Token:   "file-token",
		Org:     "file-org",
		Project: "file-project",
	})
	t.Setenv("PTN_HOST", "https://env.example")
	t.Setenv("PTN_TOKEN", "env-token")
	t.Setenv("PTN_ORG", "env-org")
	t.Setenv("PTN_PROJECT", "env-project")

	cfg, err := config.Load(config.Overrides{
		Host:  "https://flag.example",
		Token: "flag-token",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Host != "https://flag.example" {
		t.Errorf("Host = %q, want the flag value", cfg.Host)
	}
	if cfg.Token != "flag-token" {
		t.Errorf("Token = %q, want the flag value", cfg.Token)
	}
	// No flag for these two, so the environment wins over the file.
	if cfg.Org != "env-org" {
		t.Errorf("Org = %q, want the env value", cfg.Org)
	}
	if cfg.Project != "env-project" {
		t.Errorf("Project = %q, want the env value", cfg.Project)
	}
}

func TestLoadFallsBackToFileThenDefault(t *testing.T) {
	writeConfig(t, config.File{Org: "acme-inc"})
	for _, key := range []string{"PTN_HOST", "PTN_TOKEN", "PTN_ORG", "PTN_PROJECT"} {
		t.Setenv(key, "")
	}

	cfg, err := config.Load(config.Overrides{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Org != "acme-inc" {
		t.Errorf("Org = %q, want the file value", cfg.Org)
	}
	if cfg.Host != meta.DefaultHost {
		t.Errorf("Host = %q, want the built-in default", cfg.Host)
	}
	if cfg.Project != "" {
		t.Errorf("Project = %q, want empty", cfg.Project)
	}
}

func TestLoadNormalizesHost(t *testing.T) {
	t.Setenv("PTN_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	for _, tc := range []struct{ in, want string }{
		{"https://prompton.example/", "https://prompton.example"},
		{"prompton.example", "https://prompton.example"},
		{"localhost:4000", "http://localhost:4000"},
		{"http://127.0.0.1:4000/", "http://127.0.0.1:4000"},
	} {
		cfg, err := config.Load(config.Overrides{Host: tc.in})
		if err != nil {
			t.Fatalf("Load(%q): %v", tc.in, err)
		}
		if cfg.Host != tc.want {
			t.Errorf("Load(%q).Host = %q, want %q", tc.in, cfg.Host, tc.want)
		}
	}
}

func TestLoadOnMissingFileIsNotAnError(t *testing.T) {
	t.Setenv("PTN_CONFIG", filepath.Join(t.TempDir(), "nothing-here.json"))
	cfg, err := config.Load(config.Overrides{})
	if err != nil {
		t.Fatalf("Load on a fresh install must not fail: %v", err)
	}
	if cfg.Token != "" {
		t.Errorf("Token = %q, want empty", cfg.Token)
	}
}

func TestLoadReportsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PTN_CONFIG", path)
	if _, err := config.Load(config.Overrides{}); err == nil {
		t.Fatal("Load must report a corrupt config file rather than silently ignoring it")
	}
}

func TestWriteFileIsOwnerOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	path := filepath.Join(dir, "config.json")
	if err := config.WriteFile(path, config.File{Token: "secret"}); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600 — the file holds a token", perm)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir mode = %o, want 700", perm)
	}

	var raw map[string]any
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("config file is not valid JSON: %v", err)
	}
	if raw["token"] != "secret" {
		t.Errorf("token round-trip failed: %v", raw["token"])
	}
}

func TestWriteFileOverwritesWithoutLosingPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("PTN_CONFIG", path)
	if err := config.WriteFile(path, config.File{Token: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteFile(path, config.File{Token: "two"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode after rewrite = %o, want 600", perm)
	}

	got, _, err := config.ReadFile()
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got.Token != "two" {
		t.Errorf("Token = %q, want the rewritten value", got.Token)
	}
}

func TestClearedKeepsHostOnly(t *testing.T) {
	got := config.Cleared(config.File{
		Host:    "https://self.hosted",
		Token:   "secret",
		Org:     "acme-inc",
		Project: "heydiary",
		User:    &config.User{ID: "u1", Email: "ada@example.com"},
	})
	if got.Host != "https://self.hosted" {
		t.Errorf("Host = %q, want the host to survive logout", got.Host)
	}
	if got.Token != "" || got.User != nil || got.Org != "" || got.Project != "" {
		t.Errorf("logout left something behind: %+v", got)
	}
}

func TestOrgRefUsesPersonalLiteral(t *testing.T) {
	personal := config.Org{ID: "o1", Name: "Ada", Personal: true}
	if personal.Ref() != "personal" {
		t.Errorf("personal org Ref = %q, want %q", personal.Ref(), "personal")
	}
	team := config.Org{ID: "o2", Name: "Acme", Slug: "acme-inc"}
	if team.Ref() != "acme-inc" {
		t.Errorf("team org Ref = %q, want its slug", team.Ref())
	}
}

func TestFindOrg(t *testing.T) {
	orgs := []config.Org{
		{ID: "o1", Name: "Ada", Personal: true},
		{ID: "o2", Name: "Acme Inc", Slug: "acme-inc"},
	}
	for _, tc := range []struct{ ref, want string }{
		{"personal", "o1"},
		{"acme-inc", "o2"},
		{"o2", "o2"},
		{"Acme Inc", "o2"},
	} {
		got, ok := config.FindOrg(orgs, tc.ref)
		if !ok {
			t.Fatalf("FindOrg(%q) not found", tc.ref)
		}
		if got.ID != tc.want {
			t.Errorf("FindOrg(%q).ID = %q, want %q", tc.ref, got.ID, tc.want)
		}
	}
	if _, ok := config.FindOrg(orgs, "nope"); ok {
		t.Error("FindOrg found an org that does not exist")
	}
}

func TestDefaultOrgOnlyWhenUnambiguous(t *testing.T) {
	one := []config.Org{{ID: "o1", Personal: true}}
	if got, ok := config.DefaultOrg(one); !ok || got.Ref() != "personal" {
		t.Errorf("a single org must be adopted automatically, got %+v ok=%v", got, ok)
	}
	two := []config.Org{{ID: "o1", Personal: true}, {ID: "o2", Slug: "acme-inc"}}
	if _, ok := config.DefaultOrg(two); ok {
		t.Error("with two orgs the CLI must not guess")
	}
	if _, ok := config.DefaultOrg(nil); ok {
		t.Error("with no orgs there is nothing to adopt")
	}
}
