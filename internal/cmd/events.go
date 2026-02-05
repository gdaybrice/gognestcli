package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brice/gognestcli/internal/auth"
	"github.com/brice/gognestcli/internal/config"
	"github.com/brice/gognestcli/internal/pubsub"
	"github.com/brice/gognestcli/internal/recorder"
	"github.com/brice/gognestcli/internal/sdm"
	"github.com/brice/gognestcli/internal/secrets"
	nestwebrtc "github.com/brice/gognestcli/internal/webrtc"
	"github.com/pion/webrtc/v4"
)

type EventsCmd struct {
	OutputDir string `short:"o" help:"Directory to save event captures" default:"events"`
	Capture   bool   `help:"Auto-capture snapshot on events" default:"true"`
	Clip      bool   `help:"Also record a short video clip on events" default:"false"`
	ClipSecs  int    `help:"Clip duration in seconds" default:"10"`
}

func (e *EventsCmd) Run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	if cfg.PubSubSub == "" {
		return fmt.Errorf("pubsub_subscription not configured in config.json")
	}

	store, err := secrets.NewStore()
	if err != nil {
		return fmt.Errorf("opening keyring: %w", err)
	}

	refreshToken, err := store.LoadRefreshToken()
	if err != nil {
		return err
	}

	tm := auth.NewTokenManager(cfg.ClientID, cfg.ClientSecret)
	tokenFn := func() (string, error) {
		return tm.AccessToken(refreshToken)
	}

	sdmClient := sdm.NewClient(cfg.ProjectID, tokenFn)

	if e.Capture || e.Clip {
		if err := os.MkdirAll(e.OutputDir, 0755); err != nil {
			return fmt.Errorf("creating output dir: %w", err)
		}
	}

	listener := pubsub.NewListener(cfg.PubSubSub, tokenFn)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	var dedup sync.Map
	var captureSeq atomic.Int64

	// Semaphore: one snapshot + one clip can run concurrently
	snapSem := make(chan struct{}, 1)
	clipSem := make(chan struct{}, 1)

	return listener.Listen(ctx, func(event pubsub.Event) {
		shortType := event.EventType
		if parts := strings.Split(event.EventType, "."); len(parts) > 0 {
			shortType = parts[len(parts)-1]
		}

		// Dedup by event timestamp + type
		dedupKey := event.Timestamp.String() + event.EventType
		if _, loaded := dedup.LoadOrStore(dedupKey, true); loaded {
			return
		}
		go func() {
			time.Sleep(1 * time.Minute)
			dedup.Delete(dedupKey)
		}()

		ts := event.Timestamp.Format("15:04:05")
		deviceShort := deviceDisplayNameFromFull(event.DeviceName)
		fmt.Printf("[%s] %s: %s\n", ts, deviceShort, shortType)

		if !isActionableEvent(event.EventType) {
			return
		}

		seq := captureSeq.Add(1)

		// Snapshot via event image API (fast, no WebRTC needed)
		if e.Capture && event.EventID != "" {
			select {
			case snapSem <- struct{}{}:
				go func() {
					defer func() { <-snapSem }()
					e.captureEventImage(sdmClient, event, seq)
				}()
			default:
				fmt.Println("  Skipping snapshot (previous still in progress)")
			}
		}

		// Clip via WebRTC
		if e.Clip {
			select {
			case clipSem <- struct{}{}:
				go func() {
					defer func() { <-clipSem }()
					e.captureClip(sdmClient, cfg, event, seq)
				}()
			default:
				fmt.Println("  Skipping clip (previous still recording)")
			}
		}
	})
}

func isActionableEvent(eventType string) bool {
	return strings.Contains(eventType, "Motion") || strings.Contains(eventType, "Person")
}

func (e *EventsCmd) captureEventImage(client *sdm.Client, event pubsub.Event, seq int64) {
	shortType := "event"
	if parts := strings.Split(event.EventType, "."); len(parts) > 0 {
		shortType = strings.ToLower(parts[len(parts)-1])
	}

	filename := fmt.Sprintf("%s_%s_%03d.jpg", time.Now().Format("20060102-150405"), shortType, seq)
	outputPath := filepath.Join(e.OutputDir, filename)

	fmt.Printf("  Downloading event image: %s\n", filename)

	img, err := client.GenerateEventImage(event.DeviceName, event.EventID)
	if err != nil {
		fmt.Printf("  Warning: event image failed: %v\n", err)
		return
	}

	if err := client.DownloadEventImage(img, outputPath); err != nil {
		fmt.Printf("  Warning: image download failed: %v\n", err)
		return
	}

	fmt.Printf("  Saved: %s\n", outputPath)
}

func (e *EventsCmd) captureClip(client *sdm.Client, cfg *config.Config, event pubsub.Event, seq int64) {
	deviceName := event.DeviceName
	if deviceName == "" {
		return
	}

	shortType := "event"
	if parts := strings.Split(event.EventType, "."); len(parts) > 0 {
		shortType = strings.ToLower(parts[len(parts)-1])
	}

	filename := fmt.Sprintf("%s_%s_%03d.mp4", time.Now().Format("20060102-150405"), shortType, seq)
	outputPath := filepath.Join(e.OutputDir, filename)
	duration := time.Duration(e.ClipSecs) * time.Second

	fmt.Printf("  Recording %s clip: %s\n", duration, filename)

	err := recorder.RecordClip(outputPath, duration, func(ctx context.Context, handler func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) error {
		session, offerSDP, err := nestwebrtc.NewSession(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
			handler(track, receiver)
		})
		if err != nil {
			return err
		}

		answerSDP, mediaSessionID, err := client.GenerateWebRTCStream(deviceName, offerSDP)
		if err != nil {
			session.Close()
			return err
		}

		err = session.SetAnswer(answerSDP, mediaSessionID,
			func(msid string) error { return client.ExtendWebRTCStream(deviceName, msid) },
			func(msid string) error { return client.StopWebRTCStream(deviceName, msid) },
		)
		if err != nil {
			session.Close()
			return err
		}

		go func() {
			<-ctx.Done()
			time.Sleep(500 * time.Millisecond)
			session.Close()
		}()

		return nil
	})

	if err != nil {
		fmt.Printf("  Warning: clip failed: %v\n", err)
	} else {
		fmt.Printf("  Saved: %s\n", outputPath)
	}
}
