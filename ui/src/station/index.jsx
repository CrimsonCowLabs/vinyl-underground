import React from 'react'
import HeadsetIcon from '@material-ui/icons/Headset'
import HeadsetOutlinedIcon from '@material-ui/icons/HeadsetOutlined'
import DynamicMenuIcon from '../layout/DynamicMenuIcon'
import StationList from './StationList'

export { stationReducer } from './stationState'
export { useStationRefill } from './useStationRefill'

export default {
  list: StationList,
  icon: (
    <DynamicMenuIcon
      path={'station'}
      icon={HeadsetOutlinedIcon}
      activeIcon={HeadsetIcon}
    />
  ),
}
