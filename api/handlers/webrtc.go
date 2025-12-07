package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	embeddednats "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/embedded-nats"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/nats-io/nats.go"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/h264reader"
	"github.com/yapingcat/gomedia/go-mpeg2"
)

// H.264 timing constants
const (
	h264ClockRate    = 90000 // H.264 clock rate in Hz (90kHz)
	defaultFrameRate = 30    // Default frame rate assumption
)

// entityVideoState tracks video stream state per entity for keyframe buffering
type entityVideoState struct {
	// Keyframe buffering for new peer fast-start
	lastKeyframe    []byte    // Most recent IDR frame with SPS/PPS
	lastKeyframePTS uint64    // PTS of the last keyframe
	hasReceivedIDR  bool      // Whether we've seen at least one IDR
	keyframeTime    time.Time // Wall clock time of last keyframe

	// Frame timing for accurate duration calculation
	prevPTS       uint64        // Previous frame PTS for duration calc
	prevFrameTime time.Time     // Wall clock of previous frame
	avgDuration   time.Duration // Running average frame duration

	// Statistics
	framesReceived  uint64
	framesDropped   uint64
	keyframesCount  uint64
	greenFrameRisk  uint64 // Frames sent before first IDR (debug)
}

// WebRTCHandler handles WebRTC signaling and media streaming
type WebRTCHandler struct {
	natsEmbedded *embeddednats.EmbeddedNATS
	api          *webrtc.API

	// Active peer connections: session_id -> PeerConnection
	peers   map[string]*webrtc.PeerConnection
	peersMu sync.RWMutex

	// Active video tracks: entity_id -> TrackLocalStaticSample
	tracks   map[string]*webrtc.TrackLocalStaticSample
	tracksMu sync.RWMutex

	// Per-entity video state for keyframe buffering and timing
	entityState   map[string]*entityVideoState
	entityStateMu sync.RWMutex

	// Ingress channels for decoupling NATS from processing
	// entity_id -> chan []byte
	ingressChan   map[string]chan []byte
	ingressChanMu sync.RWMutex
}

// NewWebRTCHandler creates a new WebRTC handler
func NewWebRTCHandler(natsEmbedded *embeddednats.EmbeddedNATS) *WebRTCHandler {
	// Create a MediaEngine object to configure the supported codec
	m := &webrtc.MediaEngine{}

	// Setup the codecs you want to use.
	// We'll use H.264 since that's what we expect from the source (MPEG-TS)
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000, Channels: 0, SDPFmtpLine: "", RTCPFeedback: nil},
		PayloadType:        96,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		panic(err)
	}

	// Create the API object with the MediaEngine
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m))

	return &WebRTCHandler{
		natsEmbedded: natsEmbedded,
		api:          api,
		peers:        make(map[string]*webrtc.PeerConnection),
		tracks:       make(map[string]*webrtc.TrackLocalStaticSample),
		entityState:  make(map[string]*entityVideoState),
		ingressChan:  make(map[string]chan []byte),
	}
}

// getOrCreateEntityState returns or creates video state for an entity
func (h *WebRTCHandler) getOrCreateEntityState(entityID string) *entityVideoState {
	h.entityStateMu.Lock()
	defer h.entityStateMu.Unlock()

	if state, ok := h.entityState[entityID]; ok {
		return state
	}

	state := &entityVideoState{
		avgDuration: time.Second / defaultFrameRate, // Default 33ms for 30fps
	}
	h.entityState[entityID] = state
	return state
}

// getEntityState returns entity state if it exists (read-only access)
func (h *WebRTCHandler) getEntityState(entityID string) *entityVideoState {
	h.entityStateMu.RLock()
	defer h.entityStateMu.RUnlock()
	return h.entityState[entityID]
}

// isKeyframe checks if the H.264 NAL unit is an IDR (keyframe) or contains SPS/PPS
// Uses pion's h264reader NAL unit type constants
func isKeyframe(frame []byte) bool {
	if len(frame) < 5 {
		return false
	}

	// H.264 NAL units start with a start code (0x00 0x00 0x00 0x01 or 0x00 0x00 0x01)
	// followed by the NAL header byte where the lower 5 bits are the NAL type
	offset := 0

	// Find start code
	if bytes.HasPrefix(frame, []byte{0x00, 0x00, 0x00, 0x01}) {
		offset = 4
	} else if bytes.HasPrefix(frame, []byte{0x00, 0x00, 0x01}) {
		offset = 3
	}

	if offset == 0 || offset >= len(frame) {
		// No start code found, assume Annex B format with NAL directly
		// Check first byte as NAL header
		nalType := h264reader.NalUnitType(frame[0] & 0x1F)
		return nalType == h264reader.NalUnitTypeCodedSliceIdr ||
			nalType == h264reader.NalUnitTypeSPS ||
			nalType == h264reader.NalUnitTypePPS
	}

	nalType := h264reader.NalUnitType(frame[offset] & 0x1F)
	return nalType == h264reader.NalUnitTypeCodedSliceIdr ||
		nalType == h264reader.NalUnitTypeSPS ||
		nalType == h264reader.NalUnitTypePPS
}

