package handlers

import (
	"encoding/json"
	"net/http"
)

type HealthResponse struct {
	Status string `json:"status"`
	Name   string `json:"name"`
}

func Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	w.WriteHeader(http.StatusOK)

	response := HealthResponse{
		Status: "ok",
		Name:   "vps-azii",
	}

	_ = json.NewEncoder(w).Encode(response)
}
