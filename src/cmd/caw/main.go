package main

import (
	"os"

	"github.com/04mg/caw/internal/server"
)

func main() {
	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	server.New().ListenAndServe(port)
}
