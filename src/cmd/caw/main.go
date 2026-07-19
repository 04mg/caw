package main

import (
	"fmt"
	"log"
	"os"

	"github.com/04mg/caw/internal/embed"
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
	case "update", "--update":
		runUpdate()
		return true
	case "help", "-h", "--help":
		showHelp()
		return true
	default:
		showHelp()
		return true
	}
}

func showHelp() {
	fmt.Print(embed.IconTxt)
	fmt.Println("Usage: caw [command]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  (no args)    Start the server")
	fmt.Println("  update       Update caw to the latest release")
	fmt.Println("  version      Print the current version")
	fmt.Println("  help         Show this help message")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  -h, --help       Show help")
	fmt.Println("  -v, --version    Print version")
	fmt.Println("      --update     Update caw to the latest release")
	fmt.Println()
	fmt.Println("Environment:")
	fmt.Println("  HOST    Bind address (default: localhost)")
	fmt.Println("  PORT    Bind port (default: 8080)")
}

func printVersion() {
	fmt.Printf("caw version %s\n", Version)
}

func runUpdate() {
	if err := update.Run(Version); err != nil {
		log.Fatalf("Error updating caw: %v", err)
	}
}
