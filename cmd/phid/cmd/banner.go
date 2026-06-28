// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	_ "embed"
	"fmt"
	"io"
	"os"
)

// phiBanner is the official φ logo (the Phi symbol) shown on node startup.
// The content is embedded from banner.txt to keep the logo spacing exact and intact.
//
//go:embed banner.txt
var phiBanner string

// PrintBanner prints the φ logo to the given writer.
func PrintBanner(w io.Writer) {
	fmt.Fprint(w, phiBanner)
}

// PrintCommandBanner prints the φ logo to stderr for the commands that carry the
// brand banner: node startup ("start") and "version". It is written to stderr so
// the "version" command's stdout output stays clean and machine-parseable.
func PrintCommandBanner() {
	if len(os.Args) > 1 && (os.Args[1] == "start" || os.Args[1] == "version") {
		PrintBanner(os.Stderr)
	}
}
