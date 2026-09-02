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
	resp, err := client.Get("http://127.0.0.1:" + port + "/ready")
	if err != nil {
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}
