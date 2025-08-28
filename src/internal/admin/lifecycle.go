package admin

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/abrander/go-supervisord"
	"github.com/sirupsen/logrus"
)

// InstanceState represents the state of a GOWA instance
type InstanceState string

const (
	StateRunning  InstanceState = "RUNNING"
	StateStopped  InstanceState = "STOPPED"
	StateStarting InstanceState = "STARTING"
	StateFatal    InstanceState = "FATAL"
	StateUnknown  InstanceState = "UNKNOWN"
)

// Instance represents a GOWA instance managed by supervisord
type Instance struct {
	Port     int           `json:"port"`
	State    InstanceState `json:"state"`
	PID      int           `json:"pid"`
	Uptime   time.Duration `json:"uptime"`
	LogFiles LogFiles      `json:"logs"`
}

// LogFiles contains the paths to log files for an instance
type LogFiles struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// ILifecycleManager defines the interface for lifecycle management
type ILifecycleManager interface {
	CreateInstance(port int) (*Instance, error)
	CreateInstanceWithConfig(port int, customConfig *InstanceConfig) (*Instance, error)
	ListInstances() ([]*Instance, error)
	GetInstance(port int) (*Instance, error)
	DeleteInstance(port int) error
	IsHealthy() bool
	Ping() error
}

// LifecycleManager handles creation and deletion of GOWA instances
type LifecycleManager struct {
	supervisor   *SupervisorClient
	configWriter *ConfigWriter
	lockManager  *LockManager
	logger       *logrus.Logger
}

// NewLifecycleManager creates a new lifecycle manager
func NewLifecycleManager(supervisor *SupervisorClient, configWriter *ConfigWriter, logger *logrus.Logger) *LifecycleManager {
	return &LifecycleManager{
		supervisor:   supervisor,
		configWriter: configWriter,
		lockManager:  NewLockManager(),
		logger:       logger,
	}
}

// CreateInstance creates a new GOWA instance on the specified port
func (lm *LifecycleManager) CreateInstance(port int) (*Instance, error) {
	return lm.CreateInstanceWithConfig(port, nil)
}

// CreateInstanceWithConfig creates a new GOWA instance with custom configuration
func (lm *LifecycleManager) CreateInstanceWithConfig(port int, customConfig *InstanceConfig) (*Instance, error) {
	// Validate port
	if err := lm.validatePort(port); err != nil {
		return nil, fmt.Errorf("port validation failed: %w", err)
	}

	// Acquire lock for this port
	lockFile, err := lm.lockManager.AcquireLock(port)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer lm.lockManager.ReleaseLock(lockFile)

	programName := fmt.Sprintf("gowa_%d", port)

	// Check if instance already exists
	if lm.instanceExists(programName) {
		return nil, fmt.Errorf("instance on port %d already exists", port)
	}

	// Check if port is available
	if !lm.isPortAvailable(port) {
		return nil, fmt.Errorf("port %d is already in use", port)
	}

	lm.logger.Infof("Creating instance on port %d", port)

	// Write configuration file with custom config if provided
	if err := lm.writeConfigForInstance(port, customConfig); err != nil {
		return nil, fmt.Errorf("failed to write config: %w", err)
	}

	// Use Update() to reload configuration and add process group
	client := lm.supervisor.GetClient()
	if err := client.Update(); err != nil {
		// Clean up config file on failure
		lm.configWriter.RemoveConfig(port)
		return nil, fmt.Errorf("failed to update supervisord configuration: %w", err)
	}

	// Try to start the process (it might already be started due to autostart=true)
	if err := client.StartProcess(programName, true); err != nil {
		// Check if the error is because it's already started
		if strings.Contains(err.Error(), "ALREADY_STARTED") {
			// This is expected when autostart=true, just log it
			lm.logger.Debugf("Process %s was already started by supervisord autostart", programName)
		} else {
			// Try to remove process group and config on failure
			lm.cleanupFailedInstance(programName, port)
			return nil, fmt.Errorf("failed to start process %s: %w", programName, err)
		}
	}

	// Wait for the process to be running with timeout
	instance, err := lm.waitForInstanceState(port, StateRunning, 30*time.Second)
	if err != nil {
		// Clean up if instance didn't start properly
		lm.cleanupFailedInstance(programName, port)
		return nil, fmt.Errorf("instance failed to start within timeout: %w", err)
	}

	lm.logger.Infof("Successfully created instance on port %d", port)
	return instance, nil
}

// DeleteInstance deletes a GOWA instance on the specified port
func (lm *LifecycleManager) DeleteInstance(port int) error {
	// Acquire lock for this port
	lockFile, err := lm.lockManager.AcquireLock(port)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer lm.lockManager.ReleaseLock(lockFile)

	programName := fmt.Sprintf("gowa_%d", port)

	lm.logger.Infof("Deleting instance on port %d", port)

	client := lm.supervisor.GetClient()

	// Stop the process if it's running
	if lm.instanceExists(programName) {
		if err := client.StopProcess(programName, true); err != nil {
			lm.logger.Warnf("Failed to stop process %s: %v", programName, err)
		}

		// Remove process group
		if err := client.RemoveProcessGroup(programName); err != nil {
			lm.logger.Warnf("Failed to remove process group %s: %v", programName, err)
		}
	}

	// Remove configuration file
	if err := lm.configWriter.RemoveConfig(port); err != nil {
		return fmt.Errorf("failed to remove config: %w", err)
	}

	lm.logger.Infof("Successfully deleted instance on port %d", port)
	return nil
}

