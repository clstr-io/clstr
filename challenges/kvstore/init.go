package kvstore

import "github.com/clstr-io/clstr/internal/registry"

func init() {
	challenge := &registry.Challenge{
		Name:    "Distributed Key-Value Store",
		Summary: "Build a distributed key-value store from scratch using the Raft consensus algorithm.",
	}

	challenge.AddStage("http-api", "Store and Retrieve Data", HTTPAPI)
	challenge.AddStage("persistence", "Data Survives SIGTERM", Persistence)
	challenge.AddStage("crash-recovery", "Data Survives SIGKILL", CrashRecovery)
	challenge.AddStage("leader-election", "Cluster Elects and Maintains Leader", LeaderElection)
	challenge.AddStage("log-replication", "Data Replicates to All Nodes", LogReplication)
	challenge.AddStage("log-compaction", "System Manages Log Growth", LogCompaction)
	challenge.AddStage("membership-changes", "Add and Remove Nodes One at a Time", MembershipChanges)
	challenge.AddStage("joint-consensus", "Add and Remove Nodes in Bulk", JointConsensus)

	registry.RegisterChallenge("kv-store", challenge)
}
