# Glossary: product vocabulary

The words Mnema Talk uses in its interface and documentation, in English and
German. Use one word for one thing: UI strings (`web/src/i18n/en.json`,
`de.json`), docs, error messages and audit entries follow this table. The
German interface addresses people with **du** throughout; **Sie** appears only
in the legal texts (Impressum, Datenschutz, AGB), which are formal by design.
"Ihr" is used only for an instruction to a whole group, and then says so.

| Concept | English | German | Not | Notes |
|---|---|---|---|---|
| The space | community | Community | server (user-facing), instance, Gemeinschaft | "Server" means only the machine or software an operator runs (admin System tab, connection errors). Docs say "your server" or "this community". |
| A person with an account | member | Mitglied | user, Nutzer | The login handle is the username / Benutzername. "Person" only as a neutral pronoun substitute. |
| The single all-powerful account | Owner | Owner | admin, Herzog (in docs) | Not translated; one per community. |
| Role or permission with all rights | Administrator | Administrator | Admin as a noun for a person | "Admin console" / "Admin-Bereich" stays as the name of the area. |
| Voice room | Talk | Talk | voice channel, Sprachkanal, call, Anruf, Raum | "Talk" is the Mnema product term in both languages. The create dialog may say "Talk (voice)" / "Talk (Sprache)". Verbs: join a Talk / einem Talk beitreten, leave. |
| Text room | channel | Kanal | | Group of rooms: category / Kategorie. |
| Screen broadcast | screen share | Bildschirmübertragung | stream (as a noun for your own share) | A viewer watches a screen share / sieht eine Übertragung; "Nicht mehr ansehen" stops watching. |
| Mute your own microphone | mute / unmute | stummschalten / Stummschaltung aufheben | | Deafen = "Ton aus". Only for audio. |
| Silence notifications | mute notifications | Benachrichtigungen aus / stumm | | |
| Moderation silence | timeout | Auszeit | stummschalten, Timeout | "Auszeit verhängen / beenden". The permission is "Mitglieder in Auszeit schicken". |
| Block a member | ban | sperren / Sperre | | With a reason and a duration. |
| Stop seeing a member's messages and notifications | block / unblock | blockieren / Blockierung aufheben | ban, sperren, mute | Private: the other member is not told. "Blocked message · show" / „Blockierte Nachricht · anzeigen“. Distinct from ban (a moderation sanction). |
| Tell the moderators about a message | report | melden | flag, anzeigen | "Report message" / „Nachricht melden“. The moderators' list is "Reports" / „Meldungen“; a report is closed as resolved / erledigt or dismissed / abgelehnt. |
| Disable an account (no record) | disable | deaktivieren | sperren | "Konto deaktivieren / aktivieren". |
| Brute-force lock | sign-in lock | Anmeldesperre | Login-Sperre | |
| Private conversation | direct message | Direktnachricht | DM, private message, Privatnachricht | A conversation of two to ten members that is not a channel. "Group direct message" / „Gruppen-Direktnachricht“ for three or more. Calls in direct messages do not exist. |
| Pin | pin / unpin | anheften / Anheftung aufheben | lösen | "Angeheftete Nachrichten". |
| Poll | poll | Umfrage | Abstimmung (only for the act of voting) | |
| Custom emoji | custom emoji | eigene Emojis | | "Emojis verwalten". |
| Sign in / out | sign in, sign out | anmelden, abmelden | log in, Login | Registering is registrieren. |
| Invite | invite code | Einladungscode | | "Einladungen verwalten". |
| Audit log | audit log | Audit-Log | | |

## Style rules

- Sentence case for all UI text (proper names such as Talk and Owner keep their capital).
- Typographic quotes and apostrophes: “…” and ’ in English, „…“ in German.
- Ellipsis: `…` directly after the word in English (`Loading…`), with a space in
  German (`Lädt …`).
- Errors: "Couldn't {verb} {object}. {What to do next}." (German: "{Objekt} konnte nicht {Verb} werden. {Nächster Schritt}.")
  End with a full stop.
- No repository paths, environment variable names or protocol names in sentences
  that normal members see. Put the detail in a tooltip or in the docs.
- No "please" in English; "bitte" in German only when the member has to act.
