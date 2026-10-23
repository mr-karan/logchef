package victorialogs

import (
	"context"
	"sync"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// CachedSourceConnectionStatus returns the last health result without backend I/O.
func (p *Provider) CachedSourceConnectionStatus(sourceID models.SourceID) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.health[sourceID]
}

func (p *Provider) recordHealth(sourceID models.SourceID, connected bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.sources[sourceID]; exists && !p.closed {
		p.health[sourceID] = connected
	}
}

// StartBackgroundHealthChecks starts one owned loop for all registered sources.
func (p *Provider) StartBackgroundHealthChecks(interval time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.healthStarted || p.closed {
		return
	}
	p.healthStarted = true
	if interval <= 0 {
		interval = 30 * time.Second
	}
	p.healthWG.Go(func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			p.checkAllSourcesHealth()
			select {
			case <-ticker.C:
			case <-p.healthCtx.Done():
				return
			}
		}
	})
}

func (p *Provider) checkAllSourcesHealth() {
	p.mu.RLock()
	ids := make([]models.SourceID, 0, len(p.sources))
	for id := range p.sources {
		ids = append(ids, id)
	}
	p.mu.RUnlock()
	jobs := make(chan models.SourceID)
	var workers sync.WaitGroup
	for range min(32, len(ids)) {
		workers.Go(func() {
			for id := range jobs {
				p.checkRegisteredSourceHealth(p.healthCtx, id)
			}
		})
	}
	for _, id := range ids {
		select {
		case jobs <- id:
		case <-p.healthCtx.Done():
			close(jobs)
			workers.Wait()
			return
		}
	}
	close(jobs)
	workers.Wait()
}

func (p *Provider) checkRegisteredSourceHealth(ctx context.Context, sourceID models.SourceID) {
	ctx, cancel := context.WithTimeout(ctx, defaultHealthTimeout)
	defer cancel()
	unlock, err := p.opLocks.lockContext(ctx, sourceID)
	if err != nil {
		return
	}
	defer unlock()
	p.mu.RLock()
	conn, exists := p.sources[sourceID]
	closed := p.closed
	p.mu.RUnlock()
	if !exists || closed {
		return
	}
	p.recordHealth(sourceID, p.checkHealth(ctx, sourceID, conn) == nil)
}

// Close cancels health checks and waits for their workers before clearing source state.
func (p *Provider) Close() error {
	p.mu.Lock()
	p.closed = true
	p.cancelHealth()
	p.mu.Unlock()
	p.healthWG.Wait()
	p.client.CloseIdleConnections()
	p.mu.Lock()
	clear(p.sources)
	clear(p.health)
	p.mu.Unlock()
	return nil
}
