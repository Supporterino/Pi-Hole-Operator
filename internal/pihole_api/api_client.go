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

const (
	MaxResponseSize = 25 * 1024 * 1024 // 25MB (for DoS protection)
)

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

// Authenticate logs in and stores the session ID.
func (c *APIClient) Authenticate() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	url := fmt.Sprintf("%s/api/auth", c.BaseURL)
	payload := map[string]string{"password": c.password}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal authentication payload: %w", err)
	}

	c.logger.V(1).Info(fmt.Sprintf("Authenticating to %s", c.BaseURL))

	resp, err := c.Client.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		c.logger.Error(err, "Authentication request failed.")
		return fmt.Errorf("authentication request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Error(err, "Failed to close response body.")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("authentication failed, status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseSize)) // Prevent
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

	c.sessionID = authResp.Session.SID
	c.validity = time.Now().Add(time.Duration(authResp.Session.Validity) * time.Second)
	c.logger.V(1).Info("Authentication successful")
	return nil
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

// FetchData makes a GET request to the specified endpoint and parses the response.
func (c *APIClient) FetchData(endpoint string, result interface{}) error {
	if err := c.ensureAuth(); err != nil {
		return err
	}

	url := fmt.Sprintf("%s%s", c.BaseURL, endpoint)
	c.logger.V(1).Info(fmt.Sprintf("Fetching data from %s", url))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add security headers
	req.Header.Set("X-FTL-SID", c.sessionID)
	req.Header.Set("X-Content-Type-Options", "nosniff")

	ctx, cancel := context.WithTimeout(context.Background(), c.Client.Timeout)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := c.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch data from %s: %w", url, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Error(err, "Failed to close response body.")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("non-200 status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseSize)) // prevent reading too much data
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("failed to parse JSON response: %w", err)
	}

	c.logger.V(1).Info("Successfully fetched data from endpoint: %s", endpoint)
	return nil
}

// DownloadTeleporter fetches the binary from /api/teleporter on the RW pod.
// It returns a byte slice that callers can forward to other pods.
func (c *APIClient) DownloadTeleporter(ctx context.Context) ([]byte, error) {
	if err := c.ensureAuth(); err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	url := c.BaseURL + "/api/teleporter"
	c.logger.V(1).Info(fmt.Sprintf("GET teleporter from %s", url))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
		return nil, fmt.Errorf("GET teleporter status %d", resp.StatusCode)
	}

	// Read the whole body – it is a binary file, so no limit needed.
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read teleporter body: %w", err)
	}
	return data, nil
}

// UploadTeleporter posts the binary to /api/teleporter on a read‑only pod.
// The caller provides the byte slice that was downloaded from the RW replica.
//
// A retry loop with exponential back‑off is added so that a transient
// “connection refused” or similar error does not immediately cause the whole sync to fail.
func (c *APIClient) UploadTeleporter(ctx context.Context, data []byte) error {
	if err := c.ensureAuth(); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	url := c.BaseURL + "/api/teleporter"
	c.logger.V(1).Info(fmt.Sprintf("POST teleporter to %s", url))

	// ---------- build the multipart body ----------
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", "teleporter.zip")
	if err != nil {
		return fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return fmt.Errorf("write data to form file: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close multipart writer: %w", err)
	}

	// ---------- retry parameters ----------
	const (
		maxRetries     = 3                      // total attempts = maxRetries + 1
		initialBackoff = 500 * time.Millisecond // first wait
		maxBackoff     = 5 * time.Second        // cap on back‑off
	)

	backOff := initialBackoff

	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
		if err != nil {
			return fmt.Errorf("new request: %w", err) // not retriable – bail out
		}
		req.Header.Set("X-FTL-SID", c.sessionID)
		req.Header.Set("Content-Type", writer.FormDataContentType())

		resp, err := c.Client.Do(req)
		if err == nil {
			// Successful request – check the status code
			defer func(Body io.ReadCloser) {
				err := Body.Close()
				if err != nil {
					c.logger.Error(err, "Failed to close response body.")
				}
			}(resp.Body)
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
				return nil
			}
			err = fmt.Errorf("POST teleporter status %d", resp.StatusCode)
		}

		// If this was the last attempt, return the error.
		if attempt == maxRetries {
			return fmt.Errorf("upload failed after %d attempts: %w", attempt+1, err)
		}

		// Log the failure and wait before retrying.
		c.logger.V(1).Info("upload failed – retrying", "attempt", attempt+1, "error", err)
		time.Sleep(backOff)

		// Double the back‑off for next try, but never exceed maxBackoff.
		backOff *= 2
		if backOff > maxBackoff {
			backOff = maxBackoff
		}

		// Reset the buffer so that we can re‑send the multipart body.
		buf.Reset()
		if err := writer.WriteField("file", "teleporter.zip"); err == nil {
			writer.Close()
		}
	}

	return fmt.Errorf("unreachable – should never get here")
}

// Close cleans up resources used by the API client
func (c *APIClient) Close() {
	// Close the transport to ensure no connection leaks
	if transport, ok := c.Client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
}
