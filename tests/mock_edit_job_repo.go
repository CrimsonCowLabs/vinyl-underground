package tests

import (
	"cmp"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/navidrome/navidrome/model"
)

// MockEditJobRepo is an in-memory model.EditJobRepository.
type MockEditJobRepo struct {
	mu    sync.Mutex
	jobs  []model.EditJob
	steps []model.EditJobStep
	// FailPut, when set, can fail a Put of the job.
	FailPut func(*model.EditJob) error
}

func (m *MockEditJobRepo) Put(job *model.EditJob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailPut != nil {
		if err := m.FailPut(job); err != nil {
			return err
		}
	}
	now := time.Now()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	if job.ID == "" {
		job.ID = "job-" + strconv.Itoa(len(m.jobs)+1)
	}
	c := *job
	c.Steps = nil
	for i := range m.jobs {
		if m.jobs[i].ID == job.ID {
			m.jobs[i] = c
			return nil
		}
	}
	m.jobs = append(m.jobs, c)
	return nil
}

func (m *MockEditJobRepo) Get(id string) (*model.EditJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.ID == id {
			return &j, nil
		}
	}
	return nil, model.ErrNotFound
}

func (m *MockEditJobRepo) GetAll(limit int) (model.EditJobs, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := slices.Clone(model.EditJobs(m.jobs))
	slices.Reverse(res)
	slices.SortStableFunc(res, func(a, b model.EditJob) int { return b.CreatedAt.Compare(a.CreatedAt) })
	if limit > 0 && len(res) > limit {
		res = res[:limit]
	}
	return res, nil
}

func (m *MockEditJobRepo) Next() (*model.EditJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, state := range []model.EditJobState{model.EditJobRunning, model.EditJobQueued} {
		for _, j := range m.jobs {
			if j.State == state {
				return &j, nil
			}
		}
	}
	return nil, model.ErrNotFound
}

func (m *MockEditJobRepo) PutStep(step *model.EditJobStep) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if step.CreatedAt.IsZero() {
		step.CreatedAt = now
	}
	step.UpdatedAt = now
	if step.ID == "" {
		step.ID = "step-" + strconv.Itoa(len(m.steps)+1)
	}
	for i := range m.steps {
		if m.steps[i].ID == step.ID {
			m.steps[i] = *step
			return nil
		}
	}
	m.steps = append(m.steps, *step)
	return nil
}

func (m *MockEditJobRepo) Steps(jobID string, states ...model.EditStepState) (model.EditJobSteps, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := model.EditJobSteps{}
	for _, s := range m.steps {
		if s.JobID == jobID && (len(states) == 0 || slices.Contains(states, s.State)) {
			res = append(res, s)
		}
	}
	slices.SortStableFunc(res, func(a, b model.EditJobStep) int { return cmp.Compare(a.Position, b.Position) })
	return res, nil
}
