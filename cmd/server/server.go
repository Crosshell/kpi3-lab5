package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/Crosshell/kpi3-lab5/httptools"
	"github.com/Crosshell/kpi3-lab5/signal"
)

var port = flag.Int("port", 8080, "server port")
const confResponseDelaySec = "CONF_RESPONSE_DELAY_SEC"
const confHealthFailure = "CONF_HEALTH_FAILURE"
const teamName = "your-team-name" // Replace with your actual team name
const dbServiceURL = "http://db:8081/db/"

func main() {
	h := new(http.ServeMux)

	// Initialize database with current date
	initializeDB()

	h.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("content-type", "text/plain")
		if failConfig := os.Getenv(confHealthFailure); failConfig == "true" {
			rw.WriteHeader(http.StatusInternalServerError)
			_, _ = rw.Write([]byte("FAILURE"))
		} else {
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte("OK"))
		}
	})

	report := make(Report)

	h.HandleFunc("/api/v1/some-data", func(rw http.ResponseWriter, r *http.Request) {
		// Handle response delay if configured
		respDelayString := os.Getenv(confResponseDelaySec)
		if delaySec, parseErr := strconv.Atoi(respDelayString); parseErr == nil && delaySec > 0 && delaySec < 300 {
			time.Sleep(time.Duration(delaySec) * time.Second)
		}

		report.Process(r)

		// Get key from query parameters
		key := r.URL.Query().Get("key")
		if key == "" {
			rw.WriteHeader(http.StatusBadRequest)
			return
		}

		// Fetch data from DB service
		resp, err := http.Get(dbServiceURL + key)
		if err != nil || resp.StatusCode == http.StatusNotFound {
			rw.WriteHeader(http.StatusNotFound)
			return
		}
		defer resp.Body.Close()

		var dbResponse struct {
			Key   string      `json:"key"`
			Value interface{} `json:"value"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&dbResponse); err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}

		rw.Header().Set("content-type", "application/json")
		rw.WriteHeader(http.StatusOK)
		json.NewEncoder(rw).Encode(dbResponse.Value)
	})

	h.Handle("/report", report)

	server := httptools.CreateServer(*port, h)
	server.Start()
	signal.WaitForTerminationSignal()
}

func initializeDB() {
    currentDate := time.Now().Format("2006-01-02")
    requestBody, _ := json.Marshal(map[string]string{"value": currentDate})

    maxRetries := 10
    retryInterval := 5 * time.Second

    for i := 0; i < maxRetries; i++ {
        resp, err := http.Post(
            dbServiceURL+teamName,
            "application/json",
            bytes.NewBuffer(requestBody),
        )
        
        if err == nil && resp.StatusCode == http.StatusOK {
            fmt.Println("Successfully initialized DB")
            return
        }
        
        if err != nil {
            fmt.Printf("Attempt %d: DB connection failed: %v\n", i+1, err)
        } else {
            fmt.Printf("Attempt %d: DB returned status %d\n", i+1, resp.StatusCode)
        }
        
        if i < maxRetries-1 {
            time.Sleep(retryInterval)
        }
    }
    
    fmt.Println("Failed to initialize DB after multiple attempts")
}