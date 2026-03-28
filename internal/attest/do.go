package attest

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/clstr-io/clstr/pkg/threadsafe"
)

// Do provides the test harness and acts as the test runner.
type Do struct {
	processes  *threadsafe.Map[string, *Process]
	config     *Config
	workingDir string

	ctx    context.Context
	cancel context.CancelFunc
}

// newDo creates a new Do instance with custom configuration.
func newDo(ctx context.Context, config *Config) *Do {
	doCtx, cancel := context.WithCancel(ctx)

	// Build working directory path with timestamp
	timestamp := time.Now().Format("20060102-150405")
	workingDir := filepath.Join(config.WorkingDir, fmt.Sprintf("run-%s", timestamp))

	err := os.MkdirAll(workingDir, 0755)
	if err != nil {
		panic(fmt.Sprintf("failed to create working directory: %v", err))
	}

	return &Do{
		processes:  threadsafe.NewMap[string, *Process](),
		config:     config,
		workingDir: workingDir,
		ctx:        doCtx,
		cancel:     cancel,
	}
}

// Process represents a running process.
type Process struct {
	cmd     *exec.Cmd
	args    []string
	logFile *os.File

	realPort int
	fauxPort int
}

// getProcess retrieves a process by name or panics if not found.
func (do *Do) getProcess(name string) *Process {
	if proc, exists := do.processes.Get(name); exists {
		return proc
	}

	panic(fmt.Sprintf("process %q not found", name))
}

// Start starts the process with an OS-assigned port.
func (do *Do) Start(name string, args ...string) {
	do.startWithPort(name, 0, args...)
}

// startWithPort starts the process on the specified port.
func (do *Do) startWithPort(name string, port int, args ...string) {
	select {
	case <-do.ctx.Done():
		return
	default:
	}

	// Get OS-assigned port
	if port == 0 {
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			panic(fmt.Sprintf("Failed to get OS-assigned port: %v", err))
		}
		port = listener.Addr().(*net.TCPAddr).Port
		listener.Close()
	}

	// Start the process
	portArg := fmt.Sprintf("--port=%d", port)
	workingDirArg := fmt.Sprintf("--working-dir=%s", do.workingDir)
	newArgs := append([]string{portArg, workingDirArg}, args...)

	cmd := exec.CommandContext(do.ctx, do.config.Command, newArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Redirect stdout/stderr to log file
	logPath := filepath.Join(do.workingDir, fmt.Sprintf("%s.log", name))
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(fmt.Sprintf("failed to create log file: %v", err))
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	err = cmd.Start()
	if err != nil {
		logFile.Close()
		panic(err.Error())
	}

	proc := &Process{realPort: port, cmd: cmd, args: args, logFile: logFile}
	do.waitForPort(proc)

	do.processes.Set(name, proc)
}

// waitForPort waits for a process to accept connections on its port.
func (do *Do) waitForPort(proc *Process) {
	host := fmt.Sprintf("127.0.0.1:%d", proc.realPort)

	succeeded := eventually(do.ctx, func() bool {
		conn, err := net.DialTimeout("tcp", host, 100*time.Millisecond)
		if err != nil {
			return false
		}

		conn.Close()
		return true
	}, do.config.ProcessStartTimeout, do.config.RetryPollInterval)

	if !succeeded {
		select {
		case <-do.ctx.Done():
			return
		default:
			log.Fatalf(
				"\nCould not connect to http://%s.\n\n"+
					"Possible issues:\n"+
					"- run.sh script not executable (run: chmod +x run.sh)\n"+
					"- Process not starting on port %d\n"+
					"- Process crashing during startup\n\n"+
					"Debug with: ./run.sh and check for error messages", host, proc.realPort,
			)
		}
	}
}

// Stop sends SIGTERM to the process, then SIGKILL after timeout.
func (do *Do) Stop(name string) {
	proc := do.getProcess(name)
	if proc.cmd == nil || proc.cmd.Process == nil {
		return
	}

	pgid := proc.cmd.Process.Pid
	err := syscall.Kill(-pgid, syscall.SIGTERM)
	if err != nil {
		fmt.Println(red("Error stopping process running @"), red(proc.realPort))
		return
	}

	// Wait for graceful exit, force kill if timeout
	done := make(chan bool, 1)
	go func() {
		proc.cmd.Wait()
		done <- true
	}()

	select {
	case <-done:
		// Process exited gracefully
	case <-time.After(do.config.ProcessShutdownTimeout):
		do.Kill(name)
		<-done
	}

	// Close log file after process exits
	if proc.logFile != nil {
		proc.logFile.Close()
		proc.logFile = nil
	}
}

// Kill sends SIGKILL to kill the process immediately.
func (do *Do) Kill(name string) {
	proc := do.getProcess(name)
	if proc.cmd == nil || proc.cmd.Process == nil {
		return
	}

	pgid := proc.cmd.Process.Pid
	err := syscall.Kill(-pgid, syscall.SIGKILL)
	if err != nil {
		fmt.Println(red("Error killing process running @"), red(proc.realPort))
	}

	// Close log file if not already closed (e.g., when called directly, not via Stop)
	if proc.logFile != nil {
		proc.logFile.Close()
		proc.logFile = nil
	}
}

// Restart stops the process and starts it again.
func (do *Do) Restart(name string, sig ...syscall.Signal) {
	proc := do.getProcess(name)
	if proc.cmd == nil {
		return
	}

	signal := syscall.SIGTERM
	if len(sig) > 0 {
		signal = sig[0]
	}

	switch signal {
	case syscall.SIGTERM:
		do.Stop(name)
	case syscall.SIGKILL:
		do.Kill(name)
	default:
		do.Stop(name)
	}

	time.Sleep(do.config.ProcessRestartDelay)

	do.startWithPort(name, proc.realPort, proc.args...)
}

// Done cleans up all running processes.
func (do *Do) Done() {
	do.cancel()

	var processNames []string
	do.processes.Range(func(name string, _ *Process) bool {
		processNames = append(processNames, name)
		return true
	})

	for _, name := range processNames {
		do.Stop(name)
	}
}

// Concurrently runs multiple functions in parallel and waits for completion.
func (do *Do) Concurrently(fns ...func()) {
	var wg sync.WaitGroup
	var panicErr any
	var panicMu sync.Mutex

	for _, fn := range fns {
		wg.Add(1)
		go func(f func()) {
			defer wg.Done()
			defer func() {
				err := recover()
				if err != nil {
					panicMu.Lock()
					if panicErr == nil {
						panicErr = err
					}
					panicMu.Unlock()
				}
			}()

			f()
		}(fn)
	}

	wg.Wait()

	if panicErr != nil {
		panic(panicErr)
	}
}

// HTTP creates a test plan for an HTTP request.
func (do *Do) HTTP(name, method, path string, args ...any) *HTTPPlan {
	proc := do.getProcess(name)
	url := fmt.Sprintf("http://127.0.0.1:%d%s", proc.realPort, path)

	var body []byte
	if len(args) >= 1 {
		body = []byte(args[0].(string))
	}

	var headers H
	if len(args) >= 2 {
		headers = args[1].(H)
	}

	return &HTTPPlan{
		PlanBase: PlanBase{
			timing: TimingImmediate,
			ctx:    do.ctx,
			config: do.config,
		},

		method:  method,
		url:     url,
		headers: headers,
		body:    body,
	}
}
