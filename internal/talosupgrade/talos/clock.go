package talos

import "time"

// Clock abstracts time operations for testing.
// This allows deterministic testing of timeout and polling logic.
type Clock interface {
	// Now returns the current time
	Now() time.Time

	// After waits for the duration to elapse and returns a channel
	After(d time.Duration) <-chan time.Time

	// Sleep pauses the current goroutine for the duration
	Sleep(d time.Duration)
}

// realClock implements Clock using the real time package
type realClock struct{}

// newRealClock creates a new real clock instance
func newRealClock() Clock {
	return realClock{}
}

func (realClock) Now() time.Time {
	return time.Now()
}

func (realClock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

func (realClock) Sleep(d time.Duration) {
	time.Sleep(d)
}

// MockClock is a mock implementation of Clock for testing
type MockClock struct {
	// CurrentTime is the time returned by Now()
	CurrentTime time.Time

	// AfterFunc is called when After is invoked
	// If nil, returns a channel that receives immediately
	AfterFunc func(d time.Duration) <-chan time.Time

	// SleepFunc is called when Sleep is invoked
	// If nil, does nothing (returns immediately)
	SleepFunc func(d time.Duration)

	// AdvanceOnAfter if true, advances CurrentTime by the duration on After calls
	AdvanceOnAfter bool
}

// NewMockClock creates a mock clock with the given start time
func NewMockClock(startTime time.Time) *MockClock {
	return &MockClock{CurrentTime: startTime}
}

func (m *MockClock) Now() time.Time {
	return m.CurrentTime
}

func (m *MockClock) After(d time.Duration) <-chan time.Time {
	if m.AfterFunc != nil {
		return m.AfterFunc(d)
	}
	if m.AdvanceOnAfter {
		m.CurrentTime = m.CurrentTime.Add(d)
	}
	// Return immediately for testing
	ch := make(chan time.Time, 1)
	ch <- m.CurrentTime
	return ch
}

func (m *MockClock) Sleep(d time.Duration) {
	if m.SleepFunc != nil {
		m.SleepFunc(d)
		return
	}
	// Advance time but don't actually sleep
	m.CurrentTime = m.CurrentTime.Add(d)
}

// Advance moves the clock forward by the specified duration
func (m *MockClock) Advance(d time.Duration) {
	m.CurrentTime = m.CurrentTime.Add(d)
}

// Ensure realClock implements Clock
var _ Clock = realClock{}

// Ensure MockClock implements Clock
var _ Clock = (*MockClock)(nil)
