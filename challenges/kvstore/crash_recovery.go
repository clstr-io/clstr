package kvstore

import (
	"fmt"
	"strings"
	"syscall"

	. "github.com/clstr-io/clstr/internal/attest"
)

func CrashRecovery() *Suite {
	return New().
		// 0
		Setup(func(do *Do) {
			do.Start("node")
		}).

		// 1
		Test("Basic WAL Durability", func(do *Do) {
			// Test various operations that should all be logged
			do.HTTP("node", "PUT", "/kv/wal:basic", "initial").T().
				Status(Is(200)).
				Assert("Your server should accept PUT requests.\n" +
					"Ensure your HTTP handler processes PUT requests correctly.")

			do.HTTP("node", "PUT", "/kv/wal:updated", "v1").T().
				Status(Is(200)).
				Assert("Your server should accept PUT requests.\n" +
					"Ensure your HTTP handler processes PUT requests correctly.")

			do.HTTP("node", "PUT", "/kv/wal:updated", "v2").T().
				Status(Is(200)).
				Assert("Your server should allow overwriting existing keys.\n" +
					"Ensure PUT requests update the value of existing keys.")

			do.HTTP("node", "PUT", "/kv/wal:deleted", "temporary").T().
				Status(Is(200)).
				Assert("Your server should accept PUT requests.\n" +
					"Ensure your HTTP handler processes PUT requests correctly.")

			do.HTTP("node", "DELETE", "/kv/wal:deleted").T().
				Status(Is(200)).
				Assert("Your server should accept DELETE requests.\n" +
					"Ensure your HTTP handler processes DELETE requests correctly.")

			// Crash without warning
			do.Restart("node", syscall.SIGKILL)

			// Verify correct final state after recovery
			do.HTTP("node", "GET", "/kv/wal:basic").T().
				Status(Is(200)).
				Body(Is("initial")).
				Assert("Your server acknowledged the PUT but lost the data after crashing.\n" +
					"Implement a Write-Ahead Log (WAL) that records operations before applying them to memory.\n" +
					"Ensure writes are durably stored (fsync/flush) before or when acknowledging to the client.")

			do.HTTP("node", "GET", "/kv/wal:updated").T().
				Status(Is(200)).
				Body(Is("v2")).
				Assert("Your server should preserve updated values after crash.\n" +
					"Ensure your WAL records all PUT operations, including updates to existing keys.")

			do.HTTP("node", "GET", "/kv/wal:deleted").T().
				Status(Is(404)).
				Assert("Your server should preserve deletion state after crash.\n" +
					"Ensure your WAL records DELETE operations and replays them correctly during recovery.")
		}).

		// 2
		Test("Multiple Crash Recovery Cycles", func(do *Do) {
			// Simulate multiple crash/restart cycles
			for cycle := 1; cycle <= 4; cycle++ {
				// Add cycle-specific data
				cycleKey := fmt.Sprintf("cycle:crash_%d", cycle)
				cycleValue := fmt.Sprintf("crash_data_%d", cycle)

				do.HTTP("node", "PUT", fmt.Sprintf("/kv/%s", cycleKey), cycleValue).T().
					Status(Is(200)).
					Assert("Your server should accept PUT requests.\n" +
						"Ensure your HTTP handler processes PUT requests correctly.")

				// Crash without warning
				do.Restart("node", syscall.SIGKILL)

				// Verify cycle data survived
				do.HTTP("node", "GET", fmt.Sprintf("/kv/%s", cycleKey)).T().
					Status(Is(200)).
					Body(Is(cycleValue)).
					Assert("Your server should preserve data across crash/restart cycles.\n" +
						"Ensure your WAL is append-only and recovery replays all operations correctly.")
			}

			// Verify all historical data from all cycles still exists
			allHistoricalData := map[string]string{
				"wal:basic":     "initial",
				"wal:updated":   "v2",
				"cycle:crash_1": "crash_data_1",
				"cycle:crash_2": "crash_data_2",
				"cycle:crash_3": "crash_data_3",
				"cycle:crash_4": "crash_data_4",
			}

			for key, expectedValue := range allHistoricalData {
				do.HTTP("node", "GET", fmt.Sprintf("/kv/%s", key)).T().
					Status(Is(200)).
					Body(Is(expectedValue)).
					Assert("Your server should preserve all historical data across multiple crashes.\n" +
						"Ensure the WAL is never truncated until after a successful checkpoint.\n" +
						"Recovery should load the latest snapshot (if any) and replay all subsequent WAL operations.")
			}
		}).

		// 3
		Test("Rapid Write Burst Before Crash", func(do *Do) {
			// Write many operations rapidly in sequence
			for i := 1; i <= 500; i++ {
				do.HTTP("node", "PUT", fmt.Sprintf("/kv/burst:%d", i), strings.Repeat("data", 250)).T().
					Status(Is(200)).
					Assert("Your server should accept PUT requests.\n" +
						"Ensure your HTTP handler processes PUT requests correctly.")
			}

			// Crash immediately
			do.Restart("node", syscall.SIGKILL)

			// Verify all acknowledged writes survived
			for i := 1; i <= 500; i++ {
				do.HTTP("node", "GET", fmt.Sprintf("/kv/burst:%d", i)).T().
					Status(Is(200)).
					Body(Is(strings.Repeat("data", 250))).
					Assert("Your server acknowledged the PUT but lost the data after crashing.\n" +
						"Ensure writes are durably stored before acknowledging them to the client.\n" +
						"Call fsync/flush after writing to WAL, or batch operations and sync before responding.")
			}
		}).

		// 4
		Test("Test Recovery When Under Concurrent Load", func(do *Do) {
			// Generate concurrent load
			putFn := func(key, value string) func() {
				return func() {
					do.HTTP("node", "PUT", "/kv/large:"+key, value).T().
						Status(Is(200)).
						Assert("Your server should handle concurrent PUT requests.\n" +
							"Ensure thread-safety in your storage implementation.")
				}
			}

			fns := []func(){}
			for i := 1; i <= 10_000; i++ {
				fns = append(fns, putFn(fmt.Sprintf("key%d", i), strings.Repeat("x", 100)))
			}

			do.Concurrently(fns...)

			// Crash immediately after concurrent writes
			do.Restart("node", syscall.SIGKILL)

			// Verify all acknowledged writes survived
			for i := 1; i <= 10_000; i++ {
				do.HTTP("node", "GET", fmt.Sprintf("/kv/large:key%d", i)).T().
					Status(Is(200)).
					Body(Is(strings.Repeat("x", 100))).
					Assert("Your server should preserve all acknowledged writes after crash.\n" +
						"Ensure your WAL writes are thread-safe and durably stored before acknowledging.\n" +
						"If recovery is slow, consider implementing checkpointing to reduce replay time.")
			}
		})
}
