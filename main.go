// Command yaymlq (Yet Another YAML Query) is a small CLI for extracting values
// from YAML documents using a dotted/bracketed path expression.
package main

import (
	"os"

	"github.com/reticule-poirot/yaymlq/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
