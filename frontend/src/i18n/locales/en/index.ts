import adminAccessKeys from './adminAccessKeys'
import mcp from './mcp'
import desktop from './desktop'
import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import channelMonitorV2 from './channelMonitorV2'
import batchImage from './batchImage'
import imageStudio from './imageStudio'
import admin from './admin'
import misc from './misc'
import tickets from './tickets'

export default {
  adminAccessKeys,
  mcp,
  desktop,
  ...landing,
  ...common,
  ...dashboard,
  ...channelMonitorV2,
  ...batchImage,
  ...imageStudio,
  admin,
  ...misc,
  ...tickets,
}
