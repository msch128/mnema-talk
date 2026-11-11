# Settings: how a screen saves

One screen, one rule. A settings screen either saves every field at once or saves
every field with one explicit Save. It never mixes the two: a Save button promises
that nothing on its form has been stored yet, and an instant control next to it
breaks that promise ("did the icon save? why is Save still off?").

This follows the common guidance: NN/g, *Toggle-Switch Guidelines*: "separate
controls that produce instant results from those that require clicking a command
button", toggles take effect immediately and never wait for Save; Apple and Material
treat switches as instant and keep Save/Done for forms that commit several values at
once. Discord's server settings stage everything behind a sticky "unsaved changes"
bar while its user settings apply at once; we use the same split, in our own calm look.

## Which pattern

| Kind of setting | Pattern | Examples |
|---|---|---|
| **Personal preference**: affects only the person who sets it, is cheap to undo | **Immediate.** The change applies when it is made; a quiet "Saved" confirms it. | audio and voice, notification levels, DM policy, push preview, this device |
| **Shared or admin configuration**: affects other people, or several fields only make sense together | **Draft + Save.** Edit a draft, nothing is stored until Save; the unsaved-changes bar; a discard prompt when leaving. | Talk and channel settings, roles, community, invites, automod rules, webhooks, bots |

**Actions are not fields.** Create, delete, reorder, revoke, test, rotate, "send
reset mail" and wizards (two-factor, import, export, delete account) do something
once. They run when pressed, ask first when destructive, and report through a toast.
They may sit on a draft screen; only the screen's *fields* follow its pattern.
An action on a draft screen sits in its own titled section after the form, never
among the fields, and is never shaped like a field: no switch, checkbox or select
that writes on change. Every action confirms success (a toast, or the result shown
in place, such as a revealed secret) and reports failure with a toast.

**Images are fields.** An icon, avatar or banner on a draft screen is part of the
draft: picking a file only shows a preview, Remove only marks it, and both are
uploaded or deleted on Save (`useStagedImage`). Only a screen without a Save button
may upload an image the moment it is picked, with a toast. The profile card is such a
screen: avatar and banner are changed on the card itself, while name and bio open
their own inline editor with its own bar.

## Draft + Save

Built from two shared parts, never a one-off variant:

- `web/src/shared/composables/useSettingsDraft.ts`: the draft, dirty detection, reset, the
  save lifecycle, the discard guard and Ctrl/Cmd+S.
- `web/src/shared/components/UnsavedChangesBar.vue`: the footer with "You have unsaved
  changes", Reset, Save and the inline error.
- `web/src/shared/composables/useStagedImage.ts`: an image field of the draft (`File`,
  `'remove'` or `null` for unchanged), its preview and its normaliser.

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
8. **Save confirms itself**: a success toast from `saved` (or the dialog closing with
   one). One screen has one bar: a second draft on the same screen uses an `inline` bar
   inside its own card, never a second sticky one.

## Immediate

Use `usePreferenceSave().run(change)` and show `<SavedIndicator :visible="saved" />`
where the setting sits. A failed change is reported with a toast and the control goes
back to the real value. Do not add a Save button to such a screen. Every write of
the screen shows the indicator, including sliders and device pickers that persist
through a store watcher. A write that reports failure by a return value (not a throw)
is checked and reported the same way.

A one-off form that does something (change password, change e-mail, create a
channel) names its button after the action ("Change password", "Create"), not
"Save", and closing it with typed input asks first.

`web/src/shared/composables/settingsPatterns.test.ts` checks the parts that can be checked
mechanically: a draft component never uses the immediate helpers, an image picker on
a draft screen goes through `useStagedImage`, and a component with an unsaved bar
tracks it with a draft or a leave guard.

## Choosing

If a wrong value would surprise somebody else, or two fields have to be valid
together, use a draft. If it only changes how this person's app behaves, apply it at
once. When in doubt, ask: "does a half-finished edit make sense here?" If it does not,
it is a draft.
