package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/Crosshell/kpi3-lab5/datastore"
)

type dbHandler struct {
	db *datastore.Db
}

func (h *dbHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Path[len("/db/"):]
	
	switch r.Method {
	case http.MethodGet:
		value, err := h.db.Get(key)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		
		response := map[string]interface{}{
			"key":   key,
			"value": value,
		}
		json.NewEncoder(w).Encode(response)
		
	case http.MethodPost:
		var request struct {
			Value interface{} `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		
		stringValue := fmt.Sprintf("%v", request.Value)
		if err := h.db.Put(key, stringValue); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func main() {
	// Health check endpoint
	http.HandleFunc("/db/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Create data directory
	if err := os.MkdirAll("/opt/practice-4/out", 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	// Initialize DB
	db, err := datastore.CreateDb("/opt/practice-4/out", 250)
	if err != nil {
		log.Fatalf("DB initialization failed: %v", err)
	}

	handler := &dbHandler{db: db}
	http.Handle("/db/", handler)

	// Start server
	log.Println("Starting DB server on :8083")
	if err := http.ListenAndServe(":8083", nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}