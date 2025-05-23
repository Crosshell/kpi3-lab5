package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Тестуємо вибір сервера на основі хешу адреси клієнта
func TestLoadBalancerGetServer(t *testing.T) {
	lb := NewLoadBalancer()
	
	// Встановлюємо всі сервери як здорові для тестування
	for i := range lb.servers {
		lb.updateServerHealth(i, true)
	}
	
	// Перевіряємо, що для однієї і тієї ж адреси клієнта завжди вибирається один і той же сервер
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
	
	// Перевіряємо, що для різних адрес клієнтів можуть бути вибрані різні сервери
	client2 := "192.168.1.2:1234"
	server2, err := lb.getServer(client2)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	
	// Примітка: Не обов'язково сервери будуть різними, але ми перевіряємо консистентність
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

// Тестуємо поведінку коли немає здорових серверів
func TestLoadBalancerNoHealthyServers(t *testing.T) {
	lb := NewLoadBalancer()
	
	// Встановлюємо всі сервери як нездорові
	for i := range lb.servers {
		lb.updateServerHealth(i, false)
	}
	
	_, err := lb.getServer("192.168.1.1:1234")
	if err == nil {
		t.Fatal("Expected error when no healthy servers available, but got none")
	}
}

// Тестуємо оновлення стану здоров'я серверів
func TestLoadBalancerUpdateServerHealth(t *testing.T) {
	lb := NewLoadBalancer()
	
	// Перевіряємо початковий стан
	for i, server := range lb.servers {
		if server.health {
			t.Errorf("Server %d should be unhealthy initially", i)
		}
	}
	
	// Оновлюємо стан здоров'я
	lb.updateServerHealth(0, true)
	lb.updateServerHealth(1, true)
	
	// Перевіряємо оновлений стан
	if !lb.servers[0].health {
		t.Error("Server 0 should be healthy after update")
	}
	if !lb.servers[1].health {
		t.Error("Server 1 should be healthy after update")
	}
	if lb.servers[2].health {
		t.Error("Server 2 should still be unhealthy")
	}
	
	// Повертаємо сервер 0 в нездоровий стан
	lb.updateServerHealth(0, false)
	
	// Перевіряємо оновлений стан
	if lb.servers[0].health {
		t.Error("Server 0 should be unhealthy after update")
	}
}

// Тестуємо отримання списку здорових серверів
func TestLoadBalancerGetHealthyServers(t *testing.T) {
	lb := NewLoadBalancer()
	
	// Початково всі сервери нездорові
	healthyServers := lb.getHealthyServers()
	if len(healthyServers) != 0 {
		t.Errorf("Expected 0 healthy servers, got %d", len(healthyServers))
	}
	
	// Оновлюємо стан здоров'я
	lb.updateServerHealth(0, true)
	lb.updateServerHealth(2, true)
	
	// Перевіряємо кількість здорових серверів
	healthyServers = lb.getHealthyServers()
	if len(healthyServers) != 2 {
		t.Errorf("Expected 2 healthy servers, got %d", len(healthyServers))
	}
	
	// Перевіряємо, що повернулися правильні сервери
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

// Тестуємо функцію forward
func TestForward(t *testing.T) {
	// Створюємо тестовий сервер
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test-Header", "test-value")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	}))
	defer testServer.Close()
	
	// Налаштовуємо тестовий запит
	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	recorder := httptest.NewRecorder()
	
	// Встановлюємо traceEnabled в true для тестування заголовка lb-from
	*traceEnabled = true
	
	// Отримуємо адресу тестового сервера без схеми (http://)
	serverAddr := testServer.URL[7:] // видаляємо "http://"
	
	// Виконуємо функцію forward
	err := forward(serverAddr, recorder, req)
	if err != nil {
		t.Fatalf("Forward function failed: %v", err)
	}
	
	// Перевіряємо статус відповіді
	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, recorder.Code)
	}
	
	// Перевіряємо тіло відповіді
	if recorder.Body.String() != "test response" {
		t.Errorf("Expected body 'test response', got '%s'", recorder.Body.String())
	}
	
	// Перевіряємо заголовки
	if recorder.Header().Get("X-Test-Header") != "test-value" {
		t.Errorf("Expected X-Test-Header to be 'test-value', got '%s'", 
			recorder.Header().Get("X-Test-Header"))
	}
	
	// Перевіряємо заголовок lb-from
	if recorder.Header().Get("lb-from") != serverAddr {
		t.Errorf("Expected lb-from to be '%s', got '%s'", 
			serverAddr, recorder.Header().Get("lb-from"))
	}
}

// Тестуємо функцію health
func TestHealth(t *testing.T) {
	// Створюємо тестовий сервер, який повертає HTTP 200 OK на запит /health
	healthyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer healthyServer.Close()
	
	// Створюємо тестовий сервер, який повертає HTTP 500 на запит /health
	unhealthyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer unhealthyServer.Close()
	
	// Тестуємо, що здоровий сервер розпізнається правильно
	// Видаляємо схему (http://) з URL
	healthyServerAddr := healthyServer.URL[7:]
	
	// Встановлюємо scheme на "http" для тестування
	*https = false
	
	isHealthy := health(healthyServerAddr)
	if !isHealthy {
		t.Errorf("Server %s should be recognized as healthy", healthyServerAddr)
	}
	
	// Тестуємо, що нездоровий сервер розпізнається правильно
	unhealthyServerAddr := unhealthyServer.URL[7:]
	isHealthy = health(unhealthyServerAddr)
	if isHealthy {
		t.Errorf("Server %s should be recognized as unhealthy", unhealthyServerAddr)
	}
	
	// Тестуємо недосяжний сервер
	isHealthy = health("non-existent-server:8080")
	if isHealthy {
		t.Error("Non-existent server should be recognized as unhealthy")
	}
}