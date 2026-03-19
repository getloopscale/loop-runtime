package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

type Task struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Completed bool   `json:"completed"`
}

var tasks = []Task{
	{ID: "1", Title: "Set up infrastructure", Completed: true},
	{ID: "2", Title: "Deploy API service", Completed: false},
	{ID: "3", Title: "Configure monitoring", Completed: false},
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/tasks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(tasks)
	})

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "healthy",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	})

	log.Printf("API server listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
