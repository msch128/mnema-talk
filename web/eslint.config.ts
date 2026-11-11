import js from '@eslint/js'
import vue from 'eslint-plugin-vue'
import globals from 'globals'
import sonarjs from 'eslint-plugin-sonarjs'
import tseslint from 'typescript-eslint'
import vueParser from 'vue-eslint-parser'

// Desktop window globals and IPC are named only in lib/platform and its adapters.
const desktopGlobalSyntax = [{
  selector: "Literal[value=/^__(TAURI|TAURI_INTERNALS|MNEMA_TRUSTED_APP|MNEMA_WEB_DESKTOP|MNEMA_GAMING_BRIDGE|MNEMA_GAMING_SURFACE)(__)?$/]",
  message: 'Desktop window globals are read only in lib/platform and its adapter modules.'
}, {
  selector: "MemberExpression[property.name='isTauri']",
  message: 'Ask lib/platform whether this is the desktop client.'
}]

// HTML written as a string. Vue's Trusted Types policy passes text through, so
// the browser does not stop these; vue/no-v-html covers only the directive.
// MarkdownContent's v-html stays the one place that writes HTML.
const HTML_PROPERTY = '/^(inner|outer)HTML$/'
const HTML_SINK_MESSAGE = 'Do not write HTML as a string: render through MarkdownContent (see SECURITY.md, "Trusted Types").'
const htmlSinkSyntax = [
  // h('div', { innerHTML }), a v-bind="{ innerHTML }" object, a JSX-like props object.
  { selector: `Property[key.name=${HTML_PROPERTY}]`, message: HTML_SINK_MESSAGE },
  { selector: `Property[key.value=${HTML_PROPERTY}]`, message: HTML_SINK_MESSAGE },
  // element.innerHTML = '...'
  { selector: `AssignmentExpression[left.type='MemberExpression'][left.property.name=${HTML_PROPERTY}]`, message: HTML_SINK_MESSAGE },
  { selector: `AssignmentExpression[left.type='MemberExpression'][left.property.value=${HTML_PROPERTY}]`, message: HTML_SINK_MESSAGE }
]
// In a template: :innerHTML="...", .innerHTML="..." and v-bind:inner-html.prop="...".
const htmlSinkTemplateSyntax = [
  ...htmlSinkSyntax,
  { selector: "VAttribute[directive=true][key.name.name='bind'][key.argument.name=/^(inner|outer)-?html$/i]", message: HTML_SINK_MESSAGE }
]

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
      globals: { ...globals.browser, __APP_VERSION__: 'readonly', __ASSETS_PREFIX__: 'readonly', __MNEMA_E2E_HOOKS__: 'readonly' }
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
  // The desktop client's window globals and IPC are reached
  // only through lib/platform.ts (doc/desktop-trusted-client.md). Everything
  // else stays platform-agnostic.
  {
    files: ['**/*.ts', '**/*.vue'],
    rules: {
      'no-restricted-imports': ['error', {
        patterns: [
          { group: ['@tauri-apps/*'], message: 'Reach the desktop host through lib/platform.' },
          { group: ['**/trustedDesktop', '**/trustedSocket', '**/desktopRuntime'], message: 'Use lib/platform instead of the desktop adapter internals.' }
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
      'no-restricted-syntax': ['error', ...desktopGlobalSyntax, ...htmlSinkSyntax],
      'vue/no-restricted-syntax': ['error', ...htmlSinkTemplateSyntax]
    }
  },
  {
    files: ['**/*.test.ts', '**/*.fixture.ts', '**/*.config.ts'],
    rules: { 'no-restricted-imports': 'off', 'no-restricted-globals': 'off', 'no-restricted-properties': 'off', 'no-restricted-syntax': 'off' }
  },
  // The browser specs probe the policy with a plain string at a sink (csp.spec.ts).
  {
    files: ['**/e2e/**/*.ts'],
    rules: { 'no-restricted-syntax': ['error', ...desktopGlobalSyntax] }
  },
  // The desktop adapters name the window globals; the HTML rule still holds there.
  {
    files: [
      '**/src/lib/platform.ts',
      '**/src/lib/desktopRuntime.ts',
      '**/src/lib/osNotify.ts',
      '**/src/lib/native/trustedDesktop.ts',
      '**/src/lib/native/trustedSocket.ts'
    ],
    rules: { 'no-restricted-imports': 'off', 'no-restricted-globals': 'off', 'no-restricted-properties': 'off', 'no-restricted-syntax': ['error', ...htmlSinkSyntax] }
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
  {
    files: ['**/*.ts', '**/*.vue'],
    rules: {
      '@typescript-eslint/no-explicit-any': 'error',
      '@typescript-eslint/ban-ts-comment': ['error', { 'ts-ignore': true, 'ts-nocheck': true, 'ts-expect-error': true }],
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_', caughtErrors: 'none' }]
    }
  }
)
