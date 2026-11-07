import type { CompatBaseline } from '../types'
import v100 from './v1.0.0.json'
import v200 from './v2.0.0.json'
import v2912 from './v2.9.12.json'
import v2918 from './v2.9.18.json'

/** The releases the current web app must still work against, oldest first. */
export const allBaselines = [v100, v200, v2912, v2918] as unknown as CompatBaseline[]
