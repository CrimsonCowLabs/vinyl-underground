import { SimpleForm, Title, useTranslate } from 'react-admin'
import { Card } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import { SelectLanguage } from './SelectLanguage'
import { SelectTheme } from './SelectTheme'
import { SelectAccent } from './SelectAccent'
import { SelectDefaultView } from './SelectDefaultView'
import { NotificationsToggle } from './NotificationsToggle'
import { ListenBrainzScrobbleToggle } from './ListenBrainzScrobbleToggle'
import config from '../config'
import { APP_NAME } from '../consts'
import { ReplayGainToggle } from './ReplayGainToggle'
import { SelectStreamingQuality } from './SelectStreamingQuality'
import { DownloadsStorage } from './DownloadsStorage'
import { AudioEffectsToggle } from './AudioEffectsToggle'
import { isIOS } from '../audioplayer/audioEffects'

const useStyles = makeStyles({
  root: { marginTop: '1em' },
})

const Personal = () => {
  const translate = useTranslate()
  const classes = useStyles()

  return (
    <Card className={classes.root}>
      <Title title={APP_NAME + ' - ' + translate('menu.personal.name')} />
      <SimpleForm toolbar={null} variant={'outlined'}>
        <SelectTheme />
        <SelectAccent />
        <SelectLanguage />
        <SelectDefaultView />
        <SelectStreamingQuality />
        {config.enableReplayGain && <ReplayGainToggle />}
        {isIOS() && <AudioEffectsToggle />}
        <NotificationsToggle />
        {config.listenBrainzEnabled && <ListenBrainzScrobbleToggle />}
        <DownloadsStorage />
      </SimpleForm>
    </Card>
  )
}

export default Personal
