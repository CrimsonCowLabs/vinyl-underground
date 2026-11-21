package model

import "time"

// DJ is a persona a Listener created to host their Stations (#17), from a name and a
// personality description. Built-in DJs aren't stored; they live in core/stations. A
// Listener's DJs are private to them.
type DJ struct {
	ID          string    `structs:"id" json:"id"`
	UserID      string    `structs:"user_id" json:"-"`
	Name        string    `structs:"name" json:"name"`
	Description string    `structs:"description" json:"description"`
	CreatedAt   time.Time `structs:"created_at" json:"createdAt"`
	UpdatedAt   time.Time `structs:"updated_at" json:"updatedAt"`
}

type DJs []DJ

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
