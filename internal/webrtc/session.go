package webrtc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

const (
	extendInterval = 4 * time.Minute
	pliInterval    = 2 * time.Second
)

// TrackHandler is called when a remote track is received.
type TrackHandler func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver)

// Session manages a WebRTC connection to a Nest camera.
type Session struct {
	pc             *webrtc.PeerConnection
	mediaSessionID string

	extendFn func(mediaSessionID string) error
	stopFn   func(mediaSessionID string) error

	// Connected is closed when the ICE connection reaches the connected state.
	Connected chan struct{}

	mu     sync.Mutex
	closed bool
	cancel context.CancelFunc
}

// NewSession creates a WebRTC PeerConnection configured for Nest camera streaming.
// It returns the SDP offer to send to the SDM API.
func NewSession(onTrack TrackHandler) (*Session, string, error) {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
		BundlePolicy: webrtc.BundlePolicyMaxBundle,
	}

	m := &webrtc.MediaEngine{}

	// H264 video codec (profile 42e01f = Constrained Baseline)
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
		},
		PayloadType: 96,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, "", fmt.Errorf("registering H264 codec: %w", err)
	}

	// Opus audio codec
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeOpus,
			ClockRate: 48000,
			Channels:  2,
		},
		PayloadType: 111,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, "", fmt.Errorf("registering Opus codec: %w", err)
	}

	api := webrtc.NewAPI(webrtc.WithMediaEngine(m))

	pc, err := api.NewPeerConnection(config)
	if err != nil {
		return nil, "", fmt.Errorf("creating peer connection: %w", err)
	}

	// Add transceivers in the required order: audio recvonly, video recvonly, then data channel.
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		pc.Close()
		return nil, "", fmt.Errorf("adding audio transceiver: %w", err)
	}

	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		pc.Close()
		return nil, "", fmt.Errorf("adding video transceiver: %w", err)
	}

	// Data channel is required for Nest WebRTC
	if _, err := pc.CreateDataChannel("dataSendChannel", nil); err != nil {
		pc.Close()
		return nil, "", fmt.Errorf("creating data channel: %w", err)
	}

	sess := &Session{
		pc:        pc,
		Connected: make(chan struct{}),
	}

	connectedOnce := sync.Once{}
	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		fmt.Printf("ICE connection state: %s\n", state.String())
		if state == webrtc.ICEConnectionStateConnected {
			connectedOnce.Do(func() { close(sess.Connected) })
		}
		if state == webrtc.ICEConnectionStateFailed {
			fmt.Println("ICE connection failed — check network/firewall settings")
		}
	})

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		fmt.Printf("Track received: %s (%s)\n", track.Kind().String(), track.Codec().MimeType)
		if onTrack != nil {
			onTrack(track, receiver)
		}
	})

	// Create offer
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		pc.Close()
		return nil, "", fmt.Errorf("creating offer: %w", err)
	}

	// Set local description and wait for ICE gathering
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		pc.Close()
		return nil, "", fmt.Errorf("setting local description: %w", err)
	}
	<-gatherComplete

	return sess, pc.LocalDescription().SDP, nil
}

// SetAnswer sets the remote SDP answer and starts background tasks.
func (s *Session) SetAnswer(answerSDP, mediaSessionID string, extendFn func(string) error, stopFn func(string) error) error {
	s.mediaSessionID = mediaSessionID
	s.extendFn = extendFn
	s.stopFn = stopFn

	answer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	}
	if err := s.pc.SetRemoteDescription(answer); err != nil {
		return fmt.Errorf("setting remote description: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	go s.pliLoop(ctx)
	go s.extendLoop(ctx)

	return nil
}

// Close terminates the WebRTC session.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	if s.cancel != nil {
		s.cancel()
	}

	if s.stopFn != nil && s.mediaSessionID != "" {
		_ = s.stopFn(s.mediaSessionID)
	}

	return s.pc.Close()
}

func (s *Session) pliLoop(ctx context.Context) {
	ticker := time.NewTicker(pliInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, receiver := range s.pc.GetReceivers() {
				track := receiver.Track()
				if track != nil && track.Kind() == webrtc.RTPCodecTypeVideo {
					_ = s.pc.WriteRTCP([]rtcp.Packet{
						&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())},
					})
				}
			}
		}
	}
}

func (s *Session) extendLoop(ctx context.Context) {
	ticker := time.NewTicker(extendInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.extendFn != nil && s.mediaSessionID != "" {
				if err := s.extendFn(s.mediaSessionID); err != nil {
					fmt.Printf("Warning: failed to extend stream: %v\n", err)
				}
			}
		}
	}
}
