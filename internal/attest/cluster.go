package attest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
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
	Restart(ctx context.Context, signal syscall.Signal, timeout time.Duration) error
	Exec(ctx context.Context, args ...string) error
}

// nodeKind identifies which nodes a NodeSelector targets.
type nodeKind uint8

const (
	nodeNamed      nodeKind = iota // a single named node
	nodeExactlyOne                 // exactly one node across the cluster
	nodeAtLeastOne                 // at least one node across the cluster
	nodeAll                        // all nodes across the cluster
	nodeExcept                     // all nodes except the named ones
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

// ExceptNodes returns a selector targeting all alive nodes except those specified.
func (do *Do) ExceptNodes(names ...string) NodeSelector {
	return NodeSelector{kind: nodeExcept, names: names}
}

// Names resolves a selector to the node names it targets.
func (do *Do) Names(sel NodeSelector) []string {
	return do.resolveNames(sel)
}

// resolveNames returns the node names matched by sel.
func (do *Do) resolveNames(sel NodeSelector) []string {
	switch sel.kind {
	case nodeNamed:
		return sel.names
	case nodeExcept:
		excluded := make(map[string]bool, len(sel.names))
		for _, n := range sel.names {
			excluded[n] = true
		}

		var names []string
		do.nodes.Range(func(name string, node clusterNode) bool {
			if node.IsAlive() && !excluded[name] {
				names = append(names, name)
			}

			return true
		})
		return names
	default:
		names := sel.names
		if len(names) == 0 {
			names = do.Nodes()
		}

		return names
	}
}

// Nodes returns the names of all alive nodes in the cluster.
func (do *Do) Nodes() []string {
	var names []string
	do.nodes.Range(func(name string, node clusterNode) bool {
		if node.IsAlive() {
			names = append(names, name)
		}

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
	node.Annotate("START")

	err := node.Start(do.ctx)
	if err != nil {
		panic(fmt.Sprintf("start %q: %v", name, err))
	}

	containerName := "clstr-" + do.config.challengeKey + "-" + name
	err = waitUntilNodeReady(
		do.ctx, name, containerName, node,
		do.config.nodeStartupTimeout, do.config.pollInterval, do.config.requestTimeout,
	)
	if err != nil {
		panic(err.Error())
	}
}

// Stop sends SIGTERM to the node, then SIGKILL after the shutdown timeout.
func (do *Do) Stop(name string) {
	node := do.getNode(name)
	node.Annotate("STOP")

	err := node.Stop(do.ctx, do.config.nodeShutdownTimeout)
	if err != nil {
		fmt.Println(red("Error stopping"), red(name), ":", err)
	}
}

// Kill sends SIGKILL to the node immediately.
func (do *Do) Kill(name string) {
	node := do.getNode(name)
	node.Annotate("KILL")

	err := node.Kill(do.ctx)
	if err != nil {
		fmt.Println(red("Error killing"), red(name), ":", err)
	}
}

// Restart restarts the node. Pass syscall.SIGKILL to crash immediately instead of graceful shutdown.
func (do *Do) Restart(name string, sig ...syscall.Signal) {
	node := do.getNode(name)
	node.Annotate("RESTART")

	signal := syscall.SIGTERM
	timeout := do.config.nodeShutdownTimeout
	if len(sig) > 0 && sig[0] == syscall.SIGKILL {
		signal = syscall.SIGKILL
		timeout = 0
	}

	err := node.Restart(do.ctx, signal, timeout)
	if err != nil {
		panic(fmt.Sprintf("restart %q: %v", name, err))
	}

	containerName := "clstr-" + do.config.challengeKey + "-" + name
	err = waitUntilNodeReady(
		do.ctx, name, containerName, node,
		do.config.nodeStartupTimeout, do.config.pollInterval, do.config.requestTimeout,
	)
	if err != nil {
		panic(err.Error())
	}
}

// Partition installs iptables DROP rules so nodes in different groups cannot
// reach each other. Rules are bidirectional.
func (do *Do) Partition(groups ...[]string) {
	type nodeInfo struct {
		node     clusterNode
		cutoffs  []string
		blockIPs []string
	}
	nodes := map[string]*nodeInfo{}

	for i, g1 := range groups {
		for j, g2 := range groups {
			if i >= j {
				continue
			}

			for _, a := range g1 {
				for _, b := range g2 {
					nA, nB := do.getNode(a), do.getNode(b)
					ipA, ipB := nA.ContainerIP(), nB.ContainerIP()

					if nodes[a] == nil {
						nodes[a] = &nodeInfo{node: nA}
					}
					if nodes[b] == nil {
						nodes[b] = &nodeInfo{node: nB}
					}

					nodes[a].cutoffs = append(nodes[a].cutoffs, b)
					nodes[a].blockIPs = append(nodes[a].blockIPs, ipB)
					nodes[b].cutoffs = append(nodes[b].cutoffs, a)
					nodes[b].blockIPs = append(nodes[b].blockIPs, ipA)
				}
			}
		}
	}

	var wg sync.WaitGroup
	for name, info := range nodes {
		wg.Add(1)
		go func(name string, info *nodeInfo) {
			defer wg.Done()

			sort.Strings(info.cutoffs)
			info.node.Annotate("PARTITIONED FROM: " + strings.Join(info.cutoffs, ", "))

			for _, ip := range info.blockIPs {
				execOnNode(do.ctx, info.node, "iptables", "-A", "INPUT", "-s", ip, "-j", "DROP")
				execOnNode(do.ctx, info.node, "iptables", "-A", "OUTPUT", "-d", ip, "-j", "DROP")
			}
		}(name, info)
	}
	wg.Wait()

	do.Settle(do.config.clusterSettleDuration)
}

// Heal flushes all iptables rules on every node, restoring full connectivity.
func (do *Do) Heal() {
	var wg sync.WaitGroup
	do.nodes.Range(func(name string, node clusterNode) bool {
		wg.Add(1)
		go func(name string, node clusterNode) {
			defer wg.Done()

			node.Annotate("PARTITION HEALED")

			err := node.Exec(do.ctx, "iptables", "-F")
			if err != nil {
				fmt.Println(red("Error healing"), red(name), ":", err)
			}
		}(name, node)
		return true
	})
	wg.Wait()

	do.Settle(do.config.clusterSettleDuration)
}

// Impair applies network impairments to outgoing traffic on the selected nodes.
func (do *Do) Impair(sel NodeSelector, impairments ...Impairment) {
	tcArgs := []string{"tc", "qdisc", "replace", "dev", "eth0", "root", "netem"}
	descs := make([]string, len(impairments))
	for i, imp := range impairments {
		tcArgs = append(tcArgs, imp.args...)
		descs[i] = imp.desc
	}
	annotation := "IMPAIRED: " + strings.Join(descs, ", ")

	var wg sync.WaitGroup
	for _, name := range do.resolveNames(sel) {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()

			node := do.getNode(name)
			node.Annotate(annotation)

			execOnNode(do.ctx, node, tcArgs...)
		}(name)
	}
	wg.Wait()
}

// Repair removes tc netem impairments from the selected nodes.
// If no selector is given, all nodes are repaired.
func (do *Do) Repair(sel ...NodeSelector) {
	var names []string
	if len(sel) == 0 {
		names = do.Nodes()
	} else {
		names = do.resolveNames(sel[0])
	}

	var wg sync.WaitGroup
	for _, name := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()

			node := do.getNode(name)
			node.Annotate("REPAIRED")

			node.Exec(do.ctx, "tc", "qdisc", "del", "dev", "eth0", "root")
		}(name)
	}
	wg.Wait()
}

// Settle pauses for the given duration to let the cluster settle.
func (do *Do) Settle(d time.Duration) {
	select {
	case <-do.ctx.Done():
	case <-time.After(d):
	}
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
