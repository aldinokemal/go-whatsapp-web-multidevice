package whatsapp

import (
	"fmt"
	"sync"

	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
)

type ActiveCallRegistry struct {
	mu    sync.RWMutex
	calls map[string]map[string]*domainCall.CallInfo
}

func NewActiveCallRegistry() *ActiveCallRegistry {
	return &ActiveCallRegistry{calls: make(map[string]map[string]*domainCall.CallInfo)}
}

func (r *ActiveCallRegistry) Upsert(info *domainCall.CallInfo) error {
	if info == nil {
		return fmt.Errorf("call info is required")
	}
	if info.DeviceID == "" {
		return fmt.Errorf("device id is required")
	}
	if info.CallID == "" {
		return fmt.Errorf("call id is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.calls[info.DeviceID]; !ok {
		r.calls[info.DeviceID] = make(map[string]*domainCall.CallInfo)
	}
	copyInfo := *info
	r.calls[info.DeviceID][info.CallID] = &copyInfo
	return nil
}

func (r *ActiveCallRegistry) Get(deviceID, callID string) (*domainCall.CallInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	deviceCalls, ok := r.calls[deviceID]
	if !ok {
		return nil, false
	}
	info, ok := deviceCalls[callID]
	if !ok {
		return nil, false
	}
	copyInfo := *info
	return &copyInfo, true
}

func (r *ActiveCallRegistry) List(deviceID string) []domainCall.CallInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	deviceCalls := r.calls[deviceID]
	result := make([]domainCall.CallInfo, 0, len(deviceCalls))
	for _, info := range deviceCalls {
		result = append(result, *info)
	}
	return result
}

func (r *ActiveCallRegistry) Remove(deviceID, callID string) (*domainCall.CallInfo, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	deviceCalls, ok := r.calls[deviceID]
	if !ok {
		return nil, false
	}
	info, ok := deviceCalls[callID]
	if !ok {
		return nil, false
	}
	delete(deviceCalls, callID)
	if len(deviceCalls) == 0 {
		delete(r.calls, deviceID)
	}
	copyInfo := *info
	return &copyInfo, true
}
