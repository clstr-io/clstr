package kvstore

import (
	"fmt"
	"strings"
	"time"

	. "github.com/clstr-io/clstr/internal/attest"
)

func Persistence() *Suite {
	return New(
		WithCluster(1),
		WithRequestTimeout(time.Second),
	).

		// 1
		Test("Data Survives a Graceful Restart", func(do *Do) {
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

			do.Restart("n1")

			do.GET(Node("n1"), "/kv/wal:basic").
				Status(Is(200)).
				Body(Is("initial")).
				Hint("Your server lost data after a graceful restart.\n" +
					"Implement data persistence to disk (file-based storage, database, etc.).\n" +
					"Ensure data is written to persistent storage before acknowledging the client.").
				Run()

			do.GET(Node("n1"), "/kv/wal:updated").
				Status(Is(200)).
				Body(Is("v2")).
				Hint("Your server should persist the most recent value after restart.\n" +
					"Ensure overwrite operations are persisted correctly.").
				Run()

			do.GET(Node("n1"), "/kv/wal:deleted").
				Status(Is(404)).
				Hint("Your server should persist deletion state across restarts.\n" +
					"Ensure DELETE operations are persisted, not just PUT operations.").
				Run()
		}).

		// 2
		Test("All Data Survives Repeated Graceful Restarts", func(do *Do) {
			for cycle := 1; cycle <= 4; cycle++ {
				cycleKey := fmt.Sprintf("cycle:restart_%d", cycle)
				cycleValue := fmt.Sprintf("restart_data_%d", cycle)

				do.PUT(Node("n1"), fmt.Sprintf("/kv/%s", cycleKey), cycleValue).
					Status(Is(200)).
					Hint("Your server should accept PUT requests.\n" +
						"Ensure your HTTP handler processes PUT requests correctly.").
					Run()

				do.Restart("n1")

				do.GET(Node("n1"), fmt.Sprintf("/kv/%s", cycleKey)).
					Status(Is(200)).
					Body(Is(cycleValue)).
					Hint("Your server should preserve data across restart cycles.\n" +
						"Ensure your persistence layer correctly stores and loads data on restart.").
					Run()
			}

			allHistoricalData := map[string]string{
				"wal:basic":       "initial",
				"wal:updated":     "v2",
				"cycle:restart_1": "restart_data_1",
				"cycle:restart_2": "restart_data_2",
				"cycle:restart_3": "restart_data_3",
				"cycle:restart_4": "restart_data_4",
			}
			for key, expectedValue := range allHistoricalData {
				do.GET(Node("n1"), fmt.Sprintf("/kv/%s", key)).
					Status(Is(200)).
					Body(Is(expectedValue)).
					Hint("Your server should preserve all historical data across restarts.\n" +
						"Ensure no data corruption or loss occurs during persistence operations.").
					Run()
			}
		}).

		// 3
		Test("Rapid Sequential Writes Survive a Graceful Restart", func(do *Do) {
			for i := 1; i <= 500; i++ {
				do.PUT(Node("n1"), fmt.Sprintf("/kv/burst:%d", i), strings.Repeat("data", 250)).
					Status(Is(200)).
					Hint("Your server should accept PUT requests.\n" +
						"Ensure your HTTP handler processes PUT requests correctly.").
					Run()
			}

			do.Restart("n1")

			for i := 1; i <= 500; i++ {
				do.GET(Node("n1"), fmt.Sprintf("/kv/burst:%d", i)).
					Status(Is(200)).
					Body(Is(strings.Repeat("data", 250))).
					Hint("Your server lost data during a burst of writes followed by a restart.\n" +
						"Ensure data is durably written to disk before acknowledging the client.").
					Run()
			}
		}).

		// 4
		Test("Rapid Concurrent Writes Survive a Graceful Restart", func(do *Do) {
			do.Concurrently(1_000, func(i int) {
				do.PUT(Node("n1"), fmt.Sprintf("/kv/large:key%d", i), strings.Repeat("x", 100)).
					Status(Is(200)).
					Hint("Your server should handle concurrent PUT requests.\n" +
						"Ensure thread-safety in your storage implementation.").
					Run()
			})

			do.Restart("n1")

			for i := 1; i <= 1_000; i++ {
				do.GET(Node("n1"), fmt.Sprintf("/kv/large:key%d", i)).
					Status(Is(200)).
					Body(Is(strings.Repeat("x", 100))).
					Hint("Your server should persist all concurrent writes.\n" +
						"Ensure thread-safe persistence and no data loss under load.").
					Run()
			}
		}).

		// 5
		Test("CLEAR Survives a Graceful Restart", func(do *Do) {
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

			do.Restart("n1")

			for key := range clearKeys {
				do.GET(Node("n1"), fmt.Sprintf("/kv/%s", key)).
					Status(Is(404)).
					Hint("Your server should persist the cleared state across restarts.\n" +
						"Ensure your persistence layer records CLEAR operations or persists the empty state on shutdown.").
					Run()
			}
		})
}
