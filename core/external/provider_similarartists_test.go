package external_test

import (
	"context"
	"errors"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/conf/configtest"
	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/core/external"
	"github.com/navidrome/navidrome/core/matcher"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
)

var _ = Describe("Provider - SimilarArtists", func() {
	var (
		ctx        context.Context
		p          external.Provider
		ag         *mockAgents
		artistRepo *mockArtistRepo
	)

	// byName matches the lookup of one artist by name, given as an escaped LIKE pattern.
	byName := func(pattern string) any {
		return mock.MatchedBy(func(opt model.QueryOptions) bool {
			if opt.Filters == nil || opt.Max != 1 {
				return false
			}
			sql, args, err := opt.Filters.ToSql()
			return err == nil && sql == `artist.name LIKE ? ESCAPE '\'` && len(args) == 1 && args[0] == pattern
		})
	}
	// byNames matches the lookup of Last.fm's similar artists by name.
	byNames := mock.MatchedBy(func(opt model.QueryOptions) bool {
		_, ok := opt.Filters.(squirrel.Or)
		return ok
	})
	// byIDs matches the lookup of artists by ID.
	byIDs := mock.MatchedBy(func(opt model.QueryOptions) bool {
		eq, ok := opt.Filters.(squirrel.Eq)
		_, hasID := eq["artist.id"]
		return ok && hasID
	})

	BeforeEach(func() {
		DeferCleanup(configtest.SetupConfig())
		conf.Server.DevArtistInfoTimeToLive = time.Hour
		ctx = GinkgoT().Context()
		artistRepo = newMockArtistRepo()
		ds := &tests.MockDataStore{MockedArtist: artistRepo}
		ag = new(mockAgents)
		p = external.NewProvider(ds, ag, matcher.New(ds), &fakeBroker{})
	})

	It("asks the agents by name for an artist that isn't in the Library, keeping those that are", func() {
		artistRepo.On("GetAll", byName("Sonic Youth")).Return(model.Artists{}, nil).Once()
		ag.On("GetSimilarArtists", ctx, "", "Sonic Youth", "", 100).Return([]agents.Artist{
			{Name: "Dinosaur Jr."}, {Name: "Pixies"}, {Name: "Swans"}, {Name: "Pavement"},
		}, nil).Once()
		artistRepo.On("GetAll", byNames).Return(model.Artists{
			{ID: "ar-pavement", Name: "Pavement"}, {ID: "ar-pixies", Name: "Pixies"},
		}, nil).Once()

		similar, err := p.SimilarArtists(ctx, "Sonic Youth", 5)

		Expect(err).ToNot(HaveOccurred())
		Expect(similar).To(Equal(model.Artists{
			{ID: "ar-pixies", Name: "Pixies"}, {ID: "ar-pavement", Name: "Pavement"},
		}))
		ag.AssertExpectations(GinkgoT())
	})

	It("uses the cached artist info of an artist in the Library, keeping those in the Library", func() {
		updated := time.Now()
		pixies := model.Artist{ID: "ar-pixies", Name: "Pixies", ExternalInfoUpdatedAt: &updated,
			SimilarArtists: model.Artists{{ID: "ar-breeders", Name: "The Breeders"}, {Name: "Not Here"},
				{ID: "ar-pavement", Name: "Pavement"}}}
		artistRepo.On("GetAll", byName("Pixies")).Return(model.Artists{pixies}, nil).Once()
		artistRepo.On("Get", "ar-pixies").Return(&pixies, nil)
		artistRepo.On("GetAll", byIDs).Return(model.Artists{
			{ID: "ar-pavement", Name: "Pavement"}, {ID: "ar-breeders", Name: "The Breeders"},
		}, nil).Once()

		similar, err := p.SimilarArtists(ctx, "Pixies", 1)

		Expect(err).ToNot(HaveOccurred())
		Expect(similar).To(Equal(model.Artists{{ID: "ar-breeders", Name: "The Breeders"}}))
		ag.AssertNotCalled(GinkgoT(), "GetSimilarArtists", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})

	It("matches the name literally, so a % or _ in it is no wildcard", func() {
		artistRepo.On("GetAll", byName(`100\% Pure\_Pop`)).Return(model.Artists{}, nil).Once()
		ag.On("GetSimilarArtists", ctx, "", "100% Pure_Pop", "", 100).Return(nil, agents.ErrNotFound).Once()

		_, err := p.SimilarArtists(ctx, "100% Pure_Pop", 5)

		Expect(err).To(MatchError(agents.ErrNotFound))
		artistRepo.AssertExpectations(GinkgoT())
	})

	It("reports an agent that can't be reached", func() {
		artistRepo.On("GetAll", byName("Sonic Youth")).Return(model.Artists{}, nil).Once()
		ag.On("GetSimilarArtists", ctx, "", "Sonic Youth", "", 100).
			Return(nil, errors.New("dial tcp: no route to host")).Once()

		_, err := p.SimilarArtists(ctx, "Sonic Youth", 5)

		Expect(err).To(HaveOccurred())
	})
})
