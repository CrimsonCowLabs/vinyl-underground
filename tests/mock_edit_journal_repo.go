package tests

import (
	"maps"
	"strconv"
	"sync"
	"time"

	"github.com/navidrome/navidrome/model"
)

// MockEditJournalRepo is an in-memory model.EditJournalRepository. Err, when set, fails Put.
type MockEditJournalRepo struct {
	mu      sync.Mutex
	entries []model.EditEntry
	Err     error
}

func (m *MockEditJournalRepo) Put(e *model.EditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	now := time.Now()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	if e.ID == "" {
		e.ID = "edit-" + strconv.Itoa(len(m.entries)+1)
	}
	c := *e
	c.OriginalTags, c.Changes = maps.Clone(e.OriginalTags), maps.Clone(e.Changes)
	for i := range m.entries {
		if m.entries[i].ID == e.ID {
			m.entries[i] = c
			return nil
		}
	}
	m.entries = append(m.entries, c)
	return nil
}

func (m *MockEditJournalRepo) Get(id string) (*model.EditEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.entries {
		if e.ID == id {
			return &e, nil
		}
	}
	return nil, model.ErrNotFound
}

func (m *MockEditJournalRepo) Pending() ([]model.EditEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := []model.EditEntry{}
	for _, e := range m.entries {
		if e.State == model.EditPending {
			res = append(res, e)
		}
	}
	return res, nil
}

func (m *MockEditJournalRepo) LastWritten(libraryID int, path string) (*model.EditEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.entries) - 1; i >= 0; i-- {
		e := m.entries[i]
		if e.State == model.EditWritten && e.LibraryID == libraryID && e.NewPath == path {
			return &e, nil
		}
	}
	return nil, model.ErrNotFound
}

func (m *MockEditJournalRepo) ForJob(jobID string) ([]model.EditEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := []model.EditEntry{}
	for _, e := range m.entries {
		if e.JobID == jobID {
			res = append(res, e)
		}
	}
	return res, nil
}

func (m *MockEditJournalRepo) After(e *model.EditEntry) ([]model.EditEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := []model.EditEntry{}
	seen := false
	for _, l := range m.entries {
		if l.ID == e.ID {
			seen = true
			continue
		}
		sameFile := (e.MediaFileID != "" && l.MediaFileID == e.MediaFileID) ||
			(l.LibraryID == e.LibraryID && (l.Path == e.NewPath || l.NewPath == e.NewPath))
		if seen && sameFile && l.State != model.EditFailed {
			res = append(res, l)
		}
	}
	return res, nil
}

// GetAll returns the entries newest first, paged by Max and Offset (other options are ignored).
func (m *MockEditJournalRepo) GetAll(options ...model.QueryOptions) ([]model.EditEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := []model.EditEntry{}
	for i := len(m.entries) - 1; i >= 0; i-- {
		res = append(res, m.entries[i])
	}
	if len(options) > 0 {
		res = res[min(options[0].Offset, len(res)):]
		if options[0].Max > 0 {
			res = res[:min(options[0].Max, len(res))]
		}
	}
	return res, nil
}

func (m *MockEditJournalRepo) CountAll(...model.QueryOptions) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.entries)), nil
}

// All returns every entry, in the order they were first recorded.
func (m *MockEditJournalRepo) All() []model.EditEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.EditEntry(nil), m.entries...)
}
