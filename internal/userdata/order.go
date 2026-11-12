package userdata

// Erasure statement order. These numbers are the original sequence; auth sorts
// every domain's steps with its own before running them. Negative numbers run
// before that sequence (they collect storage keys, then drop export rows).
const (
	OrderMediaOwn   = -40
	OrderExportKeys = -30
	OrderMediaDM    = -20
	OrderMediaTalk  = -10
	OrderExportRows = -5
)

const (
	OrderReactions int = iota
	OrderMentions
	OrderPollVotes
	OrderPollVoters
	OrderPolls
	OrderEdits
	OrderMessages
	OrderOutgoingQueue
	OrderDiscordImports
	OrderInvites
	OrderChannelReads
	OrderCategoryNotify
	OrderServerNotify
	OrderThreadFollows
	OrderForumReads
	OrderSlowMode
	OrderUserRoles
	OrderOverwrites
	OrderPush
	OrderSessions
	OrderBotTokens
	OrderBots
	OrderWebhooks
	OrderOutgoingWebhooks
	OrderOutgoingWebhookURL
	OrderAccountTokens
	OrderTOTP
	OrderRecoveryCodes
	OrderLoginChallenges
	OrderSecondFactor
	OrderBlocks
	OrderSaved
	OrderReminders
	OrderScheduled
	OrderRSVPs
	OrderSignups
	OrderEventSeries
	OrderEventComments
	OrderEventCommentMutes
	OrderEvents
	OrderWikiRevisions
	OrderWikiCreated
	OrderWikiUpdated
	OrderWikiDeleted
	OrderReportReporter
	OrderReportReported
	OrderReportResolved
	OrderDMChannels
	OrderDMMembers
	OrderDMAddedBy
	OrderDMCreatedBy
	OrderScrubReports
	OrderTalkLeader
	OrderPinnedBy
	OrderBannedBy
	OrderTimeoutBy
	OrderEmoji
	OrderDiscordImportCreatedBy
	OrderDiscordImportUndoneBy
	OrderLoginUnlock
	OrderMessageNonces
	OrderUploadUsage
	OrderAuditFlag
	OrderScrubAudit
)

// Export file order, after README.txt and before truncated.json.
const (
	FileProfile        = 10
	FileSettings       = 20
	FileEdits          = 30
	FileReactions      = 40
	FilePollVotes      = 50
	FileUploads        = 60
	FilePolls          = 70
	FilePins           = 80
	FileThreadFollows  = 90
	FilePush           = 100
	FileSessions       = 110
	FileEmoji          = 120
	FileAudit          = 130
	FileSecurity       = 140
	FileDirectMessages = 150
	FileBlocks         = 160
	FileReports        = 170
	FileSaved          = 180
	FileReminders      = 190
	FileScheduled      = 200
	FileRSVPs          = 210
	FileEventSignups   = 220
	FileEventComments  = 230
	FileWikiRevisions  = 235
	FileMessages       = 240
)
