package server

import (
	"context"
	"time"
)

const (
	tokenLastUsedInterval = time.Minute
	tokenLastUsedCapacity = 4096
)

func (s *Server) recordTokenLastUsed(id int) {
	s.backgroundMu.Lock()
	defer s.backgroundMu.Unlock()
	select {
	case <-s.stop:
		s.tokenDropped.Add(1)
		return
	default:
	}
	now := time.Now()
	if last, ok := s.tokenLastQueued[id]; ok && now.Sub(last) < tokenLastUsedInterval {
		return
	}
	if len(s.tokenLastQueued) >= tokenLastUsedCapacity {
		for key, last := range s.tokenLastQueued {
			if now.Sub(last) >= tokenLastUsedInterval {
				delete(s.tokenLastQueued, key)
			}
		}
		if len(s.tokenLastQueued) >= tokenLastUsedCapacity {
			s.tokenDropped.Add(1)
			return
		}
	}
	select {
	case s.tokenQueue <- id:
		s.tokenLastQueued[id] = now
	default:
		s.tokenDropped.Add(1)
	}
}

func (s *Server) startTokenLastUsedWorker() {
	s.startBackgroundTask(func() {
		for {
			select {
			case <-s.stop:
				s.tokenDropped.Add(uint64(len(s.tokenQueue)))
				return
			case id := <-s.tokenQueue:
				ctx, cancel := context.WithTimeout(s.baseCtx, time.Second)
				err := s.store.UpdateAPITokenLastUsed(ctx, id)
				cancel()
				if err != nil {
					s.tokenDropped.Add(1)
				}
			}
		}
	})
}
