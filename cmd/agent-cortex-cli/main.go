package main

import (
	"flag"
	"log"

	"github.com/ayu-v0/agent-cortex/internal/cli"
)

func main() {
	configPath := flag.String("config", "", "path to a YAML config file")
	flag.Parse()

	if err := cli.RunWithConfigPath(*configPath); err != nil {
		log.Fatal(err)
	}
}
