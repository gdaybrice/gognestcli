package sdm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

const baseURL = "https://smartdevicemanagement.googleapis.com/v1"

// Client is a lightweight SDM REST API client.
type Client struct {
	projectID  string
	httpClient *http.Client
	token      func() (string, error)
}

// NewClient creates a new SDM client. tokenFn is called to get a valid access token.
func NewClient(projectID string, tokenFn func() (string, error)) *Client {
	return &Client{
		projectID:  projectID,
		httpClient: &http.Client{},
		token:      tokenFn,
	}
}

// Device represents a Nest device from the SDM API.
type Device struct {
	Name       string                            `json:"name"`
	Type       string                            `json:"type"`
	Traits     map[string]json.RawMessage        `json:"traits"`
	ParentRelations []ParentRelation             `json:"parentRelations"`
}

// ParentRelation links a device to its parent structure/room.
type ParentRelation struct {
	Parent      string `json:"parent"`
	DisplayName string `json:"displayName"`
}

// DeviceListResponse is the response from ListDevices.
type DeviceListResponse struct {
	Devices []Device `json:"devices"`
}

// ListDevices returns all devices in the project.
func (c *Client) ListDevices() ([]Device, error) {
	var resp DeviceListResponse
	if err := c.get(fmt.Sprintf("/enterprises/%s/devices", c.projectID), &resp); err != nil {
		return nil, err
	}
	return resp.Devices, nil
}

// GetDevice returns a single device by its full resource name.
func (c *Client) GetDevice(name string) (*Device, error) {
	var dev Device
	if err := c.get("/"+name, &dev); err != nil {
		return nil, err
	}
	return &dev, nil
}

// ExecuteCommand sends a command to a device.
func (c *Client) ExecuteCommand(deviceName, command string, params map[string]interface{}) (json.RawMessage, error) {
	body := map[string]interface{}{
		"command": command,
		"params":  params,
	}
	var result struct {
		Results json.RawMessage `json:"results"`
	}
	if err := c.post(fmt.Sprintf("/%s:executeCommand", deviceName), body, &result); err != nil {
		return nil, err
	}
	return result.Results, nil
}

// GenerateWebRTCStream initiates a WebRTC stream for a camera device.
func (c *Client) GenerateWebRTCStream(deviceName, offerSDP string) (answerSDP string, mediaSessionID string, err error) {
	params := map[string]interface{}{
		"offerSdp": offerSDP,
	}
	raw, err := c.ExecuteCommand(deviceName, "sdm.devices.commands.CameraLiveStream.GenerateWebRtcStream", params)
	if err != nil {
		return "", "", err
	}
	var result struct {
		AnswerSDP      string `json:"answerSdp"`
		MediaSessionID string `json:"mediaSessionId"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", "", fmt.Errorf("parsing WebRTC response: %w", err)
	}
	return result.AnswerSDP, result.MediaSessionID, nil
}

// ExtendWebRTCStream extends an active WebRTC stream session.
func (c *Client) ExtendWebRTCStream(deviceName, mediaSessionID string) error {
	params := map[string]interface{}{
		"mediaSessionId": mediaSessionID,
	}
	_, err := c.ExecuteCommand(deviceName, "sdm.devices.commands.CameraLiveStream.ExtendWebRtcStream", params)
	return err
}

// StopWebRTCStream stops an active WebRTC stream session.
func (c *Client) StopWebRTCStream(deviceName, mediaSessionID string) error {
	params := map[string]interface{}{
		"mediaSessionId": mediaSessionID,
	}
	_, err := c.ExecuteCommand(deviceName, "sdm.devices.commands.CameraLiveStream.StopWebRtcStream", params)
	return err
}

// EventImage holds the URL and token for downloading a camera event image.
type EventImage struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// GenerateEventImage requests a camera event image for the given eventId.
func (c *Client) GenerateEventImage(deviceName, eventID string) (*EventImage, error) {
	params := map[string]interface{}{
		"eventId": eventID,
	}
	raw, err := c.ExecuteCommand(deviceName, "sdm.devices.commands.CameraEventImage.GenerateImage", params)
	if err != nil {
		return nil, err
	}
	var img EventImage
	if err := json.Unmarshal(raw, &img); err != nil {
		return nil, fmt.Errorf("parsing event image response: %w", err)
	}
	return &img, nil
}

// DownloadEventImage downloads the JPEG image from an EventImage to the given path.
func (c *Client) DownloadEventImage(img *EventImage, outputPath string) error {
	req, err := http.NewRequest("GET", img.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Basic "+img.Token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("image download returned %d: %s", resp.StatusCode, string(body))
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func (c *Client) get(path string, out interface{}) error {
	tok, err := c.token()
	if err != nil {
		return fmt.Errorf("getting access token: %w", err)
	}

	req, err := http.NewRequest("GET", baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned %d: %s", resp.StatusCode, string(body))
	}

	return json.Unmarshal(body, out)
}

func (c *Client) post(path string, payload interface{}, out interface{}) error {
	tok, err := c.token()
	if err != nil {
		return fmt.Errorf("getting access token: %w", err)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned %d: %s", resp.StatusCode, string(body))
	}

	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}
