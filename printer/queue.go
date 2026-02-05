package printer

import (
	"context"
	"errors"
	"sync"
)

type Job struct {
	Request PrintRequest
	Result  chan error
}

type Manager struct {
	tmpDir   string
	mu       sync.Mutex
	queues   map[string]chan Job
	capacity int
}

func NewManager(tmpDir string, capacity int) *Manager {
	return &Manager{
		tmpDir:   tmpDir,
		queues:   make(map[string]chan Job),
		capacity: capacity,
	}
}

func (m *Manager) Enqueue(ctx context.Context, printer string, request PrintRequest) error {
	queue := m.getQueue(printer)
	job := Job{Request: request, Result: make(chan error, 1)}

	select {
	case queue <- job:
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-job.Result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) getQueue(printer string) chan Job {
	m.mu.Lock()
	defer m.mu.Unlock()

	if queue, ok := m.queues[printer]; ok {
		return queue
	}

	capacity := m.capacity
	if capacity <= 0 {
		capacity = 100
	}

	queue := make(chan Job, capacity)
	m.queues[printer] = queue
	go m.worker(printer, queue)
	return queue
}

func (m *Manager) worker(printer string, queue chan Job) {
	for job := range queue {
		err := PrintHTML(context.Background(), m.tmpDir, job.Request)
		if errors.Is(err, ErrUnsupported) {
			job.Result <- err
			continue
		}
		job.Result <- err
	}
}
