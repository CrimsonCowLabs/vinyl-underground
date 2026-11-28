import React from 'react'
import PropTypes from 'prop-types'
import { useTranslate } from 'react-admin'
import { CircularProgress } from '@material-ui/core'
import { Alert } from '@material-ui/lab'
import { FindOnMusicBrainz } from './FindOnMusicBrainz'
import { ReleasePairing } from './ReleasePairing'

// Find on MusicBrainz and, once a release is picked, how the album's Tracks pair with it and the
// options to tick (or why the pairing couldn't be had), for the lookup useReleaseLookup keeps.
export const ReleaseLookup = ({ lookup, disabled }) => {
  const translate = useTranslate()
  const { album, release, pick, match, matchError, options, setOptions } =
    lookup
  return (
    <>
      <FindOnMusicBrainz
        albumId={album.id}
        trackCount={album.trackCount}
        artist={album.artist}
        title={album.title}
        compilation={!!album.compilation}
        picked={release}
        onPick={pick}
        disabled={disabled}
      />
      {release && !match && !matchError && (
        <CircularProgress size={16} data-testid="mbz-pairing-loading" />
      )}
      {matchError && (
        <Alert severity="warning" style={{ marginTop: 8 }}>
          {translate(`resources.album.musicbrainz.errors.${matchError}`)}
        </Alert>
      )}
      {release && match && (
        <ReleasePairing
          match={match}
          options={options}
          onChange={setOptions}
          disabled={disabled}
        />
      )}
    </>
  )
}

ReleaseLookup.propTypes = {
  lookup: PropTypes.shape({
    album: PropTypes.shape({
      id: PropTypes.string,
      trackCount: PropTypes.number,
      artist: PropTypes.string.isRequired,
      title: PropTypes.string.isRequired,
      compilation: PropTypes.bool,
    }).isRequired,
    release: PropTypes.object,
    pick: PropTypes.func.isRequired,
    match: PropTypes.object,
    matchError: PropTypes.string,
    options: PropTypes.object.isRequired,
    setOptions: PropTypes.func.isRequired,
  }).isRequired,
  disabled: PropTypes.bool,
}

export default ReleaseLookup
