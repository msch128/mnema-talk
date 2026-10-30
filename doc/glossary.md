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
| The single all-powerful account | Owner | Owner | admin, cool_guy (in docs) | Not translated; one per community. |
| Role or permission with all rights | Administrator | Administrator | Admin as a noun for a person | "Admin console" / "Admin-Bereich" stays as the name of the area. |
| Voice room | Talk | Talk | voice channel, Sprachkanal, call, Anruf, Raum | "Talk" is the Mnema product term in both languages. The create dialog may say "Talk (voice)" / "Talk (Sprache)". Verbs: join a Talk / einem Talk beitreten, leave. |
| Ring a member to a Talk | call (verb: “is calling you”; noun only as “missed call”) | anrufen: „ruft dich an“ (no noun) | Anruf, Telefonat | What “Start a Talk” does in a direct message: the others see an incoming-call card until they join, it stops, or it times out. The room stays a Talk. |
| Lobby that opens Talks | Talk lobby | Talk-Lobby | Create-a-Talk room, Warteraum | A Talk marked as a lobby: joining it opens a temporary Talk named after the lobby ("<lobby> 1", "<lobby> 2", ...) instead of a seat. Shown as "Create a Talk" / „Talk erstellen“ in the sidebar when named so. |
| Who may rename and limit a temporary Talk | Talk leader | Talk-Leitung | host, owner, Talk-Admin | The member who opened a temporary Talk, then the member who has been in it longest. Tooltip "Talk leader" / „Talk-Leitung“. |
| Text room | channel | Kanal | | Group of rooms: category / Kategorie. |
| Structured discussion room | forum channel | Forum-Kanal | | A channel of posts as cards with threads and tags. |
| Discussion starter in a forum | post | Beitrag | thread (for the root message) | A root message in a forum channel with a required title and initial message. |
| Forum tag | tag | Tag | label, category | Up to 20 admin-defined tags per forum channel, with an optional colour; a moderated tag is applied by moderators only. |
| Screen broadcast | screen share | Bildschirmübertragung | stream (as a noun for your own share) | A viewer watches a screen share / sieht eine Übertragung; "Nicht mehr ansehen" stops watching. |
| Mute your own microphone | mute / unmute | stummschalten / Stummschaltung aufheben | | Deafen = "Ton aus". Only for audio. |
| Silence notifications | mute notifications | Benachrichtigungen aus / stumm | | |
| Moderation silence | timeout | Auszeit | stummschalten, Timeout | "Auszeit verhängen / beenden". The permission is "Mitglieder in Auszeit schicken". |
| Block a member | ban | sperren / Sperre | | With a reason and a duration. |
| Stop seeing a member's messages and notifications | block / unblock | blockieren / Blockierung aufheben | ban, sperren, mute | Private: the other member is not told. "Blocked message · show" / „Blockierte Nachricht · anzeigen“. Distinct from ban (a moderation sanction). |
| Tell the moderators about a message | report | melden | flag, anzeigen | "Report message" / „Nachricht melden“. The moderators' list is "Reports" / „Meldungen“; a report is closed as resolved / erledigt or dismissed / abgelehnt. |
| Send a member away for now, no sanction | kick | rauswerfen | ban, sperren, disable | Signs the member out on every device and drops them from their Talk; they can sign in again at once. The permission is „Mitglieder rauswerfen“. |
| Disable an account (no record) | disable | deaktivieren | sperren | "Konto deaktivieren / aktivieren". |
| Brute-force lock | sign-in lock | Anmeldesperre | Login-Sperre | |
| Pin | pin / unpin | anheften / Anheftung aufheben | lösen | "Angeheftete Nachrichten". "Pinned by {name}" / „Angeheftet von {name}“. |
| Keep a message for yourself | save / remove from saved | speichern / aus Gespeichert entfernen | merken, favorisieren, lesezeichen | Private list: "Saved messages" / „Gespeicherte Nachrichten“. A message you can no longer see is "no longer available" / „nicht mehr verfügbar“. One header button opens the panel "Saved & reminders" / „Gespeichert & Erinnerungen“ with the tabs "Saved" / „Gespeichert“ and "Later" / „Später“. |
| Send a message on to another place | forward | weiterleiten | teilen, senden an | "Forward…" / „Weiterleiten …“; the quote is "Forwarded" / „Weitergeleitet“. |
| Private conversation | direct message | Direktnachricht | DM, private message, Privatnachricht | A conversation of two to ten members that is not a channel. "Group direct message" / „Gruppen-Direktnachricht“ for three or more. A conversation has no call of its own: "Start a Talk" opens a temporary Talk and posts an invitation (see [Talks](talks.md)). |
| Poll | poll | Umfrage | Abstimmung (only for the act of voting) | |
| Custom emoji | custom emoji | eigene Emojis | | "Emojis verwalten". |
| Sign in / out | sign in, sign out | anmelden, abmelden | log in, Login | Registering is registrieren. |
| Invite | invite code | Einladungscode | | "Einladungen verwalten". |
| Audit log | audit log | Audit-Log | | |

