package attest

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
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
	node    string
	status  int
	headers http.Header
	body    string
	err     error
	passed  bool
	failure string
}

// Check describes an HTTP request and the conditions its response must satisfy.
type Check struct {
	timing  timing
	timeout time.Duration
	hint    string

	ctx    context.Context
	config *config
	client *http.Client

	method  string
	headers H
	body    []byte

	selector  NodeSelector
	urls      []string
	nodeNames []string
	results   []result

	statusMatchers []Matcher[int]
	headerMatchers []headerMatcher
	bodyMatchers   []Matcher[string]
	jsonMatchers   []Matcher[string]
}

// headerMatcher pairs a header name with matchers for its value.
type headerMatcher struct {
	name     string
	matchers []Matcher[string]
}

// Eventually configures the assertion to retry until success or timeout.
func (c *Check) Eventually(timeout ...time.Duration) *Check {
	c.timing = timingEventually

	c.timeout = c.config.retryTimeout
	if len(timeout) > 0 {
		c.timeout = timeout[0]
	}

	return c
}

// Consistently configures the assertion to verify success for the entire duration.
func (c *Check) Consistently(timeout ...time.Duration) *Check {
	c.timing = timingConsistently

	c.timeout = c.config.retryTimeout
	if len(timeout) > 0 {
		c.timeout = timeout[0]
	}

	return c
}

// Status adds expected HTTP response status code matchers.
// All matchers must pass.
func (c *Check) Status(matchers ...Matcher[int]) *Check {
	c.statusMatchers = append(c.statusMatchers, matchers...)
	return c
}

// Body adds expected HTTP response body matchers.
// All matchers must pass.
func (c *Check) Body(matchers ...Matcher[string]) *Check {
	c.bodyMatchers = append(c.bodyMatchers, matchers...)
	return c
}

// JSON adds expected matchers for a JSON field at the given gjson path.
// All matchers must pass.
func (c *Check) JSON(path string, matchers ...Matcher[string]) *Check {
	for _, matcher := range matchers {
		c.jsonMatchers = append(c.jsonMatchers, JSON(path, matcher))
	}

	return c
}

// Header adds matchers for the named response header. All matchers must pass.
func (c *Check) Header(name string, matchers ...Matcher[string]) *Check {
	c.headerMatchers = append(c.headerMatchers, headerMatcher{name: name, matchers: matchers})
	return c
}

// Hint sets the help text shown when the assertion fails.
func (c *Check) Hint(help string) *Check {
	c.hint = help
	return c
}

// Run executes the assertion and panics on failure.
func (c *Check) Run() {
	switch c.timing {
	case timingEventually:
		eventually(c.ctx, c.execute, c.timeout, c.config.pollInterval)
	case timingConsistently:
		consistently(c.ctx, c.execute, c.timeout, c.config.pollInterval)
	default:
		c.execute()
	}

	c.verify()
}

func (c *Check) execute() bool {
	results := make([]result, len(c.urls))

	for i, url := range c.urls {
		r := result{url: url}
		if i < len(c.nodeNames) {
			r.node = c.nodeNames[i]
		}
		r.passed, r.err = c.executeOne(url, &r)
		results[i] = r
	}

	slices.SortFunc(results, func(a, b result) int {
		return cmp.Compare(a.node, b.node)
	})
	c.results = results

	passed := 0
	for _, r := range results {
		if r.passed {
			passed++
		}
	}

	switch c.selector.kind {
	case nodeNamed, nodeAll:
		return passed == len(results)
	case nodeExactlyOne:
		return passed == 1
	case nodeAtLeastOne:
		return passed >= 1
	}

	return false
}

func (c *Check) executeOne(url string, r *result) (bool, error) {
	ctx, cancel := context.WithTimeout(c.ctx, c.config.requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, c.method, url, bytes.NewReader(c.body))
	if err != nil {
		return false, err
	}

	for key, value := range c.headers {
		req.Header.Set(key, value)
	}

	resp, err := c.client.Do(req)
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

	if !checkAll(r.status, c.statusMatchers, func(m Matcher[int], actual int) {
		r.failure = fmt.Sprintf("Expected status %s, got %d %s", m.Expected(), actual, http.StatusText(actual))
	}) {
		return false, nil
	}

	for _, hm := range c.headerMatchers {
		if !checkAll(r.headers.Get(hm.name), hm.matchers, func(m Matcher[string], actual string) {
			r.failure = fmt.Sprintf("Expected header %s: %s, got %q", hm.name, m.Expected(), actual)
		}) {
			return false, nil
		}
	}

	if !checkAll(r.body, c.bodyMatchers, func(m Matcher[string], _ string) {
		r.failure = fmt.Sprintf("Expected body: %s", m.Expected())
	}) {
		return false, nil
	}

	if !checkAll(r.body, c.jsonMatchers, func(m Matcher[string], _ string) {
		r.failure = fmt.Sprintf("Expected %s", m.Expected())
	}) {
		return false, nil
	}

	return true, nil
}

