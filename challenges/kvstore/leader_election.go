package kvstore

import (
	"fmt"
	"time"

	. "github.com/clstr-io/clstr/internal/attest"
)

const (
	// electionTimeout is the upper bound of the randomized election timeout.
	electionTimeout = time.Second

	// heartbeatInterval is how often the leader sends AppendEntries heartbeats.
	heartbeatInterval = 100 * time.Millisecond
)

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
		WithRetryTimeout(3*electionTimeout),
	).

		// 1
		Test("/cluster/info Returns Pre-Election State", func(do *Do) {
			do.GET(do.AllNodes(), "/cluster/info").
				Status(Is(200)).
				JSON("id", Matches(`^10\.0\.42\.\d+:8080$`)).
				JSON("role", OneOf("leader", "follower", "candidate")).
				JSON("term", Is("0")).
				JSON("leader", IsNull[string]()).
				JSON("peers", HasLen[string](4)).
				Hint("GET /cluster/info must return a JSON object with:\n" +
					"  id: this node's own address (from ADDR)\n" +
					"  role: \"leader\", \"follower\", or \"candidate\"\n" +
					"  term: 0 before any election has occurred\n" +
					"  leader: null before a leader is known\n" +
					"  peers: the 4 other nodes' addresses").
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

			time.Sleep(3 * electionTimeout)

			do.GET(Node(leaderNode), "/cluster/info").
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
				Hint("The leader should accept PUT requests.\n" +
					"Ensure your leader node processes write operations.").
				Run()

			do.GET(Node(leaderNode), "/kv/leader:key").
				Status(Is(200)).
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

			currentTerm := leaderInfo.JSON("term")
			do.Kill(prevLeaderNode)

			do.GET(do.ExactlyOneNode(), "/cluster/info").
				Eventually(3*electionTimeout).
				Status(Is(200)).
				JSON("role", Is("leader")).
				Hint("Expected exactly one new leader (found 0 or more than 1).\n" +
					"If no leader: ensure followers start an election when heartbeats stop.\n" +
					"If multiple leaders: each node must grant at most one vote per term.").
				Run()

			do.GET(do.AllNodes(), "/cluster/info").
				Eventually(3*electionTimeout).
				Status(Is(200)).
				JSON("term", GreaterThan(currentTerm)).
				Hint(fmt.Sprintf("Term should increment after a new election (was %s).\n"+
					"Candidates must increment currentTerm before starting an election.", currentTerm)).
				Run()

			do.Start(prevLeaderNode)

			do.GET(Node(prevLeaderNode), "/cluster/info").
				Eventually(3*electionTimeout).
				Status(Is(200)).
				JSON("term", GreaterThan(currentTerm)).
				JSON("role", Is("follower")).
				JSON("leader", Matches(`^10\.0\.42\.\d+:8080$`)).
				Hint(fmt.Sprintf("Restarted node should catch up to the current term (was %s), become a follower, and learn the current leader.\n"+
					"The leader's heartbeats will update the rejoining node's term and leader.", currentTerm)).
				Run()
		}).

		// 8
		Test("Service Unavailable During Election", func(do *Do) {
			prevLeaderNode, _ := findLeader(do)
			if prevLeaderNode == "" {
				panic("No leader node found.")
			}

			do.Kill(prevLeaderNode)

			hint503 := "When no leader is known, return 503 Service Unavailable.\n" +
				"Clear the known-leader state when contact with the leader is lost."

			do.GET(do.AllNodes(), "/kv/foo").
				Eventually(3 * electionTimeout).
				Status(Is(503)).
				Hint(hint503).
				Run()

			do.PUT(do.AllNodes(), "/kv/foo", "value").
				Status(Is(503)).
				Hint(hint503).
				Run()

			do.DELETE(do.AllNodes(), "/kv/foo").
				Status(Is(503)).
				Hint(hint503).
				Run()

			do.Start(prevLeaderNode)
		}).

		// 9
		Test("Partition Enforces Quorum", func(do *Do) {
			// Restart the current leader so no node enters the partition as an incumbent leader.
			// This prevents a minority node from failing the Consistently check with a stale role=leader.
			prevLeaderNode, _ := findLeader(do)
			if prevLeaderNode != "" {
				do.Restart(prevLeaderNode)
			}

			do.Partition([]string{"n1", "n2"}, []string{"n3", "n4", "n5"})

			do.GET(do.ExactlyOneNode("n3", "n4", "n5"), "/cluster/info").
				Eventually(3*electionTimeout).
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

			do.GET(do.AllNodes("n1", "n2"), "/kv/foo").
				Status(Is(503)).
				Hint("Minority partition nodes [n1, n2] have no leader and cannot serve requests.\n" +
					"Return 503 Service Unavailable when no leader is known.").
				Run()
		}).

		// 10
		Test("Cluster Converges After Partition Heals", func(do *Do) {
			do.Heal()

			// Wait for the cluster to settle.
			time.Sleep(3 * electionTimeout)

			do.GET(do.ExactlyOneNode(), "/cluster/info").
				Eventually(3*electionTimeout).
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
		})
}
