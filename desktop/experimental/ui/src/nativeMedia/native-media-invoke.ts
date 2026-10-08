import { Channel, invoke } from '@tauri-apps/api/core'
import type { BoundMedia, NativeMediaBridge, NativeMediaEvent } from './native-media-channel'
// Commands exist only in the separately compiled native synthetic-media fixture.
// Renderer inputs contain no origin/account/channel/key/grant fields.
export function createNativeMediaBridge(): NativeMediaBridge {
  const listeners = new Set<(event: NativeMediaEvent) => void>()
  return {
    async listen(receive) { listeners.add(receive); return () => { listeners.delete(receive) } },
    async open(role) {
      const onEvent = new Channel<NativeMediaEvent>()
      onEvent.onmessage = event => { for (const receive of listeners) receive(event) }
      return invoke<BoundMedia>('native_fixture_media_open', { role, onEvent })
    },
    async send(binding, action) { await invoke<void>('native_fixture_media_send', { handle: binding.handle, generation: binding.generation, action }) },
    async close(binding) { await invoke<void>('native_fixture_media_close', { handle: binding.handle, generation: binding.generation }) }
  }
}
