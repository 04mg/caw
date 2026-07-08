package main

import (
	"os"

	"github.com/04mg/caw/internal/server"
)

func main() {
	host := "localhost"
	if h := os.Getenv("HOST"); h != "" {
		host = h
	}

	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	server.New().ListenAndServe(host, port)
}
