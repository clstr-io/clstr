package kvstore

import (
	"fmt"
	"strings"

	. "github.com/clstr-io/clstr/internal/attest"
)

func HTTPAPI() *Suite {
	return New().
		// 0
		Setup(func(do *Do) {
			do.Start("node")
		}).

		// 1
		Test("PUT Basic Operations", func(do *Do) {
			// Set initial key-value pairs that subsequent tests can rely on
			capitals := map[string]string{
				"kenya":    "Nairobi",
				"uganda":   "Kampala",
				"tanzania": "Dar es Salaam",
			}
			for country, capital := range capitals {
				do.HTTP("node", "PUT", fmt.Sprintf("/kv/%s:capital", country), capital).T().
					Status(Is(200)).
					Assert("Your server should accept PUT requests.\n" +
						"Ensure your HTTP handler processes PUT requests to /kv/{key}.")
			}

			// Test overwrite behavior
			do.HTTP("node", "PUT", "/kv/tanzania:capital", "Dodoma").T().
				Status(Is(200)).
				Assert("Your server should allow overwriting existing keys.\n" +
					"Ensure PUT requests update the value of existing keys.")

			// Verify overwrite worked
			do.HTTP("node", "GET", "/kv/tanzania:capital").T().
				Status(Is(200)).
				Body(Is("Dodoma")).
				Assert("Your server should return the updated value after overwrite.\n" +
					"Ensure GET requests return the most recently stored value.")
		}).

		// 2
		Test("PUT Edge and Error Cases", func(do *Do) {
			// Empty value
			do.HTTP("node", "PUT", "/kv/empty").T().
				Status(Is(400)).
				Body(Is("value cannot be empty\n")).
				Assert("Your server should reject empty values.\n" +
					"Add validation to return 400 Bad Request for empty values.")

			// Empty key
			do.HTTP("node", "PUT", "/kv/", "some_value").T().
				Status(Is(400)).
				Body(Is("key cannot be empty\n")).
				Assert("Your server should reject empty keys.\n" +
					"Add validation to return 400 Bad Request for empty keys.")

			// Unicode handling
			do.HTTP("node", "PUT", "/kv/unicode:key", "🌍 Nairobi").T().
				Status(Is(200)).
				Assert("Your server should handle Unicode characters in values.\n" +
					"Ensure your HTTP handler properly processes UTF-8 encoded data.")

			// Long key and value
			longKey := "long:" + strings.Repeat("k", 100)
			longValue := strings.Repeat("v", 10_000)
			do.HTTP("node", "PUT", fmt.Sprintf("/kv/%s", longKey), longValue).T().
				Status(Is(200)).
				Assert("Your server should handle long keys and values.\n" +
					"Ensure your server doesn't have arbitrary key & value length limits.")

			// Special characters in key/value
			do.HTTP("node", "PUT", "/kv/special:key-with_symbols.123", "value with spaces & symbols! \t").T().
				Status(Is(200)).
				Assert("Your server should handle special characters in keys and values.\n" +
					"Ensure proper URL path parsing and value encoding/decoding.")

			// Verify special characters are retrieved correctly
			do.HTTP("node", "GET", "/kv/special:key-with_symbols.123").T().
				Status(Is(200)).
				Body(Is("value with spaces & symbols! \t")).
				Assert("Your server should preserve special characters in stored values.\n" +
					"Ensure proper encoding/decoding doesn't corrupt the data.")
		}).

		// 3
		Test("GET Basic Operations", func(do *Do) {
			// Retrieve values we know exist from PUT tests
			do.HTTP("node", "GET", "/kv/kenya:capital").T().
				Status(Is(200)).
				Body(Is("Nairobi")).
				Assert("Your server should return stored values with GET requests.\n" +
					"Ensure your key-value storage and retrieval logic is working correctly.")
			do.HTTP("node", "GET", "/kv/uganda:capital").T().
				Status(Is(200)).
				Body(Is("Kampala")).
				Assert("Your server should return stored values with GET requests.\n" +
					"Ensure your key-value storage and retrieval logic is working correctly.")
			do.HTTP("node", "GET", "/kv/tanzania:capital").T().
				Status(Is(200)).
				Body(Is("Dodoma")).
				Assert("Your server should return the most recently stored value.\n" +
					"Ensure overwrite operations update the stored value correctly.")

			// Verify Unicode handling
			do.HTTP("node", "GET", "/kv/unicode:key").T().
				Status(Is(200)).
				Body(Is("🌍 Nairobi")).
				Assert("Your server should preserve Unicode characters in stored values.\n" +
					"Ensure proper UTF-8 handling in your storage and retrieval logic.")

			// Verify long values
			longKey := "long:" + strings.Repeat("k", 100)
			longValue := strings.Repeat("v", 10_000)
			do.HTTP("node", "GET", fmt.Sprintf("/kv/%s", longKey)).T().
				Status(Is(200)).
				Body(Is(longValue)).
				Assert("Your server should handle retrieval of long keys and values.\n" +
					"Ensure your storage doesn't truncate or corrupt large data.")
		}).

		// 4
		Test("GET Edge and Error Cases", func(do *Do) {
			// Non-existent key
			do.HTTP("node", "GET", "/kv/nonexistent:key").T().
				Status(Is(404)).
				Body(Is("key not found\n")).
				Assert("Your server should return 404 Not Found when a key doesn't exist.\n" +
					"Check your key lookup logic and error handling.")

			// Case sensitivity test
			do.HTTP("node", "GET", "/kv/KENYA:CAPITAL").T().
				Status(Is(404)).
				Body(Is("key not found\n")).
				Assert("Your server should return 404 Not Found when a key doesn't exist.\n" +
					"Check your key lookup logic and error handling.")

			// Empty key
			do.HTTP("node", "GET", "/kv/").T().
				Status(Is(400)).
				Body(Is("key cannot be empty\n")).
				Assert("Your server should reject empty keys.\n" +
					"Add validation to return 400 Bad Request for empty keys.")
		}).

		// 5
		Test("DELETE Basic Operations", func(do *Do) {
			// Delete an existing key
			do.HTTP("node", "DELETE", "/kv/tanzania:capital").T().
				Status(Is(200)).
				Assert("Your server should accept DELETE requests.\n" +
					"Ensure your HTTP handler processes DELETE requests to /kv/{key}.")

			// Verify deletion worked
			do.HTTP("node", "GET", "/kv/tanzania:capital").T().
				Status(Is(404)).
				Body(Is("key not found\n")).
				Assert("Your server should return 404 Not Found when a key doesn't exist.\n" +
					"Check your key lookup logic and error handling.")

			// Verify other keys still exist
			do.HTTP("node", "GET", "/kv/kenya:capital").T().
				Status(Is(200)).
				Body(Is("Nairobi")).
				Assert("Your server should only delete the specified key, not affect others.\n" +
					"Ensure your delete operation doesn't remove unrelated data.")
		}).

		// 6
		Test("DELETE Edge and Error Cases", func(do *Do) {
			// Delete non-existent key
			do.HTTP("node", "DELETE", "/kv/nonexistent:key").T().
				Status(Is(200)).
				Assert("Your server should gracefully handle deletion of non-existent keys.\n" +
					"Returning 200 OK for missing keys is acceptable (idempotent).")

			// Delete same key twice
			do.HTTP("node", "PUT", "/kv/delete:twice", "value").T().
				Status(Is(200)).
				Assert("Your server should accept PUT requests.\n" +
					"Ensure your HTTP handler processes PUT requests to /kv/{key}.")
			do.HTTP("node", "DELETE", "/kv/delete:twice").T().
				Status(Is(200)).
				Assert("Your server should successfully delete existing keys.\n" +
					"Implement proper key removal in your storage logic.")
			do.HTTP("node", "DELETE", "/kv/delete:twice").T().
				Status(Is(200)).
				Assert("Your server should handle repeated deletions gracefully.\n" +
					"Deleting the same key twice should be idempotent (return 200 OK).")

			// Empty key
			do.HTTP("node", "DELETE", "/kv/").T().
				Status(Is(400)).
				Body(Is("key cannot be empty\n")).
				Assert("Your server should reject empty keys.\n" +
					"Add validation to return 400 Bad Request for empty keys.")
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
				do.HTTP("node", "PUT", fmt.Sprintf("/kv/%s", key), value).T().
					Status(Is(200)).
					Assert("Your server should accept PUT requests.\n" +
						"Ensure your HTTP handler processes PUT requests to /kv/{key}.")
			}

			// Verify keys exist
			for key, value := range testKeys {
				do.HTTP("node", "GET", fmt.Sprintf("/kv/%s", key)).T().
					Status(Is(200)).
					Body(Is(value)).
					Assert("Your server should store and retrieve key-value pairs correctly.\n" +
						"Check your storage logic.")
			}

			// Clear all keys
			do.HTTP("node", "DELETE", "/clear").T().
				Status(Is(200)).
				Assert("Your server should implement a /clear endpoint.\n" +
					"Add a DELETE /clear method that deletes all key-value pairs.")

			// Verify all keys are gone
			for key := range testKeys {
				do.HTTP("node", "GET", fmt.Sprintf("/kv/%s", key)).T().
					Status(Is(404)).
					Body(Is("key not found\n")).
					Assert("Your server should delete all keys when /clear is called.\n" +
						"Ensure the /clear endpoint removes all stored key-value pairs.")
			}

			// Verify old keys from previous tests are also gone
			do.HTTP("node", "GET", "/kv/kenya:capital").T().
				Status(Is(404)).
				Body(Is("key not found\n")).
				Assert("Your server should delete ALL keys when /clear is called.\n" +
					"Ensure the /clear endpoint removes every key-value pair, not just recent ones.")

			// Test that clear on empty store works
			do.HTTP("node", "DELETE", "/clear").T().
				Status(Is(200)).
				Assert("Your server should handle clearing an empty store gracefully.\n" +
					"Calling /clear on an empty store should return 200 OK.")
		}).

		// 8
		Test("Concurrent Operations - Different Keys", func(do *Do) {
			// Test concurrent writes to different keys
			putFn := func(key, value string) func() {
				return func() {
					do.HTTP("node", "PUT", "/kv/concurrent:"+key, value).T().
						Status(Is(200)).
						Assert("Your server should handle concurrent PUT requests.\n" +
							"Ensure thread-safety in your storage implementation.")
				}
			}

			fns := []func(){}
			for i := 1; i <= 100; i++ {
				fns = append(fns, putFn(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i)))
			}

			do.Concurrently(fns...)

			// Verify all concurrent writes succeeded
			for i := 1; i <= 100; i++ {
				do.HTTP("node", "GET", fmt.Sprintf("/kv/concurrent:key%d", i)).T().
					Status(Is(200)).
					Body(Is(fmt.Sprintf("value%d", i))).
					Assert("Your server should store all concurrent writes.\n" +
						"Ensure no data corruption or loss occurs during concurrent operations.")
			}
		}).

		// 9
		Test("Concurrent Operations - Same Key", func(do *Do) {
			// Test concurrent writes to the SAME key
			// Last write should win, but no crashes or data corruption
			putFn := func(key, value string) func() {
				return func() {
					do.HTTP("node", "PUT", "/kv/concurrent:"+key, value).T().
						Status(Is(200)).
						Assert("Your server should handle concurrent PUT requests.\n" +
							"Ensure thread-safety in your storage implementation.")
				}
			}

			raceFns := []func(){}
			expectedValues := []string{}
			for i := 1; i <= 100; i++ {
				raceFns = append(raceFns, putFn("racekey", fmt.Sprintf("value%d", i)))
				expectedValues = append(expectedValues, fmt.Sprintf("value%d", i))
			}

			do.Concurrently(raceFns...)

			// Verify the key exists with one of the values (doesn't matter which)
			do.HTTP("node", "GET", "/kv/concurrent:racekey").T().
				Status(Is(200)).
				Body(OneOf(expectedValues...)).
				Assert("Your server should handle concurrent writes to the same key.\n" +
					"Ensure thread-safety prevents crashes or data corruption.\n" +
					"The value should be one of the concurrently written values (value1-value100).")
		}).

		// 10
		Test("Check Allowed HTTP Methods", func(do *Do) {
			// POST & PATCH /kv/{key} not allowed
			for _, method := range []string{"POST", "PATCH"} {
				do.HTTP("node", method, "/kv/test:key").T().
					Status(Is(405)).
					Body(Is("method not allowed\n")).
					Assert("Your server should reject unsupported HTTP methods on /kv/{key}.\n" +
						"Add logic to return 405 Method Not Allowed for unsupported methods.")
			}

			// GET, POST, PUT, PATCH /clear not allowed
			for _, method := range []string{"GET", "POST", "PUT", "PATCH"} {
				do.HTTP("node", method, "/clear").T().
					Status(Is(405)).
					Body(Is("method not allowed\n")).
					Assert("Your server should reject unsupported HTTP methods on /clear.\n" +
						"Only DELETE /clear should be allowed. Return 405 Method Not Allowed for other methods.")
			}
		})
}
