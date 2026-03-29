package attest

import "strconv"

// Do

func (do *Do) Cancel() {
	do.cancel()
}

func (do *Do) MockNode(name, port string) {
	node := &Node{}
	node.port, _ = strconv.Atoi(port)

	do.nodes.Set(name, node)
}
