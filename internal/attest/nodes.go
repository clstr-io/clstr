package attest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/tidwall/gjson"
)

// clusterNode is the interface satisfied by nodes.
type clusterNode interface {
	ContainerIP() string
	MappedPort() int
	IsAlive() bool
	Logs() string
	Annotate(msg string)

	Start(ctx context.Context) error
	Stop(ctx context.Context, timeout time.Duration) error
	Kill(ctx context.Context) error
	Exec(ctx context.Context, args ...string) error
}

// nodeKind identifies which nodes a NodeSelector targets.
type nodeKind uint8

const (
	nodeNamed      nodeKind = iota // a single named node
	nodeExactlyOne                 // exactly one node across the cluster
	nodeAtLeastOne                 // at least one node across the cluster
	nodeAll                        // all nodes across the cluster
)

// NodeSelector targets one or more nodes for an HTTP assertion.
type NodeSelector struct {
	kind  nodeKind
	names []string // nodeNamed uses names[0]; others treat empty as whole cluster
}

// Node returns a selector targeting a specific named node.
func Node(name string) NodeSelector {
	return NodeSelector{kind: nodeNamed, names: []string{name}}
}

// ExactlyOneNode returns a selector that passes when exactly one node satisfies
// the assertion. If names are provided, only those nodes are checked.
func (do *Do) ExactlyOneNode(names ...string) NodeSelector {
	return NodeSelector{kind: nodeExactlyOne, names: names}
}

// AtLeastOneNode returns a selector that passes when at least one node satisfies
// the assertion. If names are provided, only those nodes are checked.
func (do *Do) AtLeastOneNode(names ...string) NodeSelector {
	return NodeSelector{kind: nodeAtLeastOne, names: names}
}

// AllNodes returns a selector that passes when every node satisfies the assertion.
// If names are provided, only those nodes are checked.
func (do *Do) AllNodes(names ...string) NodeSelector {
	return NodeSelector{kind: nodeAll, names: names}
}

// Nodes returns the names of all nodes in the cluster.
func (do *Do) Nodes() []string {
	var names []string
	do.nodes.Range(func(name string, _ clusterNode) bool {
		names = append(names, name)
		return true
	})

	return names
}

// getNode retrieves a node by name or panics if not found.
func (do *Do) getNode(name string) clusterNode {
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

	containerName := "clstr-" + do.config.challengeKey + "-" + name
	err = waitUntilNodeReady(do.ctx, name, containerName, node, do.config.nodeStartTimeout, do.config.pollInterval)
	if err != nil {
		panic(err.Error())
	}
}

// Stop sends SIGTERM to the node, then SIGKILL after the shutdown timeout.
func (do *Do) Stop(name string) {
	node := do.getNode(name)
	node.Annotate("stopped")

	err := node.Stop(do.ctx, do.config.nodeShutdownTimeout)
	if err != nil {
		fmt.Println(red("Error stopping"), red(name))
	}
}

// Kill sends SIGKILL to the node immediately.
func (do *Do) Kill(name string) {
	node := do.getNode(name)
	node.Annotate("killed")

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
	cutoffs := map[string][]string{}
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

					execOnNode(do.ctx, nA, "iptables", "-A", "INPUT", "-s", ipB, "-j", "DROP")
					execOnNode(do.ctx, nA, "iptables", "-A", "OUTPUT", "-d", ipB, "-j", "DROP")
					execOnNode(do.ctx, nB, "iptables", "-A", "INPUT", "-s", ipA, "-j", "DROP")
					execOnNode(do.ctx, nB, "iptables", "-A", "OUTPUT", "-d", ipA, "-j", "DROP")

					cutoffs[a] = append(cutoffs[a], b)
					cutoffs[b] = append(cutoffs[b], a)
				}
			}
		}
	}

	for name, cutoff := range cutoffs {
		sort.Strings(cutoff)
		do.getNode(name).Annotate("partitioned from: " + strings.Join(cutoff, ", "))
	}
}

// Heal flushes all iptables rules on every node, restoring full connectivity.
func (do *Do) Heal() {
	do.nodes.Range(func(name string, node clusterNode) bool {
		node.Annotate("partition healed")

		err := node.Exec(do.ctx, "iptables", "-F")
		if err != nil {
			fmt.Println(red("Error healing"), red(name), ":", err)
		}

		return true
	})
}

// FetchResponse is the result of a Fetch call.
type FetchResponse struct {
	Status int
	Body   []byte
}

// JSON returns the string value at the given gjson path.
func (r *FetchResponse) JSON(path string) string {
	return gjson.GetBytes(r.Body, path).String()
}

// Fetch makes a GET request to the named node and returns the raw response.
// Returns nil if the request fails (e.g. node is down).
func (do *Do) Fetch(name, path string) *FetchResponse {
	node := do.getNode(name)
	url := fmt.Sprintf("http://127.0.0.1:%d%s", node.MappedPort(), path)

	ctx, cancel := context.WithTimeout(do.ctx, do.config.requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil
	}

	resp, err := do.client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	return &FetchResponse{Status: resp.StatusCode, Body: body}
}
