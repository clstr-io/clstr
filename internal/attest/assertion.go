package attest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type timing int

const (
	timingImmediate timing = iota
	timingEventually
	timingConsistently
)

// H is a convenience type for HTTP headers.
type H map[string]string

// eventually checks that the condition becomes true within the given period.
func eventually(ctx context.Context, condition func() bool, timeout, pollInterval time.Duration) bool {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(pollInterval):
			if condition() {
				return true
			}
		}
	}

	return false
}

// consistently checks that the condition is always true for the given period.
func consistently(ctx context.Context, condition func() bool, timeout, pollInterval time.Duration) bool {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(pollInterval):
			if !condition() {
				return false
			}
		}
	}

	return true
}

// Assertion represents a pending HTTP request with optional checkers and hint text.
type Assertion struct {
	timing  timing
	timeout time.Duration
	hint    string

	ctx    context.Context
	config *config

	method  string
	url     string
	headers H
	body    []byte

	responseBody   string
	responseStatus int

	statusCheckers []Checker[int]
	bodyCheckers   []Checker[string]
	jsonCheckers   []Checker[string]
}

// Eventually configures the assertion to retry until success or timeout.
func (a *Assertion) Eventually(timeout ...time.Duration) *Assertion {
	a.timing = timingEventually
	a.timeout = a.config.assertTimeout
	if len(timeout) > 0 {
		a.timeout = timeout[0]
	}
	return a
}

// Consistently configures the assertion to verify success for the entire duration.
func (a *Assertion) Consistently(timeout ...time.Duration) *Assertion {
	a.timing = timingConsistently
	a.timeout = a.config.assertTimeout
	if len(timeout) > 0 {
		a.timeout = timeout[0]
	}
	return a
}

// Status adds expected HTTP response status code checkers.
// All checkers must pass.
func (a *Assertion) Status(checkers ...Checker[int]) *Assertion {
	a.statusCheckers = append(a.statusCheckers, checkers...)
	return a
}

// Body adds expected HTTP response body checkers.
// All checkers must pass.
func (a *Assertion) Body(checkers ...Checker[string]) *Assertion {
	a.bodyCheckers = append(a.bodyCheckers, checkers...)
	return a
}

// JSON adds expected checkers for a JSON field at the given gjson path.
// All checkers must pass.
func (a *Assertion) JSON(path string, checkers ...Checker[string]) *Assertion {
	for _, checker := range checkers {
		a.jsonCheckers = append(a.jsonCheckers, JSON(path, checker))
	}

	return a
}

// Hint sets the help text shown when the assertion fails.
func (a *Assertion) Hint(help string) *Assertion {
	a.hint = help
	return a
}

// Check executes the assertion and panics on failure.
func (a *Assertion) Check() {
	switch a.timing {
	case timingEventually:
		eventually(a.ctx, a.execute, a.timeout, a.config.pollInterval)
	case timingConsistently:
		consistently(a.ctx, a.execute, a.timeout, a.config.pollInterval)
	default:
		a.execute()
	}

	a.verify()
}

func (a *Assertion) execute() bool {
	client := &http.Client{Timeout: a.config.requestTimeout}

	req, err := http.NewRequestWithContext(a.ctx, a.method, a.url, bytes.NewReader(a.body))
	if err != nil {
		panic(fmt.Sprintf("An error occurred: %v", err))
	}

	for key, value := range a.headers {
		req.Header.Set(key, value)
	}

	resp, err := client.Do(req)
	if err != nil {
		panic(fmt.Sprintf("An error occurred: %v", err))
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(fmt.Sprintf("An error occurred: %v", err))
	}

	a.responseBody = string(responseBody)
	a.responseStatus = resp.StatusCode

	return checkAll(a.responseStatus, a.statusCheckers, nil) &&
		checkAll(a.responseBody, a.bodyCheckers, nil) &&
		checkAll(a.responseBody, a.jsonCheckers, nil)
}

func (a *Assertion) verify() {
	formatHelp := func() string {
		if a.hint == "" {
			return ""
		}

		return "\n\n  " + strings.ReplaceAll(a.hint, "\n", "\n  ")
	}

	checkAll(a.responseStatus, a.statusCheckers, func(m Checker[int], actual int) {
		panic(fmt.Sprintf("%s %s\n  Expected status: %s\n  Actual status: %d %s%s",
			a.method, a.url, m.Expected(), actual, http.StatusText(actual), formatHelp()))
	})

	checkAll(a.responseBody, a.bodyCheckers, func(m Checker[string], actual string) {
		panic(fmt.Sprintf("%s %s\n  Expected response: %s\n  Actual response: %q%s",
			a.method, a.url, m.Expected(), actual, formatHelp()))
	})

	checkAll(a.responseBody, a.jsonCheckers, func(m Checker[string], actual string) {
		panic(fmt.Sprintf("%s %s\n  Expected JSON: %s\n  Actual value: %v%s",
			a.method, a.url, m.Expected(), actual, formatHelp()))
	})
}
