package recorder

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/at-wat/ebml-go/webm"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
)

// Recorder captures WebRTC media tracks to a WebM file.
type Recorder struct {
	mu       sync.Mutex
	writers  []webm.BlockWriteCloser
	filename string
	started  time.Time
}

// NewRecorder creates a new WebM recorder that writes to the given file.
func NewRecorder(filename string) *Recorder {
	return &Recorder{filename: filename}
}

// HandleTrack processes an incoming WebRTC track and writes it to the WebM file.
func (r *Recorder) HandleTrack(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver, ctx context.Context) {
	codec := track.Codec()

	if strings.EqualFold(codec.MimeType, webrtc.MimeTypeH264) {
		r.recordVideo(track, ctx)
	} else if strings.EqualFold(codec.MimeType, webrtc.MimeTypeOpus) {
		r.recordAudio(track, ctx)
	}
}

func (r *Recorder) recordVideo(track *webrtc.TrackRemote, ctx context.Context) {
	builder := samplebuilder.New(128, &codecs.H264Packet{}, track.Codec().ClockRate)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		pkt, _, err := track.ReadRTP()
		if err != nil {
			return
		}

		builder.Push(pkt)
		for {
			sample := builder.Pop()
			if sample == nil {
				break
			}
			r.writeVideoSample(sample.Data, sample.Duration)
		}
	}
}

func (r *Recorder) recordAudio(track *webrtc.TrackRemote, ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		pkt, _, err := track.ReadRTP()
		if err != nil {
			return
		}

		r.writeAudioSample(pkt)
	}
}

func (r *Recorder) initWriters() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.writers != nil {
		return nil
	}

	f, err := os.Create(r.filename)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}

	tracks := []webm.TrackEntry{
		{
			Name:            "Video",
			TrackNumber:     1,
			TrackUID:        1,
			CodecID:         "V_MPEG4/ISO/AVC",
			TrackType:       1,
			DefaultDuration: 33333333, // ~30fps
			Video: &webm.Video{
				PixelWidth:  1920,
				PixelHeight: 1080,
			},
		},
		{
			Name:        "Audio",
			TrackNumber: 2,
			TrackUID:    2,
			CodecID:     "A_OPUS",
			TrackType:   2,
			Audio: &webm.Audio{
				SamplingFrequency: 48000,
				Channels:          2,
			},
		},
	}

	ws, err := webm.NewSimpleBlockWriter(f, tracks)
	if err != nil {
		f.Close()
		return fmt.Errorf("creating WebM writer: %w", err)
	}

	r.writers = ws
	r.started = time.Now()
	return nil
}

func (r *Recorder) writeVideoSample(data []byte, duration time.Duration) {
	if err := r.initWriters(); err != nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	ts := int64(time.Since(r.started) / time.Millisecond)
	if len(r.writers) > 0 {
		_, _ = r.writers[0].Write(true, ts, data)
	}
	_ = duration // timestamp based on wall clock
}

func (r *Recorder) writeAudioSample(pkt *rtp.Packet) {
	if err := r.initWriters(); err != nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	ts := int64(time.Since(r.started) / time.Millisecond)
	if len(r.writers) > 1 {
		_, _ = r.writers[1].Write(true, ts, pkt.Payload)
	}
}

// Close finalizes the WebM file.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, w := range r.writers {
		w.Close()
	}
	r.writers = nil
	return nil
}

// ExtractSnapshot takes a WebM file and extracts the first frame as a JPEG using ffmpeg.
func ExtractSnapshot(webmPath, jpegPath string) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not found in PATH; install ffmpeg for JPEG snapshots")
	}

	cmd := exec.Command("ffmpeg",
		"-y",
		"-i", webmPath,
		"-frames:v", "1",
		"-q:v", "2",
		jpegPath,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w\n%s", err, string(output))
	}
	return nil
}

// TakeSnapshot records a brief clip and extracts a JPEG frame.
// If ffmpeg is not available, saves a short WebM clip instead.
func TakeSnapshot(outputPath string, startStream func(ctx context.Context, handler func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) error) error {
	ext := strings.ToLower(filepath.Ext(outputPath))

	webmPath := outputPath
	wantJPEG := ext == ".jpg" || ext == ".jpeg"
	if wantJPEG {
		webmPath = outputPath + ".tmp.webm"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rec := NewRecorder(webmPath)

	gotData := make(chan struct{}, 1)
	err := startStream(ctx, func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		select {
		case gotData <- struct{}{}:
		default:
		}
		rec.HandleTrack(track, receiver, ctx)
	})
	if err != nil {
		return fmt.Errorf("starting stream: %w", err)
	}

	// Wait for first track data, then record a few more seconds for a usable frame
	select {
	case <-gotData:
		fmt.Println("Receiving media, capturing...")
		time.Sleep(3 * time.Second)
	case <-ctx.Done():
		// timed out waiting for media
	}

	if err := rec.Close(); err != nil {
		return fmt.Errorf("closing recorder: %w", err)
	}

	if wantJPEG {
		defer os.Remove(webmPath)
		if err := ExtractSnapshot(webmPath, outputPath); err != nil {
			// Fallback: keep the WebM
			finalWebM := strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + ".webm"
			os.Rename(webmPath, finalWebM)
			fmt.Printf("Warning: %v\nSaved WebM clip instead: %s\n", err, finalWebM)
			return nil
		}
	}

	return nil
}
