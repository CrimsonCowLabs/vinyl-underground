package stations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/navidrome/navidrome/model"
)

// A Listener can create their own DJs from a name and a personality description (#17). They
// are private to that Listener and kept in the dj table; the built-in DJs are read-only. Station
// history (station_track, station_skip) only holds a DJ's id, never a reference to its row, so
// deleting a DJ leaves play history as it is.

const (
	// MaxDJNameLength is the longest DJ name accepted, in characters.
	MaxDJNameLength = 60
	// MaxDJDescriptionLength is the longest DJ personality description accepted, in characters.
	MaxDJDescriptionLength = 1000
)

var (
	ErrBuiltInDJ            = errors.New("built-in DJs can't be edited or deleted")
	ErrInvalidDJName        = fmt.Errorf("a DJ needs a name of at most %d characters", MaxDJNameLength)
	ErrInvalidDJDescription = fmt.Errorf("a DJ needs a personality description of at most %d characters", MaxDJDescriptionLength)
)

// listenerTaste is how a Listener's own DJ chooses Tracks without an LLM. Its personality is
// free text the rules can't read, so it leans on what the Listener loves and plays, with room
// for Tracks they haven't heard. With an LLM, its name and description choose instead.
var listenerTaste = Taste{
	FavoriteWeight: 2.5,
	HistoryWeight:  1.5,
	ArtistWeight:   2,
	UnplayedWeight: 1,
}

func (s *service) DJs(ctx context.Context) ([]DJ, error) {
	own, err := s.ds.DJ(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("listing the Listener's DJs: %w", err)
	}
	res := DJs()
	for _, dj := range own {
		res = append(res, listenerDJ(dj))
	}
	return res, nil
}

func (s *service) DJ(ctx context.Context, id string) (DJ, error) {
	if dj, ok := FindDJ(id); ok {
		return dj, nil
	}
	if id == "" {
		return DJ{}, ErrUnknownDJ
	}
	dj, err := s.ds.DJ(ctx).Get(id)
	if errors.Is(err, model.ErrNotFound) {
		return DJ{}, ErrUnknownDJ
	}
	if err != nil {
		return DJ{}, fmt.Errorf("loading the DJ: %w", err)
	}
	return listenerDJ(*dj), nil
}

func (s *service) CreateDJ(ctx context.Context, name, description string) (DJ, error) {
	dj, err := cleanDJ(name, description)
	if err != nil {
		return DJ{}, err
	}
	if err := s.ds.DJ(ctx).Put(&dj); err != nil {
		return DJ{}, fmt.Errorf("saving the DJ: %w", err)
	}
	return listenerDJ(dj), nil
}

func (s *service) UpdateDJ(ctx context.Context, id, name, description string) (DJ, error) {
	if _, ok := FindDJ(id); ok {
		return DJ{}, ErrBuiltInDJ
	}
	if id == "" {
		return DJ{}, ErrUnknownDJ
	}
	dj, err := cleanDJ(name, description)
	if err != nil {
		return DJ{}, err
	}
	dj.ID = id
	err = s.ds.DJ(ctx).Put(&dj)
	if errors.Is(err, model.ErrNotFound) {
		return DJ{}, ErrUnknownDJ
	}
	if err != nil {
		return DJ{}, fmt.Errorf("saving the DJ: %w", err)
	}
	return listenerDJ(dj), nil
}

func (s *service) DeleteDJ(ctx context.Context, id string) error {
	if _, ok := FindDJ(id); ok {
		return ErrBuiltInDJ
	}
	err := s.ds.DJ(ctx).Delete(id)
	if errors.Is(err, model.ErrNotFound) {
		return ErrUnknownDJ
	}
	if err != nil {
		return fmt.Errorf("deleting the DJ: %w", err)
	}
	return nil
}

// cleanDJ tidies a DJ's name (one line, single spaces) and description (trimmed), and checks
// both are there and not too long.
func cleanDJ(name, description string) (model.DJ, error) {
	name = strings.Join(strings.Fields(name), " ")
	description = strings.TrimSpace(description)
	if name == "" || utf8.RuneCountInString(name) > MaxDJNameLength {
		return model.DJ{}, ErrInvalidDJName
	}
	if description == "" || utf8.RuneCountInString(description) > MaxDJDescriptionLength {
		return model.DJ{}, ErrInvalidDJDescription
	}
	return model.DJ{Name: name, Description: description}, nil
}

func listenerDJ(dj model.DJ) DJ {
	return DJ{ID: dj.ID, Name: dj.Name, Description: dj.Description, Taste: listenerTaste}
}
