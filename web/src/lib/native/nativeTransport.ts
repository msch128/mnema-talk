import { invoke, Channel } from '@tauri-apps/api/core'
import { connectNative, disconnectNative, initializeNativeContext } from './nativeFlow'
import { nativeApiTransport } from './nativeRoutes'
import { bindNativePort, installNativePort } from './nativeSession'
import { NativeSocket } from './nativeSocket'
import { captureNativeTrustPresentation } from './nativeTrustSession'
import type { NativeNotice } from './nativeSession'

export type { NativeChannel, NativeInvoke, NativeNotice, NativePort, NativeReply } from './nativeSession'
export {
  NativeSocket,
  captureNativeTrustPresentation,
  connectNative,
  disconnectNative,
  initializeNativeContext,
  installNativePort,
  nativeApiTransport,
}

bindNativePort({
  invoke,
  channel: () => new Channel<NativeNotice>(),
  trustChannel: () => new Channel<unknown>(),
  chatChannel: () => new Channel<unknown>(),
})
