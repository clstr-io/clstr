package attest

import "time"

// Config holds configuration options for the test framework.
type Config struct {
	// Command is the script/command used to build & run the system under test.
	Command string

	// WorkingDir is the base directory for test runs.
	WorkingDir string

	// ProcessStartTimeout for process startup.
	ProcessStartTimeout time.Duration
	// ProcessShutdownTimeout for process shutdown.
	ProcessShutdownTimeout time.Duration
	// ProcessRestartDelay between stop and start during restart.
	ProcessRestartDelay time.Duration

	// DefaultRetryTimeout for Eventually and Consistently operations.
	DefaultRetryTimeout time.Duration
	// RetryPollInterval for Eventually and Consistently operations.
	RetryPollInterval time.Duration

	// ExecuteTimeout for HTTP client requests.
	ExecuteTimeout time.Duration
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		Command:                "./run.sh",
		WorkingDir:             ".clstr",
		ProcessStartTimeout:    15 * time.Second,
		ProcessShutdownTimeout: 15 * time.Second,
		ProcessRestartDelay:    time.Second,
		DefaultRetryTimeout:    5 * time.Second,
		RetryPollInterval:      100 * time.Millisecond,
		ExecuteTimeout:         15 * time.Second,
	}
}
