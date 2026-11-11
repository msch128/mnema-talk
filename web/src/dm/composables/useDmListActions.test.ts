import { describe, it, expect, beforeEach, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from '../../stores/chat'
import { useToastStore } from '../../stores/toast'
import { t } from '../../i18n'
import type { ChatDirectMessage } from '../../types/rest'

const dmFixture = (o: { id: string }) => o as ChatDirectMessage
import { useDmListActions } from './useDmListActions'

beforeEach(() => {
  setActivePinia(createPinia())
  vi.spyOn(console, 'warn').mockImplementation(() => {})
})

describe('muting a conversation', () => {
  it('says so in a toast when the store reports a failure without throwing', async () => {
    const chat = useChatStore()
    const toasts = useToastStore()
    vi.spyOn(chat, 'setNotificationLevel').mockResolvedValue(false)
    await useDmListActions().toggleMute(dmFixture({ id: 'd1' }))
    expect(toasts.toasts.map(x => x.text)).toEqual([t('notifications.serverSaveFailed')])
  })

  it('stays quiet when it went through', async () => {
    const chat = useChatStore()
    const toasts = useToastStore()
    const set = vi.spyOn(chat, 'setNotificationLevel').mockResolvedValue(true)
    await useDmListActions().toggleMute(dmFixture({ id: 'd1' }))
    expect(set).toHaveBeenCalledWith('d1', 'mute')
    expect(toasts.toasts).toEqual([])
  })

  it('still reports a thrown list change with the list message', async () => {
    const chat = useChatStore()
    const toasts = useToastStore()
    vi.spyOn(chat, 'toggleFavourite').mockRejectedValue(new Error('down'))
    await useDmListActions().toggleFavourite(dmFixture({ id: 'd1' }))
    expect(toasts.toasts.map(x => x.text)).toEqual([t('dm.prefsFailed')])
  })
})
