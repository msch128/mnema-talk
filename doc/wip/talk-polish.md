# WIP: Talk view polish (0.4.0)

Status: unfinished, rescued from a stopped agent. Unit tests and lint pass,
not yet checked in the browser or e2e.

Goal (owner decisions, see doc/roadmap-0.4.0.md):
- Control bar floats over the stage and hides after ~3 s without mouse
  movement (`TalkControlBar.vue`, `useAutoHide.js`); keyboard focus keeps it visible.
- Participant strip under a stream is collapsible, state remembered.
- Spacing and tile sizing like Discord (`talkLayout.js`, `useElementSize.js`).

Next steps:
1. Check the stage at 1280x800 and 1920x1080 with one stream, two streams and
   cameras only; screenshots in the PR.
2. Run the Talk e2e specs (`e2e/`), fix selectors that moved into TalkControlBar.
3. Open the PR, CI green, merge.
