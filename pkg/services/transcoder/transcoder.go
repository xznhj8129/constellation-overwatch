package transcoder

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/nats-io/nats.go"
)

// Magic bytes for format detection
var (
	mpegTSSync = byte(0x47)                                       // MPEG-TS sync byte
	jpegMagic  = []byte{0xFF, 0xD8, 0xFF}                         // JPEG start
	jpegEnd    = []byte{0xFF, 0xD9}                               // JPEG end
	pngMagic   = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A} // PNG header
)

// Transcoder converts video streams (MPEG-TS/H.264) to JPEG frames
type Transcoder struct {
	nc       *nats.Conn
	sessions map[string]*transcoderSession
	mu       sync.RWMutex
}

// transcoderSession handles transcoding for a single entity stream
type transcoderSession struct {
	entityID   string
	nc         *nats.Conn
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	cancel     context.CancelFunc
	lastActive time.Time
	mu         sync.Mutex
}

// New creates a new Transcoder
func New(nc *nats.Conn) *Transcoder {
	return &Transcoder{
		nc:       nc,
		sessions: make(map[string]*transcoderSession),
	}
}

// Start begins listening for video streams that need transcoding
func (t *Transcoder) Start(ctx context.Context) error {
	// Subscribe to all video subjects
	sub, err := t.nc.Subscribe("constellation.video.>", func(msg *nats.Msg) {
		t.handleMessage(ctx, msg)
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	// Cleanup goroutine for stale sessions
	go t.cleanupLoop(ctx)

	<-ctx.Done()
	sub.Unsubscribe()
	t.closeAllSessions()
	return nil
}

// handleMessage processes incoming video data
func (t *Transcoder) handleMessage(ctx context.Context, msg *nats.Msg) {
	if len(msg.Data) < 4 {
		return
	}

	// Check if this is already a decoded format (JPEG/PNG) - pass through
	if bytes.HasPrefix(msg.Data, jpegMagic) || bytes.HasPrefix(msg.Data, pngMagic) {
		return // Already decoded, no transcoding needed
	}

	// Check if this looks like MPEG-TS (sync byte 0x47 every 188 bytes)
	if !isMPEGTS(msg.Data) {
		return
	}

	// Extract entity ID from subject: constellation.video.{entity_id}
	entityID := extractEntityID(msg.Subject)
	if entityID == "" {
		return
	}

	// Get or create transcoder session for this entity
	session := t.getOrCreateSession(ctx, entityID)
	if session == nil {
		return
	}

	// Write data to ffmpeg stdin
	session.write(msg.Data)
}

// isMPEGTS checks if data looks like MPEG-TS format
func isMPEGTS(data []byte) bool {
	// MPEG-TS packets are 188 bytes with 0x47 sync byte
	if len(data) < 188 {
		// Could be a partial packet, check for sync byte
		return data[0] == mpegTSSync
	}

	// Check for sync bytes at expected positions
	syncCount := 0
	for i := 0; i < len(data) && i < 188*4; i += 188 {
		if data[i] == mpegTSSync {
			syncCount++
		}
	}
	return syncCount >= 1
}

// extractEntityID gets entity ID from NATS subject
func extractEntityID(subject string) string {
	// Subject format: constellation.video.{entity_id}
	const prefix = "constellation.video."
	if len(subject) <= len(prefix) {
		return ""
	}
	return subject[len(prefix):]
}

// getOrCreateSession returns existing or creates new transcoder session
func (t *Transcoder) getOrCreateSession(ctx context.Context, entityID string) *transcoderSession {
	t.mu.RLock()
	session, exists := t.sessions[entityID]
	t.mu.RUnlock()

	if exists {
		session.mu.Lock()
		session.lastActive = time.Now()
		session.mu.Unlock()
		return session
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Double-check after acquiring write lock
	if session, exists = t.sessions[entityID]; exists {
		return session
	}

	// Create new session
	session, err := newTranscoderSession(ctx, entityID, t.nc)
	if err != nil {
		logger.Errorw("Failed to create transcoder session",
			"component", "Transcoder",
			"entity_id", entityID,
			"error", err)
		return nil
	}

	t.sessions[entityID] = session
	logger.Infow("Created transcoder session",
		"component", "Transcoder",
		"entity_id", entityID)

	return session
}

// newTranscoderSession creates a new ffmpeg transcoding session
func newTranscoderSession(ctx context.Context, entityID string, nc *nats.Conn) (*transcoderSession, error) {
	sessionCtx, cancel := context.WithCancel(ctx)

	// ffmpeg command: read MPEG-TS from stdin, output MJPEG frames to stdout
	cmd := exec.CommandContext(sessionCtx, "ffmpeg",
		"-f", "mpegts", // Input format
		"-i", "pipe:0", // Read from stdin
		"-f", "image2pipe", // Output format: pipe of images
		"-vcodec", "mjpeg", // Output codec
		"-q:v", "3", // Quality (2-31, lower is better)
		"-r", "30", // Output framerate
		"-an", // No audio
		"-vsync", "vfr", // Variable framerate (drop frames if needed)
		"pipe:1", // Write to stdout
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		stdin.Close()
		return nil, fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	session := &transcoderSession{
		entityID:   entityID,
		nc:         nc,
		cmd:        cmd,
		stdin:      stdin,
		cancel:     cancel,
		lastActive: time.Now(),
	}

	// Goroutine to read JPEG frames from ffmpeg stdout and publish to NATS
	go session.readFrames(stdout)

	// Goroutine to wait for process exit
	go func() {
		cmd.Wait()
		logger.Infow("Transcoder session ended",
			"component", "Transcoder",
			"entity_id", entityID)
	}()

	return session, nil
}

// write sends data to ffmpeg stdin
func (s *transcoderSession) write(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stdin == nil {
		return
	}

	s.lastActive = time.Now()
	_, err := s.stdin.Write(data)
	if err != nil {
		logger.Debugw("Failed to write to ffmpeg",
			"component", "Transcoder",
			"entity_id", s.entityID,
			"error", err)
	}
}

// readFrames reads JPEG frames from ffmpeg stdout and publishes them
func (s *transcoderSession) readFrames(stdout io.ReadCloser) {
	defer stdout.Close()

	subject := "constellation.video." + s.entityID
	buffer := make([]byte, 0, 256*1024) // 256KB buffer for frames
	readBuf := make([]byte, 32*1024)    // 32KB read chunks

	for {
		n, err := stdout.Read(readBuf)
		if err != nil {
			if err != io.EOF {
				logger.Debugw("Error reading from ffmpeg",
					"component", "Transcoder",
					"entity_id", s.entityID,
					"error", err)
			}
			return
		}

		buffer = append(buffer, readBuf[:n]...)

		// Extract complete JPEG frames from buffer
		for {
			frame, remaining := extractJPEGFrame(buffer)
			if frame == nil {
				break
			}

			// Publish decoded frame to NATS (overwrites the original MPEG-TS data)
			if err := s.nc.Publish(subject, frame); err != nil {
				logger.Debugw("Failed to publish decoded frame",
					"component", "Transcoder",
					"entity_id", s.entityID,
					"error", err)
			}

			buffer = remaining
		}

		// Prevent buffer from growing too large
		if len(buffer) > 512*1024 {
			// Find last JPEG start marker and keep from there
			lastStart := bytes.LastIndex(buffer, jpegMagic)
			if lastStart > 0 {
				buffer = buffer[lastStart:]
			} else {
				buffer = buffer[:0]
			}
		}
	}
}

// extractJPEGFrame finds and extracts a complete JPEG frame from buffer
func extractJPEGFrame(data []byte) (frame []byte, remaining []byte) {
	start := bytes.Index(data, jpegMagic)
	if start == -1 {
		return nil, data
	}

	end := bytes.Index(data[start+3:], jpegEnd)
	if end == -1 {
		return nil, data
	}

	endPos := start + 3 + end + 2 // Include the end marker
	return data[start:endPos], data[endPos:]
}

// close terminates the transcoder session
func (s *transcoderSession) close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stdin != nil {
		s.stdin.Close()
		s.stdin = nil
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

// cleanupLoop periodically removes stale transcoder sessions
func (t *Transcoder) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.cleanupStaleSessions()
		}
	}
}

// cleanupStaleSessions removes sessions inactive for more than 30 seconds
func (t *Transcoder) cleanupStaleSessions() {
	t.mu.Lock()
	defer t.mu.Unlock()

	cutoff := time.Now().Add(-30 * time.Second)
	for entityID, session := range t.sessions {
		session.mu.Lock()
		if session.lastActive.Before(cutoff) {
			session.mu.Unlock()
			session.close()
			delete(t.sessions, entityID)
			logger.Infow("Cleaned up stale transcoder session",
				"component", "Transcoder",
				"entity_id", entityID)
		} else {
			session.mu.Unlock()
		}
	}
}

// closeAllSessions terminates all active sessions
func (t *Transcoder) closeAllSessions() {
	t.mu.Lock()
	defer t.mu.Unlock()

	for entityID, session := range t.sessions {
		session.close()
		delete(t.sessions, entityID)
		logger.Infow("Closed transcoder session",
			"component", "Transcoder",
			"entity_id", entityID)
	}
}
