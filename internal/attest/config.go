package attest

import (
	"fmt"
	"time"
)

type config struct {
	command             string
	workingDir          string
	nodes               []string
	nodeStartTimeout    time.Duration
	nodeShutdownTimeout time.Duration
	assertTimeout       time.Duration
	pollInterval        time.Duration
	requestTimeout      time.Duration
}

func defaultConfig() *config {
	return &config{
		command:             "./run.sh",
		workingDir:          ".clstr",
		nodeStartTimeout:    10 * time.Second,
		nodeShutdownTimeout: 5 * time.Second,
		assertTimeout:       5 * time.Second,
		pollInterval:        100 * time.Millisecond,
		requestTimeout:      5 * time.Second,
	}
}

// Option configures a Suite.
type Option func(*config)

// WithCommand sets the script or binary used to start each node.
func WithCommand(cmd string) Option {
	return func(c *config) { c.command = cmd }
}

// WithWorkingDir sets the base directory for test run artifacts and node data.
func WithWorkingDir(dir string) Option {
	return func(c *config) { c.workingDir = dir }
}

// WithCluster declares N nodes named n1, n2, ... nN.
func WithCluster(n int) Option {
	if n < 1 {
		panic("cluster must have at least one node")
	}

	return func(c *config) {
		c.nodes = make([]string, n)
		for i := range n {
			c.nodes[i] = fmt.Sprintf("n%d", i+1)
		}
	}
}

// WithNodeStartTimeout sets how long to wait for a node to accept connections after starting.
func WithNodeStartTimeout(d time.Duration) Option {
	return func(c *config) { c.nodeStartTimeout = d }
}

// WithNodeShutdownTimeout sets how long to wait for a node to exit before sending SIGKILL.
func WithNodeShutdownTimeout(d time.Duration) Option {
	return func(c *config) { c.nodeShutdownTimeout = d }
}

// WithAssertTimeout sets the default timeout for Eventually and Consistently.
func WithAssertTimeout(d time.Duration) Option {
	return func(c *config) { c.assertTimeout = d }
}

// WithPollInterval sets how often Eventually and Consistently poll.
func WithPollInterval(d time.Duration) Option {
	return func(c *config) { c.pollInterval = d }
}

// WithRequestTimeout sets the HTTP client timeout per request.
func WithRequestTimeout(d time.Duration) Option {
	return func(c *config) { c.requestTimeout = d }
}
