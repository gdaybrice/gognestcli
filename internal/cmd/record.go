package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/brice/gognestcli/internal/config"
	"github.com/brice/gognestcli/internal/recorder"
	"github.com/brice/gognestcli/internal/sdm"
	nestwebrtc "github.com/brice/gognestcli/internal/webrtc"
	"github.com/pion/webrtc/v4"
)

type RecordCmd struct {
	Duration int    `short:"d" help:"Recording duration in seconds" default:"15"`
	Output   string `short:"o" help:"Output file path" default:"recording.mp4"`
	DeviceID string `help:"Device ID (uses config default if omitted)"`
}

func (r *RecordCmd) Run() error {
	client, cfg, err := newSDMClient()
	if err != nil {
		return err
	}

	deviceName, err := resolveDevice(client, cfg, r.DeviceID)
	if err != nil {
		return err
	}

	duration := time.Duration(r.Duration) * time.Second
	fmt.Printf("Recording %s for %s...\n", deviceDisplayNameFromFull(deviceName), duration)

	err = recorder.RecordClip(r.Output, duration, func(ctx context.Context, handler func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) error {
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
		return fmt.Errorf("recording failed: %w", err)
	}

	fmt.Printf("Recording saved to %s\n", r.Output)
	return nil
}

// resolveDevice determines the device name to use, checking the argument,
// config, or auto-detecting the first camera.
func resolveDevice(client *sdm.Client, cfg *config.Config, deviceID string) (string, error) {
	if deviceID != "" {
		if strings.HasPrefix(deviceID, "enterprises/") {
			return deviceID, nil
		}
		return fmt.Sprintf("enterprises/%s/devices/%s", cfg.ProjectID, deviceID), nil
	}

	if cfg.DeviceID != "" {
		if strings.HasPrefix(cfg.DeviceID, "enterprises/") {
			return cfg.DeviceID, nil
		}
		return fmt.Sprintf("enterprises/%s/devices/%s", cfg.ProjectID, cfg.DeviceID), nil
	}

	// Auto-detect first camera
	devices, err := client.ListDevices()
	if err != nil {
		return "", fmt.Errorf("listing devices: %w", err)
	}
	for _, dev := range devices {
		if strings.Contains(dev.Type, "CAMERA") {
			return dev.Name, nil
		}
	}
	return "", fmt.Errorf("no camera device found; specify --device-id or set device_id in config")
}
