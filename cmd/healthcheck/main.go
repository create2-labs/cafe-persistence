// healthcheck probes PERSISTENCE_HEALTH_PORT /ready for distroless container healthchecks.
package main

import (
	"net/http"
	"os"
	"time"
)

func main() {
	port := os.Getenv("PERSISTENCE_HEALTH_PORT")
	if port == "" {
		port = "8081"
	}

	client := &http.Client{Timeout: 4 * time.Second}
	// G704: the host is loopback; the port comes from PERSISTENCE_HEALTH_PORT.
	resp, err := client.Get("http://127.0.0.1:" + port + "/ready") // #nosec G704
	if err != nil {
		os.Exit(1)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}
