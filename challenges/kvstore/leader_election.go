package kvstore

import (
	"fmt"
	"syscall"
	"time"

	. "github.com/clstr-io/clstr/internal/attest"
)

// electionTimeout is the upper bound of the randomized election timeout.
const electionTimeout = time.Second

// findLeader returns the name and cluster info of the first node currently
// reporting role=leader. If no node names are provided, all nodes are searched.
func findLeader(do *Do, nodes ...string) (string, *FetchResponse) {
	if len(nodes) == 0 {
		nodes = do.Nodes()
	}

	for _, node := range nodes {
		r := do.Fetch(node, "/cluster/info")
		if r != nil && r.JSON("role") == "leader" {
			return node, r
		}
	}

	return "", nil
}

// findFollower returns the name and cluster info of the first node currently
// reporting role=follower. If no node names are provided, all nodes are searched.
func findFollower(do *Do, nodes ...string) (string, *FetchResponse) {
	if len(nodes) == 0 {
		nodes = do.Nodes()
	}

	for _, node := range nodes {
		r := do.Fetch(node, "/cluster/info")
		if r != nil && r.JSON("role") == "follower" {
			return node, r
		}
	}

	return "", nil
}

func LeaderElection() *Suite {
	return New(
		WithCluster(5),
		WithClusterSettleDuration(3*electionTimeout),
	).

		// 1
		Test("/cluster/info Returns Cluster State", func(do *Do) {
			do.GET(do.AllNodes(), "/cluster/info").
				Status(Is(200)).
				Header("Content-Type", Contains("application/json")).
				JSON("id", Matches(`^10\.0\.42\.\d+:8080$`)).
				JSON("role", OneOf("leader", "follower", "candidate")).
				JSON("peers", HasLen[string](4)).
				JSON("peers.0", Matches(`^10\.0\.42\.\d+:8080$`)).
				Hint("GET /cluster/info must return a JSON object with:\n" +
					"  id: this node's own address (from ADDR)\n" +
					"  role: \"leader\", \"follower\", or \"candidate\"\n" +
					"  peers: the 4 other nodes' addresses (e.g. \"10.0.42.102:8080\")").
				Run()
		}).

		// 2
		Test("Leader Election Completes", func(do *Do) {
			do.GET(do.AtLeastOneNode(), "/cluster/info").
				Eventually(3*electionTimeout).
				Status(Is(200)).
				JSON("role", Is("leader")).
				JSON("term", GreaterThan("0")).
				JSON("leader", Matches(`^10\.0\.42\.\d+:8080$`)).
				Hint("No leader elected after 3 seconds.\n" +
					"Implement RequestVote RPC - candidates must request votes from all peers.\n" +
					"A candidate becomes leader once it receives votes from a majority (3 of 5).").
				Run()
		}).

		// 3
		Test("Exactly One Leader Per Term", func(do *Do) {
			do.GET(do.ExactlyOneNode(), "/cluster/info").
				Consistently(3*electionTimeout).
				Status(Is(200)).
				JSON("role", Is("leader")).
				Hint("Expected exactly one leader (found 0 or more than 1).\n" +
					"Each node must grant at most one vote per term.\n" +
					"A candidate must step down if it discovers a higher term.").
				Run()

			leaderNode, leaderInfo := findLeader(do)
			if leaderNode == "" {
				panic("No leader node found.")
			}

			leaderID := leaderInfo.JSON("id")

			do.GET(do.AllNodes(), "/cluster/info").
				Status(Is(200)).
				JSON("leader", Is(leaderID)).
				Hint(fmt.Sprintf("All nodes should agree on the same leader (%s).\n"+
					"Followers learn the leader's address from the leader-id field in AppendEntries.", leaderID)).
				Run()
		}).

		// 4
		Test("Leader Maintains Authority via Heartbeats", func(do *Do) {
			leaderNode, leaderInfo := findLeader(do)
			if leaderNode == "" {
				panic("No leader node found.")
			}

			currentTerm := leaderInfo.JSON("term")

			do.GET(Node(leaderNode), "/cluster/info").
				Consistently(3*electionTimeout).
				Status(Is(200)).
				JSON("role", Is("leader")).
				JSON("term", Is(currentTerm)).
				Hint("Leader changed during steady state - heartbeats may not be working.\n" +
					"Send empty AppendEntries RPCs every 100ms to prevent followers from timing out.").
				Run()
		}).

		// 5
		Test("Followers Redirect to Leader", func(do *Do) {
			followerNode, followerInfo := findFollower(do)
			if followerNode == "" {
				panic("No follower node found.")
			}

			leaderAddr := followerInfo.JSON("leader")
			if leaderAddr == "" {
				panic(fmt.Sprintf("Follower %s does not know the leader address.", followerNode))
			}

			hint307 := "Followers should redirect all requests to the leader.\n" +
				"Return HTTP 307 Temporary Redirect with a Location header pointing to\n" +
				"the leader's address: http://" + leaderAddr + "/kv/foo"

			do.GET(Node(followerNode), "/kv/foo").
				Status(Is(307)).
				Header("Location", Is("http://"+leaderAddr+"/kv/foo")).
				Hint(hint307).
				Run()

			do.PUT(Node(followerNode), "/kv/foo", "value").
				Status(Is(307)).
				Header("Location", Is("http://"+leaderAddr+"/kv/foo")).
				Hint(hint307).
				Run()

			do.DELETE(Node(followerNode), "/kv/foo").
				Status(Is(307)).
				Header("Location", Is("http://"+leaderAddr+"/kv/foo")).
				Hint(hint307).
				Run()
		}).

		// 6
		Test("Leader Handles KV Operations", func(do *Do) {
			leaderNode, _ := findLeader(do)
			if leaderNode == "" {
				panic("No leader node found.")
			}

			do.PUT(Node(leaderNode), "/kv/leader:key", "initial").
				Status(Is(200)).
				Header("Content-Type", Contains("text/plain")).
				Hint("The leader should accept PUT requests.\n" +
					"Ensure your leader node processes write operations.").
				Run()

			do.GET(Node(leaderNode), "/kv/leader:key").
				Status(Is(200)).
				Header("Content-Type", Contains("text/plain")).
				Body(Is("initial")).
				Hint("The leader should return stored values.\n" +
					"Ensure your leader node processes read operations.").
				Run()

			do.PUT(Node(leaderNode), "/kv/leader:key", "updated").
				Status(Is(200)).
				Hint("The leader should allow overwriting existing keys.\n" +
					"Ensure PUT requests update the value of existing keys.").
				Run()

			do.GET(Node(leaderNode), "/kv/leader:key").
				Status(Is(200)).
				Body(Is("updated")).
				Hint("The leader should return the most recently written value.\n" +
					"Ensure overwrite operations update the stored value correctly.").
				Run()

			do.DELETE(Node(leaderNode), "/kv/leader:key").
				Status(Is(200)).
				Hint("The leader should accept DELETE requests.\n" +
					"Ensure your leader node processes delete operations.").
				Run()

			do.GET(Node(leaderNode), "/kv/leader:key").
				Status(Is(404)).
				Header("Content-Type", Contains("text/plain")).
				Hint("The leader should return 404 after a key is deleted.\n" +
					"Ensure delete operations are applied correctly.").
				Run()

			do.DELETE(Node(leaderNode), "/clear").
				Status(Is(200)).
				Hint("The leader should accept CLEAR requests.\n" +
					"Ensure your leader node processes the /clear endpoint.").
				Run()
		}).

		// 7
		Test("New Leader Elected After Leader Crash", func(do *Do) {
			prevLeaderNode, leaderInfo := findLeader(do)
			if prevLeaderNode == "" {
				panic("No leader node found.")
			}

			prevTerm := leaderInfo.JSON("term")
			do.Kill(prevLeaderNode)

			do.GET(do.ExactlyOneNode(), "/cluster/info").
				Eventually(3*electionTimeout).
				Status(Is(200)).
				JSON("role", Is("leader")).
				JSON("term", GreaterThan(prevTerm)).
				Hint("Expected exactly one new leader with a higher term (found 0 or more than 1).\n" +
					"If no leader: ensure followers start an election when heartbeats stop.\n" +
					"If multiple leaders: each node must grant at most one vote per term.\n" +
					"If term did not increment: candidates must increment currentTerm before starting an election.").
				Run()

			newLeaderNode, newLeaderInfo := findLeader(do)
			if newLeaderNode == "" {
				panic("No new leader found after previous leader was killed.")
			}
			newLeaderID := newLeaderInfo.JSON("id")

			do.GET(do.AllNodes(), "/cluster/info").
				Eventually(3*electionTimeout).
				Status(Is(200)).
				JSON("leader", Is(newLeaderID)).
				Hint(fmt.Sprintf("All surviving nodes should agree on the new leader (%s).\n"+
					"Followers learn the leader's address from the leader-id field in AppendEntries.", newLeaderID)).
				Run()

			do.Start(prevLeaderNode)

			do.GET(Node(prevLeaderNode), "/cluster/info").
				Eventually(3*electionTimeout).
				Status(Is(200)).
				JSON("term", GreaterThan(prevTerm)).
				JSON("role", Is("follower")).
				JSON("leader", Is(newLeaderID)).
				Hint(fmt.Sprintf("Restarted node should catch up to the current term (was %s), become a follower, and converge on the current leader (%s).\n"+
					"The leader's heartbeats will update the rejoining node's term and leader.", prevTerm, newLeaderID)).
				Run()
		}).

		// 8
		Test("Partition Enforces Quorum", func(do *Do) {
			do.Partition([]string{"n1", "n2"}, []string{"n3", "n4", "n5"})

			do.GET(do.ExactlyOneNode("n3", "n4", "n5"), "/cluster/info").
				Status(Is(200)).
				JSON("role", Is("leader")).
				Hint("Expected exactly one leader in the majority partition [n3, n4, n5] (found 0 or more than 1).\n" +
					"If no leader: the 3-node partition has quorum and should elect a leader.\n" +
					"If multiple leaders: each node must grant at most one vote per term.").
				Run()

			majorityLeaderNode, leaderInfo := findLeader(do, "n3", "n4", "n5")
			if majorityLeaderNode == "" {
				panic("No leader node found in majority partition.")
			}

			majorityLeaderID := leaderInfo.JSON("id")

			do.GET(do.AllNodes("n3", "n4", "n5"), "/cluster/info").
				Status(Is(200)).
				JSON("leader", Is(majorityLeaderID)).
				Hint(fmt.Sprintf("All nodes in the majority partition should agree on the same leader (%s).\n"+
					"Followers learn the leader's address from the leader-id field in AppendEntries.", majorityLeaderID)).
				Run()

			do.GET(do.AllNodes("n1", "n2"), "/cluster/info").
				Consistently(3*electionTimeout).
				Status(Is(200)).
				JSON("role", OneOf("follower", "candidate")).
				JSON("leader", IsNull[string]()).
				Hint("The minority partition [n1, n2] must not elect a leader.\n" +
					"A candidate needs votes from at least 3 nodes; with only n1 and n2 reachable, no election can succeed.").
				Run()
		}).

		// 9
		Test("Leaderless Nodes Return 503", func(do *Do) {
			hint503 := "Minority partition nodes [n1, n2] have no leader and cannot serve requests.\n" +
				"Return 503 Service Unavailable when no leader is known."

			do.GET(do.AllNodes("n1", "n2"), "/kv/foo").
				Status(Is(503)).
				Hint(hint503).
				Run()

			do.PUT(do.AllNodes("n1", "n2"), "/kv/foo", "value").
				Status(Is(503)).
				Hint(hint503).
				Run()

			do.DELETE(do.AllNodes("n1", "n2"), "/kv/foo").
				Status(Is(503)).
				Hint(hint503).
				Run()
		}).

		// 10
		Test("Cluster Converges After Partition Heals", func(do *Do) {
			do.Heal()

			do.GET(do.ExactlyOneNode(), "/cluster/info").
				Status(Is(200)).
				JSON("role", Is("leader")).
				Hint("No leader elected after partition healed.\n" +
					"Once the partition heals, the majority partition's leader should remain\n" +
					"or a new election should complete quickly.").
				Run()

			leaderNode, leaderInfo := findLeader(do)
			if leaderNode == "" {
				panic("No leader node found.")
			}

			leaderAddr := leaderInfo.JSON("id")

			do.GET(do.AllNodes(), "/cluster/info").
				Status(Is(200)).
				JSON("leader", Is(leaderAddr)).
				Hint("After healing, all nodes should converge on the same leader.\n" +
					"When a node receives an AppendEntries or RequestVote with a higher term,\n" +
					"it must immediately revert to follower and update its term.").
				Run()
		}).

		// 11
		Test("Slow Leader Steps Down and Cluster Re-elects", func(do *Do) {
			leaderNode, leaderInfo := findLeader(do)
			if leaderNode == "" {
				panic("No leader node found.")
			}

			currentTerm := leaderInfo.JSON("term")

			do.Impair(Node(leaderNode), Delay(2*electionTimeout))

			do.GET(do.ExactlyOneNode(do.Names(do.ExceptNodes(leaderNode))...), "/cluster/info").
				Eventually(3*electionTimeout).
				Status(Is(200)).
				JSON("role", Is("leader")).
				JSON("term", GreaterThan(currentTerm)).
				Hint("No new leader elected after the leader's outgoing traffic was delayed by 2s.\n" +
					"Followers should time out waiting for heartbeats and start a new election.\n" +
					"The 4 remaining nodes have quorum and must elect a new leader among themselves.\n").
				Run()

			do.Repair(Node(leaderNode))

			do.GET(Node(leaderNode), "/cluster/info").
				Eventually(3*electionTimeout).
				Status(Is(200)).
				JSON("role", Is("follower")).
				JSON("term", GreaterThan(currentTerm)).
				Hint(fmt.Sprintf("Repaired node %s should step down and rejoin as follower.\n"+
					"When it receives an AppendEntries from the new leader with a higher term,\n"+
					"it must immediately revert to follower and update its term.", leaderNode)).
				Run()
		}).

		// 12
		Test("Election Completes Under Packet Loss", func(do *Do) {
			leaderNode, leaderInfo := findLeader(do)
			if leaderNode == "" {
				panic("No leader node found.")
			}

			currentTerm := leaderInfo.JSON("term")

			do.Impair(do.AllNodes(), Loss(20))
			do.Restart(leaderNode, syscall.SIGKILL)

			do.GET(do.ExactlyOneNode(), "/cluster/info").
				Eventually(10*electionTimeout).
				Status(Is(200)).
				JSON("role", Is("leader")).
				JSON("term", GreaterThan(currentTerm)).
				Hint(fmt.Sprintf("No leader elected under 20%% packet loss after %s was restarted.\n"+
					"At this loss rate, a candidate should be able to collect a majority of votes.", leaderNode)).
				Run()

			do.Repair()

			newLeaderNode, newLeaderInfo := findLeader(do)
			if newLeaderNode == "" {
				panic("No leader node found after repair.")
			}

			newLeaderAddr := newLeaderInfo.JSON("id")

			do.GET(do.AllNodes(), "/cluster/info").
				Eventually(3*electionTimeout).
				Status(Is(200)).
				JSON("leader", Is(newLeaderAddr)).
				Hint(fmt.Sprintf("Cluster did not converge on a single leader (%s) after packet loss was removed.\n"+
					"All nodes should agree on the same leader once the network is healthy.", newLeaderAddr)).
				Run()
		})
}
