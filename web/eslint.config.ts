import js from '@eslint/js'
import vue from 'eslint-plugin-vue'
import globals from 'globals'
import sonarjs from 'eslint-plugin-sonarjs'
import tseslint from 'typescript-eslint'
import vueParser from 'vue-eslint-parser'

export default tseslint.config(
  // third_party holds vendored upstream code (see its README).
  { ignores: ['**/.codex/**', '**/dist/**', '**/node_modules/**', '**/coverage/**', '**/src/third_party/deepfilternet3/{worklet,worker-glue}.js'] },
  js.configs.recommended,
  ...vue.configs['flat/recommended'],
  { plugins: { sonarjs } },
  {
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: { ...globals.browser, __APP_VERSION__: 'readonly', __MNEMA_E2E_HOOKS__: 'readonly' }
    },
    rules: {
      'no-unused-vars': ['error', { argsIgnorePattern: '^_', caughtErrors: 'none' }],
      'no-eval': 'error',
      'no-implied-eval': 'error',
      'no-new-func': 'error',
      // v-html is only allowed where input is escaped first (MarkdownContent).
      'vue/no-v-html': 'error',
      // Formatting rules are noise for this codebase; correctness rules stay on.
      'vue/max-attributes-per-line': 'off',
      'vue/singleline-html-element-content-newline': 'off',
      'vue/multiline-html-element-content-newline': 'off',
      'vue/html-self-closing': 'off',
      'vue/html-indent': 'off',
      'vue/html-closing-bracket-newline': 'off',
      'vue/attributes-order': 'off',
      'vue/attribute-hyphenation': 'off',
      'vue/first-attribute-linebreak': 'off',
      'vue/multi-word-component-names': 'off'
    }
  },
  {
    files: ['**/*.test.{js,ts}', '**/*.config.{js,ts}', '**/scripts/**/*.ts'],
    languageOptions: { globals: { ...globals.node } }
  }