func (c *Check) verify() {
	passed := 0
	for _, r := range c.results {
		if r.passed {
			passed++
		}
	}
	total := len(c.results)

	switch c.selector.kind {
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
		if c.hint == "" {
			return ""
		}

		return "\n\n  " + strings.ReplaceAll(c.hint, "\n", "\n  ")
	}

	if total > 1 {
		var desc string
		var relevant []result
		switch c.selector.kind {
		case nodeExactlyOne:
			desc = fmt.Sprintf("%d of %d nodes passed (expected exactly 1)", passed, total)
			if passed == 0 {
				relevant = c.results
			} else {
				for _, r := range c.results {
					if r.passed {
						relevant = append(relevant, r)
					}
				}
			}
		case nodeAtLeastOne:
			desc = fmt.Sprintf("0 of %d nodes passed (expected at least 1)", total)
			relevant = c.results
		case nodeAll:
			desc = fmt.Sprintf("%d of %d nodes passed (expected all %d)", passed, total, total)
			for _, r := range c.results {
				if !r.passed {
					relevant = append(relevant, r)
				}
			}
		}

		var parts []string
		for _, r := range relevant {
			parts = append(parts, formatResult(r))
		}
		details := "\n  " + strings.Join(parts, "\n\n  ")

		panic(fmt.Sprintf("%s - %s%s%s", c.method, desc, details, formatHelp()))
	}

	c.reportFailure(c.results[0], formatHelp)
}

// prettyBody returns the input string pretty-printed if it is valid JSON,
// otherwise returns it unchanged. indent is prepended to each continuation line.
func prettyBody(s, indent string) string {
	var buf bytes.Buffer
	err := json.Indent(&buf, []byte(s), "", "  ")
	if err == nil {
		return strings.ReplaceAll(buf.String(), "\n", "\n"+indent)
	}

	return s
}

func formatResult(r result) string {
	prefix := r.url
	if r.node != "" {
		prefix = fmt.Sprintf("%s (%s)", r.url, r.node)
	}

	if r.err != nil {
		if errors.Is(r.err, context.DeadlineExceeded) {
			return fmt.Sprintf("%s → timed out", prefix)
		}
		return fmt.Sprintf("%s → %s", prefix, r.err.Error())
	}

	if r.failure == "" {
		return fmt.Sprintf("%s → %d", prefix, r.status)
	}

	return fmt.Sprintf("%s → %d\n    %s\n      %s", prefix, r.status, r.failure, prettyBody(r.body, "      "))
}

func (c *Check) reportFailure(r result, formatHelp func() string) {
	prefix := fmt.Sprintf("%s %s", c.method, r.url)
	if r.node != "" {
		prefix = fmt.Sprintf("%s (%s)", prefix, r.node)
	}

	if r.err != nil {
		errMsg := r.err.Error()
		if errors.Is(r.err, context.DeadlineExceeded) {
			errMsg = fmt.Sprintf("Request timed out: server did not respond within %s", c.config.requestTimeout)
		}

		panic(fmt.Sprintf("%s\n  %s%s", prefix, errMsg, formatHelp()))
	}

	prefix = fmt.Sprintf("%s → %d", prefix, r.status)

	checkAll(r.status, c.statusMatchers, func(m Matcher[int], actual int) {
		panic(fmt.Sprintf("%s\n  Expected status: %s\n  Actual status: %d %s%s",
			prefix, m.Expected(), actual, http.StatusText(actual), formatHelp()))
	})

	for _, hm := range c.headerMatchers {
		value := r.headers.Get(hm.name)
		checkAll(value, hm.matchers, func(m Matcher[string], actual string) {
			panic(fmt.Sprintf("%s\n  Expected header %s: %s\n  Actual value: %q%s",
				prefix, hm.name, m.Expected(), actual, formatHelp()))
		})
	}

	checkAll(r.body, c.bodyMatchers, func(m Matcher[string], actual string) {
		panic(fmt.Sprintf("%s\n  Expected response: %s\n  Actual response: %s%s",
			prefix, m.Expected(), prettyBody(actual, "  "), formatHelp()))
	})

	checkAll(r.body, c.jsonMatchers, func(m Matcher[string], actual string) {
		panic(fmt.Sprintf("%s\n  Expected JSON: %s\n  Actual response: %s%s",
			prefix, m.Expected(), prettyBody(actual, "  "), formatHelp()))
	})
}
