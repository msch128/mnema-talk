import { describe, expect, it } from 'vitest'
import { declaredLayers, layerDetail, layerName, layersKey, layersShape, MIN_LAYER_STEP, planScreenLayers } from './screenLayers'
import { streamBitrate } from './streamQuality'

const capture = (width: number, height: number): MediaTrackSettings => ({ width, height, frameRate: 60 })

describe('planScreenLayers', () => {
  it('sends a 4K capture in the chosen quality and two smaller layers', () => {
    const plans = planScreenLayers({ resolution: 'source', fps: 60 }, capture(3840, 2160))
    expect(plans.map(p => [p.width, p.height, p.fps])).toEqual([[3840, 2160, 60], [1920, 1080, 30], [1280, 720, 30]])
    // The best layer is the share as chosen, the others scale the same capture and cap the frame rate at 30.
    expect(plans.map(p => p.scale)).toEqual([1, 2, 3])
    expect(plans.map(p => p.maxFramerate)).toEqual([60, 30, 30])
    expect(plans[0]?.maxBitrate).toBe(streamBitrate({ resolution: 'source', fps: 60 }, capture(3840, 2160)))
    const [, second, third] = plans
    expect(second && third && third.maxBitrate < second.maxBitrate).toBe(true)
  })

  it('follows the quality when the encoder has to scale the capture', () => {
    const plans = planScreenLayers({ resolution: 1080, fps: 30 }, capture(3840, 2160))
    expect(plans.map(p => [p.width, p.height])).toEqual([[1920, 1080], [1280, 720], [852, 480]])
    expect(plans.map(p => p.scale)).toEqual([2, 3, 4.5])
  })

  it('rounds the width of a scaled picture down to an even size, as encoders do', () => {
    const plans = planScreenLayers({ resolution: 'source', fps: 30 }, capture(1280, 720))
    expect(plans.map(p => [p.width, p.height])).toEqual([[1280, 720], [852, 480], [640, 360]])
  })

  it('leaves out layers that are not meaningfully smaller', () => {
    expect(planScreenLayers({ resolution: 720, fps: 30 }, capture(1280, 720)).map(p => p.height)).toEqual([720, 480, 360])
    expect(planScreenLayers({ resolution: 'source', fps: 30 }, capture(854, 480)).map(p => p.height)).toEqual([480, 360])
    expect(planScreenLayers({ resolution: 'source', fps: 30 }, capture(640, 360)).map(p => p.height)).toEqual([360])
    const tall = planScreenLayers({ resolution: 'source', fps: 30 }, capture(1920, 1080))
    for (let i = 1; i < tall.length; i++) {
      expect((tall[i - 1]?.height ?? 0) / (tall[i]?.height ?? 1)).toBeGreaterThanOrEqual(MIN_LAYER_STEP)
    }
    expect(tall.length).toBeLessThanOrEqual(3)
  })

  it('keeps the frame rate of a share below 30 fps in the lower layers', () => {
    const plans = planScreenLayers({ resolution: 'source', fps: 15 }, capture(1920, 1080))
    expect(plans.map(p => p.fps)).toEqual([15, 15, 15])
  })

  it('plans nothing while the capture size is unknown', () => {
    expect(planScreenLayers({ resolution: 1080, fps: 30 }, {})).toEqual([])
    expect(planScreenLayers({ resolution: 1080, fps: 30 }, { width: 1920 })).toEqual([])
    expect(planScreenLayers({ resolution: 1080, fps: 30 })).toEqual([])
    expect(planScreenLayers('garbage', capture(1920, 1080)).length).toBeGreaterThan(0)
  })
})

describe('layer descriptions', () => {
  const plans = planScreenLayers({ resolution: 'source', fps: 60 }, capture(3840, 2160))

  it('declares only size and rate', () => {
    expect(declaredLayers(plans)).toEqual([{ width: 3840, height: 2160, fps: 60 }, { width: 1920, height: 1080, fps: 30 }, { width: 1280, height: 720, fps: 30 }])
  })

  it('names a layer by its height, with the frame rate above 30', () => {
    expect(layerName({ width: 3840, height: 2160, fps: 60 })).toBe('2160p60')
    expect(layerName({ width: 1280, height: 720, fps: 30 })).toBe('720p')
    expect(layerName({ width: 1280, height: 720, fps: 15 })).toBe('720p')
    expect(layerDetail({ width: 1920, height: 1080, fps: 30 })).toBe('1920×1080 · 30 fps')
  })

  it('tells equal layer lists apart from different ones', () => {
    const declared = declaredLayers(plans)
    expect(layersKey(declared)).toBe(layersKey([...declared]))
    expect(layersKey(declared)).not.toBe(layersKey(declared.slice(0, 2)))
    expect(layersKey([])).toBe('')
    // The shape is the count and the frame rates: new sizes do not change it.
    expect(layersShape(declared)).toBe(layersShape(declared.map(l => ({ ...l, width: l.width + 2, height: l.height + 2 }))))
    expect(layersShape(declared)).not.toBe(layersShape(declared.slice(0, 2)))
    expect(layersShape(declared)).not.toBe(layersShape(declared.map(l => ({ ...l, fps: 24 }))))
    expect(layersShape([])).toBe('')
  })
})
