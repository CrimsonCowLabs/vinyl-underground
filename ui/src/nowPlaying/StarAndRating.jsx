import React from 'react'
import { useTranslate } from 'react-admin'
import { IconButton } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import Rating from '@material-ui/lab/Rating'
import StarIcon from '@material-ui/icons/Star'
import StarBorderIcon from '@material-ui/icons/StarBorder'
import { useRating } from '../common/useRating'
import { useToggleLove } from '../common/useToggleLove'

const useStyles = makeStyles(
  (theme) => ({
    starred: { color: theme.palette.primary.main },
    rating: {
      color: theme.palette.primary.main,
      marginLeft: -2,
    },
    empty: { color: theme.palette.text.disabled },
  }),
  { name: 'NDNowPlayingStar' },
)

// StarToggle stars or unstars the Track, drawn as a star (filled when
// starred), and saves it to the server as the rest of the app does.
export const StarToggle = ({ record }) => {
  const classes = useStyles()
  const translate = useTranslate()
  const [toggleStar, toggling] = useToggleLove('song', record)
  const starred = !!record.starred
  return (
    <IconButton
      aria-label={translate('nowPlaying.star')}
      aria-pressed={starred}
      disabled={toggling || !record.id}
      onClick={toggleStar}
      className={starred ? classes.starred : undefined}
    >
      {starred ? (
        <StarIcon data-testid="star-filled" />
      ) : (
        <StarBorderIcon data-testid="star-outlined" />
      )}
    </IconButton>
  )
}

// TrackRating rates the Track from one to five stars; choosing the rating it
// has clears it.
export const TrackRating = ({ record }) => {
  const classes = useStyles()
  const translate = useTranslate()
  const [rate, rating] = useRating('song', record)
  return (
    <Rating
      name="now-playing-rating"
      className={classes.rating}
      classes={{ iconEmpty: classes.empty }}
      value={rating || 0}
      disabled={!record.id || record.missing}
      emptyIcon={<StarIcon fontSize="inherit" />}
      getLabelText={(value) =>
        translate('nowPlaying.rating', { smart_count: value })
      }
      onChange={(e, value) => rate(value ?? 0, record.id)}
    />
  )
}
