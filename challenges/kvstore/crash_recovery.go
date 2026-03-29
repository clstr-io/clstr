package kvstore

import (
	"fmt"
	"strings"
	"syscall"

	. "github.com/clstr-io/clstr/internal/attest"
)

func CrashRecovery() *Suite {
	return New(WithCluster(1)).

		// 1
		Test("Basic WAL Durability", func(do *Do) {
			// Test various operations that should all be logged
			do.PUT("n1", "/kv/wal:basic", "initial").
				Status(Is(200)).
				Hint("Your server should accept PUT requests.\n" +
					"Ensure your HTTP handler processes PUT requests correctly.").
				Check()

			do.PUT("n1", "/kv/wal:updated", "v1").
				Status(Is(200)).
				Hint("Your server should accept PUT requests.\n" +
					"Ensure your HTTP handler processes PUT requests correctly.").
				Check()

			do.PUT("n1", "/kv/wal:updated", "v2").
				Status(Is(200)).
				Hint("Your server should allow overwriting existing keys.\n" +
					"Ensure PUT requests update the value of existing keys.").
				Check()

			do.PUT("n1", "/kv/wal:deleted", "temporary").
				Status(Is(200)).
				Hint("Your server should accept PUT requests.\n" +
					"Ensure your HTTP handler processes PUT requests correctly.").
				Check()

			do.DELETE("n1", "/kv/wal:deleted").
				Status(Is(200)).
				Hint("Your server should accept DELETE requests.\n" +
					"Ensure your HTTP handler processes DELETE requests correctly.").
				Check()

			// Crash without warning
			do.Restart("n1", syscall.SIGKILL)

			// Verify correct final state after recovery
			do.GET("n1", "/kv/wal:basic").
				Status(Is(200)).
				Body(Is("initial")).
				Hint("Your server acknowledged the PUT but lost the data after crashing.\n" +
					"Implement a Write-Ahead Log (WAL) that records operations before applying them to memory.\n" +
					"Ensure writes are durably stored (fsync/flush) before or when acknowledging to the client.").
				Check()

			do.GET("n1", "/kv/wal:updated").
				Status(Is(200)).
				Body(Is("v2")).
				Hint("Your server should preserve updated values after crash.\n" +
					"Ensure your WAL records all PUT operations, including updates to existing keys.").
				Check()

			do.GET("n1", "/kv/wal:deleted").
				Status(Is(404)).
				Hint("Your server should preserve deletion state after crash.\n" +
					"Ensure your WAL records DELETE operations and replays them correctly during recovery.").
				Check()
		}).

		// 2
		Test("Multiple Crash Recovery Cycles", func(do *Do) {
			// Simulate multiple crash/restart cycles
			for cycle := 1; cycle <= 4; cycle++ {
				// Add cycle-specific data
				cycleKey := fmt.Sprintf("cycle:crash_%d", cycle)
				cycleValue := fmt.Sprintf("crash_data_%d", cycle)

				do.PUT("n1", fmt.Sprintf("/kv/%s", cycleKey), cycleValue).
					Status(Is(200)).
					Hint("Your server should accept PUT requests.\n" +
						"Ensure your HTTP handler processes PUT requests correctly.").
					Check()

				// Crash without warning
				do.Restart("n1", syscall.SIGKILL)

				// Verify cycle data survived
				do.GET("n1", fmt.Sprintf("/kv/%s", cycleKey)).
					Status(Is(200)).
					Body(Is(cycleValue)).
					Hint("Your server should preserve data across crash/restart cycles.\n" +
						"Ensure your WAL is append-only and recovery replays all operations correctly.").
					Check()
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
				do.GET("n1", fmt.Sprintf("/kv/%s", key)).
					Status(Is(200)).
					Body(Is(expectedValue)).
					Hint("Your server should preserve all historical data across multiple crashes.\n" +
						"Ensure the WAL is never truncated until after a successful checkpoint.\n" +
						"Recovery should load the latest snapshot (if any) and replay all subsequent WAL operations.").
					Check()
			}
		}).

		// 3
		Test("Rapid Write Burst Before Crash", func(do *Do) {
			// Write many operations rapidly in sequence
			for i := 1; i <= 500; i++ {
				do.PUT("n1", fmt.Sprintf("/kv/burst:%d", i), strings.Repeat("data", 250)).
					Status(Is(200)).
					Hint("Your server should accept PUT requests.\n" +
						"Ensure your HTTP handler processes PUT requests correctly.").
					Check()
			}

			// Crash immediately
			do.Restart("n1", syscall.SIGKILL)

			// Verify all acknowledged writes survived
			for i := 1; i <= 500; i++ {
				do.GET("n1", fmt.Sprintf("/kv/burst:%d", i)).
					Status(Is(200)).
					Body(Is(strings.Repeat("data", 250))).
					Hint("Your server acknowledged the PUT but lost the data after crashing.\n" +
						"Ensure writes are durably stored before acknowledging them to the client.\n" +
						"Call fsync/flush after writing to WAL, or batch operations and sync before responding.").
					Check()
			}
		}).

		// 4
		Test("Test Recovery When Under Concurrent Load", func(do *Do) {
			// Generate concurrent load
			do.Concurrently(10_000, func(i int) {
				do.PUT("n1", fmt.Sprintf("/kv/large:key%d", i), strings.Repeat("x", 100)).
					Status(Is(200)).
					Hint("Your server should handle concurrent PUT requests.\n" +
						"Ensure thread-safety in your storage implementation.").
					Check()
			})

			// Crash immediately
			do.Restart("n1", syscall.SIGKILL)

			// Verify all acknowledged writes survived
			for i := 1; i <= 10_000; i++ {
				do.GET("n1", fmt.Sprintf("/kv/large:key%d", i)).
					Status(Is(200)).
					Body(Is(strings.Repeat("x", 100))).
					Hint("Your server should preserve all acknowledged writes after crash.\n" +
						"Ensure your WAL writes are thread-safe and durably stored before acknowledging.\n" +
						"If recovery is slow, consider implementing checkpointing to reduce replay time.").
					Check()
			}
		})
}
