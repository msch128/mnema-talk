import { describe, expect, it } from 'vitest'
import { CAMERA_TOP_BITRATE, cameraLayersKey, declaredCameraLayers, pictureHeightPx, planCameraLayers } from './cameraLayers'

const settings = (width: number, height: number, frameRate = 30) => ({ width, height, frameRate })

describe('the layers of a camera', () => {
  it('sends a 720p camera as three layers, the lower ones smaller, slower and cheaper', () => {
    const plans = planCameraLayers(settings(1280, 720))
    expect(plans.map(p => [p.width, p.height, p.fps])).toEqual([[1280, 720, 30], [640, 360, 30], [320, 180, 15]])
    expect(plans.map(p => p.scale)).toEqual([1, 2, 4])
    expect(plans.map(p => p.maxBitrate)).toEqual([CAMERA_TOP_BITRATE, 500_000, 150_000])
    expect(plans.map(p => p.maxFramerate)).toEqual([30, 30, 15])
  })

  it('leaves out the layers that are not enough smaller than the one above', () => {
    expect(planCameraLayers(settings(640, 480)).map(p => p.height)).toEqual([480, 360, 180])
    expect(planCameraLayers(settings(640, 360)).map(p => p.height)).toEqual([360, 180])
    expect(planCameraLayers(settings(352, 240)).map(p => p.height)).toEqual([240, 180])
    // Nothing smaller than a thumbnail is worth sending: this is one layer.
    expect(planCameraLayers(settings(320, 180)).map(p => p.height)).toEqual([180])
    expect(planCameraLayers(settings(1920, 1080)).map(p => p.height)).toEqual([1080, 360, 180])
  })

  it('keeps the widths even and the frame rate of a slow camera', () => {
    const plans = planCameraLayers(settings(854, 480, 15))
    expect(plans.map(p => p.width % 2)).toEqual([0, 0, 0])
    expect(plans.map(p => p.fps)).toEqual([15, 15, 15])
  })

  it('has no plan while the capture size is unknown', () => {
    expect(planCameraLayers(undefined)).toEqual([])
    expect(planCameraLayers({})).toEqual([])
    expect(planCameraLayers({ width: 1280 })).toEqual([])
  })

  it('declares size and rate only, and compares declarations', () => {
    const plans = planCameraLayers(settings(1280, 720))
    expect(declaredCameraLayers(plans)).toEqual([{ width: 1280, height: 720, fps: 30 }, { width: 640, height: 360, fps: 30 }, { width: 320, height: 180, fps: 15 }])
    expect(cameraLayersKey(declaredCameraLayers(plans))).toBe('1280x720@30,640x360@30,320x180@15')
    expect(cameraLayersKey([])).toBe('')
  })

  it('counts the picture in device pixels, and none while it is unknown', () => {
    expect(pictureHeightPx(162, 2)).toBe(324)
    expect(pictureHeightPx(99.4, 1)).toBe(99)
    expect(pictureHeightPx(100, 0)).toBe(100)
    expect(pictureHeightPx(0, 2)).toBe(0)
    expect(pictureHeightPx(-5, 2)).toBe(0)
  })
})
