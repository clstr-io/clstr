package kvstore

import (
	"fmt"
	"strings"

	. "github.com/clstr-io/clstr/internal/attest"
)

func HTTPAPI() *Suite {
	return New(WithCluster(1)).

		// 1
		Test("PUT Basic Operations", func(do *Do) {
			// Set initial key-value pairs that subsequent tests can rely on
			capitals := map[string]string{
				"kenya":    "Nairobi",
				"uganda":   "Kampala",
				"tanzania": "Dar es Salaam",
			}
			for country, capital := range capitals {
				do.PUT("n1", fmt.Sprintf("/kv/%s:capital", country), capital).
					Status(Is(200)).
					Hint("Your server should accept PUT requests.\n" +
						"Ensure your HTTP handler processes PUT requests to /kv/{key}.").
					Check()
			}

			// Test overwrite behavior
			do.PUT("n1", "/kv/tanzania:capital", "Dodoma").
				Status(Is(200)).
				Hint("Your server should allow overwriting existing keys.\n" +
					"Ensure PUT requests update the value of existing keys.").
				Check()

			// Verify overwrite worked
			do.GET("n1", "/kv/tanzania:capital").
				Status(Is(200)).
				Body(Is("Dodoma")).
				Hint("Your server should return the updated value after overwrite.\n" +
					"Ensure GET requests return the most recently stored value.").
				Check()
		}).

		// 2
		Test("PUT Edge and Error Cases", func(do *Do) {
			// Empty value
			do.PUT("n1", "/kv/empty").
				Status(Is(400)).
				Body(Is("value cannot be empty\n")).
				Hint("Your server should reject empty values.\n" +
					"Add validation to return 400 Bad Request for empty values.").
				Check()

			// Empty key
			do.PUT("n1", "/kv/", "some_value").
				Status(Is(400)).
				Body(Is("key cannot be empty\n")).
				Hint("Your server should reject empty keys.\n" +
					"Add validation to return 400 Bad Request for empty keys.").
				Check()

			// Unicode handling
			do.PUT("n1", "/kv/unicode:key", "🌍 Nairobi").
				Status(Is(200)).
				Hint("Your server should handle Unicode characters in values.\n" +
					"Ensure your HTTP handler properly processes UTF-8 encoded data.").
				Check()

			// Long key and value
			longKey := "long:" + strings.Repeat("k", 100)
			longValue := strings.Repeat("v", 10_000)
			do.PUT("n1", fmt.Sprintf("/kv/%s", longKey), longValue).
				Status(Is(200)).
				Hint("Your server should handle long keys and values.\n" +
					"Ensure your server doesn't have arbitrary key & value length limits.").
				Check()

			// Special characters in key/value
			do.PUT("n1", "/kv/special:key-with_symbols.123", "value with spaces & symbols! \t").
				Status(Is(200)).
				Hint("Your server should handle special characters in keys and values.\n" +
					"Ensure proper URL path parsing and value encoding/decoding.").
				Check()

			// Verify special characters are retrieved correctly
			do.GET("n1", "/kv/special:key-with_symbols.123").
				Status(Is(200)).
				Body(Is("value with spaces & symbols! \t")).
				Hint("Your server should preserve special characters in stored values.\n" +
					"Ensure proper encoding/decoding doesn't corrupt the data.").
				Check()
		}).

		// 3
		Test("GET Basic Operations", func(do *Do) {
			// Retrieve values we know exist from PUT tests
			do.GET("n1", "/kv/kenya:capital").
				Status(Is(200)).
				Body(Is("Nairobi")).
				Hint("Your server should return stored values with GET requests.\n" +
					"Ensure your key-value storage and retrieval logic is working correctly.").
				Check()
			do.GET("n1", "/kv/uganda:capital").
				Status(Is(200)).
				Body(Is("Kampala")).
				Hint("Your server should return stored values with GET requests.\n" +
					"Ensure your key-value storage and retrieval logic is working correctly.").
				Check()
			do.GET("n1", "/kv/tanzania:capital").
				Status(Is(200)).
				Body(Is("Dodoma")).
				Hint("Your server should return the most recently stored value.\n" +
					"Ensure overwrite operations update the stored value correctly.").
				Check()

			// Verify Unicode handling
			do.GET("n1", "/kv/unicode:key").
				Status(Is(200)).
				Body(Is("🌍 Nairobi")).
				Hint("Your server should preserve Unicode characters in stored values.\n" +
					"Ensure proper UTF-8 handling in your storage and retrieval logic.").
				Check()

			// Verify long values
			longKey := "long:" + strings.Repeat("k", 100)
			longValue := strings.Repeat("v", 10_000)
			do.GET("n1", fmt.Sprintf("/kv/%s", longKey)).
				Status(Is(200)).
				Body(Is(longValue)).
				Hint("Your server should handle retrieval of long keys and values.\n" +
					"Ensure your storage doesn't truncate or corrupt large data.").
				Check()
		}).

		// 4
		Test("GET Edge and Error Cases", func(do *Do) {
			// Non-existent key
			do.GET("n1", "/kv/nonexistent:key").
				Status(Is(404)).
				Body(Is("key not found\n")).
				Hint("Your server should return 404 Not Found when a key doesn't exist.\n" +
					"Check your key lookup logic and error handling.").
				Check()

			// Case sensitivity test
			do.GET("n1", "/kv/KENYA:CAPITAL").
				Status(Is(404)).
				Body(Is("key not found\n")).
				Hint("Your server should return 404 Not Found when a key doesn't exist.\n" +
					"Check your key lookup logic and error handling.").
				Check()

			// Empty key
			do.GET("n1", "/kv/").
				Status(Is(400)).
				Body(Is("key cannot be empty\n")).
				Hint("Your server should reject empty keys.\n" +
					"Add validation to return 400 Bad Request for empty keys.").
				Check()
		}).

		// 5
		Test("DELETE Basic Operations", func(do *Do) {
			// Delete an existing key
			do.DELETE("n1", "/kv/tanzania:capital").
				Status(Is(200)).
				Hint("Your server should accept DELETE requests.\n" +
					"Ensure your HTTP handler processes DELETE requests to /kv/{key}.").
				Check()

			// Verify deletion worked
			do.GET("n1", "/kv/tanzania:capital").
				Status(Is(404)).
				Body(Is("key not found\n")).
				Hint("Your server should return 404 Not Found when a key doesn't exist.\n" +
					"Check your key lookup logic and error handling.").
				Check()

			// Verify other keys still exist
			do.GET("n1", "/kv/kenya:capital").
				Status(Is(200)).
				Body(Is("Nairobi")).
				Hint("Your server should only delete the specified key, not affect others.\n" +
					"Ensure your delete operation doesn't remove unrelated data.").
				Check()
		}).

		// 6
		Test("DELETE Edge and Error Cases", func(do *Do) {
			// Delete non-existent key
			do.DELETE("n1", "/kv/nonexistent:key").
				Status(Is(200)).
				Hint("Your server should gracefully handle deletion of non-existent keys.\n" +
					"Returning 200 OK for missing keys is acceptable (idempotent).").
				Check()

			// Delete same key twice
			do.PUT("n1", "/kv/delete:twice", "value").
				Status(Is(200)).
				Hint("Your server should accept PUT requests.\n" +
					"Ensure your HTTP handler processes PUT requests to /kv/{key}.").
				Check()
			do.DELETE("n1", "/kv/delete:twice").
				Status(Is(200)).
				Hint("Your server should successfully delete existing keys.\n" +
					"Implement proper key removal in your storage logic.").
				Check()
			do.DELETE("n1", "/kv/delete:twice").
				Status(Is(200)).
				Hint("Your server should handle repeated deletions gracefully.\n" +
					"Deleting the same key twice should be idempotent (return 200 OK).").
				Check()

			// Empty key
			do.DELETE("n1", "/kv/").
				Status(Is(400)).
				Body(Is("key cannot be empty\n")).
				Hint("Your server should reject empty keys.\n" +
					"Add validation to return 400 Bad Request for empty keys.").
				Check()
		}).

		// 7
		Test("CLEAR Operations", func(do *Do) {
			// Add some keys to clear
			testKeys := map[string]string{
				"clear:test1": "value1",
				"clear:test2": "value2",
				"clear:test3": "value3",
			}
			for key, value := range testKeys {
				do.PUT("n1", fmt.Sprintf("/kv/%s", key), value).
					Status(Is(200)).
					Hint("Your server should accept PUT requests.\n" +
						"Ensure your HTTP handler processes PUT requests to /kv/{key}.").
					Check()
			}

			// Verify keys exist
			for key, value := range testKeys {
				do.GET("n1", fmt.Sprintf("/kv/%s", key)).
					Status(Is(200)).
					Body(Is(value)).
					Hint("Your server should store and retrieve key-value pairs correctly.\n" +
						"Check your storage logic.").
					Check()
			}

			// Clear all keys
			do.DELETE("n1", "/clear").
				Status(Is(200)).
				Hint("Your server should implement a /clear endpoint.\n" +
					"Add a DELETE /clear method that deletes all key-value pairs.").
				Check()

			// Verify all keys are gone
			for key := range testKeys {
				do.GET("n1", fmt.Sprintf("/kv/%s", key)).
					Status(Is(404)).
					Body(Is("key not found\n")).
					Hint("Your server should delete all keys when /clear is called.\n" +
						"Ensure the /clear endpoint removes all stored key-value pairs.").
					Check()
			}

			// Verify old keys from previous tests are also gone
			do.GET("n1", "/kv/kenya:capital").
				Status(Is(404)).
				Body(Is("key not found\n")).
				Hint("Your server should delete ALL keys when /clear is called.\n" +
					"Ensure the /clear endpoint removes every key-value pair, not just recent ones.").
				Check()

			// Test that clear on empty store works
			do.DELETE("n1", "/clear").
				Status(Is(200)).
				Hint("Your server should handle clearing an empty store gracefully.\n" +
					"Calling /clear on an empty store should return 200 OK.").
				Check()
		}).

		// 8
		Test("Concurrent Operations - Different Keys", func(do *Do) {
			// Test concurrent writes to different keys
			do.Concurrently(100, func(i int) {
				do.PUT("n1", fmt.Sprintf("/kv/concurrent:key%d", i), fmt.Sprintf("value%d", i)).
					Status(Is(200)).
					Hint("Your server should handle concurrent PUT requests.\n" +
						"Ensure thread-safety in your storage implementation.").
					Check()
			})

			// Verify all concurrent writes succeeded
			for i := 1; i <= 100; i++ {
				do.GET("n1", fmt.Sprintf("/kv/concurrent:key%d", i)).
					Status(Is(200)).
					Body(Is(fmt.Sprintf("value%d", i))).
					Hint("Your server should store all concurrent writes.\n" +
						"Ensure no data corruption or loss occurs during concurrent operations.").
					Check()
			}
		}).

		// 9
		Test("Concurrent Operations - Same Key", func(do *Do) {
			do.Concurrently(100, func(i int) {
				do.PUT("n1", "/kv/concurrent:racekey", fmt.Sprintf("value%d", i)).
					Status(Is(200)).
					Hint("Your server should handle concurrent PUT requests.\n" +
						"Ensure thread-safety in your storage implementation.").
					Check()
			})

			// Verify the key exists with one of the values (doesn't matter which)
			expectedValues := make([]string, 100)
			for i := range expectedValues {
				expectedValues[i] = fmt.Sprintf("value%d", i+1)
			}
			do.GET("n1", "/kv/concurrent:racekey").
				Status(Is(200)).
				Body(OneOf(expectedValues...)).
				Hint("Your server should handle concurrent writes to the same key.\n" +
					"Ensure thread-safety prevents crashes or data corruption.\n" +
					"The value should be one of the concurrently written values (value1-value100).").
				Check()
		}).

		// 10
		Test("Check Allowed HTTP Methods", func(do *Do) {
			// POST & PATCH /kv/{key} not allowed
			for _, plan := range []*Assertion{
				do.POST("n1", "/kv/test:key"),
				do.PATCH("n1", "/kv/test:key"),
			} {
				plan.
					Status(Is(405)).
					Body(Is("method not allowed\n")).
					Hint("Your server should reject unsupported HTTP methods on /kv/{key}.\n" +
						"Add logic to return 405 Method Not Allowed for unsupported methods.").
					Check()
			}

			// GET, POST, PUT, PATCH /clear not allowed
			for _, plan := range []*Assertion{
				do.GET("n1", "/clear"),
				do.POST("n1", "/clear"),
				do.PUT("n1", "/clear"),
				do.PATCH("n1", "/clear"),
			} {
				plan.
					Status(Is(405)).
					Body(Is("method not allowed\n")).
					Hint("Your server should reject unsupported HTTP methods on /clear.\n" +
						"Only DELETE /clear should be allowed. Return 405 Method Not Allowed for other methods.").
					Check()
			}
		})
}
