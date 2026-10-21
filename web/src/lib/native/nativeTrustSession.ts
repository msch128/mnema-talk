import { createTrustPort } from '../trustPort'
import { captureSelector, selectorCurrent } from './nativeSession'

function trustArgs(profile: string, authentication: string, args: Record<string, unknown>): Record<string, unknown> {
  return { ...args, profileIntent: profile, authenticationIntent: authentication }
}

/** Public presentation captured once; native scope and OS dialog grant trust. */
export function captureNativeTrustPresentation(): ReturnType<typeof createTrustPort> {
  const selector = captureSelector()
  const channel = selector.port.trustChannel
  const owner = selector.owner
  const profile = selector.profile
  const authentication = selector.authentication
  if (!owner || !profile) throw new Error('Native trust presentation unavailable')
  if (!authentication || !channel) throw new Error('Native trust presentation unavailable')
  return createTrustPort({
    context: owner,
    isCurrent: () => selectorCurrent(selector),
    invoke: (command, args) => selector.port.invoke(command, trustArgs(profile, authentication, args)),
    channel,
  })
}
