// Command provider-slack は aso-bell の provider-slack バイナリ。
package main

import (
	"fmt"
	"os"

	"github.com/bear-san/aso-bell/internal/shared/version"
)

const exitUsage = 2

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: provider-slack <version>")
		return exitUsage
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(os.Stdout, version.String("provider-slack"))
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", args[0])
		return exitUsage
	}
}
