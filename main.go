package main

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"os"
)

type HealthResponse struct {
	Status string `json:"status"`
}

type HelloResponse struct {
	Message string `json:"message"`
}

var phrases = []string{
	"keep going",
	"you've got this",
	"make it happen",
	"stay curious",
	"build something awesome",
	"one step at a time",
	"dream big",
	"code on",
	"never stop learning",
	"be the change",
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HealthResponse{Status: "ok"})
}

func helloHandler(w http.ResponseWriter, r *http.Request) {
	phrase := phrases[rand.Intn(len(phrases))]
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HelloResponse{Message: "hello, " + phrase})
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
