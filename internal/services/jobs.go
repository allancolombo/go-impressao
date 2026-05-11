package services

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/goopedir/go-impressao/internal/models"
)

type PrintStatus string

const (
	StatusPendente   PrintStatus = "pendente"
	StatusImprimindo PrintStatus = "imprimindo"
	StatusImpresso   PrintStatus = "impresso"
	StatusErro       PrintStatus = "erro"
)

// Job representa uma comanda recebida, sua pré-visualização e o estado do processo de impressão.
type Job struct {
	ID          string
	CreatedAt   time.Time
	Payload     models.ImpressaoCozinhaRequest
	PreviewText string

	Status      PrintStatus
	PrintedAt   *time.Time
	ErrorPublic string
}

// JobStore armazena jobs em memória (adequado para o serviço simples/local).
type JobStore struct {
	mu   sync.RWMutex
	jobs map[string]*Job

	seenUUIDs map[string]struct{}
}

func NewJobStore() *JobStore {
	return &JobStore{
		jobs:      make(map[string]*Job),
		seenUUIDs: make(map[string]struct{}),
	}
}

func (s *JobStore) Create(payload models.ImpressaoCozinhaRequest, previewText string) *Job {
	job := &Job{
		ID:          newJobID(),
		CreatedAt:   time.Now(),
		Payload:     payload,
		PreviewText: previewText,
		Status:      StatusPendente,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = job
	return job
}

func (s *JobStore) Get(id string) (*Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	return job, ok
}

func (s *JobStore) Update(id string, fn func(j *Job)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok {
		return errors.New("job não encontrado")
	}

	fn(job)
	return nil
}

func (s *JobStore) MarkUUID(uuid string) (already bool) {
	u := strings.ToLower(strings.TrimSpace(uuid))
	if u == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.seenUUIDs[u]; ok {
		return true
	}
	s.seenUUIDs[u] = struct{}{}
	return false
}

func (s *JobStore) ReserveUUIDs(uuids []string) (duplicate string, ok bool) {
	uniq := make(map[string]struct{}, len(uuids))
	for _, u := range uuids {
		u = strings.ToLower(strings.TrimSpace(u))
		if u == "" {
			continue
		}
		uniq[u] = struct{}{}
	}
	if len(uniq) == 0 {
		return "", true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for u := range uniq {
		if _, exists := s.seenUUIDs[u]; exists {
			return u, false
		}
	}
	for u := range uniq {
		s.seenUUIDs[u] = struct{}{}
	}
	return "", true
}

func newJobID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000000")))
	}
	return hex.EncodeToString(b[:])
}
