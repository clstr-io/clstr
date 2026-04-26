package attest

import (
	"fmt"
	"time"
)

type config struct {
	challengeKey          string
	nodes                 []string
	nodeStartupTimeout    time.Duration
	nodeShutdownTimeout   time.Duration
	clusterSettleDuration time.Duration
	pollInterval          time.Duration
	requestTimeout        time.Duration
}

func defaultConfig() *config {
	return &config{
		nodeStartupTimeout:  10 * time.Second,
		nodeShutdownTimeout: 5 * time.Second,
		pollInterval:        time.Second,
		requestTimeout:      500 * time.Millisecond,
	}
}

// Option configures a Suite.
type Option func(*config)

// WithChallenge sets the challenge key used to namespace Docker resources.
func WithChallenge(key string) Option {
	return func(c *config) {
		c.challengeKey = key
	}
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

// WithNodeStartupTimeout sets how long to wait for a node to accept connections after starting.
func WithNodeStartupTimeout(d time.Duration) Option {
	return func(c *config) {
		c.nodeStartupTimeout = d
	}
}

// WithNodeShutdownTimeout sets how long to wait for a node to exit before sending SIGKILL.
func WithNodeShutdownTimeout(d time.Duration) Option {
	return func(c *config) {
		c.nodeShutdownTimeout = d
	}
}

// WithClusterSettleDuration sets how long Partition and Heal wait after
// topology changes for in-flight RPCs to drain and timeouts to expire.
func WithClusterSettleDuration(d time.Duration) Option {
	return func(c *config) {
		c.clusterSettleDuration = d
	}
}

// WithPollInterval sets how often Eventually and Consistently poll.
func WithPollInterval(d time.Duration) Option {
	return func(c *config) {
		c.pollInterval = d
	}
}

// WithRequestTimeout sets the HTTP client timeout per request.
func WithRequestTimeout(d time.Duration) Option {
	return func(c *config) {
		c.requestTimeout = d
	}
}
