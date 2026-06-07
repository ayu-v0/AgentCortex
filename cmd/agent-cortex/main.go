package main

import (
	"flag"
	"log"

	"github.com/ayu-v0/agent-cortex/internal/app"
)

func main() {
	configPath := flag.String("config", "", "path to a YAML config file")
	flag.Parse()

	if err := app.RunWithConfigPath(*configPath); err != nil {
		log.Fatal(err)
	}
}
