import React, { useCallback, useState } from 'react'
import {
  ReferenceManyField,
  ShowContextProvider,
  useShowContext,
  useShowController,
  useTranslate,
  Title as RaTitle,
} from 'react-admin'
import { makeStyles } from '@material-ui/core/styles'
import { Alert } from '@material-ui/lab'
import AlbumSongs from './AlbumSongs'
import AlbumDetails from './AlbumDetails'
import AlbumActions from './AlbumActions'
import { useResourceRefresh, useScrollRestoration, Title } from '../common'

const useStyles = makeStyles(
  (theme) => ({
    albumActions: {
      width: '100%',
    },
  }),
  {
    name: 'NDAlbumShow',
  },
)

const AlbumShowLayout = (props) => {
  const { loading, ...context } = useShowContext(props)
  const { record } = context
  const classes = useStyles()
  useResourceRefresh('album', 'song')
  useScrollRestoration(!!record?.id)

  return (
    <>
      {record && <RaTitle title={<Title subTitle={record.name} />} />}
      {record && <AlbumDetails {...context} />}
      {record && (
        <ReferenceManyField
          {...context}
          addLabel={false}
          reference="song"
          target="album_id"
          sort={{ field: 'album', order: 'ASC' }}
          perPage={0}
          pagination={null}
        >
          <AlbumSongs
            resource={'song'}
            exporter={false}
            album={record}
            actions={
              <AlbumActions className={classes.albumActions} record={record} />
            }
          />
        </ReferenceManyField>
      )}
    </>
  )
}

// An edit to the album's title or album artist gives it a new ID, so once its job's rescan is done
// this album is gone and the page's reload of it fails. The page stays, saying so, rather than
// react-admin's sending the admin to the album list from wherever the edit had left them.
const AlbumShow = (props) => {
  const translate = useTranslate()
  const [goneId, setGoneId] = useState(null)
  const onFailure = useCallback(() => setGoneId(props.id), [props.id])
  const controllerProps = useShowController({ ...props, onFailure })
  if (goneId !== null && goneId === props.id) {
    return (
      <Alert severity="info" style={{ margin: 16 }}>
        {translate('resources.album.gone')}
      </Alert>
    )
  }
  return (
    <ShowContextProvider value={controllerProps}>
      <AlbumShowLayout {...props} {...controllerProps} />
    </ShowContextProvider>
  )
}

export default AlbumShow
