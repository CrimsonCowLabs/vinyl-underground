package tests

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
	"github.com/navidrome/navidrome/model/request"
)

// MockDJRepo is an in-memory store of every Listener's DJs. ForUser gives the
// model.DJRepository one Listener sees, which, like the real one, only holds their own.
type MockDJRepo struct {
	mu  sync.Mutex
	djs model.DJs
	Err error // returned by every call when set
}

func CreateMockDJRepo() *MockDJRepo {
	return &MockDJRepo{}
}

// ForUser returns the Listener's view of the DJs.
func (m *MockDJRepo) ForUser(userID string) model.DJRepository {
	return &mockUserDJRepo{all: m, userID: userID}
}

type mockUserDJRepo struct {
	all    *MockDJRepo
	userID string
}

func (r *mockUserDJRepo) GetAll() (model.DJs, error) {
	m := r.all
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	var res model.DJs
	for _, dj := range m.djs {
		if dj.UserID == r.userID {
			res = append(res, dj)
		}
	}
	slices.SortStableFunc(res, func(a, b model.DJ) int {
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return res, nil
}

func (r *mockUserDJRepo) Get(djID string) (*model.DJ, error) {
	m := r.all
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	if i := r.find(djID); i >= 0 {
		dj := m.djs[i]
		return &dj, nil
	}
	return nil, model.ErrNotFound
}

func (r *mockUserDJRepo) Put(dj *model.DJ) error {
	m := r.all
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	now := time.Now()
	if dj.ID == "" {
		dj.ID = id.NewRandom()
		dj.UserID = r.userID
		dj.CreatedAt, dj.UpdatedAt = now, now
		m.djs = append(m.djs, *dj)
		return nil
	}
	i := r.find(dj.ID)
	if i < 0 {
		return model.ErrNotFound
	}
	dj.UserID, dj.CreatedAt, dj.UpdatedAt = r.userID, m.djs[i].CreatedAt, now
	m.djs[i] = *dj
	return nil
}

func (r *mockUserDJRepo) Delete(djID string) error {
	m := r.all
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	i := r.find(djID)
	if i < 0 {
		return model.ErrNotFound
	}
	m.djs = slices.Delete(m.djs, i, i+1)
	return nil
}

// find returns the index of the Listener's DJ with the id, or -1. The lock must be held.
func (r *mockUserDJRepo) find(djID string) int {
	return slices.IndexFunc(r.all.djs, func(dj model.DJ) bool { return dj.ID == djID && dj.UserID == r.userID })
}

func (db *MockDataStore) DJ(ctx context.Context) model.DJRepository {
	db.repoMu.Lock()
	defer db.repoMu.Unlock()
	if db.MockedDJ == nil && db.RealDS != nil {
		return db.RealDS.DJ(ctx)
	}
	if db.MockedDJ == nil {
		db.MockedDJ = CreateMockDJRepo()
	}
	usr, _ := request.UserFrom(ctx)
	return db.MockedDJ.ForUser(usr.ID)
}

var _ model.DJRepository = (*mockUserDJRepo)(nil)
