package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/LaurPl/shiptrace/internal/cli"
)

//go:embed all:web/dist
var bundleFS embed.FS

func main() {
	cli.SetBundle(bundleFS)
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
