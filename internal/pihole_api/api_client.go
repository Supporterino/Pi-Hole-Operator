package pihole_api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/go-logr/logr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type APIClient struct {
	BaseURL   string
	Client    *http.Client
	password  string
	sessionID string
	logger    logr.Logger
	validity  time.Time
	mu        sync.Mutex
}

type authResponse struct {
	Session struct {
		Valid    bool   `json:"valid"`
		SID      string `json:"sid"`
		Validity int    `json:"validity"`
	} `json:"session"`
}

// AdList represents a single ad‑list entry that PiHole exposes.
type AdList struct {
	Address string  `json:"address"`
	ID      int     `json:"id,omitempty"`
	Comment *string `json:"comment,omitempty"`
}

// listsResponse is the JSON shape returned by /api/lists.
type listsResponse struct {
	Lists []AdList `json:"lists"`
}

const (
	MaxResponseSize = 25 * 1024 * 1024 // 25MB (for DoS protection)
)

// ---------------------------------------------------------------------------
// Generic exponential back‑off helper
// ---------------------------------------------------------------------------

// retryFunc defines the signature of a function that can be retried.
// It should return nil on success or an error to retry.
type retryFunc func() error

// backoffConfig holds the parameters for a single retry loop.
type backoffConfig struct {
	// How many attempts we will make in total (maxRetries + 1).
	MaxRetries int
	// First wait interval.
	Initial time.Duration
	// Largest interval we will wait between attempts.
	Max time.Duration
}

// retryWithBackoff runs fn repeatedly until it succeeds or we exhaust the
// number of attempts.  On each failure it waits an exponentially increasing
// interval (capped by cfg.Max). The function logs each retry attempt.
func retryWithBackoff(ctx context.Context, log logr.Logger, cfg backoffConfig, fn retryFunc) error {
	// We make a copy of the config so we can modify backOff.
	backOff := cfg.Initial

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if err := fn(); err == nil { // success
			return nil
		} else {

			// If we’re on the last attempt, give up.
			if attempt == cfg.MaxRetries {
				return fmt.Errorf("after %d attempts: %w", attempt+1, err)
			}

			// Log the failure and wait before retrying.
			log.V(1).Info("retrying operation", "attempt", attempt+1, "err", err)
			select {
			case <-time.After(backOff):
				// nothing – continue loop
			case <-ctx.Done():
				return ctx.Err()
			}

			backOff *= 2
			if backOff > cfg.Max {
				backOff = cfg.Max
			}
		}
	}

	// Unreachable – the loop always returns.
	return nil
}

// NewAPIClient initializes and returns a new APIClient.
func NewAPIClient(baseURL string, password string, timeout time.Duration, skipTLSVerification bool, ctx context.Context) *APIClient {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: skipTLSVerification,
		},
	}

	return &APIClient{
		BaseURL:  baseURL,
		password: password,
		logger:   logf.FromContext(ctx),
		Client: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}
}

// ---------------------------------------------------------------------------
// APIClient.Authenticate – now with back‑off
// ---------------------------------------------------------------------------

