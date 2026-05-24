package main

import (
	"log"

	"github.com/ayu-v0/agent-cortex/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
