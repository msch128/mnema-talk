# Windows game detection

The next Desktop DEV client update bundles a reference of 300 games. It checks
only the visible foreground window's executable basename against that reference
and your additional executable names. There is no game injection, background
process enumeration or runtime catalog download. Paths and window titles are
not sent to the instance. A matching name is a detection hint, not proof of
executable identity or exclusive fullscreen support.

The reference includes World of Warcraft, Genshin Impact, Star Wars: Empire at
War, Payday 1–3, Act of Aggression, Wardogs, Minecraft, League of Legends, Elder
Scrolls Online, Black Desert, Call of Duty and Beyond All Reason. Several titles
have multiple executable variants. Java Minecraft additionally requires a window
title containing “Minecraft”, so unrelated Java applications do not activate
Gaming. Modded launchers with different titles may need further detection work.

## Settings

Use the native **Gaming** menu in the instance selector or main desktop window,
or the settings button in **Alt+M**. The global switch controls both the overlay
and sidepeek. Their visibility still requires your own connected Talk and a
foreground game. The separate settings window works without those conditions
and offers no Talk controls or participant roster.

Background opacity applies to the passive sidepeek; text remains fully opaque.
The interactive overlay keeps its darker background for readable controls.
Additional EXE names are entered one per line (up to 32, basenames only).
Settings are stored locally and require the updated client; a server-only update
cannot add native detection or replace the bundled Gaming surfaces.

## Reference sources and scope

The [bundled catalog](../desktop/src/game-catalog.json) records source URLs and
its retrieval date. It combines requested games, Valve's
[most-played snapshot](https://api.steampowered.com/ISteamChartsService/GetMostPlayedGames/v1/)
and a [SteamSpy snapshot](https://steamspy.com/api.php?request=all&page=0), using
concurrent players within the returned 1,000-title subset. This is a curated
300-title reference, **not a global ranking of the 300 most-played games**.

Executable basenames come from Discord's public
[detectable applications catalog](https://discord.com/api/v9/applications/detectable),
with additional developer references for
[Wardogs](https://steamcommunity.com/app/1867240/discussions/3/562541966849738867/)
and [Beyond All Reason](https://www.beyondallreason.info/faq).
Only factual titles and executable names are included, without artwork or SDK code.
Launcher entries and generic helper executables are excluded.

Discord also supports manually registering games and Rich Presence, as described
in its [activity sharing guide](https://support.discord.com/hc/en-us/articles/7931156448919-Activity-Sharing-on-Discord-FAQ).
Mnema uses the narrower foreground-window approach above. A catalog entry does
not qualify every installation variant, anti-cheat system or fullscreen mode.
