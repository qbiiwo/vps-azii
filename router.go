package main

import (
	"net/http"

	"vps-azii/handlers"
)

func NewRouter() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", handlers.Health)
	mux.HandleFunc("POST /api/audio", handlers.UploadAudio)

	return mux
}
