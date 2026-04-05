package kvstore

import (
	"fmt"
	"time"

	. "github.com/clstr-io/clstr/internal/attest"
)

// electionTimeout is the upper bound of the randomized election timeout.
const electionTimeout = 1_000 * time.Millisecond

// heartbeatInterval is how often the leader sends AppendEntries heartbeats.
const heartbeatInterval = 100 * time.Millisecond

// findLeader returns the name of the first node currently reporting role=leader.
func findLeader(do *Do) string {
	for _, node := range do.Nodes() {
		r := do.Fetch(node, "/cluster/info")
		if r != nil && r.JSON("role") == "leader" {
			return node
		}
	}

	return ""
}

// findFollower returns the name of the first node currently reporting role=follower.
func findFollower(do *Do) string {
	for _, node := range do.Nodes() {
		r := do.Fetch(node, "/cluster/info")
		if r != nil && r.JSON("role") == "follower" {
			return node
		}
	}

	return ""
}

func LeaderElection() *Suite {
	return New(
		WithCluster(5),
		WithRetryTimeout(3*time.Second),
	).

		// 1
		Test("Cluster Info Returns Pre-Election State", func(do *Do) {
			do.GET(do.AllNodes(), "/cluster/info").
				Status(Is(200)).
				JSON("id", Matches(`^\d+\.\d+\.\d+\.\d+:\d+$`)).
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
				Eventually(2*time.Second).
				Status(Is(200)).
				JSON("role", Is("leader")).
				Hint("No leader elected after 2 seconds.\n" +
					"Implement RequestVote RPC - candidates must request votes from all peers.\n" +
					"A candidate becomes leader once it receives votes from a majority (3 of 5).").
				Run()
		}).

		// 3
		Test("Exactly One Leader Per Term", func(do *Do) {
			do.GET(do.ExactlyOneNode(), "/cluster/info").
				Consistently(2*time.Second).
				Status(Is(200)).
				JSON("role", Is("leader")).
				Hint("Expected exactly one leader (found 0 or more than 1).\n" +
					"Each node must grant at most one vote per term.\n" +
					"A candidate must step down if it discovers a higher term.").
				Run()

			leaderNode := findLeader(do)
			if leaderNode == "" {
				panic("No leader node found.")
			}

			leaderAddr := do.Fetch(leaderNode, "/cluster/info").JSON("leader")

			do.GET(do.AllNodes(), "/cluster/info").
				Status(Is(200)).
				JSON("leader", Is(leaderAddr)).
				Hint(fmt.Sprintf("All nodes should agree on the same leader (%s).\n"+
					"Followers learn the leader's address from the leader-id field in AppendEntries.", leaderAddr)).
				Run()
		}).

		// 4
		Test("New Leader Elected After Leader Crash", func(do *Do) {
			prevLeaderNode := findLeader(do)
			if prevLeaderNode == "" {
				panic("No leader node found.")
			}

			initialTerm := do.Fetch(prevLeaderNode, "/cluster/info").JSON("term")
			do.Kill(prevLeaderNode)

			do.GET(do.ExactlyOneNode(), "/cluster/info").
				Eventually(2*time.Second).
				Status(Is(200)).
				JSON("role", Is("leader")).
				Hint("Expected exactly one new leader (found 0 or more than 1).\n" +
					"If no leader: ensure followers start an election when heartbeats stop.\n" +
					"If multiple leaders: each node must grant at most one vote per term.").
				Run()

			do.GET(do.AllNodes(), "/cluster/info").
				Eventually(2*time.Second).
				Status(Is(200)).
				JSON("term", GreaterThan(initialTerm)).
				Hint(fmt.Sprintf("Term should increment after a new election (was %s).\n"+
					"Candidates must increment currentTerm before starting an election.", initialTerm)).
				Run()

			do.Start(prevLeaderNode)

			do.GET(Node(prevLeaderNode), "/cluster/info").
				Eventually(2*time.Second).
				Status(Is(200)).
				JSON("term", GreaterThan(initialTerm)).
				JSON("role", Is("follower")).
				Hint(fmt.Sprintf("Restarted node should catch up to the current term (was %s) and become a follower.\n"+
					"The leader's heartbeats will update the rejoining node's term.", initialTerm)).
				Run()
		}).

		// 5
		Test("Leader Maintains Authority via Heartbeats", func(do *Do) {
			leaderNode := findLeader(do)
			if leaderNode == "" {
				panic("No leader node found.")
			}

			initialTerm := do.Fetch(leaderNode, "/cluster/info").JSON("term")

			time.Sleep(3 * electionTimeout)

			do.GET(Node(leaderNode), "/cluster/info").
				Status(Is(200)).
				JSON("role", Is("leader")).
				JSON("term", Is(initialTerm)).
				Hint("Leader changed during steady state - heartbeats may not be working.\n" +
					"Send empty AppendEntries RPCs every 100ms to prevent followers from timing out.").
				Run()
		}).

		// 6
		Test("Followers Redirect to Leader", func(do *Do) {
			followerNode := findFollower(do)
			if followerNode == "" {
				panic("No follower node found.")
			}

			leaderAddr := do.Fetch(followerNode, "/cluster/info").JSON("leader")
			if leaderAddr == "" {
				panic(fmt.Sprintf("Follower %s does not know the leader address.", followerNode))
			}

			hint := "Followers should redirect all requests to the leader.\n" +
				"Return HTTP 307 Temporary Redirect with a Location header pointing to\n" +
				"the leader's address: http://" + leaderAddr + "/kv/foo"

			do.GET(Node(followerNode), "/kv/foo").
				Status(Is(307)).
				Header("Location", Is("http://"+leaderAddr+"/kv/foo")).
				Hint(hint).
				Run()

			do.PUT(Node(followerNode), "/kv/foo", "value").
				Status(Is(307)).
				Header("Location", Is("http://"+leaderAddr+"/kv/foo")).
				Hint(hint).
				Run()

			do.DELETE(Node(followerNode), "/kv/foo").
				Status(Is(307)).
				Header("Location", Is("http://"+leaderAddr+"/kv/foo")).
				Hint(hint).
				Run()
		}).

		// 7
		Test("Service Unavailable During Election", func(do *Do) {
			prevLeaderNode := findLeader(do)
			if prevLeaderNode == "" {
				panic("No leader node found.")
			}

			do.Kill(prevLeaderNode)

			do.PUT(do.AllNodes(), "/kv/foo", "value").
				Eventually(2 * heartbeatInterval).
				Status(Is(503)).
				Hint("When no leader is known, return 503 Service Unavailable.\n" +
					"Clear the known-leader state when contact with the leader is lost.").
				Run()

			do.Start(prevLeaderNode)
		}).

		// 8
		Test("Partition Enforces Quorum", func(do *Do) {
			do.Partition([]string{"n1", "n2"}, []string{"n3", "n4", "n5"})

			do.GET(do.ExactlyOneNode("n3", "n4", "n5"), "/cluster/info").
				Eventually(2*time.Second).
				Status(Is(200)).
				JSON("role", Is("leader")).
				Hint("Expected exactly one leader in the majority partition (found 0 or more than 1).\n" +
					"If no leader: the 3-node partition has quorum and should elect a leader.\n" +
					"If multiple leaders: each node must grant at most one vote per term.").
				Run()

			do.GET(do.AllNodes("n1", "n2"), "/cluster/info").
				Consistently(2*time.Second).
				Status(Is(200)).
				JSON("leader", IsNull[string]()).
				Hint("The minority partition (2 of 5) must not elect a leader.\n" +
					"A candidate needs votes from at least 3 nodes; with only 2 reachable, no election can succeed.").
				Run()
		}).

		// 9
		Test("Cluster Converges After Partition Heals", func(do *Do) {
			do.Heal()

			do.GET(do.AtLeastOneNode(), "/cluster/info").
				Eventually(2*time.Second).
				Status(Is(200)).
				JSON("role", Is("leader")).
				Hint("No leader elected after partition healed.\n" +
					"Once the partition heals, the majority partition's leader should remain\n" +
					"or a new election should complete quickly.").
				Run()

			leaderNode := findLeader(do)
			if leaderNode == "" {
				panic("No leader node found.")
			}

			info := do.Fetch(leaderNode, "/cluster/info")
			leaderAddr := info.JSON("leader")
			term := info.JSON("term")

			do.GET(do.AllNodes(), "/cluster/info").
				Eventually(2*time.Second).
				Status(Is(200)).
				JSON("leader", Is(leaderAddr)).
				JSON("term", Is(term)).
				Hint("After healing, all nodes should converge on the same leader.\n" +
					"When a node receives an AppendEntries or RequestVote with a higher term,\n" +
					"it must immediately revert to follower and update its term.").
				Run()
		})
}
