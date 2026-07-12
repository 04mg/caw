package main

import (
	"fmt"
	"log"
	"os"

	"github.com/04mg/caw/internal/server"
	"github.com/04mg/caw/internal/update"
)

var Version = "dev"

func main() {
	if handleSubcommands() {
		return
	}

	host := envOrDefault("HOST", "localhost")
	port := envOrDefault("PORT", "8080")

	server.New().ListenAndServe(host, port)
}

func envOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func handleSubcommands() bool {
	if len(os.Args) < 2 {
		return false
	}

	switch os.Args[1] {
	case "version", "-v", "--version":
		printVersion()
		return true
	case "update":
		runUpdate()
		return true
	default:
		return false
	}
}

func printVersion() {
	fmt.Printf("caw version %s\n", Version)
}

func runUpdate() {
	if err := update.Run(Version); err != nil {
		log.Fatalf("Error updating caw: %v", err)
	}
}

