package attest

import (
	"bytes"
	"context"
	"errors"
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

// result holds the response from a single node.
type result struct {
	url     string
	status  int
	headers http.Header
	body    string
	err     error
	passed  bool
}

// Assertion describes an HTTP request and the conditions its response must satisfy.
type Assertion struct {
	timing  timing
	timeout time.Duration
	hint    string

	ctx    context.Context
	config *config
	client *http.Client

	method  string
	headers H
	body    []byte

	selector NodeSelector
	urls     []string
	results  []result

	statusCheckers []Checker[int]
	headerCheckers []headerChecker
	bodyCheckers   []Checker[string]
	jsonCheckers   []Checker[string]
}

// headerChecker pairs a header name with checkers for its value.
type headerChecker struct {
	name     string
	checkers []Checker[string]
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

// Header adds checkers for the named response header. All checkers must pass.
func (a *Assertion) Header(name string, checkers ...Checker[string]) *Assertion {
	a.headerCheckers = append(a.headerCheckers, headerChecker{name: name, checkers: checkers})
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
	results := make([]result, len(a.urls))

	for i, url := range a.urls {
		r := result{url: url}
		r.passed, r.err = a.executeOne(url, &r)
		results[i] = r
	}

	a.results = results

	passed := 0
	for _, r := range results {
		if r.passed {
			passed++
		}
	}

	switch a.selector.kind {
	case nodeNamed, nodeAll:
		return passed == len(results)
	case nodeExactlyOne:
		return passed == 1
	case nodeAtLeastOne:
		return passed >= 1
	}

	return false
}

func (a *Assertion) executeOne(url string, r *result) (bool, error) {
	ctx, cancel := context.WithTimeout(a.ctx, a.config.requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, a.method, url, bytes.NewReader(a.body))
	if err != nil {
		return false, err
	}

	for key, value := range a.headers {
		req.Header.Set(key, value)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	r.status = resp.StatusCode
	r.headers = resp.Header
	r.body = string(responseBody)

	headersOk := true
	for _, hc := range a.headerCheckers {
		if !checkAll(r.headers.Get(hc.name), hc.checkers, nil) {
			headersOk = false
			break
		}
	}

	return checkAll(r.status, a.statusCheckers, nil) &&
		headersOk &&
		checkAll(r.body, a.bodyCheckers, nil) &&
		checkAll(r.body, a.jsonCheckers, nil), nil
}

func (a *Assertion) verify() {
	passed := 0
	for _, r := range a.results {
		if r.passed {
			passed++
		}
	}
	total := len(a.results)

	switch a.selector.kind {
	case nodeNamed, nodeAll:
		if passed == total {
			return
		}
	case nodeExactlyOne:
		if passed == 1 {
			return
		}
	case nodeAtLeastOne:
		if passed >= 1 {
			return
		}
	}

	formatHelp := func() string {
		if a.hint == "" {
			return ""
		}

		return "\n\n  " + strings.ReplaceAll(a.hint, "\n", "\n  ")
	}

	if total > 1 {
		var desc string
		var relevant []result
		switch a.selector.kind {
		case nodeExactlyOne:
			desc = fmt.Sprintf("Exactly 1 node to satisfy, %d did", passed)
			if passed == 0 {
				relevant = a.results
			} else {
				for _, r := range a.results {
					if r.passed {
						relevant = append(relevant, r)
					}
				}
			}
		case nodeAtLeastOne:
			desc = "At least 1 node to satisfy, 0 did"
			relevant = a.results
		case nodeAll:
			desc = fmt.Sprintf("All %d nodes to satisfy, %d did not", total, total-passed)
			for _, r := range a.results {
				if !r.passed {
					relevant = append(relevant, r)
				}
			}
		}

		var details strings.Builder
		for _, r := range relevant {
			details.WriteString("\n  ")
			details.WriteString(formatResult(r))
		}

		panic(fmt.Sprintf("%s - expected %s%s%s", a.method, desc, details.String(), formatHelp()))
	}

	a.reportFailure(a.results[0], formatHelp)
}

func formatResult(r result) string {
	if r.err != nil {
		if errors.Is(r.err, context.DeadlineExceeded) {
			return fmt.Sprintf("%s → timed out", r.url)
		}

		return fmt.Sprintf("%s → %s", r.url, r.err.Error())
	}

	body := r.body
	if len(body) > 100 {
		body = body[:100] + "..."
	}

	return fmt.Sprintf("%s → %d %s", r.url, r.status, body)
}

func (a *Assertion) reportFailure(r result, formatHelp func() string) {
	if r.err != nil {
		errMsg := r.err.Error()
		if errors.Is(r.err, context.DeadlineExceeded) {
			errMsg = fmt.Sprintf("Request timed out: server did not respond within %s", a.config.requestTimeout)
		}

		panic(fmt.Sprintf("%s %s\n  %s%s", a.method, r.url, errMsg, formatHelp()))
	}

	checkAll(r.status, a.statusCheckers, func(m Checker[int], actual int) {
		panic(fmt.Sprintf("%s %s\n  Expected status: %s\n  Actual status: %d %s%s",
			a.method, r.url, m.Expected(), actual, http.StatusText(actual), formatHelp()))
	})

	for _, hc := range a.headerCheckers {
		value := r.headers.Get(hc.name)
		checkAll(value, hc.checkers, func(m Checker[string], actual string) {
			panic(fmt.Sprintf("%s %s\n  Expected header %s: %s\n  Actual value: %q%s",
				a.method, r.url, hc.name, m.Expected(), actual, formatHelp()))
		})
	}

	checkAll(r.body, a.bodyCheckers, func(m Checker[string], actual string) {
		panic(fmt.Sprintf("%s %s\n  Expected response: %s\n  Actual response: %q%s",
			a.method, r.url, m.Expected(), actual, formatHelp()))
	})

	checkAll(r.body, a.jsonCheckers, func(m Checker[string], actual string) {
		panic(fmt.Sprintf("%s %s\n  Expected JSON: %s\n  Actual value: %v%s",
			a.method, r.url, m.Expected(), actual, formatHelp()))
	})
}
