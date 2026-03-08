package kvstore

// Notes:
//
// Timing:
//   - Election timeout: 500-1,000ms (randomized)
//   - Heartbeat interval: 100ms
//   - Elections complete within 2s under normal conditions
//   - Wait ≥2s to verify "no leader elected" scenarios
//
// Observability (black-box testing via APIs):
//   - GET /cluster/info: role, term, leader, votedFor
//   - GET/PUT/DELETE /kv/*: 307 redirect to leader, 503 if no leader
//   - POST /cluster/partition: isolate nodes (persists across restarts)
//   - POST /cluster/heal: restore connectivity
//
// Scenarios:
//   1. Leader Election Completes
//   2. Exactly One Leader Per Term
//   3. Leader Maintains Authority via Heartbeats
//   4. Follower Redirects Clients to Leader
//   5. State Survives Crashes
//   6. Minority Partition Cannot Elect Leader
//   7. Majority Partition Elects Leader
//   8. Healing After Partition

import (
	"fmt"

	. "github.com/clstr-io/clstr/internal/attest"
)

func LeaderElection() *Suite {
	return New().
		// 0
		Setup(func(do *Do) {
			for i := range 5 {
				do.Start(fmt.Sprintf("node-%d", i+1))
			}
		})
}
