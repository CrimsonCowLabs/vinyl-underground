import React from 'react'
import { Title, useTranslate } from 'react-admin'
import { Card, CardActions, CardContent } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import { DownloadsStorage } from '../personal/DownloadsStorage'
import { ShuffleDownloadsButton } from './ShuffleDownloadsButton'

const useStyles = makeStyles({
  root: { marginTop: '1em' },
})

// DownloadsPage is what the phone's Downloads tab opens: shuffle everything
// downloaded on this device, or open one of the Downloads, each with the
// space it uses. It works offline.
export const DownloadsPage = () => {
  const translate = useTranslate()
  const classes = useStyles()
  return (
    <Card className={classes.root}>
      <Title title={translate('download.storage.title')} />
      <CardActions>
        <ShuffleDownloadsButton />
      </CardActions>
      <CardContent>
        <DownloadsStorage />
      </CardContent>
    </Card>
  )
}

export default DownloadsPage
