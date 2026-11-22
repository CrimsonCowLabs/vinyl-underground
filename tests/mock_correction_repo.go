package tests

import (
	"slices"
	"sync"
	"time"

	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
)

// MockCorrectionRepo keeps Corrections in memory. Folders maps a Correction ID to the folders
// FolderIDs returns, and AlbumFolders an album Correction's key to the folders AlbumFolderIDs
// returns.
type MockCorrectionRepo struct {
	mu           sync.Mutex
	All          model.Corrections
	Folders      map[string][]string
	AlbumFolders map[string][]string
	Err          error
}

func (m *MockCorrectionRepo) GetAll() (model.Corrections, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.All), m.Err
}

func (m *MockCorrectionRepo) Get(id string) (*model.Correction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	for _, c := range m.All {
		if c.ID == id {
			return &c, nil
		}
	}
	return nil, model.ErrNotFound
}

func (m *MockCorrectionRepo) GetByMatchKey(kind model.CorrectionKind, key string) (*model.Correction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.All {
		if c.Kind == kind && c.MatchKey == key {
			return &c, nil
		}
	}
	return nil, model.ErrNotFound
}

func (m *MockCorrectionRepo) Add(c *model.Correction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	for _, o := range m.All {
		if o.Kind == c.Kind && o.MatchKey == c.MatchKey {
			return model.ErrAlreadyExists
		}
	}
	c.ID = id.NewRandom()
	c.CreatedAt = time.Now()
	m.All = append(m.All, *c)
	return nil
}

func (m *MockCorrectionRepo) SetUndoing(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.All {
		if m.All[i].ID == id {
			m.All[i].Undoing = true
			return nil
		}
	}
	return model.ErrNotFound
}

func (m *MockCorrectionRepo) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.All)
	m.All = slices.DeleteFunc(m.All, func(c model.Correction) bool { return c.ID == id })
	if len(m.All) == n {
		return model.ErrNotFound
	}
	return nil
}

func (m *MockCorrectionRepo) FolderIDs(id string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Folders[id], m.Err
}

// AlbumFolderIDs returns AlbumFolders[key], or just key.
func (m *MockCorrectionRepo) AlbumFolderIDs(key string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.AlbumFolders[key]; ok {
		return f, m.Err
	}
	return []string{key}, m.Err
}

var _ model.CorrectionRepository = (*MockCorrectionRepo)(nil)

func (m *MockCorrectionRepo) MergeArtistAnnotations(string, string, string) error { return m.Err }

func (m *MockCorrectionRepo) SplitArtistAnnotations(string, string, string) error { return m.Err }
