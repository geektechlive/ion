import './state'
import { wireSessionPlaneEvents, wireEngineBridgeEvents, wireRemoteSessionPlaneForwarding } from './event-wiring'
import { registerAllIpc } from './ipc/register'
import { setupAppLifecycle } from './app-lifecycle'
import { initAutoUpdater } from './updater'

wireSessionPlaneEvents()
wireEngineBridgeEvents()
wireRemoteSessionPlaneForwarding()
registerAllIpc()
setupAppLifecycle()
initAutoUpdater()
