package attest_test

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	. "github.com/clstr-io/clstr/internal/attest"
)

func TestCLI(t *testing.T) {
	tests := []struct {
		name       string
		config     *Config
		testFunc   func(*Do)
		cancel     func(*Do)
		shouldPass bool
	}{
		{
			name:   "Basic OK",
			config: &Config{Command: "echo"},
			testFunc: func(do *Do) {
				do.Exec("Hello World").T().
					ExitCode(Is(0)).
					Output(Is("Hello World\n")).
					Assert("Echo command should return expected output")
			},
			shouldPass: true,
		},
		{
			name:   "Exit Code Mismatch",
			config: &Config{Command: "sh"},
			testFunc: func(do *Do) {
				do.Exec("-c", "false").T().
					ExitCode(Is(0)).
					Assert("Should fail when expecting exit code 0 but command returns 1")
			},
			shouldPass: false,
		},
		{
			name:   "Output Mismatch",
			config: &Config{Command: "sh"},
			testFunc: func(do *Do) {
				do.Exec("-c", "echo Wrong Output").T().
					Output(Is("Expected Output")).
					Assert("Should fail when command output doesn't match expected text")
			},
			shouldPass: false,
		},
		{
			name:   "Timeout",
			config: &Config{Command: "sleep", ExecuteTimeout: 50 * time.Millisecond},
			testFunc: func(do *Do) {
				do.Exec("20").T().
					ExitCode(Is(0)).
					Output(Is("Expected Output")).
					Assert("Should fail when command execution exceeds timeout")
			},
			shouldPass: false,
		},
		{
			name:   "Eventually OK",
			config: &Config{Command: "sh"},
			testFunc: func(do *Do) {
				testFile := "/tmp/attest_ready_" + fmt.Sprintf("%d", time.Now().UnixNano())

				go func() {
					time.Sleep(500 * time.Millisecond)
					exec.Command("touch", testFile).Run()
				}()
				defer exec.Command("rm", testFile).Run()

				do.Exec("-c", fmt.Sprintf("test -f '%s' && echo 'Ready' || (echo 'Not Ready'; exit 1)", testFile)).
					Eventually().T().
					ExitCode(Is(0)).
					Output(Is("Ready\n")).
					Assert("Command should eventually succeed when file exists")
			},
			shouldPass: true,
		},
		{
			name:   "Eventually Timeout",
			config: &Config{Command: "sh"},
			testFunc: func(do *Do) {
				do.Exec("-c", "echo 'Never Ready'; exit 1").
					Eventually().Within(time.Second).T().
					ExitCode(Is(0)).
					Output(Is("Ready\n")).
					Assert("Should fail when command never succeeds within timeout")
			},
			shouldPass: false,
		},
		{
			name:   "Eventually Cancellation",
			config: &Config{Command: "sh"},
			testFunc: func(do *Do) {
				do.Exec("-c", "echo 'Never Ready'; exit 1").
					Eventually().T().
					ExitCode(Is(0)).
					Output(Is("Ready\n")).
					Assert("Should fail when operation is cancelled before completion")
			},
			cancel: func(do *Do) {
				go func() {
					time.Sleep(500 * time.Millisecond)
					do.Cancel()
				}()
			},
			shouldPass: false,
		},
		{
			name:   "Consistently OK",
			config: &Config{Command: "echo"},
			testFunc: func(do *Do) {
				do.Exec("Stable").
					Consistently().For(500 * time.Millisecond).T().
					ExitCode(Is(0)).
					Output(Is("Stable\n")).
					Assert("Command should consistently produce stable output")
			},
			shouldPass: true,
		},
		{
			name:   "Consistently Failure",
			config: &Config{Command: "sh"},
			testFunc: func(do *Do) {
				do.Exec("-c", "date +%N").
					Consistently().For(500 * time.Millisecond).T().
					Output(Is("12345\n")).
					Assert("Should fail when command output changes between executions")
			},
			shouldPass: false,
		},
		{
			name:   "Consistently Cancellation",
			config: &Config{Command: "echo"},
			testFunc: func(do *Do) {
				do.Exec("Stable").
					Consistently().For(3 * time.Second).T().
					ExitCode(Is(0)).
					Output(Is("Stable\n")).
					Assert("Should pass when cancelled during consistency check")
			},
			cancel: func(do *Do) {
				go func() {
					time.Sleep(500 * time.Millisecond)
					do.Cancel()
				}()
			},
			shouldPass: true,
		},
		{
			name:   "Contains Checker - matches substring",
			config: &Config{Command: "echo"},
			testFunc: func(do *Do) {
				do.Exec("Error: something went wrong").T().
					ExitCode(Is(0)).
					Output(Contains("went wrong")).
					Assert("Should match when output contains substring")
			},
			shouldPass: true,
		},
		{
			name:   "Contains Checker - fails when substring not present",
			config: &Config{Command: "echo"},
			testFunc: func(do *Do) {
				do.Exec("Success").T().
					ExitCode(Is(0)).
					Output(Contains("error")).
					Assert("Should fail when substring is not in output")
			},
			shouldPass: false,
		},
		{
			name:   "Matches Checker - matches regex pattern",
			config: &Config{Command: "echo"},
			testFunc: func(do *Do) {
				do.Exec("Version 1.2.3").T().
					ExitCode(Is(0)).
					Output(Matches(`Version \d+\.\d+\.\d+`)).
					Assert("Should match regex pattern")
			},
			shouldPass: true,
		},
		{
			name:   "Matches Checker - fails when pattern doesn't match",
			config: &Config{Command: "echo"},
			testFunc: func(do *Do) {
				do.Exec("Version abc").T().
					ExitCode(Is(0)).
					Output(Matches(`Version \d+\.\d+\.\d+`)).
					Assert("Should fail when pattern doesn't match")
			},
			shouldPass: false,
		},
		{
			name:   "OneOf Checker - matches one of several values",
			config: &Config{Command: "echo"},
			testFunc: func(do *Do) {
				do.Exec("hello").T().
					ExitCode(Is(0)).
					Output(OneOf("hello\n", "world\n", "test\n")).
					Assert("Should accept hello as one of the valid outputs")
			},
			shouldPass: true,
		},
		{
			name:   "OneOf Checker - fails when value not in list",
			config: &Config{Command: "echo"},
			testFunc: func(do *Do) {
				do.Exec("invalid").T().
					ExitCode(Is(0)).
					Output(OneOf("hello\n", "world\n", "test\n")).
					Assert("Should fail when output is not in the list of valid values")
			},
			shouldPass: false,
		},
		{
			name:   "Not Checker - negates another checker",
			config: &Config{Command: "echo"},
			testFunc: func(do *Do) {
				do.Exec("Success").T().
					ExitCode(Is(0)).
					Output(Not(Contains("error"))).
					Assert("Should pass when negated checker doesn't match")
			},
			shouldPass: true,
		},
		{
			name:   "Not Checker - fails when negated checker matches",
			config: &Config{Command: "echo"},
			testFunc: func(do *Do) {
				do.Exec("Error occurred").T().
					ExitCode(Is(0)).
					Output(Not(Contains("Error"))).
					Assert("Should fail when negated checker matches")
			},
			shouldPass: false,
		},
		{
			name:   "Multiple Checkers - multiple exit code checkers",
			config: &Config{Command: "echo"},
			testFunc: func(do *Do) {
				do.Exec("test").T().
					ExitCode(Is(0), Not(Is(1)), Not(Is(127))).
					Assert("Should pass when all exit code checkers pass")
			},
			shouldPass: true,
		},
		{
			name:   "Multiple Checkers - multiple output checkers",
			config: &Config{Command: "echo"},
			testFunc: func(do *Do) {
				do.Exec("Hello World").T().
					ExitCode(Is(0)).
					Output(Contains("Hello"), Contains("World"), Not(Contains("Goodbye"))).
					Assert("Should pass when all output checkers pass")
			},
			shouldPass: true,
		},
		{
			name:   "Multiple Checkers - fails when one checker fails",
			config: &Config{Command: "echo"},
			testFunc: func(do *Do) {
				do.Exec("Hello World").T().
					ExitCode(Is(0)).
					Output(Contains("Hello"), Contains("Goodbye")).
					Assert("Should fail when one of the checkers fails")
			},
			shouldPass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.config == nil {
				tt.config = &Config{}
			}
			tt.config.WorkingDir = t.TempDir()

			suite := New().WithConfig(tt.config)

			success := suite.
				Setup(func(do *Do) {
					if tt.cancel != nil {
						tt.cancel(do)
					}
				}).
				Test(tt.name, func(do *Do) {
					tt.testFunc(do)
				}).
				Run(context.Background())

			if success != tt.shouldPass {
				if tt.shouldPass {
					t.Errorf("%s test should pass but failed", tt.name)
				} else {
					t.Errorf("%s test should fail but passed", tt.name)
				}
			}
		})
	}
}
