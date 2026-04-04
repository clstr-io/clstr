package attest

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/clstr-io/clstr/pkg/threadsafe"
)

// Do provides the test harness and acts as the test runner.
type Do struct {
	nodes  *threadsafe.Map[string, clusterNode]
	config *config
	client *http.Client

	ctx    context.Context
	cancel context.CancelFunc
}

// newDo creates a new Do instance with the given configuration.
func newDo(ctx context.Context, cfg *config) *Do {
	doCtx, cancel := context.WithCancel(ctx)

	return &Do{
		nodes:  threadsafe.NewMap[string, clusterNode](),
		config: cfg,
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 100,
				MaxConnsPerHost:     200,
			},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
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
	do.nodes.Range(func(_ string, node clusterNode) bool {
		node.Stop(bg, do.config.nodeShutdownTimeout)
		return true
	})
}

// http creates an assertion for an HTTP request to the node(s) described by sel.
func (do *Do) http(sel NodeSelector, method, path string, args ...any) *Assertion {
	var body []byte
	if len(args) >= 1 {
		body = []byte(args[0].(string))
	}

	var headers H
	if len(args) >= 2 {
		headers = args[1].(H)
	}

	a := &Assertion{
		timing:   timingImmediate,
		ctx:      do.ctx,
		config:   do.config,
		client:   do.client,
		method:   method,
		headers:  headers,
		body:     body,
		selector: sel,
	}

	if sel.kind == nodeNamed {
		node := do.getNode(sel.names[0])
		a.urls = []string{fmt.Sprintf("http://127.0.0.1:%d%s", node.MappedPort(), path)}
	} else {
		names := sel.names
		if len(names) == 0 {
			names = do.Nodes()
		}

		for _, name := range names {
			node := do.getNode(name)
			if node.IsAlive() {
				a.urls = append(a.urls, fmt.Sprintf("http://127.0.0.1:%d%s", node.MappedPort(), path))
			}
		}
	}

	return a
}

// GET creates an assertion for an HTTP GET request.
func (do *Do) GET(sel NodeSelector, path string, args ...any) *Assertion {
	return do.http(sel, "GET", path, args...)
}

// POST creates an assertion for an HTTP POST request.
func (do *Do) POST(sel NodeSelector, path string, args ...any) *Assertion {
	return do.http(sel, "POST", path, args...)
}

// PUT creates an assertion for an HTTP PUT request.
func (do *Do) PUT(sel NodeSelector, path string, args ...any) *Assertion {
	return do.http(sel, "PUT", path, args...)
}

// DELETE creates an assertion for an HTTP DELETE request.
func (do *Do) DELETE(sel NodeSelector, path string, args ...any) *Assertion {
	return do.http(sel, "DELETE", path, args...)
}

// PATCH creates an assertion for an HTTP PATCH request.
func (do *Do) PATCH(sel NodeSelector, path string, args ...any) *Assertion {
	return do.http(sel, "PATCH", path, args...)
}
