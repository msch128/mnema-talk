<script setup lang="ts">
// The hosts of what opens over the app from anywhere: toasts, calls, the
// confirmation and the dialogs of message and member actions. The order is the
// stacking order: later ones open above earlier ones.
import ToastHost from './ToastHost.vue'
import CallHost from './CallHost.vue'
import ConfirmDialog from './ConfirmDialog.vue'
import MediaViewer from './MediaViewer.vue'
import ModerationDialog from './ModerationDialog.vue'
import MessageModerationDialog from './MessageModerationDialog.vue'
import EditHistoryDialog from './EditHistoryDialog.vue'
import ReportDialog from './ReportDialog.vue'
import ForwardDialog from './ForwardDialog.vue'
import LaterDialog from './LaterDialog.vue'
import TalkSettingsHost from './TalkSettingsHost.vue'
</script>

<template>
  <ToastHost />
  <!-- Before the confirm host: its "Discard changes?" must open above the Talk settings. -->
  <TalkSettingsHost />
  <CallHost />
  <ConfirmDialog />
  <MediaViewer />
  <ModerationDialog />
  <MessageModerationDialog />
  <EditHistoryDialog />
  <ReportDialog />
  <ForwardDialog />
  <LaterDialog />
</template>
