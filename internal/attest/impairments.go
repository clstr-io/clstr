package attest

import (
	"fmt"
	"time"
)

// Impairment describes a network condition applied to a node's outgoing traffic via tc netem.
type Impairment struct {
	args []string
	desc string
}

// Delay adds a fixed latency to outgoing packets.
// An optional jitter value adds random variation: delay ± jitter.
func Delay(d time.Duration, jitter ...time.Duration) Impairment {
	args := []string{"delay", fmt.Sprintf("%dms", d.Milliseconds())}
	desc := fmt.Sprintf("packet delay %gs", d.Seconds())
	if len(jitter) > 0 {
		args = append(args, fmt.Sprintf("%dms", jitter[0].Milliseconds()), "25%", "distribution", "normal")
		desc += fmt.Sprintf(" ±%gs", jitter[0].Seconds())
	}

	return Impairment{args: args, desc: desc}
}

// Loss randomly drops a percentage of outgoing packets.
func Loss(pct float64) Impairment {
	s := fmt.Sprintf("%.2f%%", pct)
	return Impairment{
		args: []string{"loss", s, "25%"},
		desc: fmt.Sprintf("packet loss %g%%", pct),
	}
}

// Duplicate sends a percentage of outgoing packets twice.
func Duplicate(pct float64) Impairment {
	s := fmt.Sprintf("%.2f%%", pct)
	return Impairment{
		args: []string{"duplicate", s},
		desc: fmt.Sprintf("packet duplicate %g%%", pct),
	}
}

// Reorder delivers a percentage of packets out of order. Must be combined with
// Delay. Without it, all packets go out immediately and no reordering occurs.
func Reorder(pct float64) Impairment {
	s := fmt.Sprintf("%.2f%%", pct)
	return Impairment{
		args: []string{"reorder", s, "25%"},
		desc: fmt.Sprintf("packet reorder %g%%", pct),
	}
}
