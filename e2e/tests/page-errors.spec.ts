import { test, expect } from '@playwright/test'
import { isCancelledApiRequest } from './page-errors'

// The page errors the specs treat as fatal come through Playwright's own
// parsing, which splits the text at the first colon. These are the shapes it
// really produces (copied from a failed CI run), and the ones that must stay fatal.
test('WebKit\'s report of a cancelled API request is not an app error, however Playwright split it', () => {
  // As seen in CI: name "Fetch API cannot load http", message without "://".
  expect(isCancelledApiRequest({ name: 'Fetch API cannot load http', message: '/127.0.0.1:46487/api/users/me/dm-settings due to access control checks.' })).toBe(true)
  expect(isCancelledApiRequest({ name: 'Error', message: 'Fetch API cannot load http://127.0.0.1:46487/api/channels due to access control checks.' })).toBe(true)
})

test('every other error still counts', () => {
  expect(isCancelledApiRequest({ name: 'TypeError', message: "undefined is not an object (evaluating 'x.y')" })).toBe(false)
  expect(isCancelledApiRequest({ name: 'Fetch API cannot load https', message: '//evil.example/api/x due to access control checks.' })).toBe(false)
  expect(isCancelledApiRequest({ name: 'Fetch API cannot load http', message: '/127.0.0.1:46487/assets/app.js due to access control checks.' })).toBe(false)
  expect(isCancelledApiRequest({ name: 'Fetch API cannot load http', message: '/127.0.0.1:46487/api/x for another reason.' })).toBe(false)
})