## Voice and network terms

Words from the installation and troubleshooting guides, in plain language.
They are not interface strings, so there is no German column. Start with
[How voice works](quick-start.md#how-it-fits-together) if these are new to you.

| Term | What it means here |
|---|---|
| Reverse proxy | A small web server in front of the app. It owns the HTTPS certificate, accepts connections from the internet on port 443 and passes them on to the app on port 8080. Caddy, nginx, Nginx Proxy Manager and the DSM reverse proxy are examples. It carries chat and the web page, **not** voice. |
| TLS / HTTPS | The encryption of web traffic (the padlock). It needs a certificate for your domain name; Let's Encrypt issues them for free. Browsers only allow microphone and camera on HTTPS pages (and on `localhost`). |
| WebSocket | A long-lived connection between the browser and the app that carries live events: new messages, who is typing, who joined a Talk. A proxy that does not pass it on leaves the page loaded but dead ("Connection lost – reconnecting…"). |
| WebRTC | The browser technology for real-time audio, video and screen share. Media travels as UDP packets directly between the browser and the voice server, not through the reverse proxy. |
| SFU | Selective forwarding unit: the part of the Mnema Talk server that receives each member's audio and video once and forwards the packets to the others. It does not mix or re-encode them. |
| UDP / TCP | The two ways data travels over a network. Web pages use TCP. Real-time media uses UDP because a late packet is worth less than a missing one. Routers and firewalls treat the two separately, so a port opened for TCP is not open for UDP. |
| NAT | Network address translation: your home router shares one public address between all devices in the house and only lets traffic in that you forwarded on purpose. That is why a server at home needs port forwarding. |
| Port forwarding | A router rule that sends incoming traffic on a port to one device inside your network. See [Network ports](ports.md#router-and-firewall-copy-paste-table). |
| NAT loopback / hairpin | Reaching your own server from inside your home network through its public address. Some routers cannot do it; the symptom is "works from mobile data, not from the home Wi-Fi". The fix is to tell the server its LAN address: `WEBRTC_LAN_IP=<LAN IP>`. |
| CGNAT / DS-Lite | Your provider shares one public IPv4 address between many customers (carrier-grade NAT; DS-Lite is a common form). Your router then has no public address of its own, so forwarded ports never receive anything from outside. Hosting at home does not work; use a VPS. See [Can I host at home?](quick-start.md#can-i-host-at-home). |
| STUN | A tiny service that tells a device its public address ("what do you see as my IP?"). The Mnema Talk server asks one (Google's or Cloudflare's by default) once at startup and every 10 minutes to find its own public IPv4 address, unless you set `WEBRTC_NAT_1TO1_IP`. |
| TURN | A relay for members whose network blocks direct UDP, typically company networks, some VPNs and hotels. Their media goes to the relay over a single port and the relay hands it to the voice server. Mnema Talk has one built in (port 3479, UDP and TCP), on by default. |
| ICE | The procedure a browser runs to find a working path to the server: it collects candidates, tries them in order of preference and picks one. "ICE failed" in a log means no candidate worked. |
| Candidate | One possible address a connection could use. **host** is a device's own LAN address, **srflx** (server reflexive) is the public address as seen from outside through STUN, **relay** is an address on a TURN relay. See [Reading chrome://webrtc-internals](troubleshooting-voice.md#reading-chromewebrtc-internals-and-aboutwebrtc). |
| Announced address | The address the server tells browsers to send media to (`WEBRTC_NAT_1TO1_IP`, or the one it found by itself). If it is wrong, browsers connect to nothing. |
| Dynamic DNS | A host name (for example from your router vendor or a free DNS service) that always points to your changing home IP address. |

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
