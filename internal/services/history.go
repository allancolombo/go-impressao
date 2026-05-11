package services

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/goopedir/go-impressao/internal/models"
)

type HistoryRecord struct {
	ID               string                     `json:"id"`
	OccurredAt       time.Time                  `json:"occurred_at"`
	Status           PrintStatus                `json:"status"`
	Erro             string                     `json:"erro,omitempty"`
	Canal            string                     `json:"canal,omitempty"`
	Tipo             string                     `json:"tipo"`
	Numero           int                        `json:"numero"`
	Usuario          string                     `json:"usuario"`
	ImpressoraERP    string                     `json:"impressora_erp"`
	ImpressoraWindows string                    `json:"impressora_windows"`
	Resumo           string                     `json:"resumo"`
	TextoImpressao   string                     `json:"texto_impressao"`
	Payload          models.ImpressaoCozinhaRequest `json:"payload"`
}

type HistoryListItem struct {
	ID         string      `json:"id"`
	OccurredAt time.Time   `json:"occurred_at"`
	Status     PrintStatus `json:"status"`
	Canal      string      `json:"canal,omitempty"`
	Tipo       string      `json:"tipo"`
	Numero     int         `json:"numero"`
	Usuario    string      `json:"usuario"`
	Resumo     string      `json:"resumo"`
}

type HistoryFilter struct {
	From   *time.Time
	To     *time.Time
	Canal  *string
	Numero *int
	Status *PrintStatus
}

type HistoryStore struct {
	logger  *log.Logger
	path    string
	enabled bool

	mu      sync.RWMutex
	records []HistoryRecord
}

func NewHistoryStore(logger *log.Logger) *HistoryStore {
	return &HistoryStore{
		logger:  logger,
		path:    defaultHistoryPath(),
		enabled: true,
	}
}

func (s *HistoryStore) Init() {
	if !s.enabled {
		return
	}
	if err := s.load(); err != nil {
		s.logger.Printf("historico: falha ao carregar: %v", err)
	}
}

func (s *HistoryStore) Add(r HistoryRecord) error {
	if !s.enabled {
		return nil
	}

	r.ID = strings.TrimSpace(r.ID)
	if r.ID == "" {
		r.ID = strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	if r.OccurredAt.IsZero() {
		r.OccurredAt = time.Now()
	}
	if strings.TrimSpace(r.Canal) == "" {
		r.Canal = "cozinha"
	}

	if err := s.appendToFile(r); err != nil {
		s.logger.Printf("historico: falha ao persistir registro id=%s: %v", r.ID, err)
		return err
	}

	s.mu.Lock()
	s.records = append(s.records, r)
	s.mu.Unlock()
	return nil
}

func (s *HistoryStore) Get(id string) (HistoryRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := len(s.records) - 1; i >= 0; i-- {
		if s.records[i].ID == id {
			return s.records[i], true
		}
	}
	return HistoryRecord{}, false
}

func (s *HistoryStore) List(filter HistoryFilter, cursor int, limit int) ([]HistoryListItem, *int) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if cursor < 0 {
		cursor = 0
	}

	s.mu.RLock()
	recs := s.records
	s.mu.RUnlock()

	var items []HistoryListItem
	skipped := 0

	for i := len(recs) - 1; i >= 0; i-- {
		r := recs[i]
		if !matchHistoryFilter(r, filter) {
			continue
		}
		if skipped < cursor {
			skipped++
			continue
		}

		items = append(items, HistoryListItem{
			ID:         r.ID,
			OccurredAt: r.OccurredAt,
			Status:     r.Status,
			Canal:      effectiveCanal(r),
			Tipo:       r.Tipo,
			Numero:     r.Numero,
			Usuario:    r.Usuario,
			Resumo:     r.Resumo,
		})

		if len(items) >= limit {
			next := cursor + len(items)
			return items, &next
		}
	}

	return items, nil
}

func effectiveCanal(r HistoryRecord) string {
	c := strings.TrimSpace(strings.ToLower(r.Canal))
	if c == "" {
		return "cozinha"
	}
	return c
}

func (s *HistoryStore) HasPrintedProdutoUUID(uuid string) bool {
	u := strings.ToLower(strings.TrimSpace(uuid))
	if u == "" {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for i := len(s.records) - 1; i >= 0; i-- {
		r := s.records[i]
		if r.Status != StatusImpresso {
			continue
		}
		if effectiveCanal(r) != "cozinha" {
			continue
		}
		for _, p := range r.Payload.Produtos {
			if strings.ToLower(strings.TrimSpace(p.UUID)) == u {
				return true
			}
		}
	}
	return false
}

func matchHistoryFilter(r HistoryRecord, f HistoryFilter) bool {
	if f.From != nil && r.OccurredAt.Before(*f.From) {
		return false
	}
	if f.To != nil && r.OccurredAt.After(*f.To) {
		return false
	}
	if f.Canal != nil {
		want := strings.TrimSpace(strings.ToLower(*f.Canal))
		if want != "" && effectiveCanal(r) != want {
			return false
		}
	}
	if f.Numero != nil && r.Numero != *f.Numero {
		return false
	}
	if f.Status != nil && r.Status != *f.Status {
		return false
	}
	return true
}

func defaultHistoryPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "history.jsonl"
	}
	return filepath.Join(dir, "go-impressao", "history.jsonl")
}

func (s *HistoryStore) appendToFile(r HistoryRecord) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("não foi possível criar diretório do histórico: %w", err)
	}

	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("não foi possível abrir arquivo do histórico: %w", err)
	}
	defer f.Close()

	b, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("não foi possível serializar registro: %w", err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("não foi possível gravar registro: %w", err)
	}
	return nil
}

func (s *HistoryStore) load() error {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	var loaded []HistoryRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var r HistoryRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			s.logger.Printf("historico: linha inválida ignorada: %v", err)
			continue
		}
		if strings.TrimSpace(r.ID) == "" {
			continue
		}
		loaded = append(loaded, r)
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	s.records = loaded
	s.mu.Unlock()
	return nil
}

func BuildResumo(payload models.ImpressaoCozinhaRequest) string {
	if len(payload.Produtos) == 0 {
		return ""
	}
	var parts []string
	for i, p := range payload.Produtos {
		if i >= 3 {
			parts = append(parts, "…")
			break
		}
		name := strings.TrimSpace(p.Nome)
		if name == "" {
			continue
		}
		if p.Quantidade > 1 {
			parts = append(parts, fmt.Sprintf("%dUn %s", p.Quantidade, name))
		} else {
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, ", ")
}

func ParseCursor(s string) (int, error) {
	if strings.TrimSpace(s) == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, errors.New("cursor inválido")
	}
	if n < 0 {
		return 0, errors.New("cursor inválido")
	}
	return n, nil
}