,
  ...tseslint.configs.recommended.map(config => ({ ...config, files: ['**/*.ts', '**/*.vue'] })),
  {
    files: ['**/*.vue'],
    languageOptions: { parser: vueParser, parserOptions: { parser: tseslint.parser } }
  },
  // The desktop client's window globals, IPC and native transports are reached
  // only through lib/platform.ts (doc/desktop-trusted-client.md). Everything
  // else stays platform-agnostic.
  {
    files: ['**/*.ts', '**/*.vue'],
    rules: {
      'no-restricted-imports': ['error', {
        patterns: [
          { group: ['@tauri-apps/*'], message: 'Reach the desktop host through lib/platform.' },
          { group: ['**/nativeTransport', '**/trustedDesktop', '**/trustedSocket', '**/desktopRuntime'], message: 'Use lib/platform instead of the desktop adapter internals.' }
        ]
      }],
      'no-restricted-globals': ['error',
        { name: '__TAURI__', message: 'Use lib/platform.' },
        { name: '__TAURI_INTERNALS__', message: 'Use lib/platform.' },
        { name: '__MNEMA_TRUSTED_APP__', message: 'Use lib/platform.' }
      ],
      'no-restricted-properties': ['error',
        { object: 'window', property: 'isTauri', message: 'Use lib/platform.' },
        { object: 'window', property: '__TAURI__', message: 'Use lib/platform.' },
        { object: 'window', property: '__TAURI_INTERNALS__', message: 'Use lib/platform.' },
        { object: 'window', property: '__MNEMA_TRUSTED_APP__', message: 'Use lib/platform.' }
      ],
      'no-restricted-syntax': ['error', {
        selector: "Literal[value=/^__(TAURI|TAURI_INTERNALS|MNEMA_TRUSTED_APP|MNEMA_WEB_DESKTOP|MNEMA_GAMING_BRIDGE|MNEMA_GAMING_SURFACE)(__)?$/]",
        message: 'Desktop window globals are read only in lib/platform and its adapter modules.'
      }, {
        selector: "MemberExpression[property.name='isTauri']",
        message: 'Ask lib/platform whether this is the desktop client.'
      }]
    }
  },
  {
    files: [
      '**/*.test.ts',
      '**/*.fixture.ts',
      '**/*.config.ts',
      '**/src/lib/platform.ts',
      '**/src/lib/desktopRuntime.ts',
      '**/src/lib/trustedDesktop.ts',
      '**/src/lib/trustedSocket.ts',
      '**/src/lib/nativeTransport.ts'
    ],
    rules: { 'no-restricted-imports': 'off', 'no-restricted-globals': 'off', 'no-restricted-properties': 'off', 'no-restricted-syntax': 'off' }
  },
  // Production source files stay under 400 lines (cognitive load): split by
  // responsibility, centralise duplicates. Generated files and tests are exempt.
  {
    files: ['**/web/src/**/*.{ts,vue}'],
    ignores: ['**/*.test.ts', '**/*.fixture.ts', '**/web/src/types/rest.ts', '**/web/src/third_party/**'],
    rules: {
      'max-lines': ['error', { max: 400, skipBlankLines: false, skipComments: false }],
      // Same limits as the Go guard (internal/repo/limits_test.go): raw line
      // span of a function (blanks and comments count), cognitive complexity 15.
      'max-lines-per-function': ['error', { max: 80, skipBlankLines: false, skipComments: false, IIFEs: true }],
      'sonarjs/cognitive-complexity': ['error', 15]
    }
  },
  // Tests, fixtures and e2e specs may be up to 800 lines (same as Go _test.go).
  {
    files: ['**/*.test.ts', '**/*.fixture.ts', '**/e2e/**/*.ts'],
    rules: { 'max-lines': ['error', { max: 800, skipBlankLines: false, skipComments: false }] }
  },
  // TODO(limits): shrinking allowlist of files that still exceed the function
  // length / cognitive complexity limits. Refactor, then delete the entry. Never
  // add entries or raise a limit.
  {
    files: [
      'web/src/components/ContextMenu.vue',
      'web/src/composables/useAppRouting.ts',
      'web/src/composables/useChannelLayout.ts',
      'web/src/composables/useComposerAssist.ts',
      'web/src/composables/useMessageActions.ts',
      'web/src/composables/useMessageKeyboard.ts',
      'web/src/composables/useMessageMenu.ts',
      'web/src/composables/useMessageScroll.ts',
      'web/src/composables/useNavMenus.ts',
      'web/src/composables/usePictureInPicture.ts',
      'web/src/composables/useResizable.ts',
      'web/src/composables/useSidebarReorder.ts',
      'web/src/composables/useSlashCommands.ts',
      'web/src/composables/useSortableDrag.ts',
      'web/src/composables/useStreamQuality.ts',
      'web/src/composables/webrtc/layers.ts',
      'web/src/composables/webrtc/micPipeline.ts',
      'web/src/composables/webrtc/micTest.ts',
      'web/src/composables/webrtc/peer.ts',
      'web/src/composables/webrtc/remoteAudio.ts',
      'web/src/composables/webrtc/screen.ts',
      'web/src/composables/webrtc/sdp.ts',
      'web/src/composables/webrtc/senders.ts',
      'web/src/composables/webrtc/session.ts',
      'web/src/composables/webrtc/streamDiag.ts',
      'web/src/composables/webrtc/voiceAudio.ts',
      'web/src/lib/api.ts',
      'web/src/lib/channelLayout.ts',
      'web/src/lib/crypto/stateStore.ts',
      'web/src/lib/desktopGaming.ts',
      'web/src/lib/dfnBridgeWorklet.ts',
      'web/src/lib/media/browserCall.ts',
      'web/src/lib/media/codec.ts',
      'web/src/lib/media/frame.ts',
      'web/src/lib/media/group.ts',
      'web/src/lib/media/groupEpoch.ts',
      'web/src/lib/media/groupSync.ts',
      'web/src/lib/media/guard.ts',
      'web/src/lib/media/mediaFrameWorker.ts',
      'web/src/lib/nativeAdmin.ts',
      'web/src/lib/nativeChatApi.ts',
      'web/src/lib/nativeTransport.ts',
      'web/src/lib/router.ts',
      'web/src/lib/rtcStats.ts',
      'web/src/lib/soundEffects.ts',
      'web/src/lib/streamQuality.ts',
      'web/src/lib/trustPort.ts',
      'web/src/lib/voicePlayback.ts',
      'web/src/stores/auth.ts',
      'web/src/stores/chat.ts',
      'web/src/stores/chat/events.ts',
      'web/src/stores/chat/members.ts',
      'web/src/stores/chat/messageWindow.ts',
      'web/src/stores/chat/state.ts',
      'web/src/stores/permissions.ts',
      'web/src/stores/voice.ts',
      'web/src/stores/voice/room.ts',
      'web/src/stores/voice/state.ts',
      'web/src/types/events.ts'
    ],
    rules: { 'max-lines-per-function': 'off', 'sonarjs/cognitive-complexity': 'off' }
  },
  // TODO(limits): shrinking allowlist of test files still above 800 lines. Split
  // them by behaviour, then delete the entry. Never add entries.
  {
    files: [
      'web/src/components/Sidebar.test.ts',
      'web/src/components/VoiceStage.test.ts',
      'web/src/composables/useWebRTC.audio.test.ts',
      'web/src/lib/media/group.test.ts',
      'web/src/stores/chatStoreBehaviour.test.ts',
      'web/src/stores/voice.test.ts'
    ],
    rules: { 'max-lines': 'off' }
  },
  {
    files: ['**/*.ts', '**/*.vue'],
    rules: {
      '@typescript-eslint/no-explicit-any': 'error',
      '@typescript-eslint/ban-ts-comment': ['error', { 'ts-ignore': true, 'ts-nocheck': true, 'ts-expect-error': true }],
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_', caughtErrors: 'none' }]
    }
  }
)
