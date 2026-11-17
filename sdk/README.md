# Bot SDKs

The official bot SDKs for Mnema Talk live in their own repositories:

- **Go**: [`github.com/mnema-xyz/bot-sdk-go`](https://github.com/mnema-xyz/bot-sdk-go)
- **TypeScript**: [`github.com/mnema-xyz/bot-sdk-js`](https://github.com/mnema-xyz/bot-sdk-js) (npm [`@mnema-xyz/bot-sdk`](https://www.npmjs.com/package/@mnema-xyz/bot-sdk))

In CI, the contract tests check out both SDKs into `sdk/go` and `sdk/typescript` (both in `.gitignore`) and run their test suites against a live test server.
