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
	"github.com/yapingcat/gomedia/go-mpeg2"
)

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

	// MPEG-TS demuxers: entity_id -> *mpeg2.TSDemuxer
	demuxers   map[string]*mpeg2.TSDemuxer
	demuxersMu sync.Mutex
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
		demuxers:     make(map[string]*mpeg2.TSDemuxer),
	}
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

	// Cleanup when peer connection closes
	peerConnection.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
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
		// Extract entity ID
		parts := strings.Split(msg.Subject, ".")
		if len(parts) < 3 {
			return
		}
		entityID := parts[len(parts)-1]

		// Check if we have any active tracks for this entity
		h.tracksMu.RLock()
		track, exists := h.tracks[entityID]
		h.tracksMu.RUnlock()

		if !exists {
			return
		}

		// Get or create demuxer for this entity
		h.demuxersMu.Lock()
		demuxer, ok := h.demuxers[entityID]
		if !ok {
			demuxer = mpeg2.NewTSDemuxer()
			demuxer.OnFrame = func(cid mpeg2.TS_STREAM_TYPE, frame []byte, pts uint64, dts uint64) {
				if cid == mpeg2.TS_STREAM_H264 {
					// Write H.264 NALU to track
					if err := track.WriteSample(media.Sample{Data: frame, Duration: time.Millisecond * 33}); err != nil {
						logger.Debugw("Failed to write sample", "entity_id", entityID, "error", err)
					}
				}
			}
			h.demuxers[entityID] = demuxer
		}
		h.demuxersMu.Unlock()

		// Feed MPEG-TS data to demuxer
		if err := demuxer.Input(bytes.NewReader(msg.Data)); err != nil {
			logger.Debugw("Demuxer error", "entity_id", entityID, "error", err)
		}
	})

	return err
}
