package tests

import (
	"slices"
	"sync"
	"time"

	"github.com/navidrome/navidrome/model"
)

// MockStationSkipRepo is an in-memory model.StationSkipRepository for a single Listener.
type MockStationSkipRepo struct {
	mu    sync.Mutex
	skips model.StationSkips
}

func CreateMockStationSkipRepo() *MockStationSkipRepo {
	return &MockStationSkipRepo{}
}

func (m *MockStationSkipRepo) Add(s model.StationSkip) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.skips = append(m.skips, s)
	return nil
}

func (m *MockStationSkipRepo) Since(djID string, since time.Time) (model.StationSkips, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res model.StationSkips
	for _, s := range m.skips {
		if s.DJID == djID && !s.SkippedAt.Before(since) {
			res = append(res, s)
		}
	}
	slices.Reverse(res)
	return res, nil
}

func (m *MockStationSkipRepo) DeleteBefore(t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.skips = slices.DeleteFunc(m.skips, func(s model.StationSkip) bool { return s.SkippedAt.Before(t) })
	return nil
}

var _ model.StationSkipRepository = (*MockStationSkipRepo)(nil)
