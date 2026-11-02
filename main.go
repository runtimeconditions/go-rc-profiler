package main

import (
	"context"
	"os"

	"github.com/runtimeconditions/go-rc-profiler/cli"
)

func main() {
	os.Exit(cli.Exit(os.Stderr, cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)))
}
