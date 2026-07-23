package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/04mg/caw/internal/embed"
	"github.com/04mg/caw/internal/server"
	"github.com/04mg/caw/internal/update"
)

var Version = "dev"

func main() {
	args := os.Args[1:]

	// No args -> start the server.
	if len(args) == 0 {
		startServer()
		return
	}

	// Subcommands.
	switch args[0] {
	case "version":
		printVersion()
		return
	case "update":
		runUpdate()
		return
	case "help":
		showHelp()
		return
	}

	// Flags. The flag package prints help automatically for -h/--help.
	fs := flag.NewFlagSet("caw", flag.ExitOnError)
	fs.Usage = showHelp
	var version bool
	fs.BoolVar(&version, "v", false, "print version and exit")
	fs.BoolVar(&version, "version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return
	}
	if version {
		printVersion()
		return
	}
	if fs.NArg() > 0 {
		showHelp()
		return
	}
	startServer()
}

func startServer() {
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

func showHelp() {
	fmt.Print(embed.IconTxt)
	fmt.Println("Usage: caw [command]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  (no args)        Start the server")
	fmt.Println("  update           Update caw to the latest release")
	fmt.Println("  version          Print the current version")
	fmt.Println("  help             Show this help message")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  -h, --help       Show help")
	fmt.Println("  -v, --version    Print version")
	fmt.Println()
	fmt.Println("Environment:")
	fmt.Println("  HOST             Bind address (default: localhost)")
	fmt.Println("  PORT             Bind port (default: 8080)")
}

func printVersion() {
	fmt.Printf("caw version %s\n", Version)
}

func runUpdate() {
	if err := update.Run(Version); err != nil {
		log.Fatalf("Error updating caw: %v", err)
	}
}