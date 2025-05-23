package main

import (
	"context"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/roman-mazur/architecture-practice-4-template/httptools"
	"github.com/roman-mazur/architecture-practice-4-template/signal"
)

var (
	port = flag.Int("port", 8090, "load balancer port")
	timeoutSec = flag.Int("timeout-sec", 3, "request timeout time in seconds")
	https = flag.Bool("https", false, "whether backends support HTTPs")

	traceEnabled = flag.Bool("trace", false, "whether to include client info in responses")
)

var (
	timeout = time.Duration(*timeoutSec) * time.Second
	serversPool = []string{
		"server1:8080",
		"server2:8080",
		"server3:8080",
	}
)

type ServerConnections struct {
	address string
	health  bool
}

type LoadBalancer struct {
	servers []ServerConnections
}

func NewLoadBalancer() *LoadBalancer {
	servers := make([]ServerConnections, len(serversPool))
	for i, server := range serversPool {
		servers[i] = ServerConnections{
			address: server,
			health:  false,
		}
	}
	return &LoadBalancer{
		servers: servers,
	}
}

func (lb *LoadBalancer) getHealthyServers() []ServerConnections {
	healthyServers := make([]ServerConnections, 0)
	for _, server := range lb.servers {
		if server.health {
			healthyServers = append(healthyServers, server)
		}
	}
	return healthyServers
}

func (lb *LoadBalancer) getServer(clientAddr string) (*ServerConnections, error) {
	healthyServers := lb.getHealthyServers()
	if len(healthyServers) == 0 {
		return nil, fmt.Errorf("no healthy servers available")
	}
	
	// Використовуємо хеш-функцію для обчислення індексу сервера на основі адреси клієнта
	hash := fnv.New32a()
	hash.Write([]byte(clientAddr))
	serverIndex := int(hash.Sum32()) % len(healthyServers)
	
	return &healthyServers[serverIndex], nil
}

func (lb *LoadBalancer) updateServerHealth(serverIndex int, isHealthy bool) {
	lb.servers[serverIndex].health = isHealthy
}

func scheme() string {
	if *https {
		return "https"
	}
	return "http"
}

func health(dst string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	
	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s://%s/health", scheme(), dst), nil)
	
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	
	if resp.StatusCode != http.StatusOK {
		return false
	}
	
	return true
}

func forward(dst string, rw http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	
	fwdRequest := r.Clone(ctx)
	fwdRequest.RequestURI = ""
	fwdRequest.URL.Host = dst
	fwdRequest.URL.Scheme = scheme()
	fwdRequest.Host = dst
	
	resp, err := http.DefaultClient.Do(fwdRequest)
	if err != nil {
		log.Printf("Failed to get response from %s: %s", dst, err)
		rw.WriteHeader(http.StatusServiceUnavailable)
		return err
	}
	
	for k, values := range resp.Header {
		for _, value := range values {
			rw.Header().Add(k, value)
		}
	}
	if *traceEnabled {
		rw.Header().Set("lb-from", dst)
	}
	
	log.Printf("fwd %s %s -> %s", r.Method, r.URL, dst)
	
	rw.WriteHeader(resp.StatusCode)
	defer resp.Body.Close()
	_, err = io.Copy(rw, resp.Body)
	if err != nil {
		log.Printf("Failed to write response: %s", err)
	}
	
	return nil
}

func main() {
	flag.Parse()
	
	lb := NewLoadBalancer()
	
	// Запускаємо періодичну перевірку доступності серверів
	for i, server := range serversPool {
		i := i
		server := server
		
		go func() {
			for range time.Tick(10 * time.Second) {
				isHealthy := health(server)
				lb.updateServerHealth(i, isHealthy)
				log.Printf("Server %s health is %v", server, isHealthy)
			}
		}()
		
		// Перевіряємо стан сервера при запуску
		go func() {
			isHealthy := health(server)
			lb.updateServerHealth(i, isHealthy)
			log.Printf("Server %s health is %v", server, isHealthy)
		}()
	}
	
	frontend := httptools.CreateServer(*port, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		server, err := lb.getServer(r.RemoteAddr)
		if err != nil {
			log.Printf("Error getting server: %s", err)
			rw.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		
		forward(server.address, rw, r)
	}))
	
	log.Printf("Starting load balancer on port %d", *port)
	log.Printf("Tracing support enabled: %t", *traceEnabled)
	frontend.Start()
	// Замінюємо на правильний виклик з пакету signal
	signal.WaitForTerminationSignal()
}