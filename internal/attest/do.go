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
	nodes      *threadsafe.Map[string, *Node]
	config     *Config
	workingDir string

	ctx    context.Context
	cancel context.CancelFunc
}

// newDo creates a new Do instance with custom configuration.
func newDo(ctx context.Context, config *Config) *Do {
	doCtx, cancel := context.WithCancel(ctx)

	// Build working directory path with timestamp
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	workingDir := filepath.Join(config.WorkingDir, fmt.Sprintf("run-%s", timestamp))

	err := os.MkdirAll(workingDir, 0755)
	if err != nil {
		panic(fmt.Sprintf("failed to create working directory: %v", err))
	}

	return &Do{
		nodes:      threadsafe.NewMap[string, *Node](),
		config:     config,
		workingDir: workingDir,
		ctx:        doCtx,
		cancel:     cancel,
	}
}

// Node represents a running node.
type Node struct {
	cmd     *exec.Cmd
	args    []string
	logFile *os.File

	realPort int
	fauxPort int
}

// getNode retrieves a node by name or panics if not found.
func (do *Do) getNode(name string) *Node {
	if node, exists := do.nodes.Get(name); exists {
		return node
	}

	panic(fmt.Sprintf("node %q not found", name))
}

// Start starts the node with an OS-assigned port.
func (do *Do) Start(name string, args ...string) {
	do.startWithPort(name, 0, args...)
}

// startWithPort starts the node on the specified port.
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

	// Start the node
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

	node := &Node{realPort: port, cmd: cmd, args: args, logFile: logFile}
	do.waitForPort(node)

	do.nodes.Set(name, node)
}

// waitForPort waits for a node to accept connections on its port.
func (do *Do) waitForPort(node *Node) {
	host := fmt.Sprintf("127.0.0.1:%d", node.realPort)

	succeeded := eventually(do.ctx, func() bool {
		conn, err := net.DialTimeout("tcp", host, 100*time.Millisecond)
		if err != nil {
			return false
		}

		conn.Close()
		return true
	}, do.config.NodeStartTimeout, do.config.RetryPollInterval)

	if !succeeded {
		select {
		case <-do.ctx.Done():
			return
		default:
			log.Fatalf(
				"\nCould not connect to http://%s.\n\n"+
					"Possible issues:\n"+
					"- run.sh script not executable (run: chmod +x run.sh)\n"+
					"- Node not starting on port %d\n"+
					"- Node crashing during startup\n\n"+
					"Debug with: ./run.sh and check for error messages", host, node.realPort,
			)
		}
	}
}

// Stop sends SIGTERM to the node, then SIGKILL after timeout.
func (do *Do) Stop(name string) {
	n := do.getNode(name)
	if n.cmd == nil || n.cmd.Process == nil {
		return
	}

	pgid := n.cmd.Process.Pid
	err := syscall.Kill(-pgid, syscall.SIGTERM)
	if err != nil {
		fmt.Println(red("Error stopping node running @"), red(n.realPort))
		return
	}

	// Wait for graceful exit, force kill if timeout
	done := make(chan bool, 1)
	go func() {
		n.cmd.Wait()
		done <- true
	}()

	select {
	case <-done:
		// Node exited gracefully
	case <-time.After(do.config.NodeShutdownTimeout):
		do.Kill(name)
		<-done
	}

	// Close log file after node exits
	if n.logFile != nil {
		n.logFile.Close()
		n.logFile = nil
	}
}

// Kill sends SIGKILL to kill the node immediately.
func (do *Do) Kill(name string) {
	n := do.getNode(name)
	if n.cmd == nil || n.cmd.Process == nil {
		return
	}

	pgid := n.cmd.Process.Pid
	err := syscall.Kill(-pgid, syscall.SIGKILL)
	if err != nil {
		fmt.Println(red("Error killing node running @"), red(n.realPort))
	}

	// Close log file if not already closed (e.g., when called directly, not via Stop)
	if n.logFile != nil {
		n.logFile.Close()
		n.logFile = nil
	}
}

// Restart stops the node and starts it again.
func (do *Do) Restart(name string, sig ...syscall.Signal) {
	n := do.getNode(name)
	if n.cmd == nil {
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

	time.Sleep(do.config.NodeRestartDelay)

	do.startWithPort(name, n.realPort, n.args...)
}

// Done cleans up all running nodes.
func (do *Do) Done() {
	do.cancel()

	var nodes []string
	do.nodes.Range(func(node string, _ *Node) bool {
		nodes = append(nodes, node)
		return true
	})

	for _, node := range nodes {
		do.Stop(node)
	}
}

// Concurrently runs fn n times in parallel, passing each invocation a 1-based index.
func (do *Do) Concurrently(n int, fn func(i int)) {
	var wg sync.WaitGroup
	var panicErr any
	var panicMu sync.Mutex

	for i := 1; i <= n; i++ {
		wg.Add(1)
		go func(i int) {
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

			fn(i)
		}(i)
	}

	wg.Wait()

	if panicErr != nil {
		panic(panicErr)
	}
}

// GET creates a test plan for an HTTP GET request.
func (do *Do) GET(node, path string, args ...any) *HTTPPlan {
	return do.httpRequest(node, "GET", path, args...)
}

// POST creates a test plan for an HTTP POST request.
func (do *Do) POST(node, path string, args ...any) *HTTPPlan {
	return do.httpRequest(node, "POST", path, args...)
}

// PUT creates a test plan for an HTTP PUT request.
func (do *Do) PUT(node, path string, args ...any) *HTTPPlan {
	return do.httpRequest(node, "PUT", path, args...)
}

// DELETE creates a test plan for an HTTP DELETE request.
func (do *Do) DELETE(node, path string, args ...any) *HTTPPlan {
	return do.httpRequest(node, "DELETE", path, args...)
}

// PATCH creates a test plan for an HTTP PATCH request.
func (do *Do) PATCH(node, path string, args ...any) *HTTPPlan {
	return do.httpRequest(node, "PATCH", path, args...)
}

// httpRequest creates a test plan for an HTTP request with the given method.
func (do *Do) httpRequest(node, method, path string, args ...any) *HTTPPlan {
	n := do.getNode(node)
	url := fmt.Sprintf("http://127.0.0.1:%d%s", n.realPort, path)

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
