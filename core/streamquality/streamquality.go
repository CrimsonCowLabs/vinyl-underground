// Package streamquality decides the default streaming quality for the web app: Opus at
// ~160 kbps on a phone, the original File on a desktop, unless the Listener's own setting
// says otherwise. See docs/adr/0007-phone-streams-opus-by-default.md.
//
// It does not stream anything itself. It returns a model.Transcoding that callers put in
// the request context, where the existing transcode decision code honours it exactly like
// a Transcoding an admin assigned to a player.
package streamquality

import (
	"context"
	"errors"
	"slices"

	ua "github.com/mileusna/useragent"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

// WebAppClient is the Subsonic client name the web app (PWA) sends with every request.
const WebAppClient = "NavidromeUI"

// Preference is a Listener's streaming-quality setting.
type Preference string

const (
	// Auto streams Opus at ~160 kbps on a phone and the original File on a desktop.
	Auto Preference = "auto"
	// Original always streams the original File (when the browser can play it).
	Original Preference = "original"
	// DataSaver always streams Opus at ~160 kbps, on any device.
	DataSaver Preference = "dataSaver"
)

// Preferences lists every valid Preference.
var Preferences = []Preference{Auto, Original, DataSaver}

var ErrInvalidPreference = errors.New("invalid streaming quality preference")

const (
	userPropKey = "streamingQuality"
	dataFormat  = "opus"
	dataBitRate = 160 // kbps
)

type Service interface {
	// Transcoding returns the Transcoding to force on a stream request from this client,
	// or nil to let the usual negotiation stream the original File.
	Transcoding(ctx context.Context, listenerID, client, userAgent string) (*model.Transcoding, error)
	// Preference returns the Listener's setting, Auto when they never chose one.
	Preference(ctx context.Context, listenerID string) (Preference, error)
	// SetPreference stores the Listener's setting.
	SetPreference(ctx context.Context, listenerID string, pref Preference) error
}

func New(ds model.DataStore) Service {
	return &service{ds: ds}
}

type service struct {
	ds model.DataStore
}

func (s *service) Transcoding(ctx context.Context, listenerID, client, userAgent string) (*model.Transcoding, error) {
	// Native Subsonic clients have their own quality settings and their User-Agent rarely
	// says which device they run on, so they keep Navidrome's normal behaviour.
	if client != WebAppClient {
		return nil, nil
	}
	pref, err := s.Preference(ctx, listenerID)
	if err != nil {
		return nil, err
	}
	if pref == Original || (pref == Auto && !isPhone(userAgent)) {
		return nil, nil
	}
	trc, err := s.ds.Transcoding(ctx).FindByFormat(dataFormat)
	if errors.Is(err, model.ErrNotFound) {
		log.Warn(ctx, "No Opus transcoding configured, streaming the original File instead", "listenerID", listenerID)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	dataSaver := *trc
	dataSaver.DefaultBitRate = dataBitRate
	return &dataSaver, nil
}

func (s *service) Preference(ctx context.Context, listenerID string) (Preference, error) {
	v, err := s.ds.UserProps(ctx).DefaultGet(listenerID, userPropKey, string(Auto))
	if err != nil {
		return Auto, err
	}
	pref := Preference(v)
	if !slices.Contains(Preferences, pref) {
		return Auto, nil
	}
	return pref, nil
}

func (s *service) SetPreference(ctx context.Context, listenerID string, pref Preference) error {
	if !slices.Contains(Preferences, pref) {
		return ErrInvalidPreference
	}
	return s.ds.UserProps(ctx).Put(listenerID, userPropKey, string(pref))
}

// isPhone reports whether the User-Agent comes from a phone (or tablet) operating system.
// iPadOS Safari reports itself as macOS by default, so iPads count as desktops.
func isPhone(userAgent string) bool {
	u := ua.Parse(userAgent)
	return u.OS == ua.Android || u.OS == ua.IOS
}
