package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"example.com/shared"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message": shared.Hello(),
			"version": shared.Version(),
		})
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})

	log.Printf("API server listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
