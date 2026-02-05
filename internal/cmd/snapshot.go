package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	nestwebrtc "github.com/brice/gognestcli/internal/webrtc"

	"github.com/brice/gognestcli/internal/recorder"
	"github.com/pion/webrtc/v4"
)

type SnapshotCmd struct {
	Output   string `short:"o" help:"Output file path" default:"snapshot.jpg"`
	DeviceID string `short:"d" help:"Device ID (uses config default if omitted)"`
}

func (s *SnapshotCmd) Run() error {
	client, cfg, err := newSDMClient()
	if err != nil {
		return err
	}

	deviceName, err := resolveDevice(client, cfg, s.DeviceID)
	if err != nil {
		return err
	}

	fmt.Printf("Taking snapshot from %s...\n", deviceDisplayNameFromFull(deviceName))

	err = recorder.TakeSnapshot(s.Output, func(ctx context.Context, handler func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) error {
		session, offerSDP, err := nestwebrtc.NewSession(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
			handler(track, receiver)
		})
		if err != nil {
			return err
		}

		answerSDP, mediaSessionID, err := client.GenerateWebRTCStream(deviceName, offerSDP)
		if err != nil {
			session.Close()
			return fmt.Errorf("generating WebRTC stream: %w", err)
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
		return fmt.Errorf("snapshot failed: %w", err)
	}

	fmt.Printf("Snapshot saved to %s\n", s.Output)
	return nil
}

func deviceDisplayNameFromFull(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return name
}
