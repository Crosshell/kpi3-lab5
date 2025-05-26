package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadBalancerGetServer(t *testing.T) {
	lb := NewLoadBalancer()
	
	for i := range lb.servers {
		lb.updateServerHealth(i, true)
	}
	
	client1 := "192.168.1.1:1234"
	server1, err := lb.getServer(client1)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	
	for i := 0; i < 10; i++ {
		server, err := lb.getServer(client1)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		
		if server.address != server1.address {
			t.Errorf("Different servers selected for the same client address: got %s, want %s",
				server.address, server1.address)
		}
	}
	
	client2 := "192.168.1.2:1234"
	server2, err := lb.getServer(client2)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	
	for i := 0; i < 10; i++ {
		server, err := lb.getServer(client2)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		
		if server.address != server2.address {
			t.Errorf("Different servers selected for the same client address: got %s, want %s",
				server.address, server2.address)
		}
	}
}

func TestLoadBalancerNoHealthyServers(t *testing.T) {
	lb := NewLoadBalancer()
	
	for i := range lb.servers {
		lb.updateServerHealth(i, false)
	}
	
	_, err := lb.getServer("192.168.1.1:1234")
	if err == nil {
		t.Fatal("Expected error when no healthy servers available, but got none")
	}
}

func TestLoadBalancerUpdateServerHealth(t *testing.T) {
	lb := NewLoadBalancer()
	
	for i, server := range lb.servers {
		if server.health {
			t.Errorf("Server %d should be unhealthy initially", i)
		}
	}
	
	lb.updateServerHealth(0, true)
	lb.updateServerHealth(1, true)
	
	if !lb.servers[0].health {
		t.Error("Server 0 should be healthy after update")
	}
	if !lb.servers[1].health {
		t.Error("Server 1 should be healthy after update")
	}
	if lb.servers[2].health {
		t.Error("Server 2 should still be unhealthy")
	}
	
	lb.updateServerHealth(0, false)
	
	if lb.servers[0].health {
		t.Error("Server 0 should be unhealthy after update")
	}
}

func TestLoadBalancerGetHealthyServers(t *testing.T) {
	lb := NewLoadBalancer()
	
	healthyServers := lb.getHealthyServers()
	if len(healthyServers) != 0 {
		t.Errorf("Expected 0 healthy servers, got %d", len(healthyServers))
	}
	
	lb.updateServerHealth(0, true)
	lb.updateServerHealth(2, true)
	
	healthyServers = lb.getHealthyServers()
	if len(healthyServers) != 2 {
		t.Errorf("Expected 2 healthy servers, got %d", len(healthyServers))
	}
	
	expectedAddresses := map[string]bool{
		lb.servers[0].address: true,
		lb.servers[2].address: true,
	}
	
	for _, server := range healthyServers {
		if !expectedAddresses[server.address] {
			t.Errorf("Unexpected server in healthy list: %s", server.address)
		}
	}
}

func TestForward(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test-Header", "test-value")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	}))
	defer testServer.Close()
	
	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	recorder := httptest.NewRecorder()
	
	*traceEnabled = true
	
	serverAddr := testServer.URL[7:] 
	
	err := forward(serverAddr, recorder, req)
	if err != nil {
		t.Fatalf("Forward function failed: %v", err)
	}
	
	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, recorder.Code)
	}
	
	if recorder.Body.String() != "test response" {
		t.Errorf("Expected body 'test response', got '%s'", recorder.Body.String())
	}
	
	if recorder.Header().Get("X-Test-Header") != "test-value" {
		t.Errorf("Expected X-Test-Header to be 'test-value', got '%s'", 
			recorder.Header().Get("X-Test-Header"))
	}
	
	if recorder.Header().Get("lb-from") != serverAddr {
		t.Errorf("Expected lb-from to be '%s', got '%s'", 
			serverAddr, recorder.Header().Get("lb-from"))
	}
}

func TestHealth(t *testing.T) {
	healthyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer healthyServer.Close()
	
	unhealthyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer unhealthyServer.Close()
	
	healthyServerAddr := healthyServer.URL[7:]
	
	*https = false
	
	isHealthy := health(healthyServerAddr)
	if !isHealthy {
		t.Errorf("Server %s should be recognized as healthy", healthyServerAddr)
	}
	
	unhealthyServerAddr := unhealthyServer.URL[7:]
	isHealthy = health(unhealthyServerAddr)
	if isHealthy {
		t.Errorf("Server %s should be recognized as unhealthy", unhealthyServerAddr)
	}
	
	isHealthy = health("non-existent-server:8080")
	if isHealthy {
		t.Error("Non-existent server should be recognized as unhealthy")
	}
}