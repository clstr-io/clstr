package attest

import (
	"context"
	"strconv"
	"time"
)

// mockNode is a Node backed by an already-running server (e.g. httptest.Server).
type mockNode struct {
	port int
}

func (n *mockNode) ContainerIP() string {
	return "127.0.0.1"
}

func (n *mockNode) MappedPort() int {
	return n.port
}

func (n *mockNode) Start(_ context.Context) error {
	return nil
}

func (n *mockNode) Stop(_ context.Context, _ time.Duration) error {
	return nil
}

func (n *mockNode) Kill(_ context.Context) error {
	return nil
}

func (n *mockNode) Exec(_ context.Context, _ ...string) error {
	return nil
}

// Cancel cancels the Do context, stopping all in-flight assertions.
func (do *Do) Cancel() {
	do.cancel()
}

// MockNode registers a pre-running server as a node. Used in tests.
func (do *Do) MockNode(name, port string) {
	p, _ := strconv.Atoi(port)
	do.nodes.Set(name, &mockNode{port: p})
}
