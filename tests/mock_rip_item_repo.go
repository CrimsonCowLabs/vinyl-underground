package tests

import (
	"cmp"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/navidrome/navidrome/model"
)

// MockRipItemRepo is an in-memory model.RipItemRepository.
type MockRipItemRepo struct {
	mu     sync.Mutex
	items  []model.RipItem
	nextID int
}

func CreateMockRipItemRepo() *MockRipItemRepo {
	return &MockRipItemRepo{}
}

func (m *MockRipItemRepo) Put(item *model.RipItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if item.ID == "" {
		m.nextID++
		item.ID = "item-" + strconv.Itoa(m.nextID)
	}
	for i := range m.items {
		if m.items[i].ID == item.ID {
			m.items[i] = *item
			return nil
		}
	}
	m.items = append(m.items, *item)
	return nil
}

func (m *MockRipItemRepo) ForJobs(jobIDs ...string) (model.RipItems, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := model.RipItems{}
	for _, it := range m.items {
		if slices.Contains(jobIDs, it.JobID) {
			res = append(res, it)
		}
	}
	slices.SortStableFunc(res, func(a, b model.RipItem) int {
		return cmp.Or(cmp.Compare(a.JobID, b.JobID), cmp.Compare(a.Position, b.Position))
	})
	return res, nil
}

func (m *MockRipItemRepo) RequeueInterrupted() (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for i := range m.items {
		if m.items[i].Status == model.RipDownloading {
			m.items[i].Status = model.RipQueued
			m.items[i].UpdatedAt = time.Now()
			n++
		}
	}
	return n, nil
}

func (m *MockRipItemRepo) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.items {
		if m.items[i].ID == id {
			m.items = slices.Delete(m.items, i, i+1)
			return nil
		}
	}
	return model.ErrNotFound
}

var _ model.RipItemRepository = (*MockRipItemRepo)(nil)
