package kvstore

import (
	. "github.com/clstr-io/clstr/internal/attest"
)

func FaultTolerance() *Suite {
	return New()
}
