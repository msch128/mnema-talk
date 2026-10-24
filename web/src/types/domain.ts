// Friendly names for the actual REST schemas, not looser UI lookalikes.
export type {
  AuthUser as User, AuthAdminUser as AdminUser, AuthInvite as Invite,
  AuthUserEnvelope as UserEnvelope, AuthLoginRequest as LoginRequest,
  AuthRegisterRequest as RegisterRequest, AuthUpdateProfileRequest as UpdateProfileRequest,
  AuthCreateInviteRequest as CreateInviteRequest,
  ChatMember as Member,
  PermRole as Role, PermInfo as PermissionInfo, PermOverwrite as Overwrite,
  RolesPermissionsInfo as PermissionsInfo, RolesChannelPermissions as ChannelPermissions,
  ChatChannel as Channel, ChatChannelType as ChannelType, ChatCategory as Category,
  ChatChannelHierarchy as ChannelHierarchy, ChatMessage as Message, ChatForumTag as ForumTag,
  ChatMediaAttachment as MediaAttachment, ChatReactionSummary as ReactionSummary,
  ChatReactionResult as ReactionResult, ChatReplyPreview as ReplyPreview,
  ChatReadState as ReadState, ChatNotifyLevel as NotifyLevel,
  ChatNotificationSettings as NotificationSettings, ChatChannelNotifySetting as ChannelNotifySetting,
  ChatCategoryNotifySetting as CategoryNotifySetting,
  ChatThread as Thread, ChatSearchResult as SearchResult,
  ChatCreateMessageRequest as CreateMessageRequest,
  ChatPoll as Poll, ChatPollOption as PollOption, PollDetail, PollCreateRequest,
  ChatCreateChannelRequest as CreateChannelRequest,
  ChatUpdateChannelRequest as UpdateChannelRequest,
  ChatLayoutRequest as LayoutRequest,
  LinkpreviewPreview as LinkPreview,
  MediaDashboardItem as MediaDashboardItem, MediaStats as MediaStats,
  ServerSystemStatus as SystemStatus, ServerUpdateStatus as UpdateStatus,
  ServerOpsStatus as OpsStatus, ServerOpsSample as OpsSample,
  ServerSelfUpdateStatus as SelfUpdateStatus, ServerICEConfig as ICEConfig,
  ServerLegal as Legal,
} from './rest'

export {
  decodeAuthUser as decodeUser, decodeAuthAdminUser as decodeAdminUser,
  decodeAuthInvite as decodeInvite, decodeAuthUserEnvelope as decodeUserEnvelope,
  decodePermRole as decodeRole, decodeRolesPermissionsInfo as decodePermissionsInfo,
  decodePermOverwrite as decodeOverwrite, decodeRolesOverwritesBody as decodeOverwritesBody,
  decodeChatChannel as decodeChannel, decodeChatCategory as decodeCategory,
  decodeChatChannelHierarchy as decodeChannelHierarchy, decodeChatMessage as decodeMessage,
  decodeChatForumTag as decodeForumTag,
  decodeChatReactionResult as decodeReactionResult, decodeChatReadState as decodeReadState, decodeChatNotificationSettings as decodeNotificationSettings,
  decodePollDetail, decodeSlashResult,
  decodeChatThread as decodeThread, decodeChatSearchResult as decodeSearchResult,
  decodeLinkpreviewPreview as decodeLinkPreview,
  decodeServerSystemStatus as decodeSystemStatus, decodeServerOpsStatus as decodeOpsStatus,
  decodeServerUpdateStatus as decodeUpdateStatus,
  decodeServerSelfUpdateStatus as decodeSelfUpdateStatus,
  decodeServerICEConfig as decodeICEConfig, decodeServerLegal as decodeLegal,
} from './rest'

import type { AuthUser } from './rest'
export type Locale = 'de' | 'en'
export type ChosenPresence = 'online' | 'away' | 'dnd' | 'focus'
export type Presence = ChosenPresence | 'offline'
export type VoiceUser = AuthUser & { joined_at: string; muted: boolean; deafened: boolean }

// A partial update is explicitly different from a complete User response.
export type UserUpdate = AuthUser | { id: string; disabled: boolean }
