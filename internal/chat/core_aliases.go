package chat

import (
	"github.com/msch128/mnema-talk/internal/chat/core"
)

// Aliases for symbols that moved into sub-packages while internal/chat is split
// by domain. They keep existing callers compiling and are deleted, with their
// call sites rewritten, by the last step of the split.
type Category = core.Category
type Channel = core.Channel
type ChannelInfo = core.ChannelInfo
type ChannelType = core.ChannelType

const ChannelTypeForum = core.ChannelTypeForum
const ChannelTypeText = core.ChannelTypeText
const ChannelTypeVoice = core.ChannelTypeVoice

type Embed = core.Embed
type EmbedAuthor = core.EmbedAuthor
type EmbedField = core.EmbedField
type EmbedFooter = core.EmbedFooter
type EmbedMedia = core.EmbedMedia
type ForumTag = core.ForumTag
type ForwardRef = core.ForwardRef
type HistoryQuery = core.HistoryQuery

const MaxChannelNameLen = core.MaxChannelNameLen
const MaxForumTagEmojiLen = core.MaxForumTagEmojiLen
const MaxForumTagNameLen = core.MaxForumTagNameLen
const MaxForumTags = core.MaxForumTags
const MaxMessageLen = core.MaxMessageLen
const MaxPostTags = core.MaxPostTags
const MaxPostTitleLen = core.MaxPostTitleLen
const MaxTopicLen = core.MaxTopicLen

type MediaAttachment = core.MediaAttachment
type Member = core.Member
type Message = core.Message
type Poll = core.Poll
type PollOption = core.PollOption
type ReactionSummary = core.ReactionSummary
type ReplyPreview = core.ReplyPreview
type UserRef = core.UserRef