// getNALType extracts the NAL unit type from an H.264 frame
// Returns the pion h264reader.NalUnitType for comparison with constants
func getNALType(frame []byte) h264reader.NalUnitType {
	if len(frame) < 1 {
		return h264reader.NalUnitTypeUnspecified
	}

	offset := 0
	if bytes.HasPrefix(frame, []byte{0x00, 0x00, 0x00, 0x01}) {
		offset = 4
	} else if bytes.HasPrefix(frame, []byte{0x00, 0x00, 0x01}) {
		offset = 3
	}

	if offset >= len(frame) {
		return h264reader.NalUnitTypeUnspecified
	}

	return h264reader.NalUnitType(frame[offset] & 0x1F)
}

// calculateFrameDuration computes frame duration from PTS delta or uses average
func (state *entityVideoState) calculateFrameDuration(pts uint64) time.Duration {
	now := time.Now()

	// First frame - use default
	if state.prevPTS == 0 {
		state.prevPTS = pts
		state.prevFrameTime = now
		return state.avgDuration
	}

	// Calculate duration from PTS (90kHz clock)
	var duration time.Duration
	if pts > state.prevPTS {
		ptsDelta := pts - state.prevPTS
		// Convert from 90kHz to nanoseconds: (delta / 90000) * 1e9
		duration = time.Duration(ptsDelta) * time.Second / h264ClockRate
	} else {
		// PTS wrapped or went backwards - use wall clock delta
		duration = now.Sub(state.prevFrameTime)
	}

	// Sanity check: clamp to reasonable range (1ms to 200ms)
	if duration < time.Millisecond {
		duration = state.avgDuration
	} else if duration > 200*time.Millisecond {
		duration = state.avgDuration
	}

	// Update running average (exponential moving average, alpha=0.1)
	state.avgDuration = (state.avgDuration*9 + duration) / 10

	state.prevPTS = pts
	state.prevFrameTime = now

	return duration
}

// SignalRequest represents the signaling request body
type SignalRequest struct {
	Offer    webrtc.SessionDescription `json:"offer"`
	EntityID string                    `json:"entity_id"`
}

// Signal handles the initial WebRTC handshake request
// POST /api/v1/webrtc/signal
func (h *WebRTCHandler) Signal(w http.ResponseWriter, r *http.Request) {
	var req SignalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.EntityID == "" {
		http.Error(w, "entity_id is required", http.StatusBadRequest)
		return
	}

	// Create a new PeerConnection
	peerConnection, err := h.api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get or create the video track for this entity
	track := h.getOrCreateTrack(req.EntityID)

	// Add the track to the PeerConnection
	rtpSender, err := peerConnection.AddTrack(track)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Read incoming RTCP packets
	// Before these packets are returned they are processed by interceptors. For things
	// like NACK this needs to be called.
	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()

	// Set the remote SessionDescription
	if err = peerConnection.SetRemoteDescription(req.Offer); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Create an answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Create channel that is blocked until ICE Gathering is complete
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	// Sets the LocalDescription, and starts our UDP listeners
	if err = peerConnection.SetLocalDescription(answer); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Block until ICE Gathering is complete
	<-gatherComplete

	// Return the answer
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(*peerConnection.LocalDescription())

	// Send cached keyframe to new peer for fast start (prevents green frames)
	go func() {
		// Wait for connection to be established
		time.Sleep(100 * time.Millisecond)

		if state := h.getEntityState(req.EntityID); state != nil && state.hasReceivedIDR && len(state.lastKeyframe) > 0 {
			// Send the cached keyframe so decoder can start immediately
			if err := track.WriteSample(media.Sample{
				Data:     state.lastKeyframe,
				Duration: state.avgDuration,
			}); err != nil {
				logger.Debugw("Failed to send cached keyframe to new peer",
					"entity_id", req.EntityID,
					"error", err)
			} else {
				logger.Infow("Sent cached keyframe to new peer for fast start",
					"entity_id", req.EntityID,
					"keyframe_size", len(state.lastKeyframe),
					"keyframe_age_ms", time.Since(state.keyframeTime).Milliseconds())
			}
		}
	}()

	// Cleanup when peer connection closes
	peerConnection.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		logger.Debugw("Peer connection state changed",
			"entity_id", req.EntityID,
			"state", s.String())

		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed {
			// TODO: Cleanup peer from map if we store it
		}
	})
}

