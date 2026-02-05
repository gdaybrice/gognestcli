package pubsub

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const pubsubBaseURL = "https://pubsub.googleapis.com/v1"

// Event represents a parsed Nest event from Pub/Sub.
// Event represents a parsed Nest event from Pub/Sub.
type Event struct {
	DeviceName string
	EventType  string // "CameraMotion.Motion", "CameraPerson.Person", etc.
	EventID    string // Used for CameraEventImage.GenerateImage
	Timestamp  time.Time
	Raw        json.RawMessage
}

// Listener polls a Pub/Sub subscription for Nest device events.
type Listener struct {
	subscription string
	tokenFn      func() (string, error)
	httpClient   *http.Client
}

// NewListener creates a new Pub/Sub listener.
func NewListener(subscription string, tokenFn func() (string, error)) *Listener {
	return &Listener{
		subscription: subscription,
		tokenFn:      tokenFn,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

// pullRequest is the request body for Pub/Sub pull.
type pullRequest struct {
	MaxMessages int `json:"maxMessages"`
}

// pullResponse is the response from Pub/Sub pull.
type pullResponse struct {
	ReceivedMessages []receivedMessage `json:"receivedMessages"`
}

type receivedMessage struct {
	AckID   string        `json:"ackId"`
	Message pubsubMessage `json:"message"`
}

type pubsubMessage struct {
	Data        string            `json:"data"` // base64-encoded
	Attributes  map[string]string `json:"attributes"`
	PublishTime string            `json:"publishTime"`
}

// nestEventData is the decoded Pub/Sub message for Nest events.
type nestEventData struct {
	EventID         string                     `json:"eventId"`
	Timestamp       string                     `json:"timestamp"`
	ResourceUpdate  *resourceUpdate            `json:"resourceUpdate"`
}

type resourceUpdate struct {
	Name   string                            `json:"name"`
	Events map[string]json.RawMessage        `json:"events"`
	Traits map[string]json.RawMessage        `json:"traits"`
}

// Listen starts polling for events and sends them to the handler.
// It blocks until the context is cancelled.
func (l *Listener) Listen(ctx context.Context, handler func(Event)) error {
	fmt.Printf("Listening for events on %s...\n", l.subscription)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		messages, err := l.pull(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fmt.Printf("Warning: pull error: %v\n", err)
			time.Sleep(5 * time.Second)
			continue
		}

		var ackIDs []string
		for _, msg := range messages {
			events := l.parseMessage(msg)
			for _, event := range events {
				handler(event)
			}
			ackIDs = append(ackIDs, msg.AckID)
		}

		if len(ackIDs) > 0 {
			if err := l.acknowledge(ctx, ackIDs); err != nil {
				fmt.Printf("Warning: ack error: %v\n", err)
			}
		}
	}
}

func (l *Listener) pull(ctx context.Context) ([]receivedMessage, error) {
	tok, err := l.tokenFn()
	if err != nil {
		return nil, fmt.Errorf("getting token: %w", err)
	}

	body, _ := json.Marshal(pullRequest{MaxMessages: 10})

	req, err := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s/%s:pull", pubsubBaseURL, l.subscription),
		bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pull returned %d: %s", resp.StatusCode, string(respBody))
	}

	var pr pullResponse
	if err := json.Unmarshal(respBody, &pr); err != nil {
		return nil, err
	}

	return pr.ReceivedMessages, nil
}

func (l *Listener) acknowledge(ctx context.Context, ackIDs []string) error {
	tok, err := l.tokenFn()
	if err != nil {
		return err
	}

	payload := map[string]interface{}{
		"ackIds": ackIDs,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s/%s:acknowledge", pubsubBaseURL, l.subscription),
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("acknowledge returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (l *Listener) parseMessage(msg receivedMessage) []Event {
	data, err := base64.StdEncoding.DecodeString(msg.Message.Data)
	if err != nil {
		return nil
	}

	var ned nestEventData
	if err := json.Unmarshal(data, &ned); err != nil {
		return nil
	}

	if ned.ResourceUpdate == nil || len(ned.ResourceUpdate.Events) == 0 {
		return nil
	}

	ts, _ := time.Parse(time.RFC3339Nano, ned.Timestamp)

	var events []Event
	for eventType, raw := range ned.ResourceUpdate.Events {
		// Extract eventId from the event data
		var eventData struct {
			EventSessionID string `json:"eventSessionId"`
			EventID        string `json:"eventId"`
		}
		json.Unmarshal(raw, &eventData)

		events = append(events, Event{
			DeviceName: ned.ResourceUpdate.Name,
			EventType:  eventType,
			EventID:    eventData.EventID,
			Timestamp:  ts,
			Raw:        raw,
		})
	}
	return events
}
