package attest

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/clstr-io/clstr/pkg/threadsafe"
)

// Do provides the test harness and acts as the test runner.
type Do struct {
	nodes      *threadsafe.Map[string, *Node]
	config     *config
	workingDir string
	client     *http.Client

	ctx    context.Context
	cancel context.CancelFunc
}

// newDo creates a new Do instance with custom configuration.
func newDo(ctx context.Context, config *config) *Do {
	doCtx, cancel := context.WithCancel(ctx)

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	workingDir := filepath.Join(config.workingDir, timestamp)
	err := os.MkdirAll(workingDir, 0755)
	if err != nil {
		panic(fmt.Sprintf("failed to create working directory: %v", err))
	}

	return &Do{
		nodes:      threadsafe.NewMap[string, *Node](),
		config:     config,
		workingDir: workingDir,
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 100,
				MaxConnsPerHost:     200,
			},
		},
		ctx:    doCtx,
		cancel: cancel,
	}
}

// Node represents a running node.
type Node struct {
	cmd     *exec.Cmd
	args    []string
	port    int
	logFile *os.File
}

func (n *Node) closeLog() {
	if n.logFile != nil {
		n.logFile.Close()
		n.logFile = nil
	}
}

func (do *Do) startCluster(names ...string) {
	ports := make(map[string]int, len(names))
	for _, name := range names {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			panic(fmt.Sprintf("failed to assign port for %q: %v", name, err))
		}

		ports[name] = l.Addr().(*net.TCPAddr).Port
		l.Close()
	}

	for _, name := range names {
		peers := make([]string, 0, len(names)-1)
		for _, other := range names {
			if other != name {
				peers = append(peers, fmt.Sprintf(":%d", ports[other]))
			}
		}
		peersArg := fmt.Sprintf("--peers=%s", strings.Join(peers, ","))

		do.startNode(name, ports[name], peersArg)
	}
}

func (do *Do) startNode(name string, port int, args ...string) {
	select {
	case <-do.ctx.Done():
		return
	default:
	}

	portArg := fmt.Sprintf("--port=%d", port)
	workingDirArg := fmt.Sprintf("--working-dir=%s", do.workingDir)
	newArgs := append([]string{portArg, workingDirArg}, args...)

	cmd := exec.CommandContext(do.ctx, do.config.command, newArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

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

	node := &Node{cmd: cmd, args: args, port: port, logFile: logFile}
	do.waitForPort(node)
	do.nodes.Set(name, node)
}

// waitForPort waits for a node to accept connections on its port.
func (do *Do) waitForPort(node *Node) {
	host := fmt.Sprintf("127.0.0.1:%d", node.port)

	succeeded := eventually(do.ctx, func() bool {
		conn, err := net.DialTimeout("tcp", host, 100*time.Millisecond)
		if err != nil {
			return false
		}

		conn.Close()
		return true
	}, do.config.nodeStartTimeout, do.config.pollInterval)

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
					"Debug with: ./run.sh and check for error messages", host, node.port,
			)
		}
	}
}

// getNode retrieves a node by name or panics if not found.
func (do *Do) getNode(name string) *Node {
	if node, exists := do.nodes.Get(name); exists {
		return node
	}

	panic(fmt.Sprintf("node %q not found", name))
}

// Start starts a previously stopped or killed node on its original port.
func (do *Do) Start(name string) {
	node := do.getNode(name)
	do.startNode(name, node.port, node.args...)
}

// Stop sends SIGTERM to the node, then SIGKILL after timeout.
func (do *Do) Stop(name string) {
	node := do.getNode(name)
	if node.cmd == nil || node.cmd.Process == nil {
		return
	}

	pgid := node.cmd.Process.Pid
	err := syscall.Kill(-pgid, syscall.SIGTERM)
	if err != nil {
		fmt.Println(red("Error stopping node running @"), red(node.port))
		return
	}

	done := make(chan bool, 1)
	go func() {
		node.cmd.Wait()
		done <- true
	}()

	select {
	case <-done:
	case <-time.After(do.config.nodeShutdownTimeout):
		do.Kill(name)
		<-done
	}

	node.closeLog()
}

// Kill sends SIGKILL to kill the node immediately.
func (do *Do) Kill(name string) {
	node := do.getNode(name)
	if node.cmd == nil || node.cmd.Process == nil {
		return
	}

	pgid := node.cmd.Process.Pid
	err := syscall.Kill(-pgid, syscall.SIGKILL)
	if err != nil {
		fmt.Println(red("Error killing node running @"), red(node.port))
	}

	node.closeLog()
}

// Restart stops the node and starts it again.
func (do *Do) Restart(name string, sig ...syscall.Signal) {
	node := do.getNode(name)
	if node.cmd == nil {
		return
	}

	signal := syscall.SIGTERM
	if len(sig) > 0 {
		signal = sig[0]
	}

	switch signal {
	case syscall.SIGKILL:
		do.Kill(name)
	default:
		do.Stop(name)
	}

	do.Start(name)
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

// Done cleans up all running nodes.
func (do *Do) Done() {
	do.cancel()

	do.nodes.Range(func(name string, _ *Node) bool {
		do.Stop(name)
		return true
	})
}

// http creates an assertion for an HTTP request with the given method.
func (do *Do) http(nodeName, method, path string, args ...any) *Assertion {
	node := do.getNode(nodeName)
	url := fmt.Sprintf("http://127.0.0.1:%d%s", node.port, path)

	var body []byte
	if len(args) >= 1 {
		body = []byte(args[0].(string))
	}

	var headers H
	if len(args) >= 2 {
		headers = args[1].(H)
	}

	return &Assertion{
		timing:  timingImmediate,
		ctx:     do.ctx,
		config:  do.config,
		client:  do.client,
		method:  method,
		url:     url,
		headers: headers,
		body:    body,
	}
}

// GET creates an assertion for an HTTP GET request.
func (do *Do) GET(node, path string, args ...any) *Assertion {
	return do.http(node, "GET", path, args...)
}

// POST creates an assertion for an HTTP POST request.
func (do *Do) POST(node, path string, args ...any) *Assertion {
	return do.http(node, "POST", path, args...)
}

// PUT creates an assertion for an HTTP PUT request.
func (do *Do) PUT(node, path string, args ...any) *Assertion {
	return do.http(node, "PUT", path, args...)
}

// DELETE creates an assertion for an HTTP DELETE request.
func (do *Do) DELETE(node, path string, args ...any) *Assertion {
	return do.http(node, "DELETE", path, args...)
}

// PATCH creates an assertion for an HTTP PATCH request.
func (do *Do) PATCH(node, path string, args ...any) *Assertion {
	return do.http(node, "PATCH", path, args...)
}
