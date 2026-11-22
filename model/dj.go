package model

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// DJ is a persona a Listener created to shape their Stations (#17), from a name and a
// personality description. Built-in DJs aren't stored; they live in core/stations. A
// Listener's DJs are private to them.
type DJ struct {
	ID          string `structs:"id" json:"id"`
	UserID      string `structs:"user_id" json:"-"`
	Name        string `structs:"name" json:"name"`
	Description string `structs:"description" json:"description"`
	// Genres are the Library genres the DJ plays without an LLM, by their names in the
	// Library's genre tags. GenreSource says where they came from.
	Genres      DJGenres      `structs:"genres" json:"genres"`
	GenreSource DJGenreSource `structs:"genre_source" json:"genreSource"`
	CreatedAt   time.Time     `structs:"created_at" json:"-"`
	UpdatedAt   time.Time     `structs:"updated_at" json:"-"`
}

type DJs []DJ

// DJGenreSource says where a DJ's genres came from, so a later edit knows whether it may
// work them out again.
type DJGenreSource string

const (
	// DJGenresUnset is a DJ whose genres were never worked out (one saved before #106).
	DJGenresUnset DJGenreSource = ""
	// DJGenresFromWords are genres matched from the words of the DJ's name and description,
	// without an LLM or when it failed, so the LLM is asked again on the DJ's next use. They are
	// matched again whenever those change.
	DJGenresFromWords DJGenreSource = "words"
	// DJGenresFromLLM are genres the LLM mapped the DJ's name and description to (#107). They
	// are mapped again whenever those change.
	DJGenresFromLLM DJGenreSource = "llm"
	// DJGenresByListener are genres the Listener picked by hand. Editing the DJ's name or
	// description leaves them alone.
	DJGenresByListener DJGenreSource = "listener"
)

// DJGenres are stored as a JSON array.
type DJGenres []string

func (g DJGenres) Value() (driver.Value, error) {
	if len(g) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal([]string(g))
	return string(b), err
}

func (g *DJGenres) Scan(src any) error {
	*g = DJGenres{}
	return jsonScan(src, (*[]string)(g))
}

// DJRepository holds the logged-in Listener's own DJs. Each Listener only sees and changes
// their own: another Listener's DJ is ErrNotFound to them.
type DJRepository interface {
	// GetAll returns the Listener's DJs by name.
	GetAll() (DJs, error)
	// Get returns the Listener's DJ with the id.
	Get(id string) (*DJ, error)
	// Put creates the DJ for the Listener when it has no id, or updates the Listener's DJ with
	// its id (ErrNotFound when they have none with that id).
	Put(dj *DJ) error
	// Delete removes the Listener's DJ with the id (ErrNotFound when they have none).
	Delete(id string) error
}
