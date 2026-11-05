package platform

import "time"

// SystemClock is the real clock behind every module's Clock port.
type SystemClock struct{}

func (SystemClock) Now() time.Time {
	return time.Now()
}