func (c *APIClient) Authenticate() error {
	// We only need the lock for setting the session – the retry logic
	// itself does not modify any shared state.
	c.mu.Lock()
	defer c.mu.Unlock()

	// --------------------------------------------------------------------
	// Helper that performs the actual HTTP request.
	// --------------------------------------------------------------------
	doAuth := func() error {
		address := fmt.Sprintf("%s/api/auth", c.BaseURL)
		payload := map[string]string{"password": c.password}
		jsonPayload, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal authentication payload: %w", err)
		}

		// Log the authentication attempt – this is helpful when multiple
		// clients are created, and you want to see which instance is trying.
		c.logger.V(1).Info("Authenticating", "url", address)

		resp, err := c.Client.Post(address, "application/json", bytes.NewBuffer(jsonPayload))
		if err != nil {
			// The error is non‑retryable – we log it and return.
			c.logger.V(1).Info("POST authentication request failed")
			return fmt.Errorf("authentication request failed: %w", err)
		}
		defer func() {
			if cerr := resp.Body.Close(); cerr != nil {
				c.logger.Error(cerr, "Failed to close response body.")
			}
		}()

		// Log the status code – a 4xx/5xx is very likely to be a mis‑config
		// that you want to see in the logs.
		c.logger.V(1).Info("Authentication response", "statusCode", resp.StatusCode)
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("authentication failed, status code: %d", resp.StatusCode)
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseSize))
		if err != nil {
			return fmt.Errorf("failed to read authentication response: %w", err)
		}

		var authResp authResponse
		if err := json.Unmarshal(body, &authResp); err != nil {
			return fmt.Errorf("failed to parse authentication response: %w", err)
		}

		if !authResp.Session.Valid {
			return fmt.Errorf("authentication unsuccessful")
		}

		// Successful authentication – log the session id length to avoid
		// leaking secrets while still confirming that we received a token.
		c.sessionID = authResp.Session.SID
		c.validity = time.Now().Add(time.Duration(authResp.Session.Validity) * time.Second)
		c.logger.V(1).Info("Authentication successful", "sessionIDLength", len(c.sessionID))
		return nil
	}

	// --------------------------------------------------------------------
	// Retry the authentication with exponential back‑off.
	// --------------------------------------------------------------------
	cfg := backoffConfig{
		MaxRetries: 5, // total attempts = 4
		Initial:    500 * time.Millisecond,
		Max:        5 * time.Second,
	}

	return retryWithBackoff(context.Background(), c.logger, cfg, doAuth)
}

// ensureAuth ensures the session is valid before making a request.
func (c *APIClient) ensureAuth() error {
	c.mu.Lock()
	// Check if authentication is needed
	needsAuth := time.Now().After(c.validity)
	// Always unlock the mutex before calling Authenticate
	c.mu.Unlock()

	// NOTE! Authenticate() already acquires c.mu internally.
	// introducing locking here (even RWMutex) would cause a deadlock.
	if needsAuth {
		c.logger.V(1).Info("Session expired, re-authenticating")
		return c.Authenticate()
	}
	return nil
}

// DownloadTeleporter fetches the binary from /api/teleporter on the RW pod.
// It returns a byte slice that callers can forward to other pods.
func (c *APIClient) DownloadTeleporter(ctx context.Context) ([]byte, error) {
	if err := c.ensureAuth(); err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	address := c.BaseURL + "/api/teleporter"
	c.logger.V(1).Info("Downloading teleporter", "url", address)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("X-FTL-SID", c.sessionID)

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET teleporter: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			c.logger.Error(err, "Failed to close response body.")
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		c.logger.V(1).Info("DownloadTeleporter response", "statusCode", resp.StatusCode)
		return nil, fmt.Errorf("GET teleporter status %d", resp.StatusCode)
	}

	// Read the entire body (binary) – we log its size for diagnostics.
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read teleporter body: %w", err)
	}
	c.logger.V(1).Info("Teleporter downloaded successfully", "bytesRead", len(data))
	return data, nil
}

