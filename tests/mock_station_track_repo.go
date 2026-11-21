package tests

import (
	"slices"
	"sync"
	"time"

	"github.com/navidrome/navidrome/model"
)

// MockStationTrackRepo is an in-memory model.StationTrackRepository for a single Listener.
type MockStationTrackRepo struct {
	mu     sync.Mutex
	tracks model.StationTracks
}

func CreateMockStationTrackRepo() *MockStationTrackRepo {
	return &MockStationTrackRepo{}
}

func (m *MockStationTrackRepo) Add(tracks ...model.StationTrack) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tracks = append(m.tracks, tracks...)
	return nil
}

func (m *MockStationTrackRepo) Since(since time.Time) (model.StationTracks, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res model.StationTracks
	for _, t := range m.tracks {
		if !t.ServedAt.Before(since) {
			res = append(res, t)
		}
	}
	slices.Reverse(res)
	return res, nil
}

func (m *MockStationTrackRepo) DeleteBefore(t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tracks = slices.DeleteFunc(m.tracks, func(s model.StationTrack) bool { return s.ServedAt.Before(t) })
	return nil
}

var _ model.StationTrackRepository = (*MockStationTrackRepo)(nil)
