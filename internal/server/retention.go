package server

import (
	"log"
	"time"
)

// PruneExpired deletes every vote last active more than retention ago and drops
// its in-memory state (armed timers, per-room locks, live connections).
// Returns how many votes were removed.
func (s *Server) PruneExpired(retention time.Duration) (int, error) {
	slugs, unlinked, err := s.store.DeleteVotesInactiveSince(s.now().Add(-retention).Unix())
	if err != nil {
		return 0, err
	}
	// Sources that just lost their (pruned) successor: refresh their live
	// sockets so nobody keeps a banner linking to a vote that is gone.
	// broadcast, not changed — pruning is not activity on the source.
	defer func() {
		for _, slug := range unlinked {
			s.broadcast(slug)
		}
	}()
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

// checkpointInterval is how often the WAL is folded back into the main
// database file, so a crash or hard kill never leaves more than about an
// hour of writes living only in the -wal file.
const checkpointInterval = time.Hour

// StartBackground runs the server's maintenance loop until Close: it prunes
// expired votes immediately and then daily (skipped entirely when retention
// <= 0, which keeps votes forever) and checkpoints the SQLite WAL hourly.
// Real tickers are used deliberately — the cadence isn't under test, and
// PruneExpired itself runs on the injected clock.
func (s *Server) StartBackground(retention time.Duration) {
	prune := func() {
		if retention <= 0 {
			return
		}
		n, err := s.PruneExpired(retention)
		if err != nil {
			log.Printf("quickvote: prune expired votes: %v", err)
		} else if n > 0 {
			log.Printf("quickvote: pruned %d expired vote(s)", n)
		}
	}
	prune()
	go func() {
		pruneTick := time.NewTicker(24 * time.Hour)
		defer pruneTick.Stop()
		checkpointTick := time.NewTicker(checkpointInterval)
		defer checkpointTick.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-pruneTick.C:
				prune()
			case <-checkpointTick.C:
				if err := s.store.Checkpoint(); err != nil {
					log.Printf("quickvote: wal checkpoint: %v", err)
				}
			}
		}
	}()
}
