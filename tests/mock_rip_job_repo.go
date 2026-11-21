package tests

import (
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/navidrome/navidrome/model"
)

// MockRipJobRepo is an in-memory model.RipJobRepository.
type MockRipJobRepo struct {
	mu     sync.Mutex
	jobs   []model.RipJob
	nextID int
}

func CreateMockRipJobRepo() *MockRipJobRepo {
	return &MockRipJobRepo{}
}

func (m *MockRipJobRepo) Put(job *model.RipJob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if job.ID == "" {
		m.nextID++
		job.ID = "rip-" + strconv.Itoa(m.nextID)
	}
	for i := range m.jobs {
		if m.jobs[i].ID == job.ID {
			m.jobs[i] = *job
			return nil
		}
	}
	m.jobs = append(m.jobs, *job)
	return nil
}

func (m *MockRipJobRepo) Get(id string) (*model.RipJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.ID == id {
			return &j, nil
		}
	}
	return nil, model.ErrNotFound
}

// GetAll returns the jobs newest first (insertion order breaks ties).
func (m *MockRipJobRepo) GetAll() (model.RipJobs, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := slices.Clone(m.jobs)
	slices.Reverse(res)
	slices.SortStableFunc(res, func(a, b model.RipJob) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return res, nil
}

func (m *MockRipJobRepo) NextQueued() (*model.RipJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var next *model.RipJob
	for i := range m.jobs {
		if m.jobs[i].Status == model.RipQueued && (next == nil || m.jobs[i].CreatedAt.Before(next.CreatedAt)) {
			next = &m.jobs[i]
		}
	}
	if next == nil {
		return nil, model.ErrNotFound
	}
	j := *next
	return &j, nil
}

func (m *MockRipJobRepo) RequeueInterrupted() (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for i := range m.jobs {
		if m.jobs[i].Status == model.RipDownloading {
			m.jobs[i].Status = model.RipQueued
			m.jobs[i].UpdatedAt = time.Now()
			n++
		}
	}
	return n, nil
}

func (m *MockRipJobRepo) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.jobs {
		if m.jobs[i].ID == id {
			m.jobs = slices.Delete(m.jobs, i, i+1)
			return nil
		}
	}
	return model.ErrNotFound
}

var _ model.RipJobRepository = (*MockRipJobRepo)(nil)
