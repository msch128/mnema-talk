package chat

import (
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/mentions"
	"github.com/msch128/mnema-talk/internal/chat/readstate"
)

// Aliases for symbols that moved into sub-packages while internal/chat is split
// by domain. They keep existing callers compiling and are deleted, with their
// call sites rewritten, by the last step of the split.
type Category = core.Category
type CategoryNotifySetting = readstate.CategoryNotifySetting
type Channel = core.Channel
type ChannelInfo = core.ChannelInfo
type ChannelNotifySetting = readstate.ChannelNotifySetting
type ChannelType = core.ChannelType

const ChannelTypeForum = core.ChannelTypeForum
const ChannelTypeText = core.ChannelTypeText
const ChannelTypeVoice = core.ChannelTypeVoice

type Embed = core.Embed
type EmbedAuthor = core.EmbedAuthor
type EmbedField = core.EmbedField
type EmbedFooter = core.EmbedFooter
type EmbedMedia = core.EmbedMedia
type FirstUnreadResponse = readstate.FirstUnreadResponse
type ForumTag = core.ForumTag
type ForwardRef = core.ForwardRef
type HistoryQuery = core.HistoryQuery
type MarkAllReadRequest = readstate.MarkAllReadRequest
type MarkReadRequest = readstate.MarkReadRequest
type MarkUnreadRequest = readstate.MarkUnreadRequest

const MaxChannelNameLen = core.MaxChannelNameLen
const MaxForumTagEmojiLen = core.MaxForumTagEmojiLen
const MaxForumTagNameLen = core.MaxForumTagNameLen
const MaxForumTags = core.MaxForumTags
const MaxMentionRecipients = mentions.MaxMentionRecipients
const MaxMessageLen = core.MaxMessageLen
const MaxPostTags = core.MaxPostTags
const MaxPostTitleLen = core.MaxPostTitleLen
const MaxSlowModeSeconds = core.MaxSlowModeSeconds
const MaxTopicLen = core.MaxTopicLen
const MaxUnansweredDMMessages = core.MaxUnansweredDMMessages

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
type Message = core.Message
type NotificationSettings = readstate.NotificationSettings

const NotifyAll = readstate.NotifyAll
const NotifyInherit = readstate.NotifyInherit

type NotifyLevel = readstate.NotifyLevel

const NotifyMentions = readstate.NotifyMentions
const NotifyMute = readstate.NotifyMute

type NotifySetting = readstate.NotifySetting
type OnlineSource = mentions.OnlineSource
type Poll = core.Poll
type PollOption = core.PollOption
type ReactionSummary = core.ReactionSummary
type ReadState = readstate.ReadState
type ReplyPreview = core.ReplyPreview
type ServerNotify = readstate.ServerNotify
type SetNotifyLevelRequest = readstate.SetNotifyLevelRequest
type SetServerNotifyRequest = readstate.SetServerNotifyRequest
type UserRef = core.UserRef
