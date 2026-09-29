package server

import (
	"log"
	"time"
)

// PruneExpired deletes every vote last active more than retention ago and drops
// its in-memory state (armed timers, per-room locks, live connections).
// Returns how many votes were removed.
func (s *Server) PruneExpired(retention time.Duration) (int, error) {
	slugs, err := s.store.DeleteVotesInactiveSince(s.now().Add(-retention).Unix())
	if err != nil {
		return 0, err
	}
	for _, slug := range slugs {
		s.scheduler.Clear(slug)
		for _, c := range s.hub.connsFor(slug) {
			s.hub.remove(c)
			c.close()
		}
		s.slugMu.Lock()
		delete(s.slugLocks, slug)
		s.slugMu.Unlock()
	}
	return len(slugs), nil
}

// StartPruning runs PruneExpired immediately and then once a day for the
// life of the process. A retention of 0 disables pruning.
func (s *Server) StartPruning(retention time.Duration) {
	if retention <= 0 {
		return
	}
	prune := func() {
		n, err := s.PruneExpired(retention)
		if err != nil {
			log.Printf("quickvote: prune expired votes: %v", err)
		} else if n > 0 {
			log.Printf("quickvote: pruned %d expired vote(s)", n)
		}
	}
	prune()
	go func() {
		for range time.Tick(24 * time.Hour) {
			prune()
		}
	}()
}
