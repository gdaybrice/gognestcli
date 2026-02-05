package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
)

type InfoCmd struct {
	DeviceID string `arg:"" optional:"" help:"Device ID or full resource name (uses config default if omitted)"`
}

func (i *InfoCmd) Run() error {
	client, cfg, err := newSDMClient()
	if err != nil {
		return err
	}

	deviceName := i.DeviceID
	if deviceName == "" {
		deviceName = cfg.DeviceID
	}
	if deviceName == "" {
		// Try to find the first camera device
		devices, err := client.ListDevices()
		if err != nil {
			return fmt.Errorf("listing devices: %w", err)
		}
		for _, dev := range devices {
			if strings.Contains(dev.Type, "CAMERA") {
				deviceName = dev.Name
				break
			}
		}
		if deviceName == "" {
			return fmt.Errorf("no camera device found; specify a device ID or set device_id in config")
		}
	}

	// Ensure full resource name
	if !strings.HasPrefix(deviceName, "enterprises/") {
		deviceName = fmt.Sprintf("enterprises/%s/devices/%s", cfg.ProjectID, deviceName)
	}

	dev, err := client.GetDevice(deviceName)
	if err != nil {
		return fmt.Errorf("getting device: %w", err)
	}

	fmt.Printf("Name:  %s\n", dev.Name)
	fmt.Printf("Type:  %s\n", dev.Type)
	if dn := deviceDisplayName(*dev); dn != "" {
		fmt.Printf("Room:  %s\n", dn)
	}
	fmt.Println()

	fmt.Println("Traits:")
	for name, raw := range dev.Traits {
		shortName := name
		if parts := strings.Split(name, "."); len(parts) > 0 {
			shortName = parts[len(parts)-1]
		}
		var pretty interface{}
		if err := json.Unmarshal(raw, &pretty); err == nil {
			data, _ := json.MarshalIndent(pretty, "  ", "  ")
			fmt.Printf("  %s: %s\n", shortName, string(data))
		} else {
			fmt.Printf("  %s: %s\n", shortName, string(raw))
		}
	}
	return nil
}
