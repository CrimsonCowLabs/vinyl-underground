import React from 'react'
import { Route } from 'react-router-dom'
import Personal from './personal/Personal'
import DownloadsPage from './download/DownloadsPage'

const routes = [
  <Route exact path="/personal" render={() => <Personal />} key={'personal'} />,
  <Route
    exact
    path="/downloads"
    render={() => <DownloadsPage />}
    key={'downloads'}
  />,
]

export default routes
