package whatsapp

import (
	"testing"
	"time"
)

func TestListDevices_SortsByCreatedAtAscending(t *testing.T) {
	manager := &DeviceManager{
		devices: make(map[string]*DeviceInstance),
	}

	// Create devices with different creation times (in random order)
	now := time.Now()
	devices := []*DeviceInstance{
		{id: "device-c", createdAt: now.Add(2 * time.Hour)},
		{id: "device-a", createdAt: now},
		{id: "device-b", createdAt: now.Add(1 * time.Hour)},
	}

	// Add in the given order (which is not sorted by createdAt)
	for _, d := range devices {
		manager.devices[d.id] = d
	}

	// Get list multiple times to verify consistent sorting
	for i := 0; i < 10; i++ {
		result := manager.ListDevices()

		// Verify order: device-a, device-b, device-c (oldest to newest)
		if len(result) != 3 {
			t.Fatalf("iteration %d: expected 3 devices, got %d", i, len(result))
		}
		if result[0].ID() != "device-a" {
			t.Errorf("iteration %d: expected first device to be device-a, got %s", i, result[0].ID())
		}
		if result[1].ID() != "device-b" {
			t.Errorf("iteration %d: expected second device to be device-b, got %s", i, result[1].ID())
		}
		if result[2].ID() != "device-c" {
			t.Errorf("iteration %d: expected third device to be device-c, got %s", i, result[2].ID())
		}
	}
}

func TestListDevices_EmptyList(t *testing.T) {
	manager := &DeviceManager{
		devices: make(map[string]*DeviceInstance),
	}

	result := manager.ListDevices()

	if len(result) != 0 {
		t.Errorf("expected empty slice, got %d devices", len(result))
	}
}

func TestListDevices_SingleDevice(t *testing.T) {
	manager := &DeviceManager{
		devices: make(map[string]*DeviceInstance),
	}

	device := &DeviceInstance{id: "only-device", createdAt: time.Now()}
	manager.devices[device.id] = device

	result := manager.ListDevices()

	if len(result) != 1 {
		t.Fatalf("expected 1 device, got %d", len(result))
	}
	if result[0].ID() != "only-device" {
		t.Errorf("expected device id to be only-device, got %s", result[0].ID())
	}
}

func TestListDevices_SameCreatedAt(t *testing.T) {
	manager := &DeviceManager{
		devices: make(map[string]*DeviceInstance),
	}

	// Devices with same creation time should still produce stable order
	sameTime := time.Now()
	devices := []*DeviceInstance{
		{id: "device-1", createdAt: sameTime},
		{id: "device-2", createdAt: sameTime},
		{id: "device-3", createdAt: sameTime},
	}

	for _, d := range devices {
		manager.devices[d.id] = d
	}

	// Get list multiple times - order may vary but should be stable within a run
	firstResult := manager.ListDevices()
	if len(firstResult) != 3 {
		t.Fatalf("expected 3 devices, got %d", len(firstResult))
	}

	// All devices should be present
	ids := make(map[string]bool)
	for _, d := range firstResult {
		ids[d.ID()] = true
	}
	if !ids["device-1"] || !ids["device-2"] || !ids["device-3"] {
		t.Errorf("not all devices present in result")
	}
}
