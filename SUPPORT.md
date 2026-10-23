# Getting help

Mnema Talk is a small, volunteer-run project. There is no support contract;
this is where to look, in order.

1. **Documentation.** [Fresh install and updates](doc/deployment.md),
   [operations and troubleshooting](doc/operations.md) (ports, reverse proxy,
   TURN, backups, metrics) and the [upgrade notes](doc/upgrade.md). Most
   "voice does not connect" problems are the UDP port range or
   `WEBRTC_NAT_1TO1_IP`, described there.
2. **Questions and ideas.** Ask in the repository's Discussions if they are
   enabled; otherwise open an issue and say it is a question.
3. **Bugs.** Open an [issue](https://github.com/msch128/mnema-talk/issues/new/choose)
   with your version (Admin console, System tab), how you deploy, and the
   browser. Never paste secrets, tokens or invite codes; remove personal data
   from logs.
4. **Security problems.** Report them privately as described in
   [SECURITY.md](SECURITY.md). Do not open a public issue.

Supported versions (which releases get fixes) are listed in
[SECURITY.md](SECURITY.md#supported-versions). The desktop client lives in
[mnema-talk-desktop](https://github.com/msch128/mnema-talk-desktop); it is
Beta, so please report its problems there.