// ListInstances returns a list of all GOWA instances
func (lm *LifecycleManager) ListInstances() ([]*Instance, error) {
	client := lm.supervisor.GetClient()

	processInfos, err := client.GetAllProcessInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get process info: %w", err)
	}

	var instances []*Instance
	for _, info := range processInfos {
		if strings.HasPrefix(info.Name, "gowa_") {
			instance := lm.processInfoToInstance(&info)
			if instance != nil {
				instances = append(instances, instance)
			}
		}
	}

	return instances, nil
}

// GetInstance returns information about a specific instance
func (lm *LifecycleManager) GetInstance(port int) (*Instance, error) {
	programName := fmt.Sprintf("gowa_%d", port)

	client := lm.supervisor.GetClient()
	info, err := client.GetProcessInfo(programName)
	if err != nil {
		return nil, fmt.Errorf("instance on port %d not found: %w", port, err)
	}

	return lm.processInfoToInstance(info), nil
}

// validatePort validates that the port is in a valid range
func (lm *LifecycleManager) validatePort(port int) error {
	if port < 1024 || port > 65535 {
		return fmt.Errorf("port must be between 1024 and 65535, got %d", port)
	}
	return nil
}

// isPortAvailable checks if a port is available for binding
func (lm *LifecycleManager) isPortAvailable(port int) bool {
	address := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return false
	}
	listener.Close()
	return true
}

// instanceExists checks if a supervisord process with the given name exists
func (lm *LifecycleManager) instanceExists(programName string) bool {
	client := lm.supervisor.GetClient()
	_, err := client.GetProcessInfo(programName)
	return err == nil
}

// processInfoToInstance converts supervisord process info to Instance
func (lm *LifecycleManager) processInfoToInstance(info *supervisord.ProcessInfo) *Instance {
	// Extract port from program name (gowa_3001 -> 3001)
	portStr := strings.TrimPrefix(info.Name, "gowa_")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil
	}

	// Map supervisord state to our InstanceState
	var state InstanceState
	switch info.StateName {
	case "RUNNING":
		state = StateRunning
	case "STOPPED":
		state = StateStopped
	case "STARTING":
		state = StateStarting
	case "FATAL":
		state = StateFatal
	default:
		state = StateUnknown
	}

	// Calculate uptime
	var uptime time.Duration
	if info.Start > 0 {
		uptime = time.Since(time.Unix(int64(info.Start), 0))
	}

	// Get log file paths
	logFiles := LogFiles{
		Stdout: info.StdoutLogfile,
		Stderr: info.StderrLogfile,
	}

	return &Instance{
		Port:     port,
		State:    state,
		PID:      info.Pid,
		Uptime:   uptime,
		LogFiles: logFiles,
	}
}

// waitForInstanceState waits for an instance to reach a specific state
func (lm *LifecycleManager) waitForInstanceState(port int, targetState InstanceState, timeout time.Duration) (*Instance, error) {
	programName := fmt.Sprintf("gowa_%d", port)
	client := lm.supervisor.GetClient()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		info, err := client.GetProcessInfo(programName)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}

		instance := lm.processInfoToInstance(info)
		if instance != nil && instance.State == targetState {
			return instance, nil
		}

		if instance != nil && instance.State == StateFatal {
			return nil, fmt.Errorf("instance entered FATAL state")
		}

		time.Sleep(1 * time.Second)
	}

	return nil, fmt.Errorf("timeout waiting for instance to reach state %s", targetState)
}

// cleanupFailedInstance cleans up a failed instance creation
func (lm *LifecycleManager) cleanupFailedInstance(programName string, port int) {
	client := lm.supervisor.GetClient()

	// Try to stop and remove the process group
	client.StopProcess(programName, false)
	client.RemoveProcessGroup(programName)

	// Remove configuration file
	lm.configWriter.RemoveConfig(port)
}

// writeConfigForInstance writes configuration for an instance with optional custom config
func (lm *LifecycleManager) writeConfigForInstance(port int, customConfig *InstanceConfig) error {
	if customConfig == nil {
		// Use default configuration
		return lm.configWriter.WriteConfig(port)
	}

	// Create a custom config writer with the provided configuration
	customConfig.Port = port
	customWriter, err := NewConfigWriter(customConfig)
	if err != nil {
		return fmt.Errorf("failed to create custom config writer: %w", err)
	}

	return customWriter.WriteConfig(port)
}

// IsHealthy checks if the supervisor is healthy
func (lm *LifecycleManager) IsHealthy() bool {
	return lm.supervisor.IsHealthy()
}

// Ping checks if the supervisor is reachable
func (lm *LifecycleManager) Ping() error {
	return lm.supervisor.Ping()
}
