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
	balancerURL     = "http://balancer:8090/api/v1/some-data"
	serverHeader    = "lb-from"
	totalRequests   = 10
	requestTimeout  = 3 * time.Second
	testTimeout     = 30 * time.Second
	retryDelay      = 500 * time.Millisecond
)

type testCoordinator struct {
	client     *http.Client
	ctx        context.Context
	serverHits map[string]int
}

func newTestCoordinator(ctx context.Context) *testCoordinator {
	return &testCoordinator{
		client: &http.Client{Timeout: requestTimeout},
		ctx:    ctx,
		serverHits: make(map[string]int),
	}
}

func (tc *testCoordinator) executeRequest() (string, error) {
	req, err := http.NewRequestWithContext(tc.ctx, http.MethodGet, balancerURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := tc.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	server := resp.Header.Get(serverHeader)
	if server == "" {
		return "", fmt.Errorf("missing '%s' header", serverHeader)
	}

	tc.serverHits[server]++
	return server, nil
}

func TestLoadBalancerDistribution(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") == "" {
		t.Skip("Skip integration test (set INTEGRATION_TEST to enable)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	tc := newTestCoordinator(ctx)
	results := make(chan string, totalRequests)
	errs := make(chan error, totalRequests)

	for i := 0; i < totalRequests; i++ {
		go func() {
			server, err := tc.executeRequest()
			if err != nil {
				errs <- err
				return
			}
			results <- server
		}()
		time.Sleep(retryDelay)
	}

	var errors []error
	for i := 0; i < totalRequests; i++ {
		select {
		case <-ctx.Done():
			t.Fatal("Test timeout reached")
		case err := <-errs:
			errors = append(errors, err)
		case <-results:
		}
	}

	if len(errors) > 0 {
		t.Errorf("Errors during requests (%d/%d):", len(errors), totalRequests)
		for _, err := range errors {
			t.Logf("- %v", err)
		}
	}

	if len(tc.serverHits) < 1 {
		t.Errorf("Expected at least %d servers, got %d. Hits: %v",
			1, len(tc.serverHits), tc.serverHits)
	} else {
		t.Logf("Load balanced across servers: %v", tc.serverHits)
	}
}
