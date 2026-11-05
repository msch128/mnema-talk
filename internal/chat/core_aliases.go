package chat

import (
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/dm"
	"github.com/msch128/mnema-talk/internal/chat/forum"
	"github.com/msch128/mnema-talk/internal/chat/mentions"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/chat/readstate"
	"github.com/msch128/mnema-talk/internal/chat/reports"
	"github.com/msch128/mnema-talk/internal/chat/saved"
	"github.com/msch128/mnema-talk/internal/chat/search"
)

// Aliases for symbols that moved into sub-packages while internal/chat is split
// by domain. They keep existing callers compiling and are deleted, with their
// call sites rewritten, by the last step of the split.
type AddDMMemberRequest = dm.AddDMMemberRequest

const AutoDeleteEvery = channels.AutoDeleteEvery

type AutoDeleteRequest = channels.AutoDeleteRequest
type AutoDeleter = channels.AutoDeleter
type BulkDeleteRequest = messages.BulkDeleteRequest
type BulkDeleteResult = messages.BulkDeleteResult

const CallAnswered = dm.CallAnswered
const CallCancelled = dm.CallCancelled
const CallEnded = dm.CallEnded
const CallTimeout = dm.CallTimeout

type Category = core.Category
type CategoryNotifySetting = readstate.CategoryNotifySetting
type CategoryOrder = channels.CategoryOrder
type Channel = core.Channel
type ChannelHierarchy = channels.ChannelHierarchy
type ChannelInfo = core.ChannelInfo
type ChannelNotifySetting = readstate.ChannelNotifySetting
type ChannelPlacement = channels.ChannelPlacement
type ChannelType = core.ChannelType

const ChannelTypeForum = core.ChannelTypeForum
const ChannelTypeText = core.ChannelTypeText
const ChannelTypeVoice = core.ChannelTypeVoice

type CreateCategoryRequest = channels.CreateCategoryRequest
type CreateChannelRequest = channels.CreateChannelRequest
type CreateDMRequest = dm.CreateDMRequest
type CreateForumTagRequest = forum.CreateForumTagRequest
type CreateMessageRequest = messages.CreateMessageRequest
type CreatePostRequest = forum.CreatePostRequest
type CreateReportRequest = reports.CreateReportRequest
type DMCall = dm.DMCall
type DMCallEnd = dm.DMCallEnd
type DMCallRing = dm.DMCallRing
type DMCalls = dm.DMCalls
type DMLastMessage = dm.DMLastMessage
type DMMember = dm.DMMember
type DMMemberChange = dm.DMMemberChange

const DMPolicyEveryone = dm.DMPolicyEveryone
const DMPolicyNobody = dm.DMPolicyNobody

type DMPrefsUpdated = dm.DMPrefsUpdated
type DMSettings = dm.DMSettings

const DefaultRingDuration = dm.DefaultRingDuration

type DeleteMessageRequest = messages.DeleteMessageRequest
type DirectMessage = dm.DirectMessage

const EditHistoryRate = messages.EditHistoryRate

type EditMessageRequest = messages.EditMessageRequest
type Embed = core.Embed
type EmbedAuthor = core.EmbedAuthor
type EmbedField = core.EmbedField
type EmbedFooter = core.EmbedFooter
type EmbedMedia = core.EmbedMedia
type FirstUnreadResponse = readstate.FirstUnreadResponse
type FollowedThreads = messages.FollowedThreads
type FormError = messages.FormError
type ForumSettings = forum.ForumSettings
type ForumSettingsPatch = forum.ForumSettingsPatch
type ForumTag = core.ForumTag
type ForwardQuote = messages.ForwardQuote
type ForwardRef = core.ForwardRef
type ForwardRequest = messages.ForwardRequest

const ForwardsPerMinute = messages.ForwardsPerMinute

type HistoryQuery = core.HistoryQuery
type InEmbed = messages.InEmbed
type InboxQuery = search.InboxQuery
type LayoutRequest = channels.LayoutRequest
type MarkAllReadRequest = readstate.MarkAllReadRequest
type MarkReadRequest = readstate.MarkReadRequest
type MarkUnreadRequest = readstate.MarkUnreadRequest

const MaxAttachmentBytes = messages.MaxAttachmentBytes
const MaxAttachments = messages.MaxAttachments
const MaxBulkDelete = messages.MaxBulkDelete
const MaxChannelNameLen = core.MaxChannelNameLen
const MaxDMMembers = dm.MaxDMMembers
const MaxEditVersions = messages.MaxEditVersions
const MaxEmbeds = messages.MaxEmbeds
const MaxEmojiPerMessage = messages.MaxEmojiPerMessage
const MaxFavouriteDMs = dm.MaxFavouriteDMs
const MaxFormerSlugs = channels.MaxFormerSlugs
const MaxForumGuidelinesLen = forum.MaxForumGuidelinesLen
const MaxForumTagEmojiLen = core.MaxForumTagEmojiLen
const MaxForumTagNameLen = core.MaxForumTagNameLen
const MaxForumTags = core.MaxForumTags
const MaxGroupDMsPerMember = dm.MaxGroupDMsPerMember
const MaxMentionRecipients = mentions.MaxMentionRecipients
const MaxMessageLen = core.MaxMessageLen
const MaxModerationReason = messages.MaxModerationReason
const MaxNewDMsPerDay = dm.MaxNewDMsPerDay
const MaxOpenReportsPerMember = reports.MaxOpenReportsPerMember
const MaxPinsPerChannel = messages.MaxPinsPerChannel
const MaxPostSearchLen = forum.MaxPostSearchLen
const MaxPostTags = core.MaxPostTags
const MaxPostTitleLen = core.MaxPostTitleLen
const MaxReactionsPerUser = messages.MaxReactionsPerUser
const MaxReportNote = reports.MaxReportNote
const MaxSavedNoteLen = saved.MaxSavedNoteLen
const MaxSavedPerMember = saved.MaxSavedPerMember
const MaxSlowModeSeconds = core.MaxSlowModeSeconds
const MaxSlugLength = channels.MaxSlugLength
const MaxSortOrder = channels.MaxSortOrder
const MaxTopicLen = core.MaxTopicLen
const MaxUnansweredDMMessages = core.MaxUnansweredDMMessages
const MaxUserLimit = channels.MaxUserLimit