// getOrCreateTrack returns an existing track for the entity or creates a new one
func (h *WebRTCHandler) getOrCreateTrack(entityID string) *webrtc.TrackLocalStaticSample {
	h.tracksMu.Lock()
	defer h.tracksMu.Unlock()

	if track, ok := h.tracks[entityID]; ok {
		return track
	}

	// Create a new video track
	track, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", entityID)
	if err != nil {
		logger.Errorw("Failed to create track", "entity_id", entityID, "error", err)
		return nil
	}

	h.tracks[entityID] = track
	return track
}

// Start begins consuming video from NATS and feeding WebRTC tracks
func (h *WebRTCHandler) Start() error {
	nc := h.natsEmbedded.Connection()
	if nc == nil {
		return fmt.Errorf("NATS not connected")
	}

	// Subscribe to all video subjects
	// We assume the source sends MPEG-TS packets on constellation.video.>
	_, err := nc.Subscribe("constellation.video.>", func(msg *nats.Msg) {
		// Recover from panics in the demuxer (e.g. out of range errors)
		defer func() {
			if r := recover(); r != nil {
				logger.Errorw("Panic in WebRTC video handler", "error", r, "subject", msg.Subject, "data_len", len(msg.Data))
			}
		}()

		// Extract entity ID
		parts := strings.Split(msg.Subject, ".")
		if len(parts) < 3 {
			return
		}
		entityID := parts[len(parts)-1]

		// Check if we have any active tracks for this entity
		// Note: We don't need to lock/check tracks here anymore as the worker handles it
		// But we might want to skip processing if no one is watching?
		// For now, let's process everything to ensure keyframes are seen.

		// Check for MPEG-TS sync byte (0x47)
		// We relax the length check here because we will buffer the data
		if len(msg.Data) > 0 && msg.Data[0] != 0x47 {
			// If it doesn't start with 0x47, it might be a continuation or garbage.
			// For now, we assume if the FIRST byte of a message isn't 0x47, it's not a fresh MPEG-TS packet.
			// However, with buffering, we might receive the tail end of a previous packet.
			// But typically NATS messages are framed.
			// Let's just check if it LOOKS like MJPEG (FF D8) and reject that.
			if len(msg.Data) >= 2 && msg.Data[0] == 0xFF && msg.Data[1] == 0xD8 {
				return
			}
		}

		// Get or create ingress channel for this entity
		h.ingressChanMu.Lock()
		ch, exists := h.ingressChan[entityID]
		if !exists {
			// Create buffered channel to handle bursts
			ch = make(chan []byte, 100)
			h.ingressChan[entityID] = ch

			// Start worker goroutine
			go h.processEntityVideo(entityID, ch)
		}
		h.ingressChanMu.Unlock()

		// Push data to channel (non-blocking drop if full to prevent slow consumer)
		select {
		case ch <- msg.Data:
			// Success
		default:
			logger.Warnw("Video ingress channel full, dropping frame", "entity_id", entityID)
		}
	})

	return err
}

