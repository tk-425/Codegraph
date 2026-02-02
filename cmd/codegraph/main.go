package main

import (
	"os"

	"github.com/tk-425/Codegraph/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
