package attest

import (
	"context"
	"time"
)

// timing defines when test plans should be executed.
type timing int

const (
	TimingImmediate timing = iota
	TimingEventually
	TimingConsistently
)

// Plan represents a test plan to be asserted.
type Plan[P any, A any] interface {
	// Eventually configures the plan to retry until success or timeout.
	Eventually() P
	// Within sets a custom timeout for Eventually.
	Within(time.Duration) P
	// Consistently configures the plan to verify success for the entire duration.
	Consistently() P
	// For sets a custom timeout for Consistently.
	For(time.Duration) P
	// T returns the test for this plan.
	T() A
}

var _ Plan[*HTTPPlan, *HTTPAssert] = (*HTTPPlan)(nil)

// PlanBase provides common plan functionality.
type PlanBase struct {
	timing  timing
	timeout time.Duration

	ctx context.Context

	config *Config
}

func (b *PlanBase) setEventually() {
	b.timing = TimingEventually
	b.timeout = b.config.DefaultRetryTimeout
}

func (b *PlanBase) setWithin(timeout time.Duration) {
	if b.timing != TimingEventually {
		panic("Within() can only be called after Eventually()")
	}

	b.timeout = timeout
}

func (b *PlanBase) setConsistently() {
	b.timing = TimingConsistently
	b.timeout = b.config.DefaultRetryTimeout
}

func (b *PlanBase) setFor(timeout time.Duration) {
	if b.timing != TimingConsistently {
		panic("For() can only be called after Consistently()")
	}

	b.timeout = timeout
}

// H is a convenience type for HTTP headers.
type H map[string]string

// HTTPPlan represents a test plan for an HTTP request.
type HTTPPlan struct {
	PlanBase

	method  string
	url     string
	headers H
	body    []byte
}

func (p *HTTPPlan) Eventually() *HTTPPlan {
	p.setEventually()
	return p
}

func (p *HTTPPlan) Within(timeout time.Duration) *HTTPPlan {
	p.setWithin(timeout)
	return p
}

func (p *HTTPPlan) Consistently() *HTTPPlan {
	p.setConsistently()
	return p
}

func (p *HTTPPlan) For(timeout time.Duration) *HTTPPlan {
	p.setFor(timeout)
	return p
}

func (p *HTTPPlan) T() *HTTPAssert {
	return &HTTPAssert{
		AssertBase: AssertBase{config: p.config},
		plan:       p,
	}
}