// processEntityVideo handles the video pipeline for a single entity
// It buffers incoming data, extracts MPEG-TS packets, and feeds the demuxer
// with keyframe detection and dynamic frame duration for optimal playback
func (h *WebRTCHandler) processEntityVideo(entityID string, ch chan []byte) {
	// Local buffer for this entity
	var buffer []byte

	// Get or create entity state for keyframe buffering
	state := h.getOrCreateEntityState(entityID)

	// Accumulator for building complete keyframes (SPS + PPS + IDR)
	var keyframeAccum []byte
	var pendingSPS, pendingPPS []byte

	// Create demuxer for this entity
	demuxer := mpeg2.NewTSDemuxer()
	demuxer.OnFrame = func(cid mpeg2.TS_STREAM_TYPE, frame []byte, pts uint64, dts uint64) {
		if cid != mpeg2.TS_STREAM_H264 {
			return
		}

		state.framesReceived++
		nalType := getNALType(frame)

		// Accumulate SPS/PPS for keyframe building
		switch nalType {
		case h264reader.NalUnitTypeSPS:
			pendingSPS = make([]byte, len(frame))
			copy(pendingSPS, frame)
			return // Don't send SPS alone
		case h264reader.NalUnitTypePPS:
			pendingPPS = make([]byte, len(frame))
			copy(pendingPPS, frame)
			return // Don't send PPS alone
		case h264reader.NalUnitTypeAUD, h264reader.NalUnitTypeSEI:
			// Skip access unit delimiters and SEI, don't count as frames
			return
		}

		// Check if this is a keyframe (IDR)
		isIDR := nalType == h264reader.NalUnitTypeCodedSliceIdr

		if isIDR {
			state.keyframesCount++

			// Build complete keyframe: SPS + PPS + IDR
			keyframeAccum = nil
			if len(pendingSPS) > 0 {
				keyframeAccum = append(keyframeAccum, pendingSPS...)
			}
			if len(pendingPPS) > 0 {
				keyframeAccum = append(keyframeAccum, pendingPPS...)
			}
			keyframeAccum = append(keyframeAccum, frame...)

			// Cache the complete keyframe for new peer fast-start
			state.lastKeyframe = make([]byte, len(keyframeAccum))
			copy(state.lastKeyframe, keyframeAccum)
			state.lastKeyframePTS = pts
			state.keyframeTime = time.Now()

			if !state.hasReceivedIDR {
				state.hasReceivedIDR = true
				logger.Infow("First keyframe received, video decoding enabled",
					"entity_id", entityID,
					"keyframe_size", len(keyframeAccum),
					"frames_dropped_before_idr", state.greenFrameRisk)
			}
		}

		// Drop frames until we have a keyframe (prevents green frames)
		if !state.hasReceivedIDR {
			state.greenFrameRisk++
			state.framesDropped++
			if state.greenFrameRisk%30 == 1 { // Log every ~1 second at 30fps
				logger.Debugw("Waiting for keyframe, dropping P/B frames",
					"entity_id", entityID,
					"frames_dropped", state.greenFrameRisk)
			}
			return
		}

		// Calculate frame duration from PTS
		duration := state.calculateFrameDuration(pts)

		// Get track and send frame
		track := h.getOrCreateTrack(entityID)
		if track == nil {
			return
		}

		// For keyframes, send the complete accumulated frame (SPS+PPS+IDR)
		frameToSend := frame
		if isIDR && len(keyframeAccum) > 0 {
			frameToSend = keyframeAccum
		}

		if err := track.WriteSample(media.Sample{
			Data:     frameToSend,
			Duration: duration,
		}); err != nil {
			state.framesDropped++
			// Log only occasionally
			if state.framesDropped%100 == 1 {
				logger.Warnw("Failed to write sample to track",
					"entity_id", entityID,
					"error", err,
					"total_dropped", state.framesDropped)
			}
		}
	}

	logger.Infow("Video worker started",
		"entity_id", entityID,
		"channel_capacity", cap(ch))

	// Process loop
	for data := range ch {
		// Recover from panics in this worker
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorw("Panic in video worker", "entity_id", entityID, "error", r)
					// Reset buffer on panic to clear bad data
					buffer = nil
				}
			}()

			// Append to buffer
			buffer = append(buffer, data...)

			// Fast path: if buffer is exactly aligned, process all of it
			if len(buffer)%188 == 0 {
				if err := demuxer.Input(bytes.NewReader(buffer)); err != nil {
					logger.Debugw("Demuxer error", "entity_id", entityID, "error", err)
				}
				buffer = nil
				return
			}

			// Calculate how many complete 188-byte packets we have
			nPackets := len(buffer) / 188
			if nPackets == 0 {
				return
			}

			// Extract the chunk of complete packets
			chunkSize := nPackets * 188
			chunk := buffer[:chunkSize]

			// Feed complete MPEG-TS packets to demuxer
			if err := demuxer.Input(bytes.NewReader(chunk)); err != nil {
				logger.Debugw("Demuxer error", "entity_id", entityID, "error", err)
			}

			// Keep the remainder
			remainder := buffer[chunkSize:]
			newBuffer := make([]byte, len(remainder))
			copy(newBuffer, remainder)
			buffer = newBuffer
		}()
	}

	logger.Infow("Video worker stopped",
		"entity_id", entityID,
		"total_frames", state.framesReceived,
		"total_dropped", state.framesDropped,
		"keyframes", state.keyframesCount)
}