// UploadTeleporter posts the binary to /api/teleporter on a read‑only pod.
// The caller provides the byte slice that was downloaded from the RW replica.
//
// The function now delegates all retry logic to `retryWithBackoff`.  It builds
// the multipart body once and re‑uses it for every attempt, which keeps the
// code small and easy to read.
func (c *APIClient) UploadTeleporter(ctx context.Context, data []byte) error {
	// 1️⃣ Make sure we are authenticated first.
	if err := c.ensureAuth(); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	address := c.BaseURL + "/api/teleporter"
	c.logger.V(1).Info("Uploading teleporter", "url", address, "bytesToSend", len(data))

	// --------------------------------------------------------------------
	// Build the multipart payload once.
	// --------------------------------------------------------------------
	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)

	part, err := writer.CreateFormFile("file", "teleporter.zip")
	if err != nil {
		return fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return fmt.Errorf("write data to form file: %w", err)
	}
	contentType := writer.FormDataContentType()
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close multipart writer: %w", err)
	}

	// --------------------------------------------------------------------
	// Retry configuration – same back‑off as Authenticate().
	// --------------------------------------------------------------------
	cfg := backoffConfig{
		MaxRetries: 5, // total attempts = 4
		Initial:    500 * time.Millisecond,
		Max:        5 * time.Second,
	}

	// --------------------------------------------------------------------
	// Operation that will be retried on transient failures.
	// --------------------------------------------------------------------
	doUpload := func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(payload.Bytes()))
		if err != nil {
			return fmt.Errorf("new request: %w", err) // not retriable – bail out
		}
		req.Header.Set("X-FTL-SID", c.sessionID)
		req.Header.Set("Content-Type", contentType)

		resp, err := c.Client.Do(req)
		if err != nil {
			return fmt.Errorf("POST teleporter: %w", err)
		}
		defer func() {
			if cerr := resp.Body.Close(); cerr != nil {
				c.logger.Error(cerr, "Failed to close response body.")
			}
		}()

		// Log the status code – non‑2xx indicates a problem.
		c.logger.V(1).Info("UploadTeleporter response", "statusCode", resp.StatusCode)
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			return fmt.Errorf("POST teleporter status %d", resp.StatusCode)
		}
		return nil
	}

	// --------------------------------------------------------------------
	// Retry with exponential back‑off and log each attempt
	// --------------------------------------------------------------------
	return retryWithBackoff(ctx, c.logger, cfg, doUpload)
}

// ---------------------------------------------------------------------------
// APIClient helpers – fetch /api/lists
// ---------------------------------------------------------------------------

// GetAdLists retrieves the current list of ad‑lists from the PiHole instance.
func (c *APIClient) GetAdLists(ctx context.Context) ([]AdList, error) {
	address := fmt.Sprintf("%s/api/lists", c.BaseURL)

	if err := c.ensureAuth(); err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("X-FTL-SID", c.sessionID)

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET ad-lists: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			c.logger.Error(err, "Failed to close response body.")
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned status %d", address, resp.StatusCode)
	}

	var r listsResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("decode %s: %w", address, err)
	}

	return r.Lists, nil
}

// PostAdList creates a new ad‑list with the given address.
func (c *APIClient) PostAdList(ctx context.Context, addr string) error {
	address := fmt.Sprintf("%s/api/lists", c.BaseURL)

	if err := c.ensureAuth(); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	payload := map[string]interface{}{
		"address": addr,
		"type":    "block",
		"comment": "Created by PiHole operator",
		"groups":  []int{0},
		"enabled": true,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("create POST request: %w", err)
	}
	req.Header.Set("X-FTL-SID", c.sessionID)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Client.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", address, err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("POST %s returned status %d", address, resp.StatusCode)
	}

	return nil
}

// DeleteAdList removes an ad‑list identified by its *address*.
// The PiHole API expects the address to be URL‑escaped and a query
// parameter `type=block` (the type that was used when the list was created).
func (c *APIClient) DeleteAdList(ctx context.Context, addr string) error {
	// Encode the address as required by PiHole
	encoded := url.QueryEscape(addr)
	address := fmt.Sprintf("%s/api/lists/%s?type=block", c.BaseURL, encoded)

	if err := c.ensureAuth(); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, address, nil)
	if err != nil {
		return fmt.Errorf("create DELETE request: %w", err)
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE %s: %w", address, err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("DELETE %s returned status %d", address, resp.StatusCode)
	}

	return nil
}

// RunGravity triggers an gravity run to update ad-lists
func (c *APIClient) RunGravity(ctx context.Context) error {
	address := fmt.Sprintf("%s/api/action/gravity", c.BaseURL)

	if err := c.ensureAuth(); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, nil)
	if err != nil {
		return fmt.Errorf("create POST request: %w", err)
	}
	req.Header.Set("X-FTL-SID", c.sessionID)

	resp, err := c.Client.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", address, err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("POST %s returned status %d", address, resp.StatusCode)
	}

	return nil
}

// Close cleans up resources used by the API client
func (c *APIClient) Close() {
	// Close the underlying HTTP transport to avoid leaking idle connections.
	if transport, ok := c.Client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
}
