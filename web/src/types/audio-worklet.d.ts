// Web Audio specification AudioWorkletGlobalScope; lib.dom omits its processor.
interface AudioParamDescriptor {
  name: string
  defaultValue?: number
  minValue?: number
  maxValue?: number
  automationRate?: AutomationRate
}
declare abstract class AudioWorkletProcessor {
  readonly port: MessagePort
  abstract process(inputs: Float32Array[][], outputs: Float32Array[][], parameters: Record<string, Float32Array>): boolean
}
declare function registerProcessor(name: string, processorCtor: {
  new (options?: AudioWorkletNodeOptions): AudioWorkletProcessor
  readonly parameterDescriptors?: readonly AudioParamDescriptor[]
}): void
declare const currentFrame: number
declare const currentTime: number
declare const sampleRate: number
