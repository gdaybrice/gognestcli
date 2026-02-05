package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	"github.com/brice/gognestcli/internal/recorder"
	nestwebrtc "github.com/brice/gognestcli/internal/webrtc"
	"github.com/pion/webrtc/v4"
)

type LiveCmd struct {
	DeviceID string `short:"d" help:"Device ID (uses config default if omitted)"`
}

func (l *LiveCmd) Run() error {
	if _, err := exec.LookPath("ffplay"); err != nil {
		return fmt.Errorf("ffplay is required for live view; install it with: brew install ffmpeg")
	}

	client, cfg, err := newSDMClient()
	if err != nil {
		return err
	}

	deviceName, err := resolveDevice(client, cfg, l.DeviceID)
	if err != nil {
		return err
	}

	fmt.Printf("Starting live view from %s...\n", deviceDisplayNameFromFull(deviceName))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Println("\nStopping live view...")
		cancel()
	}()

	// Start ffplay reading H264 from stdin
	ffplay := exec.CommandContext(ctx, "ffplay",
		"-f", "h264",
		"-framerate", "30",
		"-probesize", "32",
		"-analyzeduration", "0",
		"-fflags", "nobuffer",
		"-flags", "low_delay",
		"-framedrop",
		"-window_title", "gognestcli live",
		"-",
	)
	ffplay.Stderr = os.Stderr

	stdinPipe, err := ffplay.StdinPipe()
	if err != nil {
		return fmt.Errorf("creating ffplay pipe: %w", err)
	}

	if err := ffplay.Start(); err != nil {
		return fmt.Errorf("starting ffplay: %w", err)
	}

	writer := &recorder.PipeH264Writer{W: stdinPipe}

	session, offerSDP, err := nestwebrtc.NewSession(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		if strings.EqualFold(track.Codec().MimeType, webrtc.MimeTypeH264) {
			fmt.Println("Video track connected, streaming to ffplay...")
			writer.HandleVideoTrack(track, ctx)
		}
	})
	if err != nil {
		stdinPipe.Close()
		ffplay.Wait()
		return fmt.Errorf("creating WebRTC session: %w", err)
	}
	defer session.Close()

	answerSDP, mediaSessionID, err := client.GenerateWebRTCStream(deviceName, offerSDP)
	if err != nil {
		stdinPipe.Close()
		ffplay.Wait()
		return fmt.Errorf("generating WebRTC stream: %w", err)
	}

	err = session.SetAnswer(answerSDP, mediaSessionID,
		func(msid string) error { return client.ExtendWebRTCStream(deviceName, msid) },
		func(msid string) error { return client.StopWebRTCStream(deviceName, msid) },
	)
	if err != nil {
		stdinPipe.Close()
		ffplay.Wait()
		return fmt.Errorf("setting WebRTC answer: %w", err)
	}

	// Wait for ffplay to exit (user closes window) or ctrl-c
	done := make(chan error, 1)
	go func() { done <- ffplay.Wait() }()

	select {
	case err := <-done:
		if err != nil && ctx.Err() == nil {
			return fmt.Errorf("ffplay exited: %w", err)
		}
	case <-ctx.Done():
		stdinPipe.Close()
		<-done
	}

	return nil
}
