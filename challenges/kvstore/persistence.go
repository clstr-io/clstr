package kvstore

import (
	"fmt"
	"strings"

	. "github.com/clstr-io/clstr/internal/attest"
)

func Persistence() *Suite {
	return New().
		// 0
		Setup(func(do *Do) {
			do.Start("node")
		}).

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
				do.PUT("node", fmt.Sprintf("/kv/%s", key), value).T().
					Status(Is(200)).
					Assert("Your server should accept PUT requests and store data.\n" +
						"Ensure your HTTP handler processes PUT requests correctly.")
			}

			// Verify data is accessible before restart
			for key, expectedValue := range testData {
				do.GET("node", fmt.Sprintf("/kv/%s", key)).T().
					Status(Is(200)).
					Body(Is(expectedValue)).
					Assert("Your server should return stored values before persistence test.\n" +
						"Ensure basic storage functionality works correctly.")
			}

			do.Restart("node")

			// Verify data survived the restart
			for key, expectedValue := range testData {
				do.GET("node", fmt.Sprintf("/kv/%s", key)).T().
					Status(Is(200)).
					Body(Is(expectedValue)).
					Assert("Your server should persist data across clean shutdowns.\n" +
						"Implement data persistence to disk (file-based storage, database, etc.).\n" +
						"Ensure data is written to persistent storage on PUT operations.")
			}
		}).

		// 2
		Test("Check Data Integrity After Multiple Restarts", func(do *Do) {
			// Perform multiple cycles of data operations and restarts
			for cycle := 1; cycle <= 4; cycle++ {
				// Add cycle-specific data
				cycleKey := fmt.Sprintf("cycle:restart_%d", cycle)
				cycleValue := fmt.Sprintf("restart_data_%d", cycle)

				do.PUT("node", fmt.Sprintf("/kv/%s", cycleKey), cycleValue).T().
					Status(Is(200)).
					Assert("Your server should store data for integrity test cycle.\n" +
						"Ensure PUT operations work correctly during multiple restart cycles.")

				// Restart the server
				do.Restart("node")

				// Verify cycle data persisted
				do.GET("node", fmt.Sprintf("/kv/%s", cycleKey)).T().
					Status(Is(200)).
					Body(Is(cycleValue)).
					Assert("Your server should maintain data integrity across multiple restarts.\n" +
						"Ensure persistent storage remains consistent and uncorrupted.")
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
				do.GET("node", fmt.Sprintf("/kv/%s", key)).T().
					Status(Is(200)).
					Body(Is(expectedValue)).
					Assert("Your server should preserve all historical data across restarts.\n" +
						"Ensure no data corruption or loss occurs during persistence operations.")
			}
		}).

		// 3
		Test("Test Persistence When Under Concurrent Load", func(do *Do) {
			// Generate concurrent load
			do.Concurrently(10_000, func(i int) {
				do.PUT("node", fmt.Sprintf("/kv/load:concurrent%d", i), fmt.Sprintf("value%d", i)).T().
					Status(Is(200)).
					Assert("Your server should handle concurrent PUT requests under load.\n" +
						"Ensure persistence works during high-traffic scenarios.")
			})

			// Immediately restart to test persistence under concurrent load
			do.Restart("node")

			// Verify all concurrent data was persisted
			for i := 1; i <= 10_000; i++ {
				do.GET("node", fmt.Sprintf("/kv/load:concurrent%d", i)).T().
					Status(Is(200)).
					Body(Is(fmt.Sprintf("value%d", i))).
					Assert("Your server should persist all concurrent writes.\n" +
						"Ensure thread-safe persistence and no data loss under load.")
			}
		})
}
