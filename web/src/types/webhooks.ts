// Webhook shapes of the REST API under friendly names.
export type {
  WebhookWebhook as Webhook, WebhookCreatedWebhook as CreatedWebhook,
  WebhookOutgoing as OutgoingWebhook, WebhookCreatedOutgoing as CreatedOutgoingWebhook,
  WebhookDelivery as WebhookDelivery,
} from './rest'

export {
  isWebhookWebhook as isWebhook, isWebhookOutgoing as isOutgoingWebhook, isWebhookDelivery,
  decodeWebhookCreatedWebhook as decodeCreatedWebhook, decodeWebhookWebhook as decodeWebhook,
  decodeWebhookCreatedOutgoing as decodeCreatedOutgoingWebhook, decodeWebhookOutgoing as decodeOutgoingWebhook,
  decodeWebhookDelivery,
} from './rest'

/** Events a channel webhook can subscribe to, in display order. */
export const CHANNEL_WEBHOOK_EVENTS = ['MESSAGE_CREATE', 'MESSAGE_UPDATE', 'MESSAGE_DELETE', 'MESSAGE_REACTIONS_UPDATE', 'VOICE_STATE_UPDATE'] as const
