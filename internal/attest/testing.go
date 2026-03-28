package attest

import "strconv"

// Do

func (do *Do) Cancel() {
	do.cancel()
}

func (do *Do) MockNode(name, realPort string) {
	node := &Node{}
	node.realPort, _ = strconv.Atoi(realPort)

	do.nodes.Set(name, node)
}
