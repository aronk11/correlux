// Command correlux is a terminal-native Kubernetes operations UI.
package main

import (
	"os"

	"github.com/aronk11/correlux/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
