// Package meta holds the handful of identity constants for this CLI.
//
// Everything that names the product lives here, so renaming the tool is a
// single-file edit (plus the go.mod module path, which mirrors ModulePath).
package meta

import (
	"fmt"
	"os"
	"runtime"
)

// Version is the release this binary was cut from. GoReleaser stamps it with
// -ldflags "-X <module>/internal/meta.Version=<version>": the tag without its
// "v" for a release, 0.0.0-main.<sha> for the rolling main build. A plain
// `go build` keeps the value below.
var Version = "0.1.0"

const (
	// Name is the binary name (env vars use EnvPrefix, not this).
	Name = "prompton"

	// ModulePath must stay in sync with go.mod.
	ModulePath = "github.com/polimo-dev/prompton-cli"

	// DefaultHost is used when neither flag, env, nor config supplies one.
	DefaultHost = "https://app.prompton.ai"

	// ConfigDirName is the directory under $XDG_CONFIG_HOME (or ~/.config).
	ConfigDirName = "prompton"

	// ConfigFileName is the file inside ConfigDirName.
	ConfigFileName = "config.json"

	// EnvPrefix prefixes every environment variable this CLI reads.
	EnvPrefix = "PTN_"
)

// Client is the `client` string sent to POST /api/v1/device/code, e.g.
// "prompton-cli/0.1.0 (darwin/arm64)".
func Client() string {
	return fmt.Sprintf("%s-cli/%s (%s/%s)", Name, Version, runtime.GOOS, runtime.GOARCH)
}

// UserAgent is sent on every HTTP request.
func UserAgent() string { return Client() }

// DeviceName is the human-facing label shown on the browser approval screen.
func DeviceName() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown host"
	}
	return "CLI on " + host
}

// Env returns the full name of an environment variable, e.g. Env("HOST").
func Env(suffix string) string { return EnvPrefix + suffix }
