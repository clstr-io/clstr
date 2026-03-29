package kvstore

import (
	"fmt"
	"strings"

	. "github.com/clstr-io/clstr/internal/attest"
)

func Persistence() *Suite {
	return New(WithCluster(1)).

		// 1
		Test("Verify Data Survives Graceful Restart", func(do *Do) {
			// Store initial data
			testData := map[string]string{
				"persistent:key1": "value1",
				"persistent:key2": "value with spaces",
				"persistent:key3": "🌍 unicode value",
				"persistent:key4": strings.Repeat("long_value_", 50),
			}
			for key, value := range testData {
				do.PUT("n1", fmt.Sprintf("/kv/%s", key), value).
					Status(Is(200)).
					Hint("Your server should accept PUT requests and store data.\n" +
						"Ensure your HTTP handler processes PUT requests correctly.").
					Check()
			}

			// Verify data is accessible before restart
			for key, expectedValue := range testData {
				do.GET("n1", fmt.Sprintf("/kv/%s", key)).
					Status(Is(200)).
					Body(Is(expectedValue)).
					Hint("Your server should return stored values before persistence test.\n" +
						"Ensure basic storage functionality works correctly.").
					Check()
			}

			do.Restart("n1")

			// Verify data survived the restart
			for key, expectedValue := range testData {
				do.GET("n1", fmt.Sprintf("/kv/%s", key)).
					Status(Is(200)).
					Body(Is(expectedValue)).
					Hint("Your server should persist data across clean shutdowns.\n" +
						"Implement data persistence to disk (file-based storage, database, etc.).\n" +
						"Ensure data is written to persistent storage on PUT operations.").
					Check()
			}
		}).

		// 2
		Test("Check Data Integrity After Multiple Restarts", func(do *Do) {
			// Perform multiple cycles of data operations and restarts
			for cycle := 1; cycle <= 4; cycle++ {
				// Add cycle-specific data
				cycleKey := fmt.Sprintf("cycle:restart_%d", cycle)
				cycleValue := fmt.Sprintf("restart_data_%d", cycle)

				do.PUT("n1", fmt.Sprintf("/kv/%s", cycleKey), cycleValue).
					Status(Is(200)).
					Hint("Your server should store data for integrity test cycle.\n" +
						"Ensure PUT operations work correctly during multiple restart cycles.").
					Check()

				// Restart the server
				do.Restart("n1")

				// Verify cycle data persisted
				do.GET("n1", fmt.Sprintf("/kv/%s", cycleKey)).
					Status(Is(200)).
					Body(Is(cycleValue)).
					Hint("Your server should maintain data integrity across multiple restarts.\n" +
						"Ensure persistent storage remains consistent and uncorrupted.").
					Check()
			}

			// Verify all historical data still exists
			allHistoricalData := map[string]string{
				"persistent:key1": "value1",
				"persistent:key2": "value with spaces",
				"persistent:key3": "🌍 unicode value",
				"persistent:key4": strings.Repeat("long_value_", 50),
				"cycle:restart_1": "restart_data_1",
				"cycle:restart_2": "restart_data_2",
				"cycle:restart_3": "restart_data_3",
				"cycle:restart_4": "restart_data_4",
			}
			for key, expectedValue := range allHistoricalData {
				do.GET("n1", fmt.Sprintf("/kv/%s", key)).
					Status(Is(200)).
					Body(Is(expectedValue)).
					Hint("Your server should preserve all historical data across restarts.\n" +
						"Ensure no data corruption or loss occurs during persistence operations.").
					Check()
			}
		}).

		// 3
		Test("Test Persistence When Under Concurrent Load", func(do *Do) {
			// Generate concurrent load
			do.Concurrently(10_000, func(i int) {
				do.PUT("n1", fmt.Sprintf("/kv/load:concurrent%d", i), fmt.Sprintf("value%d", i)).
					Status(Is(200)).
					Hint("Your server should handle concurrent PUT requests under load.\n" +
						"Ensure persistence works during high-traffic scenarios.").
					Check()
			})

			// Immediately restart to test persistence under concurrent load
			do.Restart("n1")

			// Verify all concurrent data was persisted
			for i := 1; i <= 10_000; i++ {
				do.GET("n1", fmt.Sprintf("/kv/load:concurrent%d", i)).
					Status(Is(200)).
					Body(Is(fmt.Sprintf("value%d", i))).
					Hint("Your server should persist all concurrent writes.\n" +
						"Ensure thread-safe persistence and no data loss under load.").
					Check()
			}
		})
}
