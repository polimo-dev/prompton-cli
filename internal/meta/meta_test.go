package meta_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/polimo-dev/prompton-cli/internal/meta"
)

// clientRE is the shape the device-code endpoint expects for "client":
// prompton-cli/<version> (<os>/<arch>). The version is a release such as
// 0.1.0, or a snapshot such as 0.0.0-main.1a2b3c4 — what the rolling main
// build stamps in, with the same -ldflags as a release.
var clientRE = regexp.MustCompile(`^prompton-cli/\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)? \([a-z0-9]+/[a-z0-9]+\)$`)

func TestClientString(t *testing.T) {
	if got := meta.Client(); !clientRE.MatchString(got) {
		t.Errorf("Client() = %q, want the documented shape", got)
	}
}

func TestClientStringCarriesASnapshotVersion(t *testing.T) {
	defer func(v string) { meta.Version = v }(meta.Version)
	meta.Version = "0.0.0-main.1a2b3c4"

	got := meta.Client()
	if !clientRE.MatchString(got) || !strings.Contains(got, "-cli/0.0.0-main.1a2b3c4 (") {
		t.Errorf("Client() = %q, want the stamped snapshot version verbatim", got)
	}
}

func TestUserAgentMatchesTheClientString(t *testing.T) {
	if meta.UserAgent() != meta.Client() {
		t.Error("one identifier for both fields keeps server-side logs simple")
	}
}

func TestDeviceNameNamesTheMachine(t *testing.T) {
	got := meta.DeviceName()
	if !strings.HasPrefix(got, "CLI on ") || got == "CLI on " {
		t.Errorf("DeviceName() = %q", got)
	}
}

func TestEnvPrefixesConsistently(t *testing.T) {
	if got := meta.Env("HOST"); got != "PTN_HOST" {
		t.Errorf("Env(\"HOST\") = %q", got)
	}
}

func TestModulePathMatchesGoMod(t *testing.T) {
	// A rename has to touch both; this is the reminder.
	if meta.ModulePath != "github.com/polimo-dev/prompton-cli" {
		t.Errorf("ModulePath = %q", meta.ModulePath)
	}
}
