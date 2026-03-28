package attest_test

import (
	"context"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	. "github.com/clstr-io/clstr/internal/attest"
)

func TestHTTP(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		config     *Config
		testFunc   func(*Do)
		cancel     func(*Do)
		shouldPass bool
	}{
		{
			name: "Basic OK",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/kv/kenya:capital":
					switch r.Method {
					case "PUT":
						w.WriteHeader(http.StatusOK)
					case "GET":
						w.WriteHeader(http.StatusOK)
						w.Write([]byte("Nairobi"))
					default:
						w.WriteHeader(http.StatusMethodNotAllowed)
					}
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			testFunc: func(do *Do) {
				do.PUT("svc", "/kv/kenya:capital", "Nairobi").T().
					Status(Is(200)).
					Assert("Server should handle PUT requests properly")

				do.GET("svc", "/kv/kenya:capital").T().
					Status(Is(200)).
					Body(Is("Nairobi")).
					Assert("Server should handle GET requests properly")

				do.PATCH("svc", "/kv/kenya:capital").T().
					Status(Is(405)).
					Assert("Server should return 405 for unsupported methods")

				do.GET("svc", "/unknown").T().
					Status(Is(404)).
					Assert("Server should return 404 for non-existent endpoints")
			},
			shouldPass: true,
		},
		{
			name: "Status Code Mismatch",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").T().
					Status(Is(200)).
					Assert("Should fail when expecting 200 OK but server returns 404")
			},
			shouldPass: false,
		},
		{
			name: "Body Mismatch",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("Mombasa"))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").T().
					Status(Is(200)).
					Body(Is("Nairobi")).
					Assert("Should fail when expecting 'Nairobi' but server returns 'Mombasa'")
			},
			shouldPass: false,
		},
		{
			name: "Timeout",
			handler: func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(500 * time.Millisecond)

				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Done"))
			},
			config: &Config{ExecuteTimeout: 50 * time.Millisecond},
			testFunc: func(do *Do) {
				do.GET("svc", "/").T().
					Status(Is(200)).
					Body(Is("Done")).
					Assert("Should fail when request times out before server responds")
			},
			shouldPass: false,
		},
		{
			name: "Eventually OK",
			handler: func() http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					readyAfter := time.Now().Add(500 * time.Millisecond)
					if time.Now().Before(readyAfter) {
						w.WriteHeader(http.StatusOK)
						w.Write([]byte("Ready"))
					} else {
						w.WriteHeader(http.StatusServiceUnavailable)
						w.Write([]byte("Starting up..."))
					}
				}
			}(),
			testFunc: func(do *Do) {
				do.GET("svc", "/").
					Eventually().T().
					Status(Is(200)).
					Body(Is("Ready")).
					Assert("Service should eventually become ready")
			},
			shouldPass: true,
		},
		{
			name: "Eventually Timeout",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte("Starting up..."))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").
					Eventually().Within(500 * time.Millisecond).T().
					Status(Is(200)).
					Body(Is("Ready")).
					Assert("Should fail when service never becomes ready within timeout")
			},
			shouldPass: false,
		},
		{
			name: "Eventually Cancellation",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte("Starting up..."))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").
					Eventually().Within(time.Second).T().
					Status(Is(200)).
					Body(Is("Ready")).
					Assert("Should fail when operation is cancelled before completion")
			},
			cancel: func(do *Do) {
				go func() {
					time.Sleep(500 * time.Millisecond)
					do.Cancel()
				}()
			},
			shouldPass: false,
		},
		{
			name: "Consistently OK",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Stable"))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").
					Consistently().For(500 * time.Millisecond).T().
					Status(Is(200)).
					Body(Is("Stable")).
					Assert("Service should remain consistently available")
			},
			shouldPass: true,
		},
		{
			name: "Consistently Failure",
			handler: func() http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					if rand.IntN(2) == 1 {
						w.WriteHeader(http.StatusOK)
						w.Write([]byte("Stable"))
					} else {
						w.WriteHeader(http.StatusServiceUnavailable)
						w.Write([]byte("Unstable"))
					}
				}
			}(),
			testFunc: func(do *Do) {
				do.GET("svc", "/").
					Consistently().T().
					Status(Is(200)).
					Body(Is("Stable")).
					Assert("Should fail when service returns intermittent errors")
			},
			shouldPass: false,
		},
		{
			name: "Consistently Cancellation",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Stable"))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").
					Consistently().For(3 * time.Second).T().
					Status(Is(200)).
					Body(Is("Stable")).
					Assert("Should pass when cancelled during consistency check")
			},
			cancel: func(do *Do) {
				go func() {
					time.Sleep(500 * time.Millisecond)
					do.Cancel()
				}()
			},
			shouldPass: true,
		},
		{
			name: "Contains Checker - matches substring",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Error: file not found"))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").T().
					Status(Is(200)).
					Body(Contains("file not found")).
					Assert("Should accept response containing the substring")
			},
			shouldPass: true,
		},
		{
			name: "Contains Checker - fails when substring not present",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Success"))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").T().
					Status(Is(200)).
					Body(Contains("error")).
					Assert("Should fail when substring is not in response")
			},
			shouldPass: false,
		},
		{
			name: "Matches Checker - matches regex pattern",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("User ID: 12345"))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").T().
					Status(Is(200)).
					Body(Matches(`User ID: \d+`)).
					Assert("Should match regex pattern")
			},
			shouldPass: true,
		},
		{
			name: "Matches Checker - fails when pattern doesn't match",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("User ID: abc"))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").T().
					Status(Is(200)).
					Body(Matches(`User ID: \d+`)).
					Assert("Should fail when pattern doesn't match")
			},
			shouldPass: false,
		},
		{
			name: "HasLen Checker - string body length matches",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Hello"))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").T().
					Status(Is(200)).
					Body(HasLen[string](5)).
					Assert("Should pass when body length matches")
			},
			shouldPass: true,
		},
		{
			name: "HasLen Checker - string body length mismatch",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Hello World"))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").T().
					Status(Is(200)).
					Body(HasLen[string](5)).
					Assert("Should fail when body length doesn't match")
			},
			shouldPass: false,
		},
		{
			name: "HasLen Checker - JSON array length matches",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"items":["a","b","c"]}`))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").T().
					Status(Is(200)).
					JSON("items", HasLen[string](3)).
					Assert("Should pass when JSON array length matches")
			},
			shouldPass: true,
		},
		{
			name: "HasLen Checker - JSON array length mismatch",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"items":["a","b","c","d","e"]}`))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").T().
					Status(Is(200)).
					JSON("items", HasLen[string](3)).
					Assert("Should fail when JSON array length doesn't match")
			},
			shouldPass: false,
		},
		{
			name: "HasLen Checker - JSON string length matches",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"name":"Alice"}`))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").T().
					Status(Is(200)).
					JSON("name", HasLen[string](5)).
					Assert("Should pass when JSON string field length matches")
			},
			shouldPass: true,
		},
		{
			name: "HasLen Checker - empty array",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"items":[]}`))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").T().
					Status(Is(200)).
					JSON("items", HasLen[string](0)).
					Assert("Should pass when JSON array is empty and length is 0")
			},
			shouldPass: true,
		},
		{
			name: "OneOf Checker - matches one of several values",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("value2"))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").T().
					Status(Is(200)).
					Body(OneOf("value1", "value2", "value3")).
					Assert("Should accept value2 as one of the valid options")
			},
			shouldPass: true,
		},
		{
			name: "OneOf Checker - fails when value not in list",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("invalid"))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").T().
					Status(Is(200)).
					Body(OneOf("value1", "value2", "value3")).
					Assert("Should fail when response is not in the list of valid values")
			},
			shouldPass: false,
		},
		{
			name: "Not Checker - negates another checker",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Success"))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").T().
					Status(Is(200)).
					Body(Not(Contains("error"))).
					Assert("Should pass when negated checker doesn't match")
			},
			shouldPass: true,
		},
		{
			name: "Not Checker - fails when negated checker matches",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Error occurred"))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").T().
					Status(Is(200)).
					Body(Not(Contains("Error"))).
					Assert("Should fail when negated checker matches")
			},
			shouldPass: false,
		},
		{
			name: "JSON Checker - simple field",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"role":"follower","leader":null,"term":1}`))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/cluster/info").T().
					Status(Is(200)).
					JSON("role", Is("follower")).
					JSON("leader", IsNull[string]()).
					JSON("term", Is("1")).
					Assert("Should match JSON fields")
			},
			shouldPass: true,
		},
		{
			name: "JSON Checker - nested path",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"entries":[{"term":1,"index":0},{"term":2,"index":1}]}`))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/log").T().
					Status(Is(200)).
					JSON("entries.0.term", Is("1")).
					JSON("entries.1.index", Is("1")).
					Assert("Should match nested JSON fields")
			},
			shouldPass: true,
		},
		{
			name: "JSON Checker - field mismatch",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"role":"candidate","term":2}`))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/cluster/info").T().
					Status(Is(200)).
					JSON("role", Is("leader")).
					Assert("Should fail when JSON field doesn't match")
			},
			shouldPass: false,
		},
		{
			name: "JSON Checker - IsNull fails when not null",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"role":"follower","leader":":8001"}`))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/cluster/info").T().
					Status(Is(200)).
					JSON("leader", IsNull[string]()).
					Assert("Should fail when expecting null but value is not null")
			},
			shouldPass: false,
		},
		{
			name: "Multiple Checkers - multiple status checkers",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").T().
					Status(Is(200), Not(Is(404)), Not(Is(500))).
					Assert("Should pass when all status checkers pass")
			},
			shouldPass: true,
		},
		{
			name: "Multiple Checkers - multiple body checkers",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Hello World"))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").T().
					Status(Is(200)).
					Body(Contains("Hello"), Contains("World"), Not(Contains("Goodbye"))).
					Assert("Should pass when all body checkers pass")
			},
			shouldPass: true,
		},
		{
			name: "Multiple Checkers - multiple JSON checkers on same field",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"role":"leader"}`))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/cluster/info").T().
					Status(Is(200)).
					JSON("role", Is("leader"), Not(Is("follower")), Not(Is("candidate"))).
					Assert("Should pass when all checkers for the same JSON field pass")
			},
			shouldPass: true,
		},
		{
			name: "Multiple Checkers - fails when one checker fails",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Hello World"))
			},
			testFunc: func(do *Do) {
				do.GET("svc", "/").T().
					Status(Is(200)).
					Body(Contains("Hello"), Contains("Goodbye")).
					Assert("Should fail when one of the checkers fails")
			},
			shouldPass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			port := strings.Split(server.URL, ":")[2]

			if tt.config == nil {
				tt.config = &Config{}
			}
			tt.config.WorkingDir = t.TempDir()

			suite := New().WithConfig(tt.config)

			success := suite.
				Setup(func(do *Do) {
					do.MockNode("svc", port)
					if tt.cancel != nil {
						tt.cancel(do)
					}
				}).
				Test(tt.name, func(do *Do) {
					tt.testFunc(do)
				}).
				Run(context.Background())

			if success != tt.shouldPass {
				if tt.shouldPass {
					t.Errorf("%s test should pass but failed", tt.name)
				} else {
					t.Errorf("%s test should fail but passed", tt.name)
				}
			}
		})
	}
}
