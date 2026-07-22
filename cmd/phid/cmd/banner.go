// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	_ "embed"
	"fmt"
	"io"
	"os"
)

//go:embed banner.txt
var phiBanner string

// PrintBanner prints the φ logo to the given writer.
func PrintBanner(w io.Writer) {
	fmt.Fprint(w, phiBanner)
}

// PrintCommandBanner prints the φ logo to stderr for "start" and "version" (stderr keeps version stdout machine-parseable).
func PrintCommandBanner() {
	if len(os.Args) > 1 && (os.Args[1] == "start" || os.Args[1] == "version") {
		PrintBanner(os.Stderr)
	}
}
