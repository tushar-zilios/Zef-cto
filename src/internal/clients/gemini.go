package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"
)

const (
	maxRetries        = 3
	initialBackoff    = 2 * time.Second
	backoffMultiplier = 2
)

var retryableStatusCodes = map[int]bool{
	429: true,
	500: true,
	502: true,
	503: true,
	504: true,
}

type GeminiRequest struct {
	Contents          []Content          `json:"contents"`
	SystemInstruction *SystemInstruction `json:"systemInstruction,omitempty"`
}

type Content struct {
	Parts []Part `json:"parts"`
}

type Part struct {
	Text string `json:"text"`
}

type SystemInstruction struct {
	Parts []Part `json:"parts"`
}

type GeminiResponse struct {
	Candidates []Candidate `json:"candidates"`
}

type Candidate struct {
	Content Content `json:"content"`
}

func GenerateContent(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return GenerateContentWithModel(ctx, systemPrompt, userPrompt, "gemini-2.5-flash")
}

func GenerateContentWithModel(ctx context.Context, systemPrompt, userPrompt, model string) (string, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("GEMINI_API_KEY environment variable is not set")
	}

	reqBody := GeminiRequest{
		Contents: []Content{{Parts: []Part{{Text: userPrompt}}}},
	}
	if systemPrompt != "" {
		reqBody.SystemInstruction = &SystemInstruction{Parts: []Part{{Text: systemPrompt}}}
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	if model == "" {
		model = "gemini-2.5-flash"
	}
	apiURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, apiKey)
	client := &http.Client{Timeout: 60 * time.Second}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := initialBackoff * time.Duration(pow(backoffMultiplier, attempt-1))
			jitter := time.Duration(float64(backoff) * (0.8 + 0.4*rand.Float64()))
			log.Printf("[Gemini] Retrying (attempt %d/%d) after %v...", attempt, maxRetries, jitter)
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("request cancelled: %w", ctx.Err())
			case <-time.After(jitter):
			}
		}

		req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonBytes))
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("http request failed: %w", err)
			continue
		}

		if resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var geminiResp GeminiResponse
			if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
				return "", fmt.Errorf("failed to decode response: %w", err)
			}
			if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
				return "", fmt.Errorf("empty response from Gemini API")
			}
			return geminiResp.Candidates[0].Content.Parts[0].Text, nil
		}

		respBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		lastErr = fmt.Errorf("api request failed with status %d: %s", resp.StatusCode, string(respBytes))
		if !retryableStatusCodes[resp.StatusCode] {
			return "", lastErr
		}
	}

	return "", fmt.Errorf("request failed after %d retries: %w", maxRetries, lastErr)
}

func pow(base, exp int) int {
	result := 1
	for i := 0; i < exp; i++ {
		result *= base
	}
	return result
}
