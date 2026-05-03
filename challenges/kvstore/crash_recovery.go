package kvstore

import (
	"fmt"
	"strings"
	"syscall"
	"time"

	. "github.com/clstr-io/clstr/internal/attest"
)

func CrashRecovery() *Suite {
	return New(
		WithCluster(1),
		WithRequestTimeout(time.Second),
		WithConcurrencyLimit(50),
	).

		// 1
		Test("Data Survives a Hard Crash", func(do *Do) {
			do.PUT(Node("n1"), "/kv/wal:basic", "initial").
				Status(Is(200)).
				Hint("Your server should accept PUT requests.\n" +
					"Ensure your HTTP handler processes PUT requests correctly.").
				Run()

			do.PUT(Node("n1"), "/kv/wal:updated", "v1").
				Status(Is(200)).
				Hint("Your server should accept PUT requests.\n" +
					"Ensure your HTTP handler processes PUT requests correctly.").
				Run()

			do.PUT(Node("n1"), "/kv/wal:updated", "v2").
				Status(Is(200)).
				Hint("Your server should allow overwriting existing keys.\n" +
					"Ensure PUT requests update the value of existing keys.").
				Run()

			do.PUT(Node("n1"), "/kv/wal:deleted", "temporary").
				Status(Is(200)).
				Hint("Your server should accept PUT requests.\n" +
					"Ensure your HTTP handler processes PUT requests correctly.").
				Run()

			do.DELETE(Node("n1"), "/kv/wal:deleted").
				Status(Is(200)).
				Hint("Your server should accept DELETE requests.\n" +
					"Ensure your HTTP handler processes DELETE requests correctly.").
				Run()

			do.Restart("n1", syscall.SIGKILL)

			do.GET(Node("n1"), "/kv/wal:basic").
				Status(Is(200)).
				Body(Is("initial")).
				Hint("Your server acknowledged the PUT but lost the data after crashing.\n" +
					"Implement a Write-Ahead Log (WAL) that records operations before applying them to memory.\n" +
					"Ensure writes are durably stored (fsync/flush) before or when acknowledging to the client.").
				Run()

			do.GET(Node("n1"), "/kv/wal:updated").
				Status(Is(200)).
				Body(Is("v2")).
				Hint("Your server should preserve updated values after crash.\n" +
					"Ensure your WAL records all PUT operations, including updates to existing keys.").
				Run()

			do.GET(Node("n1"), "/kv/wal:deleted").
				Status(Is(404)).
				Hint("Your server should preserve deletion state after crash.\n" +
					"Ensure your WAL records DELETE operations and replays them correctly during recovery.").
				Run()
		}).

		// 2
		Test("All Data Survives Repeated Hard Crashes", func(do *Do) {
			for cycle := 1; cycle <= 4; cycle++ {
				cycleKey := fmt.Sprintf("cycle:crash_%d", cycle)
				cycleValue := fmt.Sprintf("crash_data_%d", cycle)

				do.PUT(Node("n1"), fmt.Sprintf("/kv/%s", cycleKey), cycleValue).
					Status(Is(200)).
					Hint("Your server should accept PUT requests.\n" +
						"Ensure your HTTP handler processes PUT requests correctly.").
					Run()

				do.Restart("n1", syscall.SIGKILL)

				do.GET(Node("n1"), fmt.Sprintf("/kv/%s", cycleKey)).
					Status(Is(200)).
					Body(Is(cycleValue)).
					Hint("Your server should preserve data across crash/restart cycles.\n" +
						"Ensure your WAL is append-only and recovery replays all operations correctly.").
					Run()
			}

			allHistoricalData := map[string]string{
				"wal:basic":     "initial",
				"wal:updated":   "v2",
				"cycle:crash_1": "crash_data_1",
				"cycle:crash_2": "crash_data_2",
				"cycle:crash_3": "crash_data_3",
				"cycle:crash_4": "crash_data_4",
			}
			for key, expectedValue := range allHistoricalData {
				do.GET(Node("n1"), fmt.Sprintf("/kv/%s", key)).
					Status(Is(200)).
					Body(Is(expectedValue)).
					Hint("Your server should preserve all historical data across multiple crashes.\n" +
						"Ensure the WAL is never truncated until after a successful checkpoint.\n" +
						"Recovery should load the latest snapshot (if any) and replay all subsequent WAL operations.").
					Run()
			}
		}).

		// 3
		Test("Rapid Sequential Writes Survive a Hard Crash", func(do *Do) {
			for i := 1; i <= 500; i++ {
				do.PUT(Node("n1"), fmt.Sprintf("/kv/burst:%d", i), strings.Repeat("data", 250)).
					Status(Is(200)).
					Hint("Your server should accept PUT requests.\n" +
						"Ensure your HTTP handler processes PUT requests correctly.").
					Run()
			}

			do.Restart("n1", syscall.SIGKILL)

			for i := 1; i <= 500; i++ {
				do.GET(Node("n1"), fmt.Sprintf("/kv/burst:%d", i)).
					Status(Is(200)).
					Body(Is(strings.Repeat("data", 250))).
					Hint("Your server acknowledged the PUT but lost the data after crashing.\n" +
						"Ensure writes are durably stored before acknowledging them to the client.\n" +
						"Call fsync/flush after writing to WAL, or batch operations and sync before responding.").
					Run()
			}
		}).

		// 4
		Test("Rapid Concurrent Writes Survive a Hard Crash", func(do *Do) {
			do.Concurrently(1_000, func(i int) {
				do.PUT(Node("n1"), fmt.Sprintf("/kv/large:key%d", i), strings.Repeat("x", 100)).
					Status(Is(200)).
					Hint("Your server should handle concurrent PUT requests.\n" +
						"Ensure thread-safety in your storage implementation.").
					Run()
			})

			do.Restart("n1", syscall.SIGKILL)

			for i := 1; i <= 1_000; i++ {
				do.GET(Node("n1"), fmt.Sprintf("/kv/large:key%d", i)).
					Status(Is(200)).
					Body(Is(strings.Repeat("x", 100))).
					Hint("Your server should preserve all acknowledged writes after crash.\n" +
						"Ensure your WAL writes are thread-safe and durably stored before acknowledging.\n" +
						"If recovery is slow, consider implementing checkpointing to reduce replay time.").
					Run()
			}
		}).

		// 5
		Test("CLEAR Survives a Hard Crash", func(do *Do) {
			clearKeys := map[string]string{
				"clear:test1": "value1",
				"clear:test2": "value2",
				"clear:test3": "value3",
			}
			for key, value := range clearKeys {
				do.PUT(Node("n1"), fmt.Sprintf("/kv/%s", key), value).
					Status(Is(200)).
					Hint("Your server should accept PUT requests.\n" +
						"Ensure your HTTP handler processes PUT requests correctly.").
					Run()
			}

			do.DELETE(Node("n1"), "/clear").
				Status(Is(200)).
				Hint("Your server should implement a /clear endpoint.\n" +
					"Add a DELETE /clear method that deletes all key-value pairs.").
				Run()

			do.Restart("n1", syscall.SIGKILL)

			for key := range clearKeys {
				do.GET(Node("n1"), fmt.Sprintf("/kv/%s", key)).
					Status(Is(404)).
					Hint("Your server should preserve the cleared state after a hard crash.\n" +
						"Ensure your WAL records CLEAR operations and replays them correctly during recovery.").
					Run()
			}
		})
}
