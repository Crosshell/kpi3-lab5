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

func getPort() int {
	if envPort := os.Getenv("PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			return p
		}
	}
	return *port
}

const (
	confResponseDelaySec = "CONF_RESPONSE_DELAY_SEC"
	confHealthFailure    = "CONF_HEALTH_FAILURE"
	teamName             = "crosshell-team" // 🔁 Заміни на свою команду!
	dbServiceURL         = "http://db:8083/db/"
)

func main() {
	h := http.NewServeMux()

	// ✅ Health endpoint — необхідний для балансувальника
	h.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if os.Getenv(confHealthFailure) != "" {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("Unhealthy"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// ✅ Ініціалізація БД
	initializeDB()

	report := make(Report)

	h.HandleFunc("/api/v1/some-data", func(rw http.ResponseWriter, r *http.Request) {
		// Optional response delay
		respDelayString := os.Getenv(confResponseDelaySec)
		if delaySec, parseErr := strconv.Atoi(respDelayString); parseErr == nil && delaySec > 0 && delaySec < 300 {
			time.Sleep(time.Duration(delaySec) * time.Second)
		}

		report.Process(r)

		// Set identification header for load balancer test
		serverID := os.Getenv("SERVER_ID")
		if serverID == "" {
			serverID = fmt.Sprintf("server-%d", getPort())
		}
		rw.Header().Set("lb-from", serverID)

		// Parse key
		key := r.URL.Query().Get("key")
		if key == "" {
			rw.WriteHeader(http.StatusBadRequest)
			return
		}

		// Fetch from DB service
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
		_ = json.NewEncoder(rw).Encode(dbResponse.Value)
	})

	// Report handler
	h.Handle("/report", report)

	// ✅ Запуск сервера
	server := httptools.CreateServer(getPort(), h)
	server.Start()

	// Очікування сигналу завершення
	signal.WaitForTerminationSignal()
}

// Функція ініціалізації БД з поточною датою
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
