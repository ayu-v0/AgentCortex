// Package clock provides a small time abstraction for runtime code and tests.
package clock

import "time"

// Clock abstracts time for deterministic tests and custom runtimes.
type Clock interface {
	Now() time.Time
}

// System is a Clock backed by time.Now.
type System struct{}

func (System) Now() time.Time {
	return time.Now()
}
