// mnema-talk-sdk: the official TypeScript SDK for Mnema Talk bots.
export {
  ActivityType,
  API_BASE_PATH,
  ButtonStyle,
  CallbackType,
  ComponentType,
  GatewayCloseCode,
  GatewayOp,
  Intents,
  InteractionType,
  MessageFlags,
  OptionType,
  TextInputStyle,
} from './constants.ts'
export type {
  ActivityTypeValue,
  ButtonStyleValue,
  CallbackTypeValue,
  ComponentTypeValue,
  IntentBit,
  IntentName,
  InteractionTypeValue,
  OptionTypeValue,
  TextInputStyleValue,
} from './constants.ts'

export { Client, gatewayUrlFor, type ClientEvents, type ClientOptions } from './client/client.ts'
export { CommandManager } from './client/commands.ts'
export { Interaction, type EditData, type ReplyData } from './client/interaction.ts'

export { RestClient } from './rest/client.ts'
export { RestError } from './rest/error.ts'
export type { RateLimitInfo, RequestOptions, RestOptions } from './rest/core.ts'

export { Gateway, type GatewayCloseInfo, type GatewayEvents, type GatewayOptions } from './gateway/gateway.ts'
export { GatewayError, closeAction, type BackoffOptions, type CloseAction } from './gateway/policy.ts'
export { FrameTooLargeError, MAX_FRAME_BYTES } from './gateway/outbox.ts'
export type { SocketCloseEvent, WebSocketFactory, WebSocketLike } from './gateway/socket.ts'

export {
  actionRow,
  button,
  channelSelect,
  linkButton,
  roleSelect,
  stringSelect,
  userSelect,
  type ButtonOptions,
  type LinkButtonOptions,
  type SelectOptions,
  type StringSelectOptionInput,
} from './builders/components.ts'
export { modal, textInput, type ModalOptions, type TextInputOptions } from './builders/modal.ts'
export { ValidationError, validateMessageComponents, validateModal } from './builders/validate.ts'
export { validateCommand, validateCommands } from './builders/commands.ts'

export { addBotLink, decodeManifest, encodeManifest, type Manifest, type ManifestPermission } from './manifest.ts'
export { Emitter } from './emitter.ts'
export type { Logger } from './util.ts'
export { VERSION } from './version.ts'

export type * from './types/messages.ts'
export type * from './types/components.ts'
export type * from './types/gateway.ts'
export type * from './types/commands.ts'
