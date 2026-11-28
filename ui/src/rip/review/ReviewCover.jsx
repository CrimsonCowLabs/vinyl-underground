import React, { useState } from 'react'
import { useTranslate } from 'react-admin'
import { makeStyles } from '@material-ui/core/styles'
import AlbumIcon from '@material-ui/icons/Album'
import subsonic from '../../subsonic'
import { baseUrl } from '../../utils'

// COVER_SIZE is the size asked of getCoverArt: twice the thumbnail's, for sharp high-DPI screens.
const COVER_SIZE = 128

const useStyles = makeStyles(
  (theme) => ({
    cover: {
      flex: '0 0 auto',
      width: 56,
      height: 56,
      // Level with the album fields' outlines (margin="dense")
      marginTop: theme.spacing(1),
      borderRadius: theme.shape.borderRadius,
      border: `1px solid ${theme.palette.divider}`,
      objectFit: 'cover',
      boxSizing: 'border-box',
    },
    placeholder: {
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      color: theme.palette.text.disabled,
      backgroundColor: theme.palette.action.hover,
    },
  }),
  { name: 'NDReviewCover' },
)

// coverUrl is getCoverArt's URL for a review card's cover. loadedAt (when the card last took in
// its review) is in it, so every reload of the card asks again rather than showing a cover the
// browser kept from before.
const coverUrl = (cover, loadedAt) =>
  baseUrl(
    subsonic.url('getCoverArt', cover.artworkId, {
      size: COVER_SIZE,
      _: loadedAt,
    }),
  )

// CoverImage is the cover itself, or the placeholder if it won't load. ReviewCover keys it by its
// URL, so a new URL tries again.
const CoverImage = ({ src, placeholder }) => {
  const translate = useTranslate()
  const classes = useStyles()
  const [failed, setFailed] = useState(false)
  if (failed) return placeholder
  return (
    <img
      src={src}
      alt={translate('resources.rip.review.cover')}
      className={classes.cover}
      onError={() => setFailed(true)}
    />
  )
}

// ReviewCover is a review card's album cover thumbnail (#218), so the admin can tell before
// Promote whether the album has art. A card with no single album (not scanned yet, or Tracks in
// several albums), an album known to have no cover, or a cover that won't load shows a
// placeholder instead.
const ReviewCover = ({ cover, loadedAt }) => {
  const translate = useTranslate()
  const classes = useStyles()
  const placeholder = (
    <div
      role="img"
      aria-label={translate('resources.rip.review.noCover')}
      title={translate('resources.rip.review.noCover')}
      className={`${classes.cover} ${classes.placeholder}`}
    >
      <AlbumIcon />
    </div>
  )
  if (!cover?.artworkId || cover.absent) return placeholder
  const src = coverUrl(cover, loadedAt)
  return <CoverImage key={src} src={src} placeholder={placeholder} />
}

export default ReviewCover
