package main

import (
	"errors"
	"log"
	"net/http"
	"time"
)

func Run() error {
	httpServer := &http.Server{
		Addr:              "127.0.0.1:8080",
		Handler:           NewRouter(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Println("[SERVER] Starting HTTP server")
	log.Println("[SERVER] Address:", httpServer.Addr)

	err := httpServer.ListenAndServe()

	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return err
}
