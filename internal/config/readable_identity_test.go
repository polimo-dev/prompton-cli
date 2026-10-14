package config_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/polimo-dev/prompton-cli/internal/config"
)

func TestLegacyConfigIDsAreIgnoredAndCannotSelectOrganizations(t *testing.T) {
	var f config.File
	if err := json.Unmarshal([]byte(`{"token":"secret","user":{"id":"private-user","email":"ada@example.com"},"organizations":[{"id":"private-org","name":"Acme","slug":"acme"}],"org":"acme"}`), &f); err != nil {
		t.Fatal(err)
	}
	if org, ok := config.FindOrg(f.Orgs, "acme"); !ok || org.Ref() != "acme" {
		t.Fatal("readable org lookup failed")
	}
	if _, ok := config.FindOrg(f.Orgs, "private-org"); ok {
		t.Fatal("legacy private id still selects an organization")
	}
	got, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), `"id"`) || strings.Contains(string(got), "private-") {
		t.Fatalf("legacy ids re-emitted: %s", got)
	}
	if f.Token != "secret" || f.User.Email != "ada@example.com" {
		t.Fatal("lost login state")
	}
}
