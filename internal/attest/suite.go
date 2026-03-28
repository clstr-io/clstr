package attest

import (
	"context"
	"fmt"

	"github.com/fatih/color"
)

var (
	green     = color.New(color.FgGreen).SprintFunc()
	red       = color.New(color.FgRed).SprintFunc()
	bold      = color.New(color.Bold).SprintFunc()
	checkMark = green("✓")
	crossMark = red("✗")
)

// Suite represents a test suite with setup and test functions.
type Suite struct {
	setupFn func(*Do)
	tests   []TestFunc
	config  *Config
}

// TestFunc represents a single test case with name and function.
type TestFunc struct {
	Name string
	Fn   func(*Do)
}

// New creates a new empty test suite.
func New() *Suite {
	return &Suite{tests: make([]TestFunc, 0)}
}

// WithConfig sets the configuration for the test suite.
func (s *Suite) WithConfig(config *Config) *Suite {
	merged := DefaultConfig()

	if config.Command != "" {
		merged.Command = config.Command
	}

	if config.WorkingDir != "" {
		merged.WorkingDir = config.WorkingDir
	}

	if config.NodeStartTimeout != 0 {
		merged.NodeStartTimeout = config.NodeStartTimeout
	}

	if config.NodeShutdownTimeout != 0 {
		merged.NodeShutdownTimeout = config.NodeShutdownTimeout
	}

	if config.NodeRestartDelay != 0 {
		merged.NodeRestartDelay = config.NodeRestartDelay
	}

	if config.DefaultRetryTimeout != 0 {
		merged.DefaultRetryTimeout = config.DefaultRetryTimeout
	}

	if config.RetryPollInterval != 0 {
		merged.RetryPollInterval = config.RetryPollInterval
	}

	if config.ExecuteTimeout != 0 {
		merged.ExecuteTimeout = config.ExecuteTimeout
	}

	s.config = merged
	return s
}

// Setup adds a setup function that runs before all tests.
func (s *Suite) Setup(fn func(*Do)) *Suite {
	s.setupFn = fn
	return s
}

// Test adds a test case to the suite.
func (s *Suite) Test(name string, fn func(*Do)) *Suite {
	s.tests = append(s.tests, TestFunc{Name: name, Fn: fn})
	return s
}

// Run executes the test suite and returns results.
func (s *Suite) Run(ctx context.Context) bool {
	config := s.config
	if config == nil {
		config = DefaultConfig()
	}

	do := newDo(ctx, config)
	defer do.Done()

	// Run setup function if defined
	var failed bool
	if s.setupFn != nil {
		func() {
			defer func() {
				err := recover()
				if err != nil {
					failed = true

					fmt.Printf("%s %s\n", crossMark, "SETUP")
					fmt.Printf("\n%s\n", err)
				}
			}()

			s.setupFn(do)
		}()
	}

	// Run each test, stopping on first failure or cancellation
	for _, test := range s.tests {
		if failed {
			break
		}

		select {
		case <-ctx.Done():
			return false
		default:
		}

		func() {
			defer func() {
				err := recover()
				if err != nil {
					failed = true

					fmt.Printf("%s %s\n", crossMark, test.Name)
					fmt.Printf("\n%s\n", err)
				}
			}()

			test.Fn(do)
		}()

		if !failed {
			fmt.Printf("%s %s\n", checkMark, test.Name)
		}
	}

	if failed {
		fmt.Printf("\n%s %s\n", bold("FAILED"), crossMark)
	} else {
		fmt.Printf("\n%s %s\n", bold("PASSED"), checkMark)
	}

	return !failed
}
