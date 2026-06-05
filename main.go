package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

type HealthResponse struct {
	Status string `json:"status"`
}

type HelloResponse struct {
	Message string `json:"message"`
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HealthResponse{Status: "ok"})
}

func helloHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HelloResponse{Message: "hello"})
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/api/v1/hello", helloHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	log.Printf("Starting backend on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
