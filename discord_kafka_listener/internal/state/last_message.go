package state

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog"
)

type LastMessageStore struct {
	path   string
	log    zerolog.Logger
	mu     sync.Mutex
	lastID string
}

func NewLastMessageStore(path string, log zerolog.Logger) *LastMessageStore {
	store := &LastMessageStore{
		path: strings.TrimSpace(path),
		log:  log,
	}
	if store.path == "" {
		return store
	}
	data, err := os.ReadFile(store.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			store.log.Warn().Err(err).Str("path", store.path).Msg("failed to read last message id file")
		}
		return store
	}
	value := strings.TrimSpace(string(data))
	if value != "" {
		store.lastID = value
	}
	return store
}

func (s *LastMessageStore) Get() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastID
}

func (s *LastMessageStore) UpdateIfNewer(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastID != "" && !snowflakeGreater(id, s.lastID) {
		return
	}
	s.lastID = id
	if s.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		s.log.Warn().Err(err).Str("path", s.path).Msg("failed to create last message id directory")
		return
	}
	if err := os.WriteFile(s.path, []byte(id+"\n"), 0o644); err != nil {
		s.log.Warn().Err(err).Str("path", s.path).Msg("failed to write last message id file")
	}
}

func snowflakeGreater(a, b string) bool {
	ai, aerr := strconv.ParseUint(a, 10, 64)
	bi, berr := strconv.ParseUint(b, 10, 64)
	if aerr == nil && berr == nil {
		return ai > bi
	}
	return a > b
}
