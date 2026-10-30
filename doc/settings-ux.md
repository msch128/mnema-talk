# Settings: how a screen saves

One screen, one rule. A settings screen either saves every field at once or saves
every field with one explicit Save. It never mixes the two.

## Which pattern

| Kind of setting | Pattern | Examples |
|---|---|---|
| **Personal preference**: affects only the person who sets it, is cheap to undo | **Immediate.** The change applies when it is made; a quiet "Saved" confirms it. | audio and voice, notification levels, DM policy, push preview, this device |
| **Shared or admin configuration**: affects other people, or several fields only make sense together | **Draft + Save.** Edit a draft, nothing is stored until Save; the unsaved-changes bar; a discard prompt when leaving. | Talk and channel settings, roles, community, invites, automod rules, webhooks, bots |

**Actions are not fields.** Create, delete, reorder, revoke, test, rotate, "send
reset mail" and wizards (two-factor, import, export, delete account) do something
once. They run when pressed, ask first when destructive, and report through a toast.
They may sit on a draft screen; only the screen's *fields* follow its pattern.

## Draft + Save

Built from two shared parts, never a one-off variant:

- `web/src/composables/useSettingsDraft.ts`: the draft, dirty detection, reset, the
  save lifecycle, the discard guard and Ctrl/Cmd+S.
- `web/src/components/UnsavedChangesBar.vue`: the footer with "You have unsaved
  changes", Reset, Save and the inline error.

Rules:

1. **Every field is in the draft**, including toggles. Nothing is persisted on change.
2. **Changed means different after normalising**, field by field (`normalise.text`,
   `.count`, `.set`, `.flag`, or your own): surrounding whitespace, `0` and empty for
   "unlimited", empty vs null, the order of a role list. The Save button is on iff the
   draft differs from the saved state **and** is valid (`validate` returns field errors).
3. **Save is one lifecycle**: `save` receives only the changed fields and writes them
   in a defined order. If the API has separate calls, call `commit(keys)` after each one
   that went through. A later failure then leaves only the rest unsaved and says
   "Some changes were saved, the rest was not". Errors show inline in the bar, next to
   the fields they belong to, not only as a toast.
4. **After Save the draft is the saved state.** If the saved state changes elsewhere,
   fields you did not touch follow it; fields you are editing keep your value.
5. **Leaving with unsaved changes asks "Discard changes?"**: closing a dialog
   (`BaseDialog :guard="form.guard"`), replacing the edited item by another (another
   role, another bot, another webhook) and reloading the page (`beforeunload`, automatic).
6. **Ctrl/Cmd+S** inside the form saves it, and so does Enter in a one-line field
   (`@keydown="form.onKeydown"`). A form with several fields has no implicit submit.
7. **A container keeps its drafts alive.** The admin console keeps every tab mounted, so
   switching tabs loses nothing; closing the console asks once for all of them
   (`provideDraftScope()` + `BaseDialog :guard`). A draft that is not a field form (the
   channel order, a list of overwrites) still uses `UnsavedChangesBar` and
   `useLeaveGuard(dirty)`, with `dirty` computed by comparing with the saved state, never
   a hand-set flag.

## Immediate

Use `usePreferenceSave().run(change)` and show `<SavedIndicator :visible="saved" />`
where the setting sits. A failed change is reported with a toast and the control goes
back to the real value. Do not add a Save button to such a screen.

## Choosing

If a wrong value would surprise somebody else, or two fields have to be valid
together, use a draft. If it only changes how this person's app behaves, apply it at
once. When in doubt, ask: "does a half-finished edit make sense here?" If it does not,
it is a draft.
