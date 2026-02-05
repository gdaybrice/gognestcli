package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/brice/gognestcli/internal/recorder"
	nestwebrtc "github.com/brice/gognestcli/internal/webrtc"
	"github.com/pion/webrtc/v4"
)

type StreamCmd struct {
	DeviceID string `short:"d" help:"Device ID (uses config default if omitted)"`
}

func (s *StreamCmd) Run() error {
	client, cfg, err := newSDMClient()
	if err != nil {
		return err
	}

	deviceName, err := resolveDevice(client, cfg, s.DeviceID)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Streaming H264 from %s to stdout...\n", deviceDisplayNameFromFull(deviceName))
	fmt.Fprintf(os.Stderr, "Pipe to a player: gognestcli stream | ffplay -f h264 -\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Fprintf(os.Stderr, "\nStopping stream...\n")
		cancel()
	}()

	// Write raw H264 directly to stdout
	writer := &recorder.StdoutH264Writer{}

	session, offerSDP, err := nestwebrtc.NewSession(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		if strings.EqualFold(track.Codec().MimeType, webrtc.MimeTypeH264) {
			fmt.Fprintf(os.Stderr, "Video track connected\n")
			writer.HandleVideoTrack(track, ctx)
		}
	})
	if err != nil {
		return fmt.Errorf("creating WebRTC session: %w", err)
	}
	defer session.Close()

	answerSDP, mediaSessionID, err := client.GenerateWebRTCStream(deviceName, offerSDP)
	if err != nil {
		return fmt.Errorf("generating WebRTC stream: %w", err)
	}

	err = session.SetAnswer(answerSDP, mediaSessionID,
		func(msid string) error { return client.ExtendWebRTCStream(deviceName, msid) },
		func(msid string) error { return client.StopWebRTCStream(deviceName, msid) },
	)
	if err != nil {
		return fmt.Errorf("setting WebRTC answer: %w", err)
	}

	<-ctx.Done()
	return nil
}
