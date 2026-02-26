package mediamtx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/shared"

	"go.uber.org/zap"
)

// Config holds connection details for the MediaMTX REST API and WebRTC endpoint.
type Config struct {
	APIURL    string
	WebRTCURL string
}

// PathStatus represents the cached state of a single MediaMTX stream path.
type PathStatus struct {
	Name          string    `json:"name"`
	EntityID      string    `json:"entity_id"`
	Ready         bool      `json:"ready"`
	ReadyTime     time.Time `json:"readyTime"`
	Tracks        []string  `json:"tracks"`
	ReaderCount   int       `json:"readerCount"`
	BytesReceived int64     `json:"bytesReceived"`
	BytesSent     int64     `json:"bytesSent"`
}

// Client polls the MediaMTX REST API for active streams and provides WHEP URL
// construction. All public methods are nil-receiver safe so callers never need
// to guard against a nil client.
type Client struct {
	httpClient *http.Client
	cfg        *Config

	mu    sync.RWMutex
	cache map[string]PathStatus

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// apiPathItem mirrors a single item in the MediaMTX /v3/paths/list response.
type apiPathItem struct {
	Name      string    `json:"name"`
	Ready     bool      `json:"ready"`
	ReadyTime time.Time `json:"readyTime"`
	Tracks    []string  `json:"tracks"`
	Readers   []struct {
		Type string `json:"type"`
	} `json:"readers"`
	BytesReceived int64 `json:"bytesReceived"`
	BytesSent     int64 `json:"bytesSent"`
}

// apiPathsResponse mirrors the top-level MediaMTX /v3/paths/list response.
type apiPathsResponse struct {
	Items []apiPathItem `json:"items"`
}

// DefaultConfig returns a Config populated from environment variables with
// sensible defaults. An empty MEDIAMTX_API_URL means MediaMTX is disabled.
func DefaultConfig() *Config {
	return &Config{
		APIURL:    shared.GetEnv("MEDIAMTX_API_URL", ""),
		WebRTCURL: shared.GetEnv("MEDIAMTX_WEBRTC_URL", ""),
	}
}

// New creates a MediaMTX client. It returns nil when cfg.APIURL is empty,
// which is the expected way to disable MediaMTX integration. Because every
// public method is nil-receiver safe, callers can store the nil *Client and
// call methods on it without additional checks.
func New(cfg *Config) *Client {
	if cfg == nil || cfg.APIURL == "" {
		return nil
	}

	return &Client{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		cfg:        cfg,
		cache:      make(map[string]PathStatus),
	}
}

// Start spawns a background goroutine that polls the MediaMTX API every 3
// seconds and caches the result. The goroutine respects both the supplied
// parent context and the internal cancel function used by Stop.
func (c *Client) Start(ctx context.Context) {
	if c == nil {
		return
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()

		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		// Perform an initial fetch immediately on start.
		c.fetchAndCache(c.ctx)

		for {
			select {
			case <-c.ctx.Done():
				return
			case <-ticker.C:
				c.fetchAndCache(c.ctx)
			}
		}
	}()

	logger.Info("MediaMTX client started",
		zap.String("api_url", c.cfg.APIURL),
		zap.String("webrtc_url", c.cfg.WebRTCURL))
}

// Stop cancels the polling goroutine and blocks until it exits.
func (c *Client) Stop() {
	if c == nil {
		return
	}

	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()

	logger.Info("MediaMTX client stopped")
}

// fetchAndCache calls the MediaMTX /v3/paths/list endpoint, builds a new
// lookup map keyed by entity ID, and swaps it into the cache under a write
// lock. Any transient error is logged but does not discard the previous cache.
func (c *Client) fetchAndCache(ctx context.Context) {
	url := strings.TrimRight(c.cfg.APIURL, "/") + "/v3/paths/list"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		logger.Error("MediaMTX: failed to create request", zap.Error(err))
		return
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Context cancellation is expected during shutdown; do not log as error.
		if ctx.Err() != nil {
			return
		}
		logger.Warn("MediaMTX: failed to fetch paths", zap.Error(err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Warn("MediaMTX: unexpected status from paths list",
			zap.Int("status", resp.StatusCode))
		return
	}

	var apiResp apiPathsResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		logger.Error("MediaMTX: failed to decode paths response", zap.Error(err))
		return
	}

	next := make(map[string]PathStatus, len(apiResp.Items))
	for _, item := range apiResp.Items {
		entityID := extractEntityID(item.Name)
		next[entityID] = PathStatus{
			Name:          item.Name,
			EntityID:      entityID,
			Ready:         item.Ready,
			ReadyTime:     item.ReadyTime,
			Tracks:        item.Tracks,
			ReaderCount:   len(item.Readers),
			BytesReceived: item.BytesReceived,
			BytesSent:     item.BytesSent,
		}
	}

	c.mu.Lock()
	c.cache = next
	c.mu.Unlock()

	logger.Debug("MediaMTX: cache refreshed", zap.Int("streams", len(next)))
}

// GetStreamStatus returns the cached status for a single entity. The second
// return value is false when the entity has no active stream.
func (c *Client) GetStreamStatus(entityID string) (PathStatus, bool) {
	if c == nil {
		return PathStatus{}, false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	ps, ok := c.cache[entityID]
	return ps, ok
}

// GetAllStreams returns every cached stream as a slice. The order is
// non-deterministic because the underlying store is a map.
func (c *Client) GetAllStreams() []PathStatus {
	if c == nil {
		return nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]PathStatus, 0, len(c.cache))
	for _, ps := range c.cache {
		out = append(out, ps)
	}
	return out
}

// WHEPEndpoint returns the full WHEP URL for the given stream path. It returns
// an empty string when the client is nil or no WebRTC URL is configured.
func (c *Client) WHEPEndpoint(streamPath string) string {
	if c == nil {
		return ""
	}
	if c.cfg.WebRTCURL == "" {
		return ""
	}

	return fmt.Sprintf("%s/%s/whep",
		strings.TrimRight(c.cfg.WebRTCURL, "/"),
		strings.TrimLeft(streamPath, "/"))
}

// extractEntityID returns the segment after the last "/" in a MediaMTX path
// name, which by convention is the entity ID. If the path contains no slash
// the entire name is returned.
func extractEntityID(pathName string) string {
	idx := strings.LastIndex(pathName, "/")
	if idx < 0 {
		return pathName
	}
	return pathName[idx+1:]
}
