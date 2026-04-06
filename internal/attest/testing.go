package attest

import (
	"context"
	"strconv"
	"time"
)

type mockNode struct {
	port int
}

func (n *mockNode) ContainerIP() string {
	return "127.0.0.1"
}

func (n *mockNode) MappedPort() int {
	return n.port
}

func (n *mockNode) IsAlive() bool {
	return true
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

func (n *mockNode) Logs() string {
	return ""
}

func (n *mockNode) Annotate(_ string) {}

func (do *Do) Cancel() {
	do.cancel()
}

func (do *Do) MockNode(name, port string) {
	p, _ := strconv.Atoi(port)
	do.nodes.Set(name, &mockNode{port: p})
}
