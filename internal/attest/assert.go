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

// Assert defines the interface for executing and validating test assertions.
type Assert interface {
	// Assert executes the test plan and validates the result.
	Assert(help string)
	// execute runs the test plan once and returns whether it meets expectations.
	execute() bool
	// check validates the result and panics with formatted error message on failure.
	check()
	// formatHelp formats help text with proper indentation for error messages.
	formatHelp() string
}

var _ Assert = (*HTTPAssert)(nil)

// AssertBase provides common assertion functionality.
type AssertBase struct {
	help string

	config *Config
}

func (a *AssertBase) formatHelp() string {
	return "\n\n  " + strings.ReplaceAll(a.help, "\n", "\n  ")
}

// HTTPAssert provides assertions for HTTP response validation.
type HTTPAssert struct {
	AssertBase

	plan           *HTTPPlan
	responseBody   string
	responseStatus int

	statusCheckers []Checker[int]
	bodyCheckers   []Checker[string]
	jsonCheckers   []Checker[string]
}

// Status adds expected HTTP response status code checkers.
// All checkers must pass.
func (a *HTTPAssert) Status(checkers ...Checker[int]) *HTTPAssert {
	a.statusCheckers = append(a.statusCheckers, checkers...)
	return a
}

// Body adds expected HTTP response body checkers.
// All checkers must pass.
func (a *HTTPAssert) Body(checkers ...Checker[string]) *HTTPAssert {
	a.bodyCheckers = append(a.bodyCheckers, checkers...)
	return a
}

// JSON adds expected checkers for a JSON field at the given gjson path.
// All checkers must pass.
func (a *HTTPAssert) JSON(path string, checkers ...Checker[string]) *HTTPAssert {
	for _, checker := range checkers {
		a.jsonCheckers = append(a.jsonCheckers, JSON(path, checker))
	}

	return a
}

func (a *HTTPAssert) Assert(help string) {
	a.help = help

	p := a.plan
	switch p.timing {
	case TimingEventually:
		eventually(p.ctx, a.execute, p.timeout, a.config.RetryPollInterval)
	case TimingConsistently:
		consistently(p.ctx, a.execute, p.timeout, a.config.RetryPollInterval)
	default:
		a.execute()
	}

	a.check()
}

func (a *HTTPAssert) execute() bool {
	client := &http.Client{Timeout: a.config.ExecuteTimeout}
	p := a.plan

	req, err := http.NewRequestWithContext(p.ctx, p.method, p.url, bytes.NewReader(p.body))
	if err != nil {
		panic(fmt.Sprintf("An error occurred: %v", err))
	}

	for key, value := range p.headers {
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

func (a *HTTPAssert) check() {
	p := a.plan

	checkAll(a.responseStatus, a.statusCheckers, func(m Checker[int], actual int) {
		msg := fmt.Sprintf("%s %s\n  Expected status: %s\n  Actual status: %d %s%s",
			p.method, p.url, m.Expected(), actual,
			http.StatusText(actual), a.formatHelp())
		panic(msg)
	})

	checkAll(a.responseBody, a.bodyCheckers, func(m Checker[string], actual string) {
		msg := fmt.Sprintf("%s %s\n  Expected response: %s\n  Actual response: %q%s",
			p.method, p.url, m.Expected(), actual, a.formatHelp())
		panic(msg)
	})

	checkAll(a.responseBody, a.jsonCheckers, func(m Checker[string], actual string) {
		msg := fmt.Sprintf("%s %s\n  Expected JSON: %s\n  Actual value: %v%s",
			p.method, p.url, m.Expected(), actual, a.formatHelp())
		panic(msg)
	})
}
