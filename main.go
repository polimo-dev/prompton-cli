// Command prompton is the PromptOn command-line interface.
package main

import (
	"os"

	"github.com/polimo-dev/prompton-cli/cmd"
)

func main() {
	os.Exit(cmd.Main())
}