type MediaAttachment = core.MediaAttachment
type Member = core.Member

const MentionAll = mentions.MentionAll
const MentionHere = mentions.MentionHere
const MentionKindGroup = core.MentionKindGroup
const MentionKindName = core.MentionKindName
const MentionKindRole = core.MentionKindRole

type MentionPolicy = mentions.MentionPolicy
type MentionRole = mentions.MentionRole
type MentionSet = mentions.MentionSet
type MentionedUser = mentions.MentionedUser
type MentionsPage = search.MentionsPage
type Message = core.Message
type MessageEdit = messages.MessageEdit
type MessageEditHistory = messages.MessageEditHistory
type NewDM = dm.NewDM

const NewDMsPerMinute = dm.NewDMsPerMinute

type NewMessage = messages.NewMessage
type NewReport = reports.NewReport
type NotificationSettings = readstate.NotificationSettings

const NotifyAll = readstate.NotifyAll
const NotifyInherit = readstate.NotifyInherit

type NotifyLevel = readstate.NotifyLevel

const NotifyMentions = readstate.NotifyMentions
const NotifyMute = readstate.NotifyMute

type NotifySetting = readstate.NotifySetting
type ObjectDeleter = core.ObjectDeleter
type OnlineSource = mentions.OnlineSource

const PinChangesPerMinute = messages.PinChangesPerMinute

type PinsChanged = messages.PinsChanged
type Poll = core.Poll
type PollOption = core.PollOption
type PostActor = forum.PostActor
type PostFilter = forum.PostFilter
type PostPatch = forum.PostPatch
type PostRead = forum.PostRead
type PreparedPost = forum.PreparedPost
type Privacy = dm.Privacy
type ReactionRequest = messages.ReactionRequest
type ReactionResult = messages.ReactionResult
type ReactionSummary = core.ReactionSummary
type ReadState = readstate.ReadState
type RenameCategoryRequest = channels.RenameCategoryRequest
type RenamedCategory = channels.RenamedCategory
type ReorderForumTagsRequest = forum.ReorderForumTagsRequest
type ReplyPreview = core.ReplyPreview
type Report = reports.Report
type ReportAttachment = reports.ReportAttachment

const ReportContextAfter = reports.ReportContextAfter
const ReportContextBefore = reports.ReportContextBefore

type ReportDetail = reports.ReportDetail

const ReportDismissed = reports.ReportDismissed

type ReportFiled = reports.ReportFiled

const ReportKindChannel = reports.ReportKindChannel
const ReportKindDM = reports.ReportKindDM
const ReportOpen = reports.ReportOpen

type ReportParty = reports.ReportParty

const ReportResolved = reports.ReportResolved

type ReportSnapshot = reports.ReportSnapshot
type ReportedMessage = reports.ReportedMessage
type ReportsPage = reports.ReportsPage

const ReportsPerHour = reports.ReportsPerHour

type ResolveReportRequest = reports.ResolveReportRequest
type SaveRequest = saved.SaveRequest

const SavedChangesPerMinute = saved.SavedChangesPerMinute

type SavedMessage = saved.SavedMessage
type SavedPage = saved.SavedPage
type SavedUpdated = saved.SavedUpdated
type SearchQuery = search.SearchQuery
type SearchResult = search.SearchResult
type SearchRoot = search.SearchRoot
type ServerNotify = readstate.ServerNotify
type SetDMFavouritesRequest = dm.SetDMFavouritesRequest
type SetDMHiddenRequest = dm.SetDMHiddenRequest
type SetNotifyLevelRequest = readstate.SetNotifyLevelRequest
type SetServerNotifyRequest = readstate.SetServerNotifyRequest
type TagPatch = forum.TagPatch
type TagSpec = forum.TagSpec
type Thread = messages.Thread
type ThreadFollow = messages.ThreadFollow

const ThreadFollowChangesPerMinute = messages.ThreadFollowChangesPerMinute

type UnreadForumPosts = forum.UnreadForumPosts
type UpdateChannelRequest = channels.UpdateChannelRequest
type UpdateForumSettingsRequest = forum.UpdateForumSettingsRequest
type UpdateForumTagRequest = forum.UpdateForumTagRequest
type UpdatePostRequest = forum.UpdatePostRequest
type UserRef = core.UserRef
type ViewAsCategory = channels.ViewAsCategory
type ViewAsChannel = channels.ViewAsChannel
type ViewAsRole = channels.ViewAsRole
type VoiceRooms = core.VoiceRooms
