package kvstore

import (
	"fmt"
	"strings"

	. "github.com/clstr-io/clstr/internal/attest"
)

func HTTPAPI() *Suite {
	return New(
		WithCluster(1),
	).

		// 1
		Test("PUT Stores Values", func(do *Do) {
			capitals := map[string]string{
				"kenya":    "Nairobi",
				"uganda":   "Kampala",
				"tanzania": "Dar es Salaam",
			}
			for country, capital := range capitals {
				do.PUT(Node("n1"), fmt.Sprintf("/kv/%s:capital", country), capital).
					Status(Is(200)).
					Hint("Your server should accept PUT requests.\n" +
						"Ensure your HTTP handler processes PUT requests to /kv/{key}.").
					Run()
			}

			do.PUT(Node("n1"), "/kv/tanzania:capital", "Dodoma").
				Status(Is(200)).
				Hint("Your server should allow overwriting existing keys.\n" +
					"Ensure PUT requests update the value of existing keys.").
				Run()

			do.GET(Node("n1"), "/kv/tanzania:capital").
				Status(Is(200)).
				Body(Is("Dodoma")).
				Hint("Your server should return the updated value after overwrite.\n" +
					"Ensure GET requests return the most recently stored value.").
				Run()

			do.PUT(Node("n1"), "/kv/unicode:key", "🌍 Nairobi").
				Status(Is(200)).
				Hint("Your server should handle Unicode characters in values.\n" +
					"Ensure your HTTP handler properly processes UTF-8 encoded data.").
				Run()

			longKey := "long:" + strings.Repeat("k", 100)
			longValue := strings.Repeat("v", 10_000)
			do.PUT(Node("n1"), fmt.Sprintf("/kv/%s", longKey), longValue).
				Status(Is(200)).
				Hint("Your server should handle long keys and values.\n" +
					"Ensure your server doesn't have arbitrary key & value length limits.").
				Run()

			do.PUT(Node("n1"), "/kv/special:key-with_symbols.123", "value with spaces & symbols! \t").
				Status(Is(200)).
				Hint("Your server should handle special characters in keys and values.\n" +
					"Ensure proper URL path parsing and value encoding/decoding.").
				Run()

			do.GET(Node("n1"), "/kv/special:key-with_symbols.123").
				Status(Is(200)).
				Body(Is("value with spaces & symbols! \t")).
				Hint("Your server should preserve special characters in stored values.\n" +
					"Ensure proper encoding/decoding doesn't corrupt the data.").
				Run()
		}).

		// 2
		Test("PUT Rejects Empty Keys and Values", func(do *Do) {
			do.PUT(Node("n1"), "/kv/empty").
				Status(Is(400)).
				Body(Matches("^value cannot be empty\n?$")).
				Hint("Your server should reject empty values.\n" +
					"Add validation to return 400 Bad Request for empty values.").
				Run()

			do.PUT(Node("n1"), "/kv/", "some_value").
				Status(Is(400)).
				Body(Matches("^key cannot be empty\n?$")).
				Hint("Your server should reject empty keys.\n" +
					"Add validation to return 400 Bad Request for empty keys.").
				Run()
		}).

		// 3
		Test("GET Returns Stored Values", func(do *Do) {
			do.GET(Node("n1"), "/kv/kenya:capital").
				Status(Is(200)).
				Body(Is("Nairobi")).
				Hint("Your server should return stored values with GET requests.\n" +
					"Ensure your key-value storage and retrieval logic is working correctly.").
				Run()

			do.GET(Node("n1"), "/kv/uganda:capital").
				Status(Is(200)).
				Body(Is("Kampala")).
				Hint("Your server should return stored values with GET requests.\n" +
					"Ensure your key-value storage and retrieval logic is working correctly.").
				Run()

			do.GET(Node("n1"), "/kv/tanzania:capital").
				Status(Is(200)).
				Body(Is("Dodoma")).
				Hint("Your server should return the most recently stored value.\n" +
					"Ensure overwrite operations update the stored value correctly.").
				Run()

			do.GET(Node("n1"), "/kv/unicode:key").
				Status(Is(200)).
				Body(Is("🌍 Nairobi")).
				Hint("Your server should preserve Unicode characters in stored values.\n" +
					"Ensure proper UTF-8 handling in your storage and retrieval logic.").
				Run()

			longKey := "long:" + strings.Repeat("k", 100)
			longValue := strings.Repeat("v", 10_000)
			do.GET(Node("n1"), fmt.Sprintf("/kv/%s", longKey)).
				Status(Is(200)).
				Body(Is(longValue)).
				Hint("Your server should handle retrieval of long keys and values.\n" +
					"Ensure your storage doesn't truncate or corrupt large data.").
				Run()
		}).

		// 4
		Test("GET Rejects Missing and Invalid Keys", func(do *Do) {
			do.GET(Node("n1"), "/kv/nonexistent:key").
				Status(Is(404)).
				Body(Matches("^key not found\n?$")).
				Hint("Your server should return 404 Not Found when a key doesn't exist.\n" +
					"Check your key lookup logic and error handling.").
				Run()

			do.GET(Node("n1"), "/kv/KENYA:CAPITAL").
				Status(Is(404)).
				Body(Matches("^key not found\n?$")).
				Hint("Your server should return 404 Not Found when a key doesn't exist.\n" +
					"Check your key lookup logic and error handling.").
				Run()

			do.GET(Node("n1"), "/kv/").
				Status(Is(400)).
				Body(Matches("^key cannot be empty\n?$")).
				Hint("Your server should reject empty keys.\n" +
					"Add validation to return 400 Bad Request for empty keys.").
				Run()
		}).

		// 5
		Test("DELETE Idempotently Removes Keys", func(do *Do) {
			do.DELETE(Node("n1"), "/kv/tanzania:capital").
				Status(Is(200)).
				Hint("Your server should accept DELETE requests.\n" +
					"Ensure your HTTP handler processes DELETE requests to /kv/{key}.").
				Run()

			do.GET(Node("n1"), "/kv/tanzania:capital").
				Status(Is(404)).
				Body(Matches("^key not found\n?$")).
				Hint("Your server should return 404 Not Found when a key doesn't exist.\n" +
					"Check your key lookup logic and error handling.").
				Run()

			do.GET(Node("n1"), "/kv/kenya:capital").
				Status(Is(200)).
				Body(Is("Nairobi")).
				Hint("Your server should only delete the specified key, not affect others.\n" +
					"Ensure your delete operation doesn't remove unrelated data.").
				Run()

			do.DELETE(Node("n1"), "/kv/nonexistent:key").
				Status(Is(200)).
				Hint("Your server should gracefully handle deletion of non-existent keys.\n" +
					"Returning 200 OK for missing keys is acceptable (idempotent).").
				Run()

			do.PUT(Node("n1"), "/kv/delete:twice", "value").
				Status(Is(200)).
				Hint("Your server should accept PUT requests.\n" +
					"Ensure your HTTP handler processes PUT requests to /kv/{key}.").
				Run()

			do.DELETE(Node("n1"), "/kv/delete:twice").
				Status(Is(200)).
				Hint("Your server should successfully delete existing keys.\n" +
					"Implement proper key removal in your storage logic.").
				Run()

			do.DELETE(Node("n1"), "/kv/delete:twice").
				Status(Is(200)).
				Hint("Your server should handle repeated deletions gracefully.\n" +
					"Deleting the same key twice should be idempotent (return 200 OK).").
				Run()

			do.PUT(Node("n1"), "/kv/delete:twice", "reinserted").
				Status(Is(200)).
				Hint("Your server should allow re-inserting a previously deleted key.\n" +
					"Ensure your storage doesn't permanently mark keys as deleted.").
				Run()

			do.GET(Node("n1"), "/kv/delete:twice").
				Status(Is(200)).
				Body(Is("reinserted")).
				Hint("Your server should return the new value after re-inserting a deleted key.\n" +
					"Ensure PUT after DELETE works correctly.").
				Run()
		}).

		// 6
		Test("DELETE Rejects Empty Keys", func(do *Do) {
			do.DELETE(Node("n1"), "/kv/").
				Status(Is(400)).
				Body(Matches("^key cannot be empty\n?$")).
				Hint("Your server should reject empty keys.\n" +
					"Add validation to return 400 Bad Request for empty keys.").
				Run()
		}).

		// 7
		Test("CLEAR Removes All Keys from the Store", func(do *Do) {
			testKeys := map[string]string{
				"clear:test1": "value1",
				"clear:test2": "value2",
				"clear:test3": "value3",
			}
			for key, value := range testKeys {
				do.PUT(Node("n1"), fmt.Sprintf("/kv/%s", key), value).
					Status(Is(200)).
					Hint("Your server should accept PUT requests.\n" +
						"Ensure your HTTP handler processes PUT requests to /kv/{key}.").
					Run()
			}

			for key, value := range testKeys {
				do.GET(Node("n1"), fmt.Sprintf("/kv/%s", key)).
					Status(Is(200)).
					Body(Is(value)).
					Hint("Your server should store and retrieve key-value pairs correctly.\n" +
						"Check your storage logic.").
					Run()
			}

			do.DELETE(Node("n1"), "/clear").
				Status(Is(200)).
				Hint("Your server should implement a /clear endpoint.\n" +
					"Add a DELETE /clear method that deletes all key-value pairs.").
				Run()

			for key := range testKeys {
				do.GET(Node("n1"), fmt.Sprintf("/kv/%s", key)).
					Status(Is(404)).
					Body(Matches("^key not found\n?$")).
					Hint("Your server should delete all keys when /clear is called.\n" +
						"Ensure the /clear endpoint removes all stored key-value pairs.").
					Run()
			}

			do.GET(Node("n1"), "/kv/kenya:capital").
				Status(Is(404)).
				Body(Matches("^key not found\n?$")).
				Hint("Your server should delete ALL keys when /clear is called.\n" +
					"Ensure the /clear endpoint removes every key-value pair, not just recent ones.").
				Run()

			do.DELETE(Node("n1"), "/clear").
				Status(Is(200)).
				Hint("Your server should handle clearing an empty store gracefully.\n" +
					"Calling /clear on an empty store should return 200 OK.").
				Run()
		}).

		// 8
		Test("Concurrent Writes to Different Keys All Succeed", func(do *Do) {
			do.Concurrently(100, func(i int) {
				do.PUT(Node("n1"), fmt.Sprintf("/kv/concurrent:key%d", i), fmt.Sprintf("value%d", i)).
					Status(Is(200)).
					Hint("Your server should handle concurrent PUT requests.\n" +
						"Ensure thread-safety in your storage implementation.").
					Run()
			})

			for i := 1; i <= 100; i++ {
				do.GET(Node("n1"), fmt.Sprintf("/kv/concurrent:key%d", i)).
					Status(Is(200)).
					Body(Is(fmt.Sprintf("value%d", i))).
					Hint("Your server should store all concurrent writes.\n" +
						"Ensure no data corruption or loss occurs during concurrent operations.").
					Run()
			}
		}).

		// 9
		Test("Concurrent Writes to the Same Key Do Not Corrupt Data", func(do *Do) {
			do.Concurrently(100, func(i int) {
				do.PUT(Node("n1"), "/kv/concurrent:racekey", fmt.Sprintf("value%d", i)).
					Status(Is(200)).
					Hint("Your server should handle concurrent PUT requests.\n" +
						"Ensure thread-safety in your storage implementation.").
					Run()
			})

			expectedValues := make([]string, 100)
			for i := range expectedValues {
				expectedValues[i] = fmt.Sprintf("value%d", i+1)
			}
			do.GET(Node("n1"), "/kv/concurrent:racekey").
				Status(Is(200)).
				Body(OneOf(expectedValues...)).
				Hint("Your server should handle concurrent writes to the same key.\n" +
					"Ensure thread-safety prevents crashes or data corruption.\n" +
					"The value should be one of the concurrently written values (value1-value100).").
				Run()
		}).

		// 10
		Test("Unsupported HTTP Methods Return 405", func(do *Do) {
			for _, check := range []*Check{
				do.POST(Node("n1"), "/kv/test:key"),
				do.PATCH(Node("n1"), "/kv/test:key"),
			} {
				check.
					Status(Is(405)).
					Body(Matches("^method not allowed\n?$")).
					Hint("Your server should reject unsupported HTTP methods on /kv/{key}.\n" +
						"Add logic to return 405 Method Not Allowed for unsupported methods.").
					Run()
			}

			for _, check := range []*Check{
				do.GET(Node("n1"), "/clear"),
				do.POST(Node("n1"), "/clear"),
				do.PUT(Node("n1"), "/clear"),
				do.PATCH(Node("n1"), "/clear"),
			} {
				check.
					Status(Is(405)).
					Body(Matches("^method not allowed\n?$")).
					Hint("Your server should reject unsupported HTTP methods on /clear.\n" +
						"Only DELETE /clear should be allowed. Return 405 Method Not Allowed for other methods.").
					Run()
			}
		})
}
