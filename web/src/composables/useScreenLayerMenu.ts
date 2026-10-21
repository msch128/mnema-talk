// The viewer's quality menu on a screen share I watch: Auto or one of the
// layers its sender declared, labelled with what they really are (see
// lib/screenLayers). The choice lives in the voice store for as long as I
// watch that share; the server forwards exactly that layer to me.
import { computed } from 'vue'
import { useVoiceStore } from '../stores/voice'
import { layerDetail, layerName } from '../lib/screenLayers'
import type { ScreenLayer } from '../types/events'
import { t } from '../i18n'
import type { ContextMenuItem } from '../components/menuTypes'

interface LayerMenuOptions {
  layers: ScreenLayer[]
  choice: number | 'auto'
  receivedLayer?: number | undefined
  nameOf(index: number): string
  choose(next: number | 'auto'): void
}

function buildLayerMenuItems(opts: LayerMenuOptions): ContextMenuItem[] {
  const showing = opts.receivedLayer !== undefined && opts.receivedLayer >= 0 ? opts.nameOf(opts.receivedLayer) : ''
  const items: ContextMenuItem[] = [
    { type: 'label', label: t('talk.layers.title') },
    {
      id: 'layer-auto',
      type: 'radio',
      label: t('talk.layers.auto'),
      subtitle: showing && opts.choice === 'auto' ? t('talk.layers.autoNow', { name: showing }) : t('talk.layers.autoHint'),
      checked: opts.choice === 'auto',
      keepOpen: true,
      action: () => opts.choose('auto')
    }
  ]
  opts.layers.forEach((layer, index) => {
    items.push({
      id: `layer-${index}`,
      type: 'radio',
      label: opts.nameOf(index),
      subtitle: layerDetail(layer),
      checked: opts.choice === index,
      keepOpen: true,
      action: () => opts.choose(index)
    })
  })
  if (typeof opts.choice === 'number' && opts.receivedLayer !== undefined && opts.receivedLayer !== opts.choice) {
    items.push({ type: 'separator' })
    items.push({ type: 'label', label: t('talk.layers.switching') })
  }
  return items
}

export function useScreenLayerMenu(userId: () => string | null) {
  const voiceStore = useVoiceStore()
  const layers = computed<ScreenLayer[]>(() => {
    const id = userId()
    return (id && voiceStore.screenLayers[id]) || []
  })
  /** Only a share with something to choose from has a menu. */
  const available = computed(() => layers.value.length > 1)
  const choice = computed(() => {
    const id = userId()
    return (id ? voiceStore.screenChoice[id] : undefined) ?? 'auto'
  })
  const received = computed(() => {
    const id = userId()
    return id ? voiceStore.receivedLayer[id] : undefined
  })

  function nameOf(index: number): string {
    const layer = layers.value[index]
    if (!layer) return ''
    return index === 0 ? t('talk.layers.source') : layerName(layer)
  }

  /** What the button says: "Auto", or the layer in use. */
  const buttonLabel = computed(() => {
    const now = received.value
    if (choice.value === 'auto') return t('talk.layers.auto')
    return nameOf(now && now.layer >= 0 ? now.layer : choice.value)
  })

  function choose(next: number | 'auto') {
    voiceStore.setScreenChoice(userId(), next)
  }

  /** ContextMenu items; read reactively, so the open menu follows every change. */
  function menuItems(): ContextMenuItem[] {
    const now = received.value
    return buildLayerMenuItems({
      layers: layers.value,
      choice: choice.value,
      receivedLayer: now?.layer,
      nameOf,
      choose
    })
  }

  return { layers, available, choice, received, buttonLabel, menuItems }
}
