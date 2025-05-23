package integration

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

const (
	balancerAddress = "http://balancer:8090/api/v1/some-data"
	serverHeader    = "lb-from"
	requiredServers = 2
	totalRequests   = 10
	requestTimeout  = 3 * time.Second
	testTimeout     = 30 * time.Second
	retryInterval   = 500 * time.Millisecond
)

type testCoordinator struct {
	client       *http.Client
	ctx          context.Context
	serverHits   map[string]int
	requestCount int
}

func newTestCoordinator(ctx context.Context) *testCoordinator {
	return &testCoordinator{
		client: &http.Client{
			Timeout: requestTimeout,
		},
		ctx:        ctx,
		serverHits: make(map[string]int),
	}
}

func (tc *testCoordinator) executeRequest() (string, error) {
	req, err := http.NewRequestWithContext(tc.ctx, "GET", balancerAddress, nil)
	if err != nil {
		return "", fmt.Errorf("request creation failed: %w", err)
	}

	resp, err := tc.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	serverID := resp.Header.Get(serverHeader)
	if serverID == "" {
		return "", fmt.Errorf("missing server identification header")
	}

	tc.serverHits[serverID]++
	tc.requestCount++
	return serverID, nil
}

func TestLoadBalancerDistribution(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") == "" {
		t.Skip("Integration test skipped (set INTEGRATION_TEST to enable)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	coordinator := newTestCoordinator(ctx)
	results := make(chan string, totalRequests)
	errors := make(chan error, totalRequests)

	for i := 0; i < totalRequests; i++ {
		go func() {
			serverID, err := coordinator.executeRequest()
			if err != nil {
				errors <- err
				return
			}
			results <- serverID
		}()
		time.Sleep(retryInterval)
	}

	// Collect results
	var collectedErrors []error
	for i := 0; i < totalRequests; i++ {
		select {
		case serverID := <-results:
			t.Logf("Request %d served by: %s", i+1, serverID)
		case err := <-errors:
			collectedErrors = append(collectedErrors, err)
		case <-ctx.Done():
			t.Fatalf("Test timed out after %s. Errors: %v", testTimeout, collectedErrors)
		}
	}

	// Verify results
	if len(collectedErrors) > 0 {
		t.Errorf("Encountered %d errors:\n%v", len(collectedErrors), collectedErrors)
	}

	uniqueServers := len(coordinator.serverHits)
	if uniqueServers < requiredServers {
		t.Errorf("Required %d servers, but only %d responded. Distribution: %v",
			requiredServers, uniqueServers, coordinator.serverHits)
	} else {
		t.Logf("Successful distribution across %d servers: %v",
			uniqueServers, coordinator.serverHits)
	}
}
