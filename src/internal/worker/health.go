package worker

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

type ServiceHealth struct {
	Name string
	URL  string
}

// StartHealthCheckWorker pings each service's /health endpoint every minute and logs the result.
func StartHealthCheckWorker(ctx context.Context, services []ServiceHealth) <-chan struct{} {
	ticker := time.NewTicker(1 * time.Minute)
	log.Println("[HEALTH WORKER] Health check worker started, ticking every 1 minute.")
	done := make(chan struct{})

	client := &http.Client{Timeout: 5 * time.Second}

	go func() {
		defer close(done)
		defer ticker.Stop()

		checkAll := func() {
			for _, svc := range services {
				if err := pingHealth(ctx, client, svc); err != nil {
					log.Printf("[HEALTH WORKER] %s is DOWN: %v", svc.Name, err)
				} else {
					// log.Printf("[HEALTH WORKER] %s is UP", svc.Name)
				}
			}
		}

		checkAll()

		for {
			select {
			case <-ticker.C:
				checkAll()
			case <-ctx.Done():
				log.Println("[HEALTH WORKER] Health check worker stopping...")
				return
			}
		}
	}()

	return done
}

func pingHealth(ctx context.Context, client *http.Client, svc ServiceHealth) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, svc.URL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}
