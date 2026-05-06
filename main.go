package main

import (
	"fmt"
	"os"

	"trafficloadsim/internal/config"
	"trafficloadsim/internal/runner"
)

func main() {
	cfg, err := config.Parse()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := runner.Run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
