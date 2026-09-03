// Package config loads and stores the CLI's persistent settings.
//
// Resolution order for every value is: flag > environment > config file >
// built-in default. The file lives at $XDG_CONFIG_HOME/prompton/config.json
// (falling back to ~/.config/prompton/config.json) and is written 0600 in a
// 0700 directory, because it carries a long-lived user token.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/polimo-dev/prompton-cli/internal/meta"
)

// PersonalOrg is the reserved slug for the caller's personal organization.
// Personal orgs have no slug of their own, so the API addresses them by this
// literal in the :org path segment.
const PersonalOrg = "personal"

// User is the identity attached to the stored token.
type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

// Org is one organization the user belongs to.
type Org struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug,omitempty"`
	Personal bool   `json:"personal"`
}

// Ref is how this org is addressed in a URL path: its slug, or the reserved
// literal "personal" when it is the caller's personal org.
func (o Org) Ref() string {
	if o.Personal || o.Slug == "" {
		return PersonalOrg
	}
	return o.Slug
}

// File is the on-disk shape of config.json.
type File struct {
	Host    string `json:"host,omitempty"`
	Token   string `json:"token,omitempty"`
	User    *User  `json:"user,omitempty"`
	Orgs    []Org  `json:"organizations,omitempty"`
	Org     string `json:"org,omitempty"`
	Project string `json:"project,omitempty"`
}

// Overrides carries the values supplied on the command line. Empty strings
// mean "not supplied".
type Overrides struct {
	Host    string
	Token   string
	Org     string
	Project string
}

// Config is the resolved view a command works against.
type Config struct {
	Host    string
	Token   string
	Org     string
	Project string

	User *User
	Orgs []Org

	// File is the file as it was read (or an empty File when absent), so a
	// command that persists a change starts from what is on disk rather than
	// from the flag-merged view.
	File File

	// Path is where File was read from and will be written back to.
	Path string
}

// Path returns the config file location, honouring PTN_CONFIG (an escape
// hatch used by tests and by anyone juggling several installations) and
// XDG_CONFIG_HOME.
func Path() (string, error) {
	if p := os.Getenv(meta.Env("CONFIG")); p != "" {
		return p, nil
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, meta.ConfigDirName, meta.ConfigFileName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate home directory: %w", err)
	}
	return filepath.Join(home, ".config", meta.ConfigDirName, meta.ConfigFileName), nil
}

// ReadFile loads config.json. A missing file is not an error: it yields a zero
// File, which is what a fresh install looks like.
func ReadFile() (File, string, error) {
	path, err := Path()
	if err != nil {
		return File{}, "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return File{}, path, nil
		}
		return File{}, path, fmt.Errorf("read %s: %w", path, err)
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return File{}, path, fmt.Errorf("parse %s: %w", path, err)
	}
	return f, path, nil
}

// Load resolves flags over environment over file over defaults.
func Load(o Overrides) (*Config, error) {
	f, path, err := ReadFile()
	if err != nil {
		return nil, err
	}
	c := &Config{File: f, Path: path, User: f.User, Orgs: f.Orgs}

	c.Host = normalizeHost(first(o.Host, os.Getenv(meta.Env("HOST")), f.Host, meta.DefaultHost))
	c.Token = first(o.Token, os.Getenv(meta.Env("TOKEN")), f.Token)
	c.Org = normalizeOrg(first(o.Org, os.Getenv(meta.Env("ORG")), f.Org))
	c.Project = first(o.Project, os.Getenv(meta.Env("PROJECT")), f.Project)
	return c, nil
}

// Save writes the file back with 0600 permissions.
func (c *Config) Save() error {
	return WriteFile(c.Path, c.File)
}

// WriteFile persists a config file at path, creating the directory 0700 and
// the file 0600. It writes to a temporary file in the same directory and
// renames, so an interrupted write cannot truncate an existing token.
func WriteFile(path string, f File) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return os.Chmod(path, 0o600)
}

// Cleared strips the stored credential and identity but keeps the host, so a
// later `login` against a self-hosted instance does not need --host again.
func Cleared(f File) File {
	return File{Host: f.Host}
}

// FindOrg looks up a stored org by slug, by the literal "personal", by name,
// or by id.
func FindOrg(orgs []Org, ref string) (Org, bool) {
	want := normalizeOrg(ref)
	for _, o := range orgs {
		if o.Ref() == want || o.ID == ref {
			return o, true
		}
	}
	for _, o := range orgs {
		if strings.EqualFold(o.Name, ref) {
			return o, true
		}
	}
	return Org{}, false
}

// DefaultOrg returns the org to adopt automatically: the only one, when there
// is exactly one. With several orgs the caller must choose, so this reports
// false.
func DefaultOrg(orgs []Org) (Org, bool) {
	if len(orgs) == 1 {
		return orgs[0], true
	}
	return Org{}, false
}

func normalizeHost(h string) string {
	h = strings.TrimSpace(h)
	h = strings.TrimRight(h, "/")
	if h == "" {
		return meta.DefaultHost
	}
	if !strings.Contains(h, "://") {
		// Bare host names are almost always meant as https; localhost is the
		// one place where a plain-text listener is the norm.
		if strings.HasPrefix(h, "localhost") || strings.HasPrefix(h, "127.0.0.1") {
			return "http://" + h
		}
		return "https://" + h
	}
	return h
}

func normalizeOrg(o string) string {
	return strings.TrimSpace(o)
}

func first(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
