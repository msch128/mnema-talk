// Friendly names for the actual REST schemas, not looser UI lookalikes.
export type {
  AuthUser as User, AuthAdminUser as AdminUser, AuthInvite as Invite,
   
   
  AuthCreateInviteRequest as CreateInviteRequest, AuthUpdateInviteRequest as UpdateInviteRequest,
  ChatMember as Member,
  PermRole as Role,  PermOverwrite as Overwrite,
  RolesPermissionsInfo as PermissionsInfo, 
  ChatChannel as Channel, ChatChannelType as ChannelType, ChatCategory as Category,
  ChatChannelHierarchy as ChannelHierarchy, ChatMessage as Message, ChatForumTag as ForumTag,
  ChatMediaAttachment as MediaAttachment, ChatReactionSummary as ReactionSummary,
   ChatReplyPreview as ReplyPreview,
  ChatReadState as ReadState, ChatNotifyLevel as NotifyLevel,
  ChatNotificationSettings as NotificationSettings, 
  
   
  ChatCreateMessageRequest as CreateMessageRequest,
  ChatPoll as Poll,  PollDetail, PollCreateRequest,
  
  
  ChatLayoutRequest as LayoutRequest,
  LinkpreviewPreview as LinkPreview,
  MediaDashboardItem as MediaDashboardItem, 
  ServerSystemStatus as SystemStatus, ServerUpdateStatus as UpdateStatus,
  ServerOpsStatus as OpsStatus, ServerOpsSample as OpsSample,
  ServerOperatorStatus as OperatorStatus,
  ServerSelfUpdateStatus as SelfUpdateStatus, 
  
  CommunityeventsCommunityEvent as CommunityEvent,
  
  CommunityeventsRSVPItem as RSVPItem,
  
  
  
  CommunityeventsUserRef as EventUserRef,
} from './rest'

export {
  decodeAuthUser as decodeUser, 
  decodeAuthInvite as decodeInvite, decodeAuthUserEnvelope as decodeUserEnvelope,
  decodePermRole as decodeRole, decodeRolesPermissionsInfo as decodePermissionsInfo,
   decodeRolesOverwritesBody as decodeOverwritesBody,
  decodeChatMediaAttachment as decodeMediaAttachment, decodeChatChannel as decodeChannel, decodeChatCategory as decodeCategory,
  decodeChatChannelHierarchy as decodeChannelHierarchy, decodeChatMessage as decodeMessage,
  decodeChatForumTag as decodeForumTag,
  decodeChatReactionResult as decodeReactionResult,  decodeChatNotificationSettings as decodeNotificationSettings,
  decodePollDetail, 
  decodeChatThread as decodeThread, decodeChatSearchResult as decodeSearchResult,
  decodeLinkpreviewPreview as decodeLinkPreview,
  decodeServerOperatorStatus as decodeOperatorStatus,
   decodeServerOpsStatus as decodeOpsStatus,
  decodeServerUpdateStatus as decodeUpdateStatus,
  
   
  
  
  
  
  
  
  
} from './rest'

import type { AuthUser } from './rest'
export type Locale = 'de' | 'en'
export type ChosenPresence = 'online' | 'away' | 'dnd' | 'focus'
export type Presence = ChosenPresence | 'offline'
/** A member of a Talk. In a Stage talk_power says they may speak (given, or implied by Manage stage) and hand_raised_at when they asked to. */
export type VoiceUser = AuthUser & { joined_at: string; muted: boolean; deafened: boolean; talk_power?: boolean | undefined; hand_raised_at?: string | undefined }

// A partial update is explicitly different from a complete User response.
export type UserUpdate = AuthUser | { id: string; disabled: boolean }
