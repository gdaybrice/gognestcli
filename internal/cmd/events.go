package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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

	if e.Capture {
		if err := os.MkdirAll(e.OutputDir, 0755); err != nil {
			return fmt.Errorf("creating output dir: %w", err)
		}
	}

	listener := pubsub.NewListener(cfg.PubSubSub, tokenFn)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	return listener.Listen(ctx, func(event pubsub.Event) {
		shortType := event.EventType
		if parts := strings.Split(event.EventType, "."); len(parts) > 0 {
			shortType = parts[len(parts)-1]
		}

		ts := event.Timestamp.Format("15:04:05")
		deviceShort := deviceDisplayNameFromFull(event.DeviceName)
		fmt.Printf("[%s] %s: %s\n", ts, deviceShort, shortType)

		if e.Capture && isActionableEvent(event.EventType) {
			go e.captureEvent(sdmClient, cfg, event)
		}
	})
}

func isActionableEvent(eventType string) bool {
	return strings.Contains(eventType, "Motion") || strings.Contains(eventType, "Person")
}

func (e *EventsCmd) captureEvent(client *sdm.Client, cfg *config.Config, event pubsub.Event) {
	deviceName := event.DeviceName
	if deviceName == "" {
		return
	}

	ts := time.Now().Format("20060102-150405")
	shortType := "event"
	if parts := strings.Split(event.EventType, "."); len(parts) > 0 {
		shortType = strings.ToLower(parts[len(parts)-1])
	}

	filename := fmt.Sprintf("%s_%s.jpg", ts, shortType)
	outputPath := filepath.Join(e.OutputDir, filename)

	fmt.Printf("  Capturing snapshot: %s\n", filename)

	err := recorder.TakeSnapshot(outputPath, func(ctx context.Context, handler func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) error {
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
		fmt.Printf("  Warning: capture failed: %v\n", err)
	} else {
		fmt.Printf("  Saved: %s\n", outputPath)
	}
}
