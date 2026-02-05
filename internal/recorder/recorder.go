package recorder

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
)

// H264Writer collects raw H264 Annex B data from a WebRTC video track.
type H264Writer struct {
	mu       sync.Mutex
	file     *os.File
	filename string
	frames   int
}

// NewH264Writer creates a writer that saves raw H264 Annex B stream.
func NewH264Writer(filename string) (*H264Writer, error) {
	f, err := os.Create(filename)
	if err != nil {
		return nil, err
	}
	return &H264Writer{file: f, filename: filename}, nil
}

// HandleVideoTrack reads H264 RTP packets and writes Annex B NAL units.
func (w *H264Writer) HandleVideoTrack(track *webrtc.TrackRemote, ctx context.Context) {
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
			w.mu.Lock()
			if w.file != nil {
				w.file.Write(sample.Data)
				w.frames++
			}
			w.mu.Unlock()
		}
	}
}

// Frames returns the number of frames written so far.
func (w *H264Writer) Frames() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.frames
}

// Close closes the file.
func (w *H264Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}

// StdoutH264Writer writes raw H264 Annex B data to stdout.
type StdoutH264Writer struct{}

// HandleVideoTrack reads H264 RTP packets and writes Annex B NAL units to stdout.
func (w *StdoutH264Writer) HandleVideoTrack(track *webrtc.TrackRemote, ctx context.Context) {
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
			if _, err := os.Stdout.Write(sample.Data); err != nil {
				return
			}
		}
	}
}

// PipeH264Writer writes raw H264 Annex B data to an io.Writer.
type PipeH264Writer struct {
	W io.Writer
}

// HandleVideoTrack reads H264 RTP packets and writes Annex B NAL units to the pipe.
func (w *PipeH264Writer) HandleVideoTrack(track *webrtc.TrackRemote, ctx context.Context) {
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
			if _, err := w.W.Write(sample.Data); err != nil {
				return
			}
		}
	}
}

// TakeSnapshot captures a JPEG frame from a WebRTC camera stream.
// It writes raw H264 to a temp file and uses ffmpeg to extract a frame.
func TakeSnapshot(outputPath string, startStream func(ctx context.Context, handler func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) error) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg is required for snapshots; install it with: brew install ffmpeg")
	}

	tmpH264 := outputPath + ".tmp.h264"
	defer os.Remove(tmpH264)

	h264w, err := NewH264Writer(tmpH264)
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gotVideo := make(chan struct{}, 1)

	err = startStream(ctx, func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		if strings.EqualFold(track.Codec().MimeType, webrtc.MimeTypeH264) {
			select {
			case gotVideo <- struct{}{}:
			default:
			}
			h264w.HandleVideoTrack(track, ctx)
		}
		// Ignore audio for snapshots
	})
	if err != nil {
		h264w.Close()
		return fmt.Errorf("starting stream: %w", err)
	}

	// Wait for video track, then collect a few seconds of frames
	select {
	case <-gotVideo:
		fmt.Println("Receiving video, capturing frames...")
	case <-ctx.Done():
		h264w.Close()
		return fmt.Errorf("timed out waiting for video track")
	}

	// Wait until we have some frames, up to 5 seconds
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			goto extract
		case <-ticker.C:
			if h264w.Frames() >= 30 {
				goto extract
			}
		}
	}

extract:
	h264w.Close()

	// Use ffmpeg to extract a JPEG from the raw H264 stream
	ext := strings.ToLower(filepath.Ext(outputPath))
	if ext == ".webm" {
		return h264ToWebM(tmpH264, outputPath)
	}

	return h264ToJPEG(tmpH264, outputPath)
}

func h264ToJPEG(h264Path, jpegPath string) error {
	cmd := exec.Command("ffmpeg",
		"-y",
		"-f", "h264",
		"-i", h264Path,
		"-frames:v", "1",
		"-q:v", "2",
		jpegPath,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg conversion failed: %w\n%s", err, string(output))
	}
	return nil
}

func h264ToWebM(h264Path, webmPath string) error {
	cmd := exec.Command("ffmpeg",
		"-y",
		"-f", "h264",
		"-i", h264Path,
		"-c:v", "copy",
		webmPath,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg conversion failed: %w\n%s", err, string(output))
	}
	return nil
}

// RecordClip records a WebRTC stream to a file using ffmpeg for muxing.
// Duration is how long to record. Output format is determined by file extension.
func RecordClip(outputPath string, duration time.Duration, startStream func(ctx context.Context, handler func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) error) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg is required for recording; install it with: brew install ffmpeg")
	}

	tmpH264 := outputPath + ".tmp.h264"
	defer os.Remove(tmpH264)

	h264w, err := NewH264Writer(tmpH264)
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), duration+15*time.Second)
	defer cancel()

	gotVideo := make(chan struct{}, 1)

	err = startStream(ctx, func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		if strings.EqualFold(track.Codec().MimeType, webrtc.MimeTypeH264) {
			select {
			case gotVideo <- struct{}{}:
			default:
			}
			h264w.HandleVideoTrack(track, ctx)
		}
	})
	if err != nil {
		h264w.Close()
		return fmt.Errorf("starting stream: %w", err)
	}

	// Wait for video then record for the requested duration
	select {
	case <-gotVideo:
		fmt.Println("Receiving video, recording...")
	case <-ctx.Done():
		h264w.Close()
		return fmt.Errorf("timed out waiting for video track")
	}

	time.Sleep(duration)
	h264w.Close()

	// Mux with ffmpeg
	ext := strings.ToLower(filepath.Ext(outputPath))
	if ext == ".mp4" {
		return h264ToMP4(tmpH264, outputPath)
	}
	return h264ToWebM(tmpH264, outputPath)
}

func h264ToMP4(h264Path, mp4Path string) error {
	cmd := exec.Command("ffmpeg",
		"-y",
		"-f", "h264",
		"-i", h264Path,
		"-c:v", "copy",
		mp4Path,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg conversion failed: %w\n%s", err, string(output))
	}
	return nil
}
