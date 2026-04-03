package attest

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"syscall"
	"time"

	"github.com/clstr-io/clstr/pkg/threadsafe"
)

// Node is the interface satisfied by containerNode and mockNode.
type Node interface {
	ContainerIP() string
	MappedPort() int

	Start(ctx context.Context) error
	Stop(ctx context.Context, timeout time.Duration) error
	Kill(ctx context.Context) error
	Exec(ctx context.Context, args ...string) error
}

// Do provides the test harness and acts as the test runner.
type Do struct {
	nodes  *threadsafe.Map[string, Node]
	config *config
	client *http.Client

	ctx    context.Context
	cancel context.CancelFunc
}

// newDo creates a new Do instance with the given configuration.
func newDo(ctx context.Context, cfg *config) *Do {
	doCtx, cancel := context.WithCancel(ctx)

	return &Do{
		nodes:  threadsafe.NewMap[string, Node](),
		config: cfg,
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

func (do *Do) startCluster(names ...string) {
	err := checkDockerDaemon(do.ctx)
	if err != nil {
		panic(err.Error())
	}

	resetDockerEnv(do.ctx, names)

	err = buildDockerImage(do.ctx)
	if err != nil {
		panic(err.Error())
	}

	err = createDockerNetwork(do.ctx)
	if err != nil {
		panic(err.Error())
	}

	ips := make(map[string]string, len(names))
	for i, name := range names {
		ips[name] = fmt.Sprintf("10.0.42.%d", i+2)
	}

	for _, name := range names {
		peers := make([]string, 0, len(names)-1)
		for _, other := range names {
			if other != name {
				peers = append(peers, fmt.Sprintf("%s:%d", ips[other], containerPort))
			}
		}

		mappedPort, err := freePort()
		if err != nil {
			panic(fmt.Sprintf("assign port for %q: %v", name, err))
		}

		containerName := "clstr-" + name
		node := &containerNode{
			name:       containerName,
			ip:         ips[name],
			mappedPort: mappedPort,
			peers:      peers,
		}

		do.nodes.Set(name, node)
	}

	var wg sync.WaitGroup
	var panicErr any
	var panicMu sync.Mutex
	for _, name := range names {
		wg.Add(1)
		go func(name string) {
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
			do.Start(name)
		}(name)
	}

	wg.Wait()

	if panicErr != nil {
		panic(panicErr)
	}
}

// getNode retrieves a node by name or panics if not found.
func (do *Do) getNode(name string) Node {
	node, exists := do.nodes.Get(name)
	if exists {
		return node
	}

	panic(fmt.Sprintf("node %q not found", name))
}

// Start starts a previously stopped or killed node.
func (do *Do) Start(name string) {
	node := do.getNode(name)

	select {
	case <-do.ctx.Done():
		return
	default:
	}

	err := node.Start(do.ctx)
	if err != nil {
		panic(fmt.Sprintf("start %q: %v", name, err))
	}

	err = waitUntilNodeReady(do.ctx, name, node, do.config.nodeStartTimeout, do.config.pollInterval)
	if err != nil {
		panic(err.Error())
	}
}

// Stop sends SIGTERM to the node, then SIGKILL after the shutdown timeout.
func (do *Do) Stop(name string) {
	node := do.getNode(name)

	err := node.Stop(do.ctx, do.config.nodeShutdownTimeout)
	if err != nil {
		fmt.Println(red("Error stopping"), red(name))
	}
}

// Kill sends SIGKILL to the node immediately.
func (do *Do) Kill(name string) {
	node := do.getNode(name)

	err := node.Kill(do.ctx)
	if err != nil {
		fmt.Println(red("Error killing"), red(name))
	}
}

// Restart stops the node and starts it again.
// Pass syscall.SIGKILL to crash immediately instead of graceful shutdown.
func (do *Do) Restart(name string, sig ...syscall.Signal) {
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

// Partition installs iptables DROP rules so nodes in different groups cannot
// reach each other. Rules are bidirectional. Call Heal to restore connectivity.
func (do *Do) Partition(groups ...[]string) {
	for i, g1 := range groups {
		for j, g2 := range groups {
			if i >= j {
				continue
			}

			for _, a := range g1 {
				for _, b := range g2 {
					nA := do.getNode(a)
					nB := do.getNode(b)
					ipA, ipB := nA.ContainerIP(), nB.ContainerIP()

					mustExecOnNode(do.ctx, nA, "iptables", "-A", "INPUT", "-s", ipB, "-j", "DROP")
					mustExecOnNode(do.ctx, nA, "iptables", "-A", "OUTPUT", "-d", ipB, "-j", "DROP")
					mustExecOnNode(do.ctx, nB, "iptables", "-A", "INPUT", "-s", ipA, "-j", "DROP")
					mustExecOnNode(do.ctx, nB, "iptables", "-A", "OUTPUT", "-d", ipA, "-j", "DROP")
				}
			}
		}
	}
}

// Heal flushes all iptables rules on every node, restoring full connectivity.
func (do *Do) Heal() {
	do.nodes.Range(func(name string, node Node) bool {
		err := node.Exec(do.ctx, "iptables", "-F")
		if err != nil {
			fmt.Println(red("Error healing"), red(name), ":", err)
		}

		return true
	})
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

// Done cancels the test context and stops all nodes. Containers are left in place
// so they can be inspected after a failure; they will be cleaned up at the start
// of the next run.
func (do *Do) Done() {
	do.cancel()

	bg := context.Background()
	do.nodes.Range(func(_ string, node Node) bool {
		node.Stop(bg, do.config.nodeShutdownTimeout)
		return true
	})
}

// http creates an assertion for an HTTP request to the named node.
func (do *Do) http(nodeName, method, path string, args ...any) *Assertion {
	node := do.getNode(nodeName)
	url := fmt.Sprintf("http://127.0.0.1:%d%s", node.MappedPort(), path)

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
func (do *Do) GET(name, path string, args ...any) *Assertion {
	return do.http(name, "GET", path, args...)
}

// POST creates an assertion for an HTTP POST request.
func (do *Do) POST(name, path string, args ...any) *Assertion {
	return do.http(name, "POST", path, args...)
}

// PUT creates an assertion for an HTTP PUT request.
func (do *Do) PUT(name, path string, args ...any) *Assertion {
	return do.http(name, "PUT", path, args...)
}

// DELETE creates an assertion for an HTTP DELETE request.
func (do *Do) DELETE(name, path string, args ...any) *Assertion {
	return do.http(name, "DELETE", path, args...)
}

// PATCH creates an assertion for an HTTP PATCH request.
func (do *Do) PATCH(name, path string, args ...any) *Assertion {
	return do.http(name, "PATCH", path, args...)
}
