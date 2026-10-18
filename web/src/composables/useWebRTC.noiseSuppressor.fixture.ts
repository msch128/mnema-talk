import { vi } from 'vitest'
import type { NoiseModel, FilterOptions } from '../lib/dfnTypes'

// The AI filter worklets can't run in jsdom; tests hand in a fake node instead.
export interface SuppressorDouble { connect: ReturnType<typeof vi.fn>; disconnect: ReturnType<typeof vi.fn>; destroy: ReturnType<typeof vi.fn> }
export const suppressor: { node: SuppressorDouble | null; models: NoiseModel[]; options: FilterOptions[]; create: (() => Promise<SuppressorDouble | null>) | null; supported: boolean } = { node: null, models: [], options: [], create: null, supported: false }
export const noiseSuppressorModule = {
  isNoiseSuppressionSupported: () => suppressor.supported || suppressor.node !== null,
  preloadNoiseSuppressor: () => {},
  createNoiseSuppressorNode: async (_ctx: AudioContext, model: NoiseModel, options: FilterOptions) => {
    suppressor.models.push(model)
    suppressor.options.push(options)
    return suppressor.create ? suppressor.create() : suppressor.node
  }
}
