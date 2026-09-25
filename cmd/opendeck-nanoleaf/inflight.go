package main

import "sync"

// inflightSet remembers which buttons have a device call in progress, so a
// second press during that time is dropped instead of racing the first:
// two overlapping toggles would both read "off" and both switch on.
//
// A map guarded by a mutex is the plain Go way to share a set between
// goroutines. The zero value is ready to use.
type inflightSet struct {
	mu   sync.Mutex
	busy map[string]bool
}

// begin marks a button busy and reports whether it was free.
func (s *inflightSet) begin(buttonContext string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy == nil {
		s.busy = map[string]bool{}
	}
	if s.busy[buttonContext] {
		return false
	}
	s.busy[buttonContext] = true
	return true
}

func (s *inflightSet) end(buttonContext string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.busy, buttonContext)
}
