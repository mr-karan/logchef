package db

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/mr-karan/logchef/pkg/config"
)

type ConnectionStatus struct {
	IsConnected bool
	LastError   error
	LastChecked time.Time
}

type SourceConnection struct {
	Conn   clickhouse.Conn
	Status ConnectionStatus
}

type ConnectionPool struct {
	mu          sync.RWMutex
	connections map[string]*SourceConnection
	stopChan    chan struct{}
}

func NewConnectionPool() *ConnectionPool {
	pool := &ConnectionPool{
		connections: make(map[string]*SourceConnection),
		stopChan:    make(chan struct{}),
	}
	go pool.startHealthCheck()
	return pool
}

func (p *ConnectionPool) startHealthCheck() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.checkConnections()
		case <-p.stopChan:
			return
		}
	}
}

func (p *ConnectionPool) checkConnections() {
	p.mu.RLock()
	defer p.mu.RUnlock()

	ctx := context.Background()
	for sourceID, conn := range p.connections {
		err := conn.Conn.Ping(ctx)
		status := ConnectionStatus{
			IsConnected: err == nil,
			LastError:   err,
			LastChecked: time.Now(),
		}
		p.connections[sourceID].Status = status
	}
}

func (p *ConnectionPool) AddConnection(sourceID string, cfg config.ClickhouseConfig) error {
	opts := &clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)},
		Auth: clickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.Username,
			Password: cfg.Password,
		},
		DialTimeout: cfg.DialTimeout,
		Compression: &clickhouse.Compression{
			Method: clickhouse.CompressionZSTD,
		},
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return fmt.Errorf("failed to open ClickHouse connection: %w", err)
	}

	// Fail fast if the connection is not healthy
	err = conn.Ping(context.Background())
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("failed to ping ClickHouse connection: %w", err)
	}

	status := ConnectionStatus{
		IsConnected: err == nil,
		LastError:   err,
		LastChecked: time.Now(),
	}

	p.mu.Lock()
	p.connections[sourceID] = &SourceConnection{
		Conn:   conn,
		Status: status,
	}
	p.mu.Unlock()

	return nil
}

func (p *ConnectionPool) GetConnection(sourceID string) (clickhouse.Conn, error) {
	p.mu.RLock()
	conn, exists := p.connections[sourceID]
	p.mu.RUnlock()

	if !exists {
		return nil, ErrConnectionNotFound
	}

	if !conn.Status.IsConnected {
		return nil, ErrConnectionUnhealthy
	}

	return conn.Conn, nil
}

func (p *ConnectionPool) RemoveConnection(sourceID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	conn, exists := p.connections[sourceID]
	if !exists {
		return nil
	}

	if err := conn.Conn.Close(); err != nil {
		return err
	}

	delete(p.connections, sourceID)
	return nil
}

func (p *ConnectionPool) Stop() {
	close(p.stopChan)

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, conn := range p.connections {
		_ = conn.Conn.Close()
	}
}

func (p *ConnectionPool) GetStatus(sourceID string) (ConnectionStatus, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	conn, exists := p.connections[sourceID]
	if !exists {
		return ConnectionStatus{}, ErrConnectionNotFound
	}

	return conn.Status, nil
}
