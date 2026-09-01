// Command kubeui is a terminal-native Kubernetes operations UI.
package main

import (
	"os"

	"github.com/aronk11/kubeui/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
