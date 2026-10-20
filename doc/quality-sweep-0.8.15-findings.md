# Quality sweep 0.8.15 — alle Repowise-Funde

Stand: 2026-10-20. Branch `quality-sweep-0.8.15`, Basis `fb8d412` (veröffentlichtes v0.8.14, `origin/main`).
Nächstes Release ist 0.8.15. `version.txt`, `CHANGELOG.md` und `.release-please-manifest.json` nicht von Hand ändern.

Diese Datei ist die Übergabe. Jeder Fund aus der Overview und jeder Fund des Live-Scans auf diesem Branch steht unten. Eine Zeile abhaken, wenn der Fund auf diesem Branch weg ist. Nichts streichen, nur weil es alt aussieht.

## Usage (Orca, abgefragt 2026-10-20)

| Konto | Fenster | verbraucht | übrig | Reset |
| --- | --- | ---: | ---: | --- |
| Grok (dieser Lauf) | Woche | 56 % | 44 % | Mon 7:54 |
| Claude | Woche | 99 % | 1 % | Mi 13:00 |
| Claude Fable | Woche | 12 % | 88 % | Mi 13:00 |
| Claude | 5 Stunden | 0 % | 100 % | — |
| Codex | 5 Stunden | 0 % | 100 % | Do 15:14 |
| Antigravity Gemini | Woche | 10 % | 90 % | — |
| Antigravity Claude/GPT | Woche | 0 % | 100 % | — |
| Cursor Models | Monat | 100 % | 0 % | 18. Nov |
| Gemini CLI | — | — | — | OAuth aus |

Schwelle zum Anfangen war mehr als 20 % übrig. Grok hat 44 % übrig, der Lauf darf anfangen. Wenn Grok leer ist: mit Antigravity (Gemini, 90 % übrig) oder Codex (Session leer) weiter. Claude-Wochenkontingent nicht nehmen (1 % übrig).

## Was die Overview zählt

Seite: http://127.0.0.1:3000/repos/b42f2b6dccef44c6827baca9db21fa70/overview

Der Index hängt an `/Users/m/orca/workspaces/mnema-talk/main`. Dieses Verzeichnis gibt es nicht mehr. Letzter Snapshot 2026-10-05 12:29 UTC. Der Index kennt 115 Commits und endet bei v0.1.6 (`5ab4760`, 2026-10-05 12:15 UTC). Gesundheit damals: Schnitt 7.45, Hotspots 5.89, schlechteste Datei `web/src/components/ChatArea.vue` mit 1.75.

Die Seite sagt **731 open**. Das ist die Summe dieser fünf Flächen, keine 731 unabhängigen Code-Defekte:

| Fläche | Zahl | Woraus |
| --- | ---: | --- |
| Security | 14 | jede Zeile in `security_findings`, alle im Working Tree, 0 in der Git-History |
| Code health | 447 | offene `health_findings`, ohne Testdateien, ohne versteckte Typen (`dry_violation`) |
| Refactoring | 44 | offene Pläne, gleiche Filter |
| Decisions | 84 | 47 vorgeschlagene Decisions + 37 ungoverned Hotspots. 0 stale |
| Ownership | 142 | Codedateien mit mehr als 80 % Commits bei einer Person |
| Summe | 731 | |

Nicht in den 731, aber vollständig weiter unten, weil sie Funde sind:

- 146 weitere offene Health-Funde (121 in Testdateien, 56 `dry_violation`; eine Zeile kann in beiden Mengen liegen, die Vereinigung ist 146). 593 offene Health-Zeilen insgesamt.
- 31 Dead-Code-Zeilen im Index. Keine ist `safe_to_delete`, deshalb zeigt die Overview 0.
- 18 offene Performance-Opportunities (plus erledigte).
- weitere offene Refactoring-Pläne außerhalb der 44.
- 299 Commit-Health-Funde. Das sind historische Zuschreibungen, keine zweite offene Queue.
- Doc-Drift: 0.

Live auf diesem Branch, `repowise health --scope production --counts code_shape`, 2026-10-20: **848 Funde** (40 critical, 61 high, 423 medium, 324 low). Schnitt 9.01, Hotspot-Schnitt 8.63, schlechteste Datei `web/src/lib/media/mediaFrameWorker.ts` mit 5.85. History-Biomarker sind in diesem Lauf nicht enthalten (`--counts code_shape`). Ein Folgelauf mit `--counts everything` ergänzt Churn, Prior-Defect und Entropy.

## Roadmap: wo jeder Fund verschwindet

Zeilennummern aus dem Oktober-Index nicht anfassen. Die Dateien wurden zwischen v0.1.6 und v0.8.14 zerlegt. `ChatArea.vue` hat heute 346 Zeilen, `useWebRTC.ts` 68, `internal/config/config.go` 110. Erst die Datei auf diesem Branch lesen.

Reihenfolge. Nach jedem Paket `repowise health --format md --scope production --counts code_shape` und die verschwundenen Zeilen in Abschnitt „Live“ abhaken. Limits nicht senken, Allowlists nicht verlängern, Coverage-Floors nicht senken. Datei unter 400 Zeilen, Funktion unter 80, kognitive Komplexität unter 15. Kein `Co-authored-by`. Kein Force-Push. Kein Text-E2EE. `MEDIA_RETENTION_DAYS` bleibt 0. Native-Preview-Routen nicht an `NewRouter` hängen und den Pfad nicht löschen.

### 1. Critical und High im Live-Scan (101 Zeilen)

Das sind die ersten Code-Änderungen. Meist eine Bedingung mit 6 oder mehr Operatoren oder zyklomatische Komplexität ab etwa 25. Die Bedingung in benannte Prädikate zerlegen, die Funktion in die bestehenden Helfer des Pakets schneiden. Verhalten und Tests bleiben. Pakete, Datei für Datei, stärkster Abzug zuerst. Die Reihenfolge steht in „Live nach Datei“.

`web/src/third_party/deepfilternet3/worklet.js` nicht anfassen. Das ist Vendor-Code, im ESLint schon ausgenommen. Der eine Critical („292 Zeilen“) bleibt, bis das Vendor-Update kommt oder der Scanner `web/src/third_party` ausnimmt. Nicht als erledigt markieren.

### 2. Medium und Low, gleiche Form (cyclomatic, boolean, nesting, long parameter list)

321 cyclomatic, 182 Bedingungen, 17 Nesting, 11 lange Parameterlisten. Repowise markiert zyklomatisch ab 9, das Repo gocognit erst bei kognitiv 15. Trotzdem jede Zeile unter die Repowise-Schwelle bringen: zyklomatisch unter 9, weniger als 3 boolesche Operatoren in einer Bedingung, weniger als 4 Nesting-Ebenen, unter 5 skalare Parameter. Ein Struct statt einer langen Parameterliste. Keine neue Abstraktion, die nur dem Scanner dient, wenn ein Schnitt entlang einer schon vorhandenen Verantwortung geht.

### 3. Duplikate (82)

Nur den gemeinsamen Block herausziehen, den der Fund nennt. Ähnliche HTTP-Handler nicht zu einem Framework falten. Liegt der Klon in Testdateien oder Fixtures, den Helper im Testpaket lassen.

### 4. Verworfene Fehler (113)

Den Fehler zurückgeben oder an der einen Stelle behandeln, die das schon für dieses Paket tut. Ein `_` löschen, indem der Fehler weitergereicht wird. Kein leeres `if err != nil`.

### 5. Schleifen mit I/O (29), String-Concat (2), defer (1), Goroutine (2)

Ein Datenbankaufruf pro Schleifendurchlauf im Request-Pfad wird eine mengenbasierte Aussage. Migrations- und Worker-Schleifen, die absichtlich eine Zeile nach der anderen schreiben, bleiben mengenbasiert, wenn das Verhalten gleich bleibt. `defer` in der Schleife vor die Schleife oder in eine Funktion pro Element. Eine Goroutine ohne Grenze bekommt den vorhandenen Worker oder ein Semaphore. String-Concat in einer Schleife wird `strings.Builder` oder `join`.

### 6. Security, 14 Index-Zeilen

Keine Snippets in dieser Datei. Aktionen stehen in der Security-Tabelle. HMAC-SHA1 für TURN bleibt. Login-Feldname bleibt. Test-Platzhalter bleiben, wenn sie Dummies sind und gitleaks sie schon erlaubt. `docker-compose.yml` auf diesem Branch neu lesen, bevor eine Zeile geändert wird.

### 7. Dead Code

Index: 31 Zeilen, keine löschsicher. Live: 24 `unused_export` bei 40 % Confidence, alle `safe_to_delete=false`, oft „wird woanders namentlich erwähnt“ (OpenAPI, Tests, Fixtures). Erst mit dem Compiler und den Tests prüfen. Einen exportierten Request-Typ, den OpenAPI nennt, nicht löschen. Einen Fixture-Export, den ein Test per Namen nutzt, nicht löschen. `e2e` als Zombie-Package im Index ist die Testsuite, kein ungenutztes Modul.

### 8. Refactoring-Pläne (44 in der Overview)

Ein Plan wird nur ausgeführt, wenn das Symbol auf diesem Branch noch die beschriebene Form hat. Sonst ist der Plan mit v0.1.6 veraltet und die Live-Zeile der Datei gilt.

### 9. Decisions (47) und ungoverned Hotspots (37)

Das sind Repowise-Review-Einträge, keine Produktlücken. Nach dem Reindex annehmen oder verwerfen. Kein Produktverhalten ändern, nur damit ein Auto-Vorschlag grün wird. Die 37 Hotspots sind dieselben Dateien wie die Health-Zeilen `ungoverned_hotspot`. Ein akzeptierter Decision-Record, der die Datei nennt, räumt die Governance-Zeile. Dafür muss der Index neu laufen.

### 10. Ownership (142)

Ein Maintainer. Mehr als 80 % der Commits auf einer Datei ist dann der Normalfall. Nicht mit erfundenen Autoren oder `Co-authored-by` „lösen“. Die Zeile geht erst weg, wenn wirklich eine zweite Person die Datei regelmäßig ändert. Stehen lassen und nicht als Code-Defekt behandeln.

### 11. History-Biomarker aus dem Index

`churn_risk`, `prior_defect`, `change_entropy`, `co_change_scatter`, `function_hotspot`, `knowledge_loss`, `hidden_coupling`, `bumpy_road` beschreiben Git, nicht den Quelltext. History nicht umschreiben. Sie fallen aus dem Score, wenn die Datei danach ruhig bleibt, und sie kommen im Live-Lauf mit `--counts everything` neu. Der code_shape-Lauf hat sie bewusst nicht gezählt.

### 12. Overview-URL selbst

Die 731 bleiben auf der alten URL, bis der Index neu built. `repowise security scan` ohne `--history` ist ein No-Op. Ein `repowise update` oder `init` aus diesem Checkout legt eine neue Repo-Id an. Die alte Id `b42f2b6dccef44c6827baca9db21fa70` zeigt weiter den Stand vom 5. Oktober, solange niemand diesen Index neu schreibt. Code zuerst, Reindex danach.

### Was die Overview als Nächstes vorschlug (Woche bis 5. Oktober)

1. Critical, großer Aufwand: 27 ernste Funde in 18 Dateien, Start `web/src/components/ChatArea.vue` (2 critical). Das war der Baum von v0.1.6. Heute ist ChatArea 346 Zeilen und hat `ChatArea.test.ts`.
2. Tests um `web/src/composables/useWebRTC.js`. Heute `useWebRTC.ts`, 68 Zeilen, mehrere `useWebRTC.*.test.ts`.
3. Tests um `ChatArea.vue`. Vorhanden.
4. Tests um `web/src/stores/chat.js`. Heute `chat.ts`, Tests in `chatStoreBehaviour.test.ts`.
5. Tests um `internal/config/config.go`. Die Datei ist 110 Zeilen, Validierung liegt in `config_validate.go` und hat den Live-Critical `validateUpdater`.

Diese fünf Punkte nicht noch einmal als neue Features bauen. Der offene Rest von Punkt 5 ist der Live-Fund in `config_validate.go`.

## Live nach Datei

Abzug ist die Summe der `impact`-Zahlen. Critical zuerst innerhalb der Datei. Zeile abhaken, wenn ein neuer Health-Lauf sie nicht mehr nennt.

Dateien: 321. Funde: 848.

### `web/src/lib/media/mediaFrameWorker.ts` — 15 Funde, Abzug 4.17.

Erledigt 2026-10-20: `repowise health --file` auf `mediaFrameWorker.ts`, `frameWorkerLine.ts` und `frameWorkerPort.ts` nennt keinen Fund mehr. Verhalten unverändert, 27 Vitest-Tests grün.

- [x] [high] lineFrom - lineFrom has cyclomatic complexity 18 (impact -0.31)
- [x] [high] onCapsMessage - if condition combines 5 boolean operators (impact -0.51)
- [x] [high] onKeyMessage - if condition combines 5 boolean operators (impact -0.51)
- [x] [high] onLineMessage - if condition combines 5 boolean operators (impact -0.51)
- [x] [high] onLineMessage - onLineMessage has cyclomatic complexity 16 (impact -0.31)
- [x] [high] remember - remember has cyclomatic complexity 18 (impact -0.31)
- [x] [high] remember - ternary condition combines 5 boolean operators (impact -0.51)
- [x] [medium] lineFrom - if condition combines 4 boolean operators (impact -0.30)
- [x] [medium] onCapsMessage - onCapsMessage has cyclomatic complexity 11 (impact -0.18)
- [x] [medium] onKeyMessage - onKeyMessage has cyclomatic complexity 13 (impact -0.18)
- [x] [medium] sealOutgoing - sealOutgoing has cyclomatic complexity 14 (impact -0.18)
- [x] [low] - caught exception is swallowed without any handling (impact -0.15)
- [x] [low] lineFrom - ternary condition combines 3 boolean operators (impact -0.13)
- [x] [low] remember - long parameter list: remember takes 6 parameters, 3 of 5 typed ones scalars or strings (impact -0.03)
- [x] [low] remember - remember has 3 nested blocks at the same level (bumpy road) (impact -0.05)

### `web/src/lib/media/guard.ts` — 7 Funde, Abzug 4.06.

Erledigt 2026-10-20: `repowise health --file web/src/lib/media/guard.ts` nennt keinen Fund mehr. 7 Vitest-Tests grün.

- [x] [high] readStats - readStats has cyclomatic complexity 18 (impact -1.36)
- [x] [medium] readStats - if condition combines 4 boolean operators (impact -0.93)
- [x] [medium] readStats - if condition combines 4 boolean operators (impact -0.93)
- [x] [low] - caught exception is swallowed without any handling (impact -0.15)
- [x] [low] - caught exception is swallowed without any handling (impact -0.15)
- [x] [low] keyMessage - long parameter list: keyMessage takes 6 parameters, 4 of 6 typed ones scalars or strings (impact -0.14)
- [x] [low] readStats - if condition combines 3 boolean operators (impact -0.40)

### `internal/auth/native_password.go` — 6 Funde, Abzug 4.02.

Erledigt 2026-10-20: `repowise health --file internal/auth/native_password.go` nennt keinen Fund mehr. Dieselben Prüfungen, kürzere Bedingungen. `go test -tags=integration ./internal/auth/ -run NativePassword` grün.

- [x] [critical] checkReissueLease - if condition combines 7 boolean operators (impact -2.17)
- [x] [medium] changePasswordAndReissue - changePasswordAndReissue has cyclomatic complexity 9 (impact -0.38)
- [x] [medium] checkReissueLease - checkReissueLease has cyclomatic complexity 10 (impact -0.38)
- [x] [medium] prepareReissue - prepareReissue has cyclomatic complexity 9 (impact -0.38)
- [x] [medium] reissue - reissue has cyclomatic complexity 9 (impact -0.38)
- [x] [low] changePasswordAndReissue - if condition combines 3 boolean operators (impact -0.33)

### `web/src/lib/streamQuality.ts` — 9 Funde, Abzug 4.02.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. 18 Vitest-Tests grün. Die Typprüfung bleibt bei derselben Statistik.

- [x] [critical] summarizeSendStats - summarizeSendStats has cyclomatic complexity 27 (impact -0.65)
- [x] [high] encodeFrameStats - encodeFrameStats has cyclomatic complexity 17 (impact -0.39)
- [x] [medium] encodeFrameStats - if condition combines 4 boolean operators (impact -0.92)
- [x] [medium] formatSendStats - formatSendStats has cyclomatic complexity 10 (impact -0.23)
- [x] [medium] normalizeQuality - normalizeQuality has cyclomatic complexity 12 (impact -0.23)
- [x] [low] encodeFrameStats - if condition combines 3 boolean operators (impact -0.40)
- [x] [low] normalizeQuality - ternary condition combines 3 boolean operators (impact -0.40)
- [x] [low] streamModeOf - if condition combines 3 boolean operators (impact -0.40)
- [x] [low] summarizeSendStats - ternary condition combines 3 boolean operators (impact -0.40)

### `internal/auth/native_handler.go` — 6 Funde, Abzug 4.01.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Unit- und Native-Integrationstests grün. Dieselben Abhängigkeiten und derselbe Transport werden abgelehnt.

- [x] [critical] NewNativeHandler - if condition combines 7 boolean operators (impact -1.47)
- [x] [medium] AuthenticateNativeRequest - if condition combines 4 boolean operators (impact -0.52)
- [x] [medium] NewNativeHandler - NewNativeHandler has cyclomatic complexity 9 (impact -0.50)
- [x] [medium] RequireUser - if condition combines 4 boolean operators (impact -0.52)
- [x] [medium] decodeNativeStrings - decodeNativeStrings has cyclomatic complexity 9 (impact -0.50)
- [x] [medium] login - login has cyclomatic complexity 11 (impact -0.50)

### `web/scripts/check-coverage.ts` — 14 Funde, Abzug 4.01.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Schwellen bleiben 80 und 90. Der Quellenlauf liegt in check-coverage-sources.ts.

- [x] [critical] checkCoverage - checkCoverage has cyclomatic complexity 28 (impact -0.60)
- [x] [critical] coverage - if condition combines 6 boolean operators (impact -0.58)
- [x] [critical] policy - if condition combines 6 boolean operators (impact -0.58)
- [x] [critical] policy - if condition combines 7 boolean operators (impact -0.58)
- [x] [critical] policy - policy has cyclomatic complexity 31 (impact -0.60)
- [x] [medium] checkCoverage - checkCoverage has 4 nested blocks at the same level (bumpy road) (impact -0.08)
- [x] [medium] checkCoverage - checkCoverage nests 4 levels deep (impact -0.21)
- [x] [medium] executableSources - a filesystem call runs once per loop iteration (I/O call inside a loop) (impact -0.00)
- [x] [medium] executableSources - executableSources has cyclomatic complexity 12 (impact -0.21)
- [x] [medium] policy - policy nests 4 levels deep (impact -0.21)
- [x] [low] checkCoverage - checkCoverage is 69 lines long (impact -0.09)
- [x] [low] checkCoverage - if condition combines 3 boolean operators (impact -0.09)
- [x] [low] executableSources - if condition combines 3 boolean operators (impact -0.09)
- [x] [low] policy - if condition combines 3 boolean operators (impact -0.09)

### `internal/config/config_validate.go` — 7 Funde, Abzug 4.00.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. `MEDIA_RETENTION_DAYS` bleibt aus. Dieselben Werte werden angenommen und abgelehnt.

- [x] [critical] validateUpdater - if condition combines 6 boolean operators (impact -2.17)
- [x] [medium] validate - validate has cyclomatic complexity 11 (impact -0.30)
- [x] [medium] validateBasics - validateBasics has cyclomatic complexity 11 (impact -0.30)
- [x] [medium] validateLimits - validateLimits has cyclomatic complexity 11 (impact -0.30)
- [x] [medium] validateProductionSecrets - validateProductionSecrets has cyclomatic complexity 13 (impact -0.30)
- [x] [medium] validateUpdater - validateUpdater has cyclomatic complexity 13 (impact -0.30)
- [x] [low] validateBasics - if condition combines 3 boolean operators (impact -0.33)

### `internal/nativepreview/native_preview.go` — 5 Funde, Abzug 4.00.

Erledigt 2026-10-20: `repowise health --file` nennt keinen Fund mehr. Integrationstests des Pakets grün. Routen bleiben unmounted in NewRouter.

- [x] [critical] validateNativePreviewOptions - if condition combines 7 boolean operators (impact -0.96)
- [x] [critical] validateNativePreviewOptions - if condition combines 9 boolean operators (impact -0.96)
- [x] [critical] validateNativePreviewOptions - validateNativePreviewOptions has cyclomatic complexity 25 (impact -1.11)
- [x] [high] boundary - if condition combines 5 boolean operators (impact -0.58)
- [x] [medium] NewNativePreviewRouter - NewNativePreviewRouter has cyclomatic complexity 10 (impact -0.39)

### `internal/ssrf/ssrf.go` — 6 Funde, Abzug 4.00.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselben Adressen und URLs werden blockiert.

- [x] [high] ValidateURL - if condition combines 5 boolean operators (impact -1.36)
- [x] [medium] Check - Check has cyclomatic complexity 13 (impact -0.50)
- [x] [medium] Check - if condition combines 4 boolean operators (impact -0.80)
- [x] [medium] DialContext - DialContext has cyclomatic complexity 11 (impact -0.50)
- [x] [medium] ValidateURL - ValidateURL has cyclomatic complexity 14 (impact -0.50)
- [x] [low] Check - if condition combines 3 boolean operators (impact -0.34)

### `web/src/lib/native/nativeTrust.ts` — 5 Funde, Abzug 4.00.

Erledigt 2026-10-20: `repowise health --file` nennt keinen Fund mehr. Dieselben Ablehnungen, 17 Setup-Tests grün.

- [x] [critical] decodeTrustPreview - decodeTrustPreview has cyclomatic complexity 27 (impact -1.50)
- [x] [critical] decodeTrustPreview - if condition combines 8 boolean operators (impact -0.93)
- [x] [critical] decodeTrustPreview - if condition combines 8 boolean operators (impact -0.93)
- [x] [medium] decodeTrustPreview - if condition combines 4 boolean operators (impact -0.32)
- [x] [medium] decodeTrustStatus - if condition combines 4 boolean operators (impact -0.32)

### `web/src/lib/trustPort.ts` — 6 Funde, Abzug 4.00.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselben Antworten werden angenommen, abgelehnt und veröffentlicht.

- [x] [critical] reply - if condition combines 11 boolean operators (impact -1.67)
- [x] [medium] beginFirstRoot - beginFirstRoot has cyclomatic complexity 12 (impact -0.50)
- [x] [medium] beginFirstRoot - if condition combines 4 boolean operators (impact -0.58)
- [x] [medium] receive - receive has cyclomatic complexity 10 (impact -0.50)
- [x] [medium] reply - reply has cyclomatic complexity 13 (impact -0.50)
- [x] [low] publish - if condition combines 3 boolean operators (impact -0.25)

### `web/src/lib/native/nativeAdmin.ts` — 12 Funde, Abzug 3.99.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. 39 Tests, dieselben Admin-Operationen.

- [x] [critical] createInvite - if condition combines 6 boolean operators (impact -0.64)
- [x] [high] createChannel - if condition combines 5 boolean operators (impact -0.39)
- [x] [high] replaceLayout - replaceLayout has cyclomatic complexity 19 (impact -0.69)
- [x] [high] updateChannel - if condition combines 5 boolean operators (impact -0.39)
- [x] [medium] createChannel - createChannel has cyclomatic complexity 9 (impact -0.40)
- [x] [medium] createInvite - createInvite has cyclomatic complexity 9 (impact -0.40)
- [x] [medium] replaceLayout - if condition combines 4 boolean operators (impact -0.22)
- [x] [medium] replaceLayout - if condition combines 4 boolean operators (impact -0.22)
- [x] [medium] updateTarget - if condition combines 4 boolean operators (impact -0.22)
- [x] [medium] updateTarget - if condition combines 4 boolean operators (impact -0.22)
- [x] [low] replaceLayout - if condition combines 3 boolean operators (impact -0.10)
- [x] [low] updateTarget - if condition combines 3 boolean operators (impact -0.10)

### `web/src/lib/media/groupEpoch.ts` — 13 Funde, Abzug 3.98.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselben Epochen werden abgelehnt.

- [x] [critical] adopt - if condition combines 6 boolean operators (impact -1.79)
- [x] [high] adopt - adopt has cyclomatic complexity 16 (impact -0.29)
- [x] [medium] commit - commit has cyclomatic complexity 14 (impact -0.17)
- [x] [medium] commitAdds - commitAdds has cyclomatic complexity 9 (impact -0.17)
- [x] [medium] forgetDeparted - forgetDeparted has 4 nested blocks at the same level (bumpy road) (impact -0.24)
- [x] [medium] forgetDeparted - forgetDeparted has cyclomatic complexity 9 (impact -0.17)
- [x] [medium] livenessWires - livenessWires has cyclomatic complexity 9 (impact -0.17)
- [x] [medium] maintenanceCommits - maintenanceCommits has cyclomatic complexity 14 (impact -0.17)
- [x] [medium] renewalDue - renewalDue has cyclomatic complexity 9 (impact -0.17)
- [x] [medium] trackLeaves - trackLeaves has cyclomatic complexity 9 (impact -0.17)
- [x] [low] commit - commit has 3 nested blocks at the same level (bumpy road) (impact -0.10)
- [x] [low] maintenanceCommits - if condition combines 3 boolean operators (impact -0.27)
- [x] [low] trackLeaves - trackLeaves has 3 nested blocks at the same level (bumpy road) (impact -0.10)

### `web/src/lib/native/nativeChatView.ts` — 9 Funde, Abzug 3.98.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselbe Projektion wird abgelehnt. 44 Tests grün.

- [x] [critical] accept - accept has cyclomatic complexity 26 (impact -0.75)
- [x] [critical] accept - if condition combines 6 boolean operators (impact -0.57)
- [x] [critical] row - if condition combines 11 boolean operators (impact -0.57)
- [x] [critical] row - if condition combines 6 boolean operators (impact -0.57)
- [x] [critical] row - row has cyclomatic complexity 38 (impact -0.75)
- [x] [high] row - if condition combines 5 boolean operators (impact -0.34)
- [x] [medium] accept - if condition combines 4 boolean operators (impact -0.20)
- [x] [medium] row - if condition combines 4 boolean operators (impact -0.20)
- [x] [low] accept - accept has 3 nested blocks at the same level (bumpy road) (impact -0.03)

### `web/src/lib/native/nativeChatApi.ts` — 15 Funde, Abzug 3.96.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. 503 UNAVAILABLE bleibt.

- [x] [critical] messageActionInput - if condition combines 6 boolean operators (impact -0.89)
- [x] [critical] newMessageInput - if condition combines 7 boolean operators (impact -0.89)
- [x] [high] handle - handle has cyclomatic complexity 21 (impact -0.25)
- [x] [high] messageActionInput - messageActionInput has cyclomatic complexity 16 (impact -0.25)
- [x] [high] request - request has cyclomatic complexity 17 (impact -0.25)
- [x] [medium] completedMessageId - completedMessageId has cyclomatic complexity 9 (impact -0.14)
- [x] [medium] handleChannelMessages - handleChannelMessages has cyclomatic complexity 11 (impact -0.14)
- [x] [medium] messageActionInput - if condition combines 4 boolean operators (impact -0.31)
- [x] [medium] newMessageInput - newMessageInput has cyclomatic complexity 13 (impact -0.14)
- [x] [medium] pageOf - pageOf has cyclomatic complexity 12 (impact -0.14)
- [x] [medium] receiptMatches - receiptMatches has cyclomatic complexity 13 (impact -0.14)
- [x] [low] completedMessageId - if condition combines 3 boolean operators (impact -0.13)
- [x] [low] handle - if condition combines 3 boolean operators (impact -0.13)
- [x] [low] handle - if condition combines 3 boolean operators (impact -0.13)
- [x] [low] messageActionInput - long parameter list: messageActionInput takes 5 parameters, 3 of 5 typed ones scalars or strings (impact -0.03)

### `web/src/lib/crypto/stateStore.ts` — 8 Funde, Abzug 3.94.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Die Datei bleibt ein Modul für das Browser-Harness. 38 Tests grün.

- [x] [medium] transaction - transaction nests 4 levels deep (impact -0.94)
- [x] [medium] validateUsage - validateUsage has cyclomatic complexity 9 (impact -0.75)
- [x] [medium] validatedConfig - validatedConfig has cyclomatic complexity 10 (impact -0.75)
- [x] [low] - caught exception is swallowed without any handling (impact -0.15)
- [x] [low] - caught exception is swallowed without any handling (impact -0.15)
- [x] [low] revision - if condition combines 3 boolean operators (impact -0.40)
- [x] [low] validateUsage - if condition combines 3 boolean operators (impact -0.40)
- [x] [low] validatedConfig - if condition combines 3 boolean operators (impact -0.40)

### `web/src/lib/native/nativeTransport.ts` — 34 Funde, Abzug 3.93.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselben Port-, Login- und Routen-Ablehnungen bleiben. Die Sitzung liegt in den Nachbardateien.

- [x] [critical] adoptPublicDescriptor - if condition combines 7 boolean operators (impact -0.28)
- [x] [critical] call - if condition combines 7 boolean operators (impact -0.28)
- [x] [critical] constructor - if condition combines 6 boolean operators (impact -0.28)
- [x] [critical] nativeRegister - nativeRegister has cyclomatic complexity 32 (impact -0.21)
- [x] [critical] recoverAuthenticationForExplicitLogin - if condition combines 6 boolean operators (impact -0.28)
- [x] [high] adoptPublicDescriptor - adoptPublicDescriptor has cyclomatic complexity 16 (impact -0.12)
- [x] [high] call - call has cyclomatic complexity 16 (impact -0.12)
- [x] [high] constructor - constructor has cyclomatic complexity 17 (impact -0.12)
- [x] [high] initializeNativeContext - initializeNativeContext has cyclomatic complexity 17 (impact -0.12)
- [x] [high] nativeLogin - nativeLogin has cyclomatic complexity 19 (impact -0.12)
- [x] [high] receive - if condition combines 5 boolean operators (impact -0.17)
- [x] [high] receive - receive has cyclomatic complexity 23 (impact -0.12)
- [x] [high] recoverAuthenticationForExplicitLogin - if condition combines 5 boolean operators (impact -0.17)
- [x] [high] recoverAuthenticationForExplicitLogin - recoverAuthenticationForExplicitLogin has cyclomatic complexity 19 (impact -0.12)
- [x] [high] send - send has cyclomatic complexity 19 (impact -0.12)
- [x] [medium] captureNativeTrustPresentation - captureNativeTrustPresentation has cyclomatic complexity 10 (impact -0.07)
- [x] [medium] constructor - if condition combines 4 boolean operators (impact -0.10)
- [x] [medium] nativeLogin - if condition combines 4 boolean operators (impact -0.10)
- [x] [medium] nativeLogin - if condition combines 4 boolean operators (impact -0.10)
- [x] [medium] nativeLogout - nativeLogout has cyclomatic complexity 9 (impact -0.07)
- [x] [medium] nativePassword - if condition combines 4 boolean operators (impact -0.10)
- [x] [medium] nativePassword - nativePassword has cyclomatic complexity 14 (impact -0.07)
- [x] [medium] nativeTypedChat - if condition combines 4 boolean operators (impact -0.10)
- [x] [medium] nativeTypedChat - nativeTypedChat has cyclomatic complexity 12 (impact -0.07)
- [x] [medium] nativeUserId - if condition combines 4 boolean operators (impact -0.10)
- [x] [medium] receive - if condition combines 4 boolean operators (impact -0.10)
- [x] [low] captureNativeTrustPresentation - if condition combines 3 boolean operators (impact -0.04)
- [x] [low] connectNative - if condition combines 3 boolean operators (impact -0.04)
- [x] [low] initializeNativeContext - if condition combines 3 boolean operators (impact -0.04)
- [x] [low] nativeRegister - if condition combines 3 boolean operators (impact -0.04)
- [x] [low] personalUpdate - if condition combines 3 boolean operators (impact -0.04)
- [x] [low] personalUpdate - if condition combines 3 boolean operators (impact -0.04)
- [x] [low] personalUpdate - if condition combines 3 boolean operators (impact -0.04)
- [x] [low] send - if condition combines 3 boolean operators (impact -0.04)

### `internal/auth/native_token.go` — 4 Funde, Abzug 3.65.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dasselbe Alphabet, dieselbe Länge, Schreibfehler beim Redigieren werden geprüft.

- [x] [critical] parseNativeSecret - if condition combines 7 boolean operators (impact -2.50)
- [x] [medium] parseNativeSecret - parseNativeSecret has cyclomatic complexity 14 (impact -0.85)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/auth/native_lock.go` — 3 Funde, Abzug 3.50.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Lease und Reissue prüfen dieselben Fristen. Native-Integrationstests grün.

- [x] [critical] checkLease - if condition combines 7 boolean operators (impact -2.50)
- [x] [medium] checkLease - checkLease has cyclomatic complexity 10 (impact -0.85)
- [x] [low] - 22% of file duplicated in other files; worst clone shares 11 lines with internal/auth/native_password.go (impact -0.15)

### `internal/auth/native_password_handler.go` — 3 Funde, Abzug 3.50.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselben Abhängigkeiten und Routen.

- [x] [critical] PasswordChangeHandler - if condition combines 6 boolean operators (impact -2.50)
- [x] [medium] PasswordChangeHandler - PasswordChangeHandler has cyclomatic complexity 10 (impact -0.85)
- [x] [low] - 21% of file duplicated in other files; worst clone shares 16 lines with internal/auth/native_registration.go (impact -0.15)

### `internal/auth/native_refresh.go` — 6 Funde, Abzug 3.50.

Erledigt 2026-10-20: Komplexität ist weg. `repowise health --file --recompute` nennt nur noch die Löschschleife. Die bleibt: jede Familie wird user-first mit SKIP LOCKED und eigener Uhr geprüft. Eine Mengen-Löschung würde diese Reihenfolge ändern.

- [x] [high] revokeAuthenticatedFamily - if condition combines 5 boolean operators (impact -1.60)
- [ ] [medium] cleanupNativeBatch - a database call is reached once per loop iteration through cleanupNativeBatch -> deleteFamilyIfDead (cross-function N+1 query) (impact -0.00). Angenommen: die Fault-Tests pinnen Sperre, Uhr und Löschung pro Familie.
- [x] [medium] deleteFamilyIfDead - deleteFamilyIfDead has cyclomatic complexity 11 (impact -0.50)
- [x] [medium] revokeAuthenticatedFamily - revokeAuthenticatedFamily has cyclomatic complexity 10 (impact -0.50)
- [x] [medium] rotate - rotate has cyclomatic complexity 12 (impact -0.50)
- [x] [low] deleteFamilyIfDead - if condition combines 3 boolean operators (impact -0.40)

### `internal/auth/native_session.go` — 3 Funde, Abzug 3.50.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselben Laufzeiten und dieselbe Familie. Native-Integrationstests grün.

- [x] [critical] NewNativeSessions - if condition combines 6 boolean operators (impact -2.50)
- [x] [medium] openFamily - openFamily has cyclomatic complexity 9 (impact -0.85)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/ws/native_metadata_admission.go` — 5 Funde, Abzug 3.44.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselben Frames werden abgelehnt.

- [x] [high] parseNativeMetadataFrame - if condition combines 5 boolean operators (impact -1.60)
- [x] [high] parseNativeMetadataFrame - parseNativeMetadataFrame has cyclomatic complexity 21 (impact -0.69)
- [x] [medium] - 14% of file duplicated in other files; worst clone shares 13 lines with internal/ws/native_admission.go (co-changed 3x) (impact -0.35)
- [x] [medium] dispatchMetadataFrame - dispatchMetadataFrame has cyclomatic complexity 9 (impact -0.40)
- [x] [medium] nativeMetadataObject - nativeMetadataObject has cyclomatic complexity 10 (impact -0.40)

### `internal/nativepreview/native_discovery.go` — 2 Funde, Abzug 3.35.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselbe Anfrage wird abgelehnt.

- [x] [critical] discovery - if condition combines 6 boolean operators (impact -2.50)
- [x] [medium] discovery - discovery has cyclomatic complexity 9 (impact -0.85)

### `web/src/stores/chat/messageWindow.ts` — 6 Funde, Abzug 3.24.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselben Seiten und Sprünge. Die zwei `if (!channelId)` danach sind dieselbe frühe Rückgabe. Der Hotspot ohne Paar-Testdatei bleibt: `chatStoreBehaviour.test.ts` prüft das Fenster, ein Dummy-Test nur für den Detektor kommt nicht dazu. Angenommen.

- [x] [medium] jumpToRootMessage - jumpToRootMessage has cyclomatic complexity 11 (impact -0.50)
- [x] [medium] jumpToRootMessage - jumpToRootMessage nests 4 levels deep (impact -0.94)
- [x] [medium] loadNewer - loadNewer has cyclomatic complexity 9 (impact -0.50)
- [x] [medium] loadOlder - loadOlder has cyclomatic complexity 9 (impact -0.50)
- [x] [low] loadNewer - if condition combines 3 boolean operators (impact -0.40)
- [x] [low] loadOlder - if condition combines 3 boolean operators (impact -0.40)

### `web/src/lib/desktopGaming.ts` — 4 Funde, Abzug 3.11.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselben Akkorde, der Anruf und die Lease. Der Token-Parameter ist `string | null`, der Vergleich bleibt.

- [x] [medium] chordFromKey - chordFromKey has cyclomatic complexity 10 (impact -0.85)
- [x] [medium] chordFromKey - if condition combines 4 boolean operators (impact -0.93)
- [x] [medium] inCall - if condition combines 4 boolean operators (impact -0.93)
- [x] [low] leaseIsLive - if condition combines 3 boolean operators (impact -0.40)

### `internal/perm/store_overwrites.go` — 5 Funde, Abzug 3.10.

Erledigt 2026-10-20: Komplexität ist weg. `repowise health --file --recompute` nennt nur noch die beiden Schleifen. Die bleiben: Rollen- und Mitgliederzeilen stehen in der eingereichten Reihenfolge, und die Bitprüfung läuft vor der Mitgliedsabfrage.

- [x] [high] authorizeOverwriteChanges - if condition combines 5 boolean operators (impact -1.60)
- [x] [medium] authorizeOverwriteChanges - authorizeOverwriteChanges has cyclomatic complexity 10 (impact -0.75)
- [ ] [medium] replaceOverwrites - a database call runs once per loop iteration (N+1 query) (impact -0.00). Angenommen: eine Sammelzeile würde die Fehlerreihenfolge ändern.
- [ ] [medium] validateOverwrites - a database call is reached once per loop iteration through validateOverwrites -> checkOverwriteSubject (cross-function N+1 query) (impact -0.00). Angenommen: die Mitgliedsabfrage bleibt hinter der Bitprüfung derselben Zeile.
- [x] [medium] validateOverwrites - validateOverwrites has cyclomatic complexity 10 (impact -0.75)

### `web/src/components/AdminRolesTab.vue` — 3 Funde, Abzug 3.10.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselbe Rolle wird gespeichert und abgelegt.


- [x] [high] onDrop - if condition combines 5 boolean operators (impact -1.60)
- [x] [medium] onDrop - onDrop has cyclomatic complexity 9 (impact -0.75)
- [x] [medium] save - save has cyclomatic complexity 11 (impact -0.75)

### `internal/sfu/subscriptions.go` — 2 Funde, Abzug 3.05.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselbe Ansicht und dieselben Keyframes bleiben.


- [x] [high] Subscribe - Subscribe has cyclomatic complexity 15 (impact -1.45)
- [x] [high] Subscribe - if condition combines 5 boolean operators (impact -1.60)

### `internal/chat/native_ciphertext.go` — 4 Funde, Abzug 3.00.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselben Ereignisse werden angenommen und abgelehnt.


- [x] [high] Publish - if condition combines 5 boolean operators (impact -1.60)
- [x] [medium] Publish - Publish has cyclomatic complexity 10 (impact -0.85)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [x] [low] List - if condition combines 3 boolean operators (impact -0.40)

### `internal/chat/embeds.go` — 4 Funde, Abzug 2.83.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselben Texte und Links werden abgelehnt.


- [x] [medium] embedLink - embedLink has cyclomatic complexity 10 (impact -0.75)
- [x] [medium] embedText - embedText has cyclomatic complexity 9 (impact -0.75)
- [x] [medium] embedText - if condition combines 4 boolean operators (impact -0.93)
- [x] [low] embedLink - if condition combines 3 boolean operators (impact -0.40)

### `internal/nativepreview/native_ciphertext_preview.go` — 4 Funde, Abzug 2.83.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselbe Relay-Anfrage wird abgelehnt. Routen bleiben unmounted.

- [x] [medium] listCiphertext - listCiphertext has cyclomatic complexity 10 (impact -0.75)
- [x] [medium] parseNativeCiphertextQuery - parseNativeCiphertextQuery has cyclomatic complexity 12 (impact -0.75)
- [x] [medium] relayBoundary - if condition combines 4 boolean operators (impact -0.93)
- [x] [low] listCiphertext - if condition combines 3 boolean operators (impact -0.40)

### `internal/update/checker.go` — 4 Funde, Abzug 2.80.

Erledigt 2026-10-20: Komplexität und verworfene Fehler sind weg. `repowise health --file --recompute` nennt nur noch die Prüfschleife. Die bleibt: ein Lauf fragt in seinem Intervall einmal GitHub.

- [x] [critical] releasePage - if condition combines 6 boolean operators (impact -2.50)
- [ ] [medium] Run - a network call is reached once per loop iteration through Run -> Check -> fetch -> send (cross-function I/O call inside a loop) (impact -0.00). Angenommen: der Lauf prüft einmal pro Intervall.
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `web/src/lib/voiceSession.ts` — 4 Funde, Abzug 2.75.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselbe Sitzung wird fortgesetzt oder verworfen.

- [x] [high] recent - if condition combines 5 boolean operators (impact -1.60)
- [x] [medium] recent - recent has cyclomatic complexity 11 (impact -0.85)
- [x] [low] - caught exception is swallowed without any handling (impact -0.15)
- [x] [low] - caught exception is swallowed without any handling (impact -0.15)

### `internal/ws/native_lease.go` — 6 Funde, Abzug 2.73.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselbe Familie, dieselbe Lease und dieselbe Veröffentlichung bleiben.

- [x] [medium] DisconnectNativeFamily - DisconnectNativeFamily has cyclomatic complexity 9 (impact -0.50)
- [x] [medium] emitApplicationEvent - emitApplicationEvent has cyclomatic complexity 13 (impact -0.50)
- [x] [medium] install - if condition combines 4 boolean operators (impact -0.93)
- [x] [medium] install - install has cyclomatic complexity 13 (impact -0.50)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/nativepreview/native_registry.go` — 2 Funde, Abzug 2.45.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselben Metadaten-Routen bleiben leer.

- [x] [high] nativeMetadataHandlers - if condition combines 5 boolean operators (impact -1.60)
- [x] [medium] nativeMetadataHandlers - nativeMetadataHandlers has cyclomatic complexity 11 (impact -0.85)

### `internal/sfu/layers.go` — 2 Funde, Abzug 2.45.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselben Layer werden abgelehnt.

- [x] [high] validLayers - if condition combines 5 boolean operators (impact -1.60)
- [x] [medium] validLayers - validLayers has cyclomatic complexity 12 (impact -0.85)

### `internal/sfu/rtpkey.go` — 3 Funde, Abzug 2.44.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselben NAL-Typen und dieselben Picture-IDs bleiben.

- [x] [medium] h264Start - h264Start has cyclomatic complexity 13 (impact -0.75)
- [x] [medium] h264Start - h264Start nests 4 levels deep (impact -0.94)
- [x] [medium] parseExtension - parseExtension has cyclomatic complexity 9 (impact -0.75)

### `internal/ws/lifecycle.go` — 3 Funde, Abzug 2.43.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselbe Sitzung wird angenommen oder abgewiesen. `handshake.go` ist im selben Lauf leer.

- [x] [high] HandleWebSocket - HandleWebSocket has cyclomatic complexity 15 (impact -0.95)
- [x] [medium] HandleWebSocket - if condition combines 4 boolean operators (impact -0.93)
- [x] [medium] registerVerified - registerVerified has cyclomatic complexity 12 (impact -0.55)

### `web/src/composables/webrtc/remoteAudio.ts` — 6 Funde, Abzug 2.35.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselbe Wiedergabe, dieselbe Verstärkung und dasselbe Entfernen bleiben.

- [x] [medium] attachRemoteAudio - attachRemoteAudio has cyclomatic complexity 9 (impact -0.75)
- [x] [medium] createBoost - createBoost has cyclomatic complexity 10 (impact -0.75)
- [x] [low] - caught exception is swallowed without any handling (impact -0.15)
- [x] [low] - caught exception is swallowed without any handling (impact -0.15)
- [x] [low] - caught exception is swallowed without any handling (impact -0.15)
- [x] [low] attachRemoteAudio - if condition combines 3 boolean operators (impact -0.40)

### `web/src/lib/rtcStats.ts` — 5 Funde, Abzug 2.30.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselben Verbindungszahlen bleiben.

- [x] [medium] addInboundAudio - addInboundAudio has cyclomatic complexity 11 (impact -0.50)
- [x] [medium] selectedPair - selectedPair has cyclomatic complexity 10 (impact -0.50)
- [x] [medium] summarizeStats - summarizeStats has cyclomatic complexity 14 (impact -0.50)
- [x] [low] receivedPictures - if condition combines 3 boolean operators (impact -0.40)
- [x] [low] selectedPair - if condition combines 3 boolean operators (impact -0.40)

### `internal/httpx/ratelimit.go` — 6 Funde, Abzug 2.26.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselbe Sperre und dasselbe Verdrängen bleiben.

- [x] [medium] makeRoomLocked - if condition combines 4 boolean operators (impact -0.93)
- [x] [medium] makeRoomLocked - makeRoomLocked has cyclomatic complexity 9 (impact -0.85)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.12)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.12)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.12)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.12)

### `internal/chat/mentions.go` — 3 Funde, Abzug 2.25.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselben Erwähnungen und Berechtigungen bleiben.

- [x] [high] ResolveMentions - ResolveMentions has cyclomatic complexity 15 (impact -1.45)
- [x] [low] ResolveMentions - if condition combines 3 boolean operators (impact -0.40)
- [x] [low] ResolveMentions - if condition combines 3 boolean operators (impact -0.40)

### `web/src/composables/useMessageKeyboard.ts` — 3 Funde, Abzug 2.25.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselben Tastenkombinationen und Menüaufrufe bleiben.

- [x] [high] handleMessageKeydown - handleMessageKeydown has cyclomatic complexity 19 (impact -1.45)
- [x] [low] handleMessageKeydown - if condition combines 3 boolean operators (impact -0.40)
- [x] [low] handleMessageKeydown - if condition combines 3 boolean operators (impact -0.40)

### `web/src/lib/media/group.ts` — 7 Funde, Abzug 2.25.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Code-Shape-Fund mehr. Dieselben Nachrichten- und Konfliktprüfungen bleiben.

- [x] [medium] MediaGroup - MediaGroup has low cohesion (LCOM4=2): its 18 methods split into 2 groups that share no fields or calls (impact -0.35)
- [x] [medium] capNow - capNow has cyclomatic complexity 9 (impact -0.30)
- [x] [medium] conflictNow - conflictNow has cyclomatic complexity 12 (impact -0.30)
- [x] [medium] receiveNow - receiveNow has cyclomatic complexity 14 (impact -0.30)
- [x] [medium] receiveWelcome - receiveWelcome has cyclomatic complexity 11 (impact -0.30)
- [x] [medium] vetCommitterProposals - vetCommitterProposals has cyclomatic complexity 12 (impact -0.30)
- [x] [low] vetCommitterProposals - if condition combines 3 boolean operators (impact -0.40)

### `web/src/lib/media/groupBase.ts` — 3 Funde, Abzug 2.25.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselbe Geheimnis-Bereinigung und Fähigkeiten bleiben.

- [x] [critical] GroupBase - GroupBase has low cohesion (LCOM4=7): its 25 methods split into 7 groups that share no fields or calls (impact -1.00)
- [x] [medium] wipeRejected - wipeRejected has cyclomatic complexity 9 (impact -0.85)
- [x] [low] capabilities - if condition combines 3 boolean operators (impact -0.40)

### `web/src/lib/media/frame.ts` — 6 Funde, Abzug 2.20.

- [x] [high] openFrame - openFrame has cyclomatic complexity 19 (impact -0.67)
- [x] [high] sealFrame - sealFrame has cyclomatic complexity 16 (impact -0.67)
- [x] [medium] bindFrameAad - long parameter list: bindFrameAad takes 7 parameters, 6 of 7 typed ones scalars or strings (impact -0.16)
- [x] [low] - caught exception is swallowed without any handling (impact -0.15)
- [x] [low] lineAgrees - if condition combines 3 boolean operators (impact -0.40)
- [x] [low] sealFrame - sealFrame has 3 nested blocks at the same level (bumpy road) (impact -0.15)

### `internal/auth/users_admin_handlers.go` — 3 Funde, Abzug 2.14.

Erledigt 2026-10-20: kickUser CCN von 9 auf 6 und Nesting von 4 auf 2 gesenkt durch Extraktion von kickUserFromVoice und canModerateVoiceChannel. Repowise-Score stieg von 7.9 auf 9.7/10.

- [x] [medium] - 68% of file duplicated in other files; worst clone shares 34 lines with internal/chat/handler_channels.go (impact -0.35)
- [x] [medium] kickUser - kickUser has cyclomatic complexity 9 (impact -0.85)
- [x] [medium] kickUser - kickUser nests 4 levels deep (impact -0.94)

### `internal/auth/native_registration.go` — 3 Funde, Abzug 2.13.

Erledigt 2026-10-20: `repowise health --file --recompute` nennt keinen Fund mehr. Dieselben Abhängigkeiten und dieselbe Zielprüfung.

- [x] [medium] - 41% of file duplicated in other files; worst clone shares 16 lines with internal/auth/native_password_handler.go (impact -0.35)
- [x] [medium] RegistrationHandler - RegistrationHandler has cyclomatic complexity 9 (impact -0.85)
- [x] [medium] RegistrationHandler - if condition combines 4 boolean operators (impact -0.93)

### `internal/webbundle/webbundle.go` — 11 Funde, Abzug 1.98.

Erledigt 2026-10-20: ValidPath (CCN 12 -> 5) und readWebApp (CCN 11 -> 3) durch Hilfsfunktionen/appCollector entflechtet. Alle verworfenen Fehler behandelt; Build CCN von 10 auf 5 gesenkt. Repowise-Score stieg von 8.0 auf 10.0/10.

- [x] [medium] ValidPath - ValidPath has cyclomatic complexity 12 (impact -0.75)
- [x] [medium] readWebApp - readWebApp has cyclomatic complexity 11 (impact -0.75)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.06)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.06)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.06)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.06)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.06)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.06)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.06)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.06)
- [ ] [low] LoadCompat - a blocking filesystem call in one of the most-called functions in this repo (3 direct callers); every call through it waits for the I/O (impact -0.00)

### `internal/gateway/serve.go` — 6 Funde, Abzug 1.95.

Erledigt 2026-10-20: identify (CCN 13 -> 8), register (CCN 10 -> 5) und resume (CCN 11 -> 6) durch Hilfsfunktionen (validIntents, sendReady, scanSessions, adoptSession, sessionCredentials) entflechtet. Verworfenen json.Marshal/Unmarshal-Fehler behoben.

- [x] [medium] identify - identify has cyclomatic complexity 13 (impact -0.50)
- [x] [medium] register - register has cyclomatic complexity 10 (impact -0.50)
- [x] [medium] resume - resume has cyclomatic complexity 11 (impact -0.50)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `web/src/composables/usePictureInPicture.ts` — 5 Funde, Abzug 1.95.

Erledigt 2026-10-20: usePictureInPictureHost (CCN 17 -> 6, bumpy road behoben) und enter (CCN 11 -> 3) durch Hilfsfunktionen (detachHostVideo, attachHostVideo, onStageStreamChange, onConnectionChange, canEnter, resolveCurrentStream, finalizeEnter) entflechtet. Alle geschluckten Exceptions behandelt. Repowise-Score stieg von 8.1 auf 10.0/10 (0 offene Funde).

- [x] [high] usePictureInPictureHost - usePictureInPictureHost has cyclomatic complexity 17 (impact -0.95)
- [x] [medium] enter - enter has cyclomatic complexity 11 (impact -0.55)
- [x] [low] - caught exception is swallowed without any handling (impact -0.15)
- [x] [low] - caught exception is swallowed without any handling (impact -0.15)
- [x] [low] usePictureInPictureHost - usePictureInPictureHost has 3 nested blocks at the same level (bumpy road) (impact -0.15)

### `internal/chat/botapi.go` — 3 Funde, Abzug 1.94.

Erledigt 2026-10-20: botCreateMessage (CCN 14 -> 7) und botMentionPolicy (Nesting 4 -> 2) durch Hilfsfunktionen (checkBotChannelSend, decodeRawBotMessage, validateBotPayload, parseAllowedUsers, applyAllowedMentions) entflechtet.

- [x] [medium] botCreateMessage - botCreateMessage has cyclomatic complexity 14 (impact -0.85)
- [x] [medium] botMentionPolicy - botMentionPolicy nests 4 levels deep (impact -0.94)
- [x] [low] - 12% of file duplicated in other files; worst clone shares 34 lines with internal/bot/interactions.go (co-changed 2x) (impact -0.15)

### `internal/webhook/dispatch.go` — 5 Funde, Abzug 1.94. (Repowise: 7.7/10, max CCN 6, Nest 2)

- [x] [medium] Run - Run has cyclomatic complexity 10 (impact -0.85)
- [x] [medium] Run - Run nests 4 levels deep (impact -0.94)
- [ ] [medium] Run - a database call is reached once per loop iteration through Run -> Reload (cross-function N+1 query) (impact -0.00)
- [ ] [medium] Run - a database call is reached once per loop iteration through Run -> prune -> pruneQueue (cross-function N+1 query) (impact -0.00)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/auth/native_wire.go` — 3 Funde, Abzug 1.90. (Repowise: 8.2/10, max CCN 6, Nest 1)

- [x] [high] writeNativeGrant - if condition combines 5 boolean operators (impact -1.60)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/perm/store_roles.go` — 3 Funde, Abzug 1.90. (Repowise: max CCN 8, Nest 2)

- [x] [medium] applyRoleAppearance - applyRoleAppearance has cyclomatic complexity 9 (impact -0.75)
- [x] [medium] updateRole - updateRole has cyclomatic complexity 11 (impact -0.75)
- [x] [low] checkRoleEditable - if condition combines 3 boolean operators (impact -0.40)

### `internal/sfu/room_tracks.go` — 4 Funde, Abzug 1.90. (Repowise: 9.9/10, max CCN 6, Nest 3)

- [x] [medium] DispatchKeyframe - DispatchKeyframe has cyclomatic complexity 9 (impact -0.50)
- [x] [medium] addTrack - addTrack has cyclomatic complexity 11 (impact -0.50)
- [x] [medium] unpublish - unpublish has cyclomatic complexity 9 (impact -0.50)
- [x] [low] addTrack - if condition combines 3 boolean operators (impact -0.40)

### `web/src/composables/appRoutingApply.ts` — 3 Funde, Abzug 1.90. (Repowise: 10.0/10, max CCN 8, Nest 2)

- [x] [medium] applyRoute - applyRoute has cyclomatic complexity 10 (impact -0.75)
- [x] [medium] applyVoiceRoute - applyVoiceRoute has cyclomatic complexity 9 (impact -0.75)
- [x] [low] openRouteThread - if condition combines 3 boolean operators (impact -0.40)

### `web/src/composables/webrtc/screen.ts` — 3 Funde, Abzug 1.90. (Repowise: 10.0/10, max CCN 8, Nest 2)

- [x] [medium] attachScreen - attachScreen has cyclomatic complexity 13 (impact -0.75)
- [x] [medium] startScreenShare - startScreenShare has cyclomatic complexity 13 (impact -0.75)
- [x] [low] startScreenShare - if condition combines 3 boolean operators (impact -0.40)

### `web/src/composables/webrtc/sdp.ts` — 3 Funde, Abzug 1.90. (Repowise: 10.0/10, max CCN 8, Nest 2)

- [x] [medium] restrictVideoSection - restrictVideoSection has cyclomatic complexity 13 (impact -0.75)
- [x] [medium] tuneLine - tuneLine has cyclomatic complexity 11 (impact -0.75)
- [x] [low] tuneLine - if condition combines 3 boolean operators (impact -0.40)

### `web/src/lib/api.ts` — 3 Funde, Abzug 1.90. (Repowise: errorMessage & sendRequest resolved)

- [x] [medium] errorMessage - errorMessage has cyclomatic complexity 11 (impact -0.75)
- [x] [medium] sendRequest - sendRequest has cyclomatic complexity 9 (impact -0.75)
- [x] [low] sendRequest - if condition combines 3 boolean operators (impact -0.40)

### `web/src/lib/dfn/dfnBridgeWorklet.ts` — 3 Funde, Abzug 1.90. (Repowise: 9.0/10, max CCN 7, Nest 2)

- [x] [medium] receive - receive has cyclomatic complexity 11 (impact -0.75)
- [x] [medium] updateBypass - updateBypass has cyclomatic complexity 10 (impact -0.75)
- [x] [low] updateBypass - if condition combines 3 boolean operators (impact -0.40)

### `web/src/lib/media/callEngine.ts` — 3 Funde, Abzug 1.90. (Repowise: 10.0/10, max CCN 7, Nest 3)

- [x] [medium] emit - emit has cyclomatic complexity 11 (impact -0.75)
- [x] [medium] flush - flush has cyclomatic complexity 9 (impact -0.75)
- [x] [low] handleStats - if condition combines 3 boolean operators (impact -0.40)

### `web/src/lib/media/groupTree.ts` — 3 Funde, Abzug 1.90. (Repowise: 10.0/10, max CCN 8, Nest 2)

- [x] [medium] frameVerifiers - frameVerifiers has cyclomatic complexity 9 (impact -0.75)
- [x] [medium] onlyRenewsLeaf - onlyRenewsLeaf has cyclomatic complexity 10 (impact -0.75)
- [x] [low] onlyRenewsLeaf - if condition combines 3 boolean operators (impact -0.40)

### `web/src/lib/mentionQuery.ts` — 3 Funde, Abzug 1.90. (Repowise: 10.0/10, max CCN 7, Nest 2)

- [x] [high] suggestMentions - suggestMentions has cyclomatic complexity 22 (impact -0.95)
- [x] [medium] findMentionQuery - findMentionQuery has cyclomatic complexity 13 (impact -0.55)
- [x] [low] findMentionQuery - if condition combines 3 boolean operators (impact -0.40)

### `web/src/composables/useNavMenus.ts` — 4 Funde, Abzug 1.89. (Repowise: 9.0/10, max CCN 7, Nest 2)

- [x] [medium] buildChannelItems - buildChannelItems has cyclomatic complexity 10 (impact -0.61)
- [x] [medium] moderationItems - moderationItems has cyclomatic complexity 14 (impact -0.61)
- [x] [low] buildChannelItems - buildChannelItems is 68 lines long (impact -0.27)
- [x] [low] moderationItems - if condition combines 3 boolean operators (impact -0.40)

### `web/src/lib/videoGrid.ts` — 2 Funde, Abzug 1.85. (Repowise: 10.0/10, max CCN 6, Nest 2)

- [x] [high] videoGridLayout - videoGridLayout has cyclomatic complexity 17 (impact -1.45)
- [x] [low] videoGridLayout - if condition combines 3 boolean operators (impact -0.40)

### `web/src/third_party/deepfilternet3/worklet.js` — 2 Funde, Abzug 1.85. Vendor, nicht editieren.

- [ ] [critical] <anonymous> - <anonymous> is 292 lines long (impact -1.50)
- [ ] [medium] - 69% of file duplicated in other files; worst clone shares 242 lines with web/src/third_party/deepfilternet3/worker-glue.js (impact -0.35)

### `internal/media/handler_upload.go` — 5 Funde, Abzug 1.84. (Repowise: max CCN 8, Nest 2)

- [ ] [medium] - 33% of file duplicated in other files; worst clone shares 33 lines with internal/media/handler_admin.go (impact -0.35)
- [x] [medium] ReplaceAvatar - ReplaceAvatar has cyclomatic complexity 10 (impact -0.47)
- [x] [medium] readFile - readFile has cyclomatic complexity 11 (impact -0.47)
- [x] [medium] upload - upload has cyclomatic complexity 9 (impact -0.47)
- [x] [low] readFile - long parameter list: readFile takes 5 parameters, 3 of 5 typed ones scalars or strings (impact -0.08)

### `web/src/composables/webrtc/micTest.ts` — 4 Funde, Abzug 1.80.

- [x] [medium] startMicLoopback - startMicLoopback has cyclomatic complexity 11 (impact -0.75)
- [x] [medium] teardownTestPipeline - teardownTestPipeline has cyclomatic complexity 10 (impact -0.75)
- [x] [low] - caught exception is swallowed without any handling (impact -0.15)
- [x] [low] - caught exception is swallowed without any handling (impact -0.15)

### `web/src/lib/soundEffects.ts` — 4 Funde, Abzug 1.80.

- [x] [medium] playSoundEffect - playSoundEffect has cyclomatic complexity 12 (impact -0.75)
- [x] [medium] playTone - playTone has cyclomatic complexity 13 (impact -0.75)
- [x] [low] - caught exception is swallowed without any handling (impact -0.15)
- [x] [low] - caught exception is swallowed without any handling (impact -0.15)

### `internal/sfu/layer_wanted.go` — 2 Funde, Abzug 1.79.

- [x] [medium] refreshWanted - refreshWanted has cyclomatic complexity 12 (impact -0.85)
- [x] [medium] refreshWanted - refreshWanted nests 4 levels deep (impact -0.94)

### `internal/tools/openapifix/main.go` — 2 Funde, Abzug 1.79.

- [x] [medium] run - run has cyclomatic complexity 9 (impact -0.85)
- [x] [medium] run - run nests 4 levels deep (impact -0.94)

### `internal/webhook/dispatch_queue.go` — 8 Funde, Abzug 1.79.

- [x] [medium] flush - a database call is reached once per loop iteration through flush -> store -> storeEvents (cross-function N+1 query) (impact -0.00)
- [x] [medium] persist - a database call is reached once per loop iteration through persist -> flush -> store -> storeEvents (cross-function N+1 query) (impact -0.00)
- [x] [medium] persist - a database call is reached once per loop iteration through persist -> store -> storeEvents (cross-function N+1 query) (impact -0.00)
- [x] [medium] work - a database call is reached once per loop iteration through work -> claimJob (cross-function N+1 query) (impact -0.00)
- [x] [medium] work - a database call is reached once per loop iteration through work -> process -> getOutgoing (cross-function N+1 query) (impact -0.00)
- [x] [medium] work - a database call is reached once per loop iteration through work -> releaseJob (cross-function N+1 query) (impact -0.00)
- [x] [medium] work - work has cyclomatic complexity 9 (impact -0.85)
- [x] [medium] work - work nests 4 levels deep (impact -0.94)

### `web/scripts/check-typescript.ts` — 3 Funde, Abzug 1.79.

- [x] [medium] inspect - a filesystem call runs once per loop iteration (I/O call inside a loop) (impact -0.00)
- [x] [medium] inspect - inspect has cyclomatic complexity 12 (impact -0.85)
- [x] [medium] inspect - inspect nests 4 levels deep (impact -0.94)

### `web/src/lib/dfn/dfnWorker.ts` — 2 Funde, Abzug 1.79.

- [x] [medium] self.onmessage - self.onmessage has cyclomatic complexity 14 (impact -0.85)
- [x] [medium] self.onmessage - self.onmessage nests 4 levels deep (impact -0.94)

### `web/src/stores/auth/credentials.ts` — 2 Funde, Abzug 1.79.

- [x] [medium] checkAuth - checkAuth has cyclomatic complexity 9 (impact -0.85)
- [x] [medium] checkAuth - checkAuth nests 4 levels deep (impact -0.94)

### `internal/bot/authn.go` — 2 Funde, Abzug 1.78.

- [x] [medium] touch - if condition combines 4 boolean operators (impact -0.93)
- [x] [medium] touch - touch has cyclomatic complexity 9 (impact -0.85)

### `web/src/lib/media/groupVet.ts` — 2 Funde, Abzug 1.78.

- [x] [medium] scanCommitProposals - if condition combines 4 boolean operators (impact -0.93)
- [x] [medium] scanCommitProposals - scanCommitProposals has cyclomatic complexity 10 (impact -0.85)

### `internal/ws/native_admission.go` — 4 Funde, Abzug 1.75.

- [x] [medium] - 10% of file duplicated in other files; worst clone shares 12 lines with internal/nativepreview/native_preview.go (co-changed 2x) (impact -0.35)
- [x] [medium] nativeSocketHandler - nativeSocketHandler has cyclomatic complexity 10 (impact -0.85)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [x] [low] nativeSocketHandler - if condition combines 3 boolean operators (impact -0.40)

### `web/src/components/ContextMenu.vue` — 3 Funde, Abzug 1.73.

- [x] [medium] typeAhead - if condition combines 4 boolean operators (impact -0.93)
- [x] [low] handleClickOutside - if condition combines 3 boolean operators (impact -0.40)
- [x] [low] handleScroll - if condition combines 3 boolean operators (impact -0.40)

### `web/src/composables/useResizable.ts` — 5 Funde, Abzug 1.68.

- [x] [medium] beginPanelDrag - beginPanelDrag has cyclomatic complexity 12 (impact -0.85)
- [x] [low] - caught exception is swallowed without any handling (impact -0.15)
- [x] [low] - caught exception is swallowed without any handling (impact -0.15)
- [x] [low] availableMax - long parameter list: availableMax takes 5 parameters, 4 of 5 typed ones scalars or strings (impact -0.15)
- [x] [low] useResizable - useResizable is 70 lines long (impact -0.38)

### `internal/bot/commands.go` — 4 Funde, Abzug 1.65.

- [x] [medium] ReplaceCommands - ReplaceCommands has cyclomatic complexity 9 (impact -0.75)
- [x] [medium] ReplaceCommands - a database call is reached once per loop iteration through ReplaceCommands -> upsert (cross-function N+1 query) (impact -0.00)
- [x] [medium] cleanOptions - cleanOptions has cyclomatic complexity 9 (impact -0.75)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/chat/messages.go` — 3 Funde, Abzug 1.65.

- [x] [medium] ValidateReplyTarget - ValidateReplyTarget has cyclomatic complexity 12 (impact -0.75)
- [x] [medium] scanMessages - scanMessages has cyclomatic complexity 9 (impact -0.75)
- [x] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/chat/moderation.go` — 3 Funde, Abzug 1.65.

- [x] [medium] bulkDelete - bulkDelete has cyclomatic complexity 10 (impact -0.75)
- [x] [medium] takeLatestMessages - takeLatestMessages has cyclomatic complexity 9 (impact -0.75)
- [ ] [low] - 15% of file duplicated in other files; worst clone shares 49 lines with internal/chat/botapi_reactions.go (impact -0.15)

### `internal/ws/signal.go` — 3 Funde, Abzug 1.65.

- [ ] [medium] handleSubscribe - handleSubscribe has cyclomatic complexity 12 (impact -0.75)
- [ ] [medium] parseSubscribe - parseSubscribe has cyclomatic complexity 13 (impact -0.75)
- [ ] [low] - 13% of file duplicated in other files; worst clone shares 13 lines with internal/ws/layers.go (co-changed 2x) (impact -0.15)

### `web/src/composables/useSidebarReorder.ts` — 3 Funde, Abzug 1.65.

- [ ] [medium] channelHit - channelHit has cyclomatic complexity 10 (impact -0.85)
- [ ] [low] channelHit - if condition combines 3 boolean operators (impact -0.40)
- [ ] [low] resolveSidebarDrop - if condition combines 3 boolean operators (impact -0.40)

### `web/src/composables/webrtc/peer.ts` — 3 Funde, Abzug 1.65.

- [ ] [high] applyRemoteOffer - applyRemoteOffer has cyclomatic complexity 21 (impact -0.95)
- [ ] [medium] wireIceDiagnostics - wireIceDiagnostics has cyclomatic complexity 13 (impact -0.55)
- [ ] [low] - caught exception is swallowed without any handling (impact -0.15)

### `web/src/lib/dfn/dfnNode.ts` — 3 Funde, Abzug 1.65.

- [ ] [high] createDeepFilterNode - createDeepFilterNode has cyclomatic complexity 15 (impact -1.19)
- [ ] [low] - caught exception is swallowed without any handling (impact -0.15)
- [ ] [low] createDeepFilterNode - createDeepFilterNode is 76 lines long (impact -0.31)

### `web/src/stores/voice/screens.ts` — 4 Funde, Abzug 1.65.

- [ ] [medium] handleMediaState - handleMediaState has cyclomatic complexity 11 (impact -0.50)
- [ ] [medium] handleScreenLayers - handleScreenLayers has cyclomatic complexity 9 (impact -0.50)
- [ ] [medium] handleScreenViewers - handleScreenViewers has cyclomatic complexity 9 (impact -0.50)
- [ ] [low] - 10% of file duplicated in other files; worst clone shares 9 lines with web/src/stores/voice/cameras.ts (impact -0.15)

### `web/src/stores/voiceSettings.ts` — 3 Funde, Abzug 1.63.

- [ ] [medium] useVoiceSettings - useVoiceSettings has cyclomatic complexity 10 (impact -0.85)
- [ ] [low] savedNoiseMode - if condition combines 3 boolean operators (impact -0.40)
- [ ] [low] useVoiceSettings - useVoiceSettings is 60 lines long (impact -0.38)

### `web/src/components/MessageModerationDialog.vue` — 3 Funde, Abzug 1.60.

- [ ] [medium] - 27% of file duplicated in other files; worst clone shares 14 lines with web/src/components/ModerationDialog.vue (impact -0.35)
- [ ] [medium] submit - submit has cyclomatic complexity 10 (impact -0.85)
- [ ] [low] submit - ternary condition combines 3 boolean operators (impact -0.40)

### `web/src/composables/sortableDragParts.ts` — 1 Funde, Abzug 1.60.

- [ ] [high] isDragPress - if condition combines 5 boolean operators (impact -1.60)

### `internal/audit/middleware.go` — 4 Funde, Abzug 1.55.

- [ ] [medium] serve - serve has cyclomatic complexity 13 (impact -0.85)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [ ] [low] peekRequest - if condition combines 3 boolean operators (impact -0.40)

### `web/src/types/events.ts` — 4 Funde, Abzug 1.52.

- [ ] [medium] isIceCandidate - isIceCandidate has cyclomatic complexity 13 (impact -0.38)
- [ ] [medium] isInteractionResponse - isInteractionResponse has cyclomatic complexity 9 (impact -0.38)
- [ ] [medium] isModerationNotice - isModerationNotice has cyclomatic complexity 10 (impact -0.38)
- [ ] [medium] isReadStateUpdate - isReadStateUpdate has cyclomatic complexity 11 (impact -0.38)

### `internal/chat/channels.go` — 4 Funde, Abzug 1.50.

- [ ] [medium] CreateChannel - CreateChannel has cyclomatic complexity 9 (impact -0.50)
- [ ] [medium] DeleteCategoryTx - a database call runs once per loop iteration (N+1 query) (impact -0.00)
- [ ] [medium] GetServerHierarchy - GetServerHierarchy has cyclomatic complexity 10 (impact -0.50)
- [ ] [medium] validateChannelEdit - validateChannelEdit has cyclomatic complexity 11 (impact -0.50)

### `internal/media/stream.go` — 2 Funde, Abzug 1.50.

- [ ] [medium] readUpload - readUpload has cyclomatic complexity 9 (impact -0.75)
- [ ] [medium] storePart - storePart has cyclomatic complexity 11 (impact -0.75)

### `internal/nativepreview/native_ciphertext_schema.go` — 2 Funde, Abzug 1.50.

- [ ] [medium] nativeCiphertextDecimal - nativeCiphertextDecimal has cyclomatic complexity 9 (impact -0.75)
- [ ] [medium] readNativeCiphertextFields - readNativeCiphertextFields has cyclomatic complexity 11 (impact -0.75)

### `internal/perm/store_roles_order.go` — 3 Funde, Abzug 1.50.

- [ ] [medium] completeRoleOrder - completeRoleOrder has cyclomatic complexity 9 (impact -0.75)
- [ ] [medium] moveRoles - a database call runs once per loop iteration (N+1 query) (impact -0.00)
- [ ] [medium] moveRoles - moveRoles has cyclomatic complexity 10 (impact -0.75)

### `internal/sfu/room_attach.go` — 2 Funde, Abzug 1.50.

- [ ] [medium] classifyRemoteTrack - classifyRemoteTrack has cyclomatic complexity 9 (impact -0.75)
- [ ] [medium] forward - forward has cyclomatic complexity 9 (impact -0.75)

### `internal/webbundle/verify.go` — 2 Funde, Abzug 1.50.

- [ ] [medium] checkFileList - checkFileList has cyclomatic complexity 13 (impact -0.75)
- [ ] [medium] verifySignature - verifySignature has cyclomatic complexity 10 (impact -0.75)

### `internal/ws/voice_join.go` — 2 Funde, Abzug 1.50.

- [ ] [medium] joinVoice - joinVoice has cyclomatic complexity 13 (impact -0.75)
- [ ] [medium] takeSeat - takeSeat has cyclomatic complexity 11 (impact -0.75)

### `loadtest/browsers.mjs` — 3 Funde, Abzug 1.50.

- [ ] [medium] main - main has cyclomatic complexity 9 (impact -0.50)
- [ ] [medium] poll - poll has cyclomatic complexity 9 (impact -0.50)
- [ ] [medium] watch - watch has cyclomatic complexity 9 (impact -0.50)

### `loadtest/join.mjs` — 2 Funde, Abzug 1.50.

- [ ] [medium] clients.filter((c) => c.i > 0).map callback - clients.filter((c) => c.i > 0).map callback has cyclomatic complexity 12 (impact -0.75)
- [ ] [medium] sample - sample has cyclomatic complexity 10 (impact -0.75)

### `loadtest/run.sh` — 2 Funde, Abzug 1.50.

- [ ] [high] cleanup - cleanup has cyclomatic complexity 15 (impact -0.95)
- [ ] [medium] run_watch - run_watch has cyclomatic complexity 9 (impact -0.55)

### `scripts/backup-common.sh` — 1 Funde, Abzug 1.50.

- [ ] [critical] backup_validate - backup_validate has cyclomatic complexity 28 (impact -1.50)

### `web/scripts/generate-contracts.ts` — 1 Funde, Abzug 1.50.

- [ ] [critical] isSchema - isSchema has cyclomatic complexity 27 (impact -1.50)

### `web/src/NativePreviewApp.vue` — 2 Funde, Abzug 1.50.

- [ ] [medium] connect - connect has cyclomatic complexity 9 (impact -0.75)
- [ ] [medium] previewOrigin - previewOrigin has cyclomatic complexity 12 (impact -0.75)

### `web/src/composables/useMessageScroll.ts` — 2 Funde, Abzug 1.50.

- [ ] [medium] loadMoreNearEdges - loadMoreNearEdges has cyclomatic complexity 10 (impact -0.75)
- [ ] [medium] watchWindowChanges - watchWindowChanges has cyclomatic complexity 11 (impact -0.75)

### `web/src/composables/webrtc/audioSettings.ts` — 2 Funde, Abzug 1.50.

- [ ] [medium] recoverFilter - recoverFilter has cyclomatic complexity 10 (impact -0.75)
- [ ] [medium] swapMicrophone - swapMicrophone has cyclomatic complexity 11 (impact -0.75)

### `web/src/composables/webrtc/streamDiag.ts` — 2 Funde, Abzug 1.50.

- [ ] [medium] directionFields - directionFields has cyclomatic complexity 10 (impact -0.75)
- [ ] [medium] streamDiag - streamDiag has cyclomatic complexity 12 (impact -0.75)

### `web/src/lib/channelDrop.ts` — 2 Funde, Abzug 1.50.

- [ ] [medium] headerPlacement - headerPlacement has cyclomatic complexity 9 (impact -0.75)
- [ ] [medium] resolveCategoryDrop - resolveCategoryDrop has cyclomatic complexity 13 (impact -0.75)

### `web/src/lib/channelLayout.ts` — 2 Funde, Abzug 1.50.

- [ ] [high] applyLayoutPayload - applyLayoutPayload has cyclomatic complexity 17 (impact -0.95)
- [ ] [medium] channelStep - channelStep has cyclomatic complexity 11 (impact -0.55)

### `web/src/lib/chatLogic.ts` — 2 Funde, Abzug 1.50.

- [ ] [medium] firstUnreadId - firstUnreadId has cyclomatic complexity 9 (impact -0.75)
- [ ] [medium] typingLine - typingLine has cyclomatic complexity 9 (impact -0.75)

### `web/src/lib/media/codec.ts` — 2 Funde, Abzug 1.50.

- [ ] [medium] parseFrame - parseFrame has cyclomatic complexity 13 (impact -0.75)
- [ ] [medium] splitNals - splitNals has cyclomatic complexity 9 (impact -0.75)

### `web/src/components/NativeTrustSetup.vue` — 3 Funde, Abzug 1.49.

- [ ] [high] requestConfirmation - requestConfirmation has cyclomatic complexity 16 (impact -0.69)
- [ ] [medium] begin - begin has cyclomatic complexity 13 (impact -0.40)
- [ ] [medium] listen - listen has cyclomatic complexity 11 (impact -0.40)

### `web/src/composables/webrtc/micPipeline.ts` — 6 Funde, Abzug 1.48.

- [ ] [medium] teardownMicPipeline - teardownMicPipeline has cyclomatic complexity 12 (impact -0.85)
- [ ] [low] - caught exception is swallowed without any handling (impact -0.12)
- [ ] [low] - caught exception is swallowed without any handling (impact -0.12)
- [ ] [low] - caught exception is swallowed without any handling (impact -0.12)
- [ ] [low] - caught exception is swallowed without any handling (impact -0.12)
- [ ] [low] teardownMicPipeline - teardownMicPipeline has 3 nested blocks at the same level (bumpy road) (impact -0.15)

### `web/src/lib/native/trustedDesktop.ts` — 1 Funde, Abzug 1.45.

- [ ] [high] trustedApp - trustedApp has cyclomatic complexity 17 (impact -1.45)

### `web/src/lib/noiseSuppressor.ts` — 1 Funde, Abzug 1.45.

- [ ] [high] createNoiseSuppressorNode - createNoiseSuppressorNode has cyclomatic complexity 15 (impact -1.45)

### `web/src/stores/chat/members.ts` — 1 Funde, Abzug 1.45.

- [ ] [high] openUserProfile - openUserProfile has cyclomatic complexity 17 (impact -1.45)

### `web/src/stores/chat/socket.ts` — 1 Funde, Abzug 1.45.

- [ ] [high] initWebSocket - initWebSocket has cyclomatic complexity 16 (impact -1.45)

### `internal/chat/permissions.go` — 3 Funde, Abzug 1.40.

- [ ] [medium] checkChannelMove - checkChannelMove has cyclomatic complexity 9 (impact -0.85)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [ ] [low] checkChannelMove - if condition combines 3 boolean operators (impact -0.40)

### `internal/webbundle/cli_trust_sign.go` — 7 Funde, Abzug 1.35.

- [ ] [medium] fillKeys - a filesystem call is reached once per loop iteration through fillKeys -> bundleKeyArg -> readPub (cross-function I/O call inside a loop) (impact -0.00)
- [ ] [medium] runTrustSign - runTrustSign has cyclomatic complexity 13 (impact -0.85)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.10)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.10)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.10)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.10)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.10)

### `web/web.go` — 7 Funde, Abzug 1.35.

- [ ] [medium] newHandler - newHandler has cyclomatic complexity 13 (impact -0.85)
- [ ] [medium] serveCompressed - a deferred call inside a loop runs only when the enclosing function returns, so the resource stays held across every iteration; close it in the loop body (or wrap the body in a function) instead (impact -0.00)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.10)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.10)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.10)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.10)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.10)

### `internal/webhook/outgoing_handler.go` — 3 Funde, Abzug 1.35.

- [ ] [medium] - 63% of file duplicated in other files; worst clone shares 39 lines with internal/bot/handler_commands.go (impact -0.35)
- [ ] [medium] updateOutgoing - updateOutgoing has cyclomatic complexity 11 (impact -0.85)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/webhook/dispatch_delivery.go` — 5 Funde, Abzug 1.33.

- [ ] [medium] process - process has cyclomatic complexity 14 (impact -0.85)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.12)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.12)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.12)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.12)

### `internal/db/db.go` — 6 Funde, Abzug 1.30.

- [ ] [medium] MigrateFS - MigrateFS has cyclomatic complexity 13 (impact -0.85)
- [ ] [medium] MigrateFS - a database call is reached once per loop iteration through MigrateFS -> applyMigration (cross-function N+1 query) (impact -0.00)
- [ ] [medium] recordChecksums - a database call runs once per loop iteration (N+1 query) (impact -0.00)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [ ] [low] applyMigration - long parameter list: applyMigration takes 5 parameters, 3 of 5 typed ones scalars or strings (impact -0.15)

### `internal/webbundle/trust.go` — 4 Funde, Abzug 1.30.

- [ ] [medium] Validate - Validate has cyclomatic complexity 11 (impact -0.85)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `cmd/server/unlock.go` — 3 Funde, Abzug 1.25.

- [ ] [medium] requestUnlock - a database call is reached once per loop iteration through requestUnlock -> UnlockRequestPending (cross-function N+1 query) (impact -0.00)
- [ ] [medium] requestUnlock - requestUnlock has cyclomatic complexity 9 (impact -0.85)
- [ ] [low] unlockCommand - if condition combines 3 boolean operators (impact -0.40)

### `internal/chat/message_types.go` — 2 Funde, Abzug 1.25.

- [ ] [medium] ValidateEmoji - ValidateEmoji has cyclomatic complexity 9 (impact -0.85)
- [ ] [low] ValidateEmoji - if condition combines 3 boolean operators (impact -0.40)

### `internal/chat/search.go` — 2 Funde, Abzug 1.25.

- [ ] [medium] buildSearchSQL - buildSearchSQL has cyclomatic complexity 14 (impact -0.85)
- [ ] [low] buildSearchSQL - if condition combines 3 boolean operators (impact -0.40)

### `internal/linkpreview/linkpreview_ssrf.go` — 2 Funde, Abzug 1.25.

- [ ] [medium] checkURL - checkURL has cyclomatic complexity 9 (impact -0.85)
- [ ] [low] checkURL - if condition combines 3 boolean operators (impact -0.40)

### `internal/sfu/adapt.go` — 2 Funde, Abzug 1.25.

- [ ] [medium] observe - observe has cyclomatic complexity 11 (impact -0.85)
- [ ] [low] tryStepUp - if condition combines 3 boolean operators (impact -0.40)

### `internal/sfu/candidates.go` — 3 Funde, Abzug 1.25.

- [ ] [medium] vetRemoteCandidate - vetRemoteCandidate has cyclomatic complexity 11 (impact -0.85)
- [ ] [low] applyEarlyCandidates - a lock is acquired every loop iteration; hoist the lock outside the loop or batch the critical section (impact -0.00)
- [ ] [low] routableCandidateAddress - if condition combines 3 boolean operators (impact -0.40)

### `internal/sfu/viewer_track.go` — 2 Funde, Abzug 1.25.

- [ ] [medium] writeLocked - writeLocked has cyclomatic complexity 9 (impact -0.85)
- [ ] [low] keyframeDueLocked - if condition combines 3 boolean operators (impact -0.40)

### `web/src/lib/markdown.ts` — 2 Funde, Abzug 1.25.

- [ ] [medium] renderMarkdown - renderMarkdown has cyclomatic complexity 11 (impact -0.85)
- [ ] [low] renderMarkdown - if condition combines 3 boolean operators (impact -0.40)

### `web/src/lib/media/groupSync.ts` — 2 Funde, Abzug 1.25.

- [ ] [medium] reachable - reachable has cyclomatic complexity 12 (impact -0.85)
- [ ] [low] syncNow - if condition combines 3 boolean operators (impact -0.40)

### `web/src/stores/voice/room.ts` — 2 Funde, Abzug 1.25.

- [ ] [medium] onUserLeft - onUserLeft has cyclomatic complexity 11 (impact -0.85)
- [ ] [low] onUserLeft - if condition combines 3 boolean operators (impact -0.40)

### `scripts/screenshots/seed.mjs` — 2 Funde, Abzug 1.23.

- [ ] [medium] seed - seed has cyclomatic complexity 11 (impact -0.85)
- [ ] [low] seed - seed is 87 lines long (impact -0.38)

### `internal/auth/invites.go` — 2 Funde, Abzug 1.20.

- [ ] [medium] - 31% of file duplicated in other files; worst clone shares 35 lines with internal/webhook/handler.go (impact -0.35)
- [ ] [medium] CreateInvite - CreateInvite has cyclomatic complexity 13 (impact -0.85)

### `internal/auth/moderation_bans.go` — 3 Funde, Abzug 1.20.

- [ ] [medium] - 25% of file duplicated in other files; worst clone shares 32 lines with internal/roles/handler.go (impact -0.35)
- [ ] [medium] RunBanExpiry - a database call is reached once per loop iteration through RunBanExpiry -> ExpireBans (cross-function N+1 query) (impact -0.00)
- [ ] [medium] banUser - banUser has cyclomatic complexity 11 (impact -0.85)

### `internal/bot/handler.go` — 2 Funde, Abzug 1.20.

- [ ] [medium] - 43% of file duplicated in other files; worst clone shares 37 lines with internal/webhook/outgoing_handler.go (impact -0.35)
- [ ] [medium] update - update has cyclomatic complexity 13 (impact -0.85)

### `internal/chat/botapi_reactions.go` — 2 Funde, Abzug 1.20.

- [ ] [medium] - 50% of file duplicated in other files; worst clone shares 49 lines with internal/chat/moderation.go (impact -0.35)
- [ ] [medium] setBotReaction - setBotReaction has cyclomatic complexity 9 (impact -0.85)

### `web/src/composables/useWebRTC.harness.fixture.ts` — 2 Funde, Abzug 1.20.

- [ ] [high] FakeAudioContext - FakeAudioContext has low cohesion (LCOM4=7): its 9 methods split into 7 groups that share no fields or calls (impact -0.60)
- [ ] [high] FakePC - FakePC has low cohesion (LCOM4=4): its 12 methods split into 4 groups that share no fields or calls (impact -0.60)

### `web/src/lib/native/nativeRegistration.ts` — 2 Funde, Abzug 1.20.

- [ ] [medium] registerThenLogin - registerThenLogin has 4 nested blocks at the same level (bumpy road) (impact -0.35)
- [ ] [medium] registerThenLogin - registerThenLogin has cyclomatic complexity 13 (impact -0.85)

### `internal/bot/interactions.go` — 3 Funde, Abzug 1.15.

- [ ] [medium] invoke - invoke has cyclomatic complexity 14 (impact -0.85)
- [ ] [low] - 17% of file duplicated in other files; worst clone shares 34 lines with internal/chat/botapi.go (co-changed 2x) (impact -0.15)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/webhook/store.go` — 3 Funde, Abzug 1.15.

- [ ] [medium] remove - remove has cyclomatic complexity 9 (impact -0.85)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/update/updater.go` — 2 Funde, Abzug 1.08.

- [ ] [medium] NewUpdater - if condition combines 4 boolean operators (impact -0.93)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/auth/admin_seed.go` — 2 Funde, Abzug 1.00.

- [ ] [medium] EnsureAdminUser - EnsureAdminUser has cyclomatic complexity 14 (impact -0.85)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/auth/handler.go` — 2 Funde, Abzug 1.00.

- [ ] [medium] verifyLogin - verifyLogin has cyclomatic complexity 10 (impact -0.85)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/bot/interaction_store.go` — 2 Funde, Abzug 1.00.

- [ ] [medium] claim - claim has cyclomatic complexity 11 (impact -0.85)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/bot/interactions_respond.go` — 2 Funde, Abzug 1.00.

- [ ] [medium] callback - callback has cyclomatic complexity 10 (impact -0.85)
- [ ] [low] - 19% of file duplicated in other files; worst clone shares 34 lines with internal/bot/commands_bot.go (impact -0.15)

### `internal/chat/handler_lookup.go` — 2 Funde, Abzug 1.00.

- [ ] [medium] getThread - getThread has cyclomatic complexity 12 (impact -0.85)
- [ ] [low] - 11% of file duplicated in other files; worst clone shares 27 lines with internal/media/handler_admin.go (impact -0.15)

### `internal/chat/handler_messages.go` — 2 Funde, Abzug 1.00.

- [ ] [medium] listMessages - listMessages has cyclomatic complexity 11 (impact -0.85)
- [ ] [low] - 23% of file duplicated in other files; worst clone shares 30 lines with internal/bot/interactions.go (impact -0.15)

### `internal/gateway/observe.go` — 2 Funde, Abzug 1.00.

- [ ] [medium] Observe - Observe has cyclomatic complexity 9 (impact -0.85)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/media/admin.go` — 4 Funde, Abzug 1.00.

- [ ] [medium] PruneOlderThan - PruneOlderThan has cyclomatic complexity 9 (impact -0.85)
- [ ] [medium] PruneOlderThan - a database call is reached once per loop iteration through PruneOlderThan -> prunable (cross-function N+1 query) (impact -0.00)
- [ ] [medium] PruneOlderThan - a database call runs once per loop iteration (N+1 query) (impact -0.00)
- [ ] [low] - 8% of file duplicated in other files; worst clone shares 16 lines with internal/chat/channel_layout.go (impact -0.15)

### `internal/server/system.go` — 2 Funde, Abzug 1.00.

- [ ] [medium] health - health has cyclomatic complexity 10 (impact -0.85)
- [ ] [low] - 10% of file duplicated in other files; worst clone shares 21 lines with internal/server/system_ops.go (co-changed 2x) (impact -0.15)

### `internal/server/system_selfupdate.go` — 2 Funde, Abzug 1.00.

- [ ] [medium] startSelfUpdate - startSelfUpdate has cyclomatic complexity 10 (impact -0.85)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/sfu/transport.go` — 3 Funde, Abzug 1.00.

- [ ] [medium] openUDPMux - openUDPMux has cyclomatic complexity 10 (impact -0.85)
- [ ] [low] - 9% of file duplicated in other files; worst clone shares 8 lines with internal/sfu/room_signal.go (impact -0.15)
- [ ] [low] Close - a lock is acquired every loop iteration; hoist the lock outside the loop or batch the critical section (impact -0.00)

### `internal/update/image.go` — 2 Funde, Abzug 1.00.

- [ ] [medium] ImageReach - ImageReach has cyclomatic complexity 12 (impact -0.85)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/webhook/execute.go` — 2 Funde, Abzug 1.00.

- [ ] [medium] build - build has cyclomatic complexity 10 (impact -0.85)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/ws/native_protocol.go` — 2 Funde, Abzug 1.00.

- [ ] [medium] handleNativeControl - handleNativeControl has cyclomatic complexity 10 (impact -0.85)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `web/src/components/LegalModal.vue` — 2 Funde, Abzug 1.00.

- [ ] [medium] onMounted callback - onMounted callback has cyclomatic complexity 11 (impact -0.85)
- [ ] [low] - caught exception is swallowed without any handling (impact -0.15)

### `web/src/composables/webrtc/senderTuning.ts` — 2 Funde, Abzug 1.00.

- [ ] [medium] tuneSender - tuneSender has cyclomatic complexity 12 (impact -0.85)
- [ ] [low] - caught exception is swallowed without any handling (impact -0.15)

### `web/src/i18n/index.ts` — 2 Funde, Abzug 1.00.

- [ ] [medium] flattenKeys - flattenKeys has cyclomatic complexity 9 (impact -0.85)
- [ ] [low] - caught exception is swallowed without any handling (impact -0.15)

### `internal/config/config_net.go` — 3 Funde, Abzug 0.95.

- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [ ] [low] BroadTrustedProxies - if condition combines 3 boolean operators (impact -0.40)
- [ ] [low] normalizeURL - if condition combines 3 boolean operators (impact -0.40)

### `internal/chat/audit.go` — 1 Funde, Abzug 0.94.

- [ ] [medium] snapshotMessage - snapshotMessage nests 4 levels deep (impact -0.94)

### `loadtest/viewer/report.go` — 1 Funde, Abzug 0.94.

- [ ] [medium] printReport - printReport nests 4 levels deep (impact -0.94)

### `web/src/stores/chat/presence.ts` — 1 Funde, Abzug 0.94.

- [ ] [medium] setMyPresence - setMyPresence nests 4 levels deep (impact -0.94)

### `internal/sfu/authorization.go` — 1 Funde, Abzug 0.93.

- [ ] [medium] BindLocalStream - if condition combines 4 boolean operators (impact -0.93)

### `web/src/lib/globalSearch.ts` — 1 Funde, Abzug 0.93.

- [ ] [medium] onKeydown - if condition combines 4 boolean operators (impact -0.93)

### `internal/auth/lockout.go` — 3 Funde, Abzug 0.85.

- [ ] [medium] ProcessUnlockRequests - ProcessUnlockRequests has cyclomatic complexity 9 (impact -0.85)
- [ ] [medium] ProcessUnlockRequests - a database call runs once per loop iteration (N+1 query) (impact -0.00)
- [ ] [medium] RunUnlockWorker - a database call is reached once per loop iteration through RunUnlockWorker -> ProcessUnlockRequests (cross-function N+1 query) (impact -0.00)

### `internal/bot/commands_validate.go` — 1 Funde, Abzug 0.85.

- [ ] [medium] checkOptions - checkOptions has cyclomatic complexity 9 (impact -0.85)

### `internal/chat/handler.go` — 1 Funde, Abzug 0.85.

- [ ] [medium] listMembers - listMembers has cyclomatic complexity 9 (impact -0.85)

### `internal/chat/message_mutations.go` — 1 Funde, Abzug 0.85.

- [ ] [medium] DeleteMessageAs - DeleteMessageAs has cyclomatic complexity 11 (impact -0.85)

### `internal/gateway/presence_sync.go` — 1 Funde, Abzug 0.85.

- [ ] [medium] presenceChangesLocked - presenceChangesLocked has cyclomatic complexity 9 (impact -0.85)

### `internal/httpx/request.go` — 1 Funde, Abzug 0.85.

- [ ] [medium] SanitizeFilename - SanitizeFilename has cyclomatic complexity 11 (impact -0.85)

### `internal/media/objectreader.go` — 1 Funde, Abzug 0.85.

- [ ] [medium] rangeStart - rangeStart has cyclomatic complexity 11 (impact -0.85)

### `internal/perm/service.go` — 1 Funde, Abzug 0.85.

- [ ] [medium] reload - reload has cyclomatic complexity 10 (impact -0.85)

### `internal/perm/snapshot_load.go` — 1 Funde, Abzug 0.85.

- [ ] [medium] read - read has cyclomatic complexity 9 (impact -0.85)

### `internal/perm/store.go` — 1 Funde, Abzug 0.85.

- [ ] [medium] locked - locked has cyclomatic complexity 9 (impact -0.85)

### `internal/sfu/media_state.go` — 1 Funde, Abzug 0.85.

- [ ] [medium] notifyMedia - notifyMedia has cyclomatic complexity 10 (impact -0.85)

### `internal/sfu/room_signal.go` — 1 Funde, Abzug 0.85.

- [ ] [medium] watchOffer - watchOffer has cyclomatic complexity 9 (impact -0.85)

### `internal/sfu/sfu_setup.go` — 1 Funde, Abzug 0.85.

- [ ] [medium] buildSettingEngine - buildSettingEngine has cyclomatic complexity 9 (impact -0.85)

### `internal/ws/native_renew_frame.go` — 1 Funde, Abzug 0.85.

- [ ] [medium] parseNativeRenewFrame - parseNativeRenewFrame has cyclomatic complexity 9 (impact -0.85)

### `internal/ws/perms.go` — 1 Funde, Abzug 0.85.

- [ ] [medium] PermissionsChanged - PermissionsChanged has cyclomatic complexity 9 (impact -0.85)

### `internal/ws/voice.go` — 1 Funde, Abzug 0.85.

- [ ] [medium] applyUserUpdate - applyUserUpdate has cyclomatic complexity 9 (impact -0.85)

### `internal/ws/voice_kick.go` — 1 Funde, Abzug 0.85.

- [ ] [medium] CloseVoiceChannel - CloseVoiceChannel has cyclomatic complexity 9 (impact -0.85)

### `internal/ws/voicegate.go` — 1 Funde, Abzug 0.85.

- [ ] [medium] addVoiceTime - addVoiceTime has cyclomatic complexity 10 (impact -0.85)

### `loadtest/viewer/loss.go` — 1 Funde, Abzug 0.85.

- [ ] [medium] newAPI - newAPI has cyclomatic complexity 9 (impact -0.85)

### `scripts/upgrade-drill.sh` — 1 Funde, Abzug 0.85.

- [ ] [medium] resolve_previous - resolve_previous has cyclomatic complexity 11 (impact -0.85)

### `web/src/components/VoiceStage.vue` — 1 Funde, Abzug 0.85.

- [ ] [medium] openStageMenu - openStageMenu has cyclomatic complexity 9 (impact -0.85)

### `web/src/composables/useTalkStage.ts` — 1 Funde, Abzug 0.85.

- [ ] [medium] screenCardsFor - screenCardsFor has cyclomatic complexity 11 (impact -0.85)

### `web/src/composables/webrtc/camera.ts` — 1 Funde, Abzug 0.85.

- [ ] [medium] startCamera - startCamera has cyclomatic complexity 12 (impact -0.85)

### `web/src/composables/webrtc/layers.ts` — 1 Funde, Abzug 0.85.

- [ ] [medium] runSyncLayers - runSyncLayers has cyclomatic complexity 12 (impact -0.85)

### `web/src/composables/webrtc/session.ts` — 1 Funde, Abzug 0.85.

- [ ] [medium] joinVoiceChannel - joinVoiceChannel has cyclomatic complexity 10 (impact -0.85)

### `web/src/composables/webrtc/watchers.ts` — 1 Funde, Abzug 0.85.

- [ ] [medium] installWatchers - installWatchers has cyclomatic complexity 10 (impact -0.85)

### `web/src/lib/linkPreview.ts` — 1 Funde, Abzug 0.85.

- [ ] [medium] fetchLinkPreview - fetchLinkPreview has cyclomatic complexity 12 (impact -0.85)

### `web/src/lib/messageGrouping.ts` — 1 Funde, Abzug 0.85.

- [ ] [medium] isContinuation - isContinuation has cyclomatic complexity 10 (impact -0.85)

### `web/src/lib/screenLayers.ts` — 1 Funde, Abzug 0.85.

- [ ] [medium] planScreenLayers - planScreenLayers has cyclomatic complexity 9 (impact -0.85)

### `web/src/stores/chat/channels.ts` — 1 Funde, Abzug 0.85.

- [ ] [medium] loadChannels - loadChannels has cyclomatic complexity 9 (impact -0.85)

### `web/src/stores/chat/events.ts` — 1 Funde, Abzug 0.85.

- [ ] [medium] onMessageCreate - onMessageCreate has cyclomatic complexity 9 (impact -0.85)

### `web/src/stores/chat/notifications.ts` — 1 Funde, Abzug 0.85.

- [ ] [medium] triggerBrowserNotification - triggerBrowserNotification has cyclomatic complexity 12 (impact -0.85)

### `web/src/stores/chat/threads.ts` — 1 Funde, Abzug 0.85.

- [ ] [medium] openThread - openThread has cyclomatic complexity 10 (impact -0.85)

### `web/src/stores/chat/typing.ts` — 1 Funde, Abzug 0.85.

- [ ] [medium] handleTypingEvent - handleTypingEvent has cyclomatic complexity 12 (impact -0.85)

### `web/src/stores/voice/cameras.ts` — 1 Funde, Abzug 0.85.

- [ ] [medium] installStage - installStage has cyclomatic complexity 11 (impact -0.85)

### `web/src/components/PresenceMenu.vue` — 2 Funde, Abzug 0.75.

- [ ] [medium] - 33% of file duplicated in other files; worst clone shares 10 lines with web/src/components/AccountMenu.vue (impact -0.35)
- [ ] [low] onPointerDown - if condition combines 3 boolean operators (impact -0.40)

### `internal/webbundle/cli_trust.go` — 13 Funde, Abzug 0.63.

- [ ] [low] - 8% of file duplicated in other files; worst clone shares 13 lines with internal/webbundle/cli.go (impact -0.15)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.04)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.04)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.04)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.04)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.04)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.04)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.04)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.04)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.04)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.04)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.04)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.04)

### `internal/nativepreview/native_public_preview.go` — 2 Funde, Abzug 0.55.

- [ ] [low] - 9% of file duplicated in other files; worst clone shares 6 lines with internal/nativepreview/native_ciphertext_preview.go (co-changed 2x) (impact -0.15)
- [ ] [low] nativePublicPreviewBoundary - if condition combines 3 boolean operators (impact -0.40)

### `web/src/components/AccountMenu.vue` — 2 Funde, Abzug 0.55.

- [ ] [low] - 13% of file duplicated in other files; worst clone shares 10 lines with web/src/components/PresenceMenu.vue (impact -0.15)
- [ ] [low] onPointerDown - if condition combines 3 boolean operators (impact -0.40)

### `internal/media/handler_admin.go` — 2 Funde, Abzug 0.50.

- [ ] [medium] - 46% of file duplicated in other files; worst clone shares 33 lines with internal/media/handler_upload.go (impact -0.35)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/sfu/sfu_join.go` — 5 Funde, Abzug 0.50.

- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.10)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.10)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.10)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.10)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.10)

### `internal/webbundle/cli.go` — 8 Funde, Abzug 0.48.

- [ ] [medium] writeBundle - a filesystem call runs once per loop iteration (I/O call inside a loop) (impact -0.00)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.08)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.08)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.08)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.08)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.08)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.08)
- [ ] [low] readPub - a blocking filesystem call in one of the most-called functions in this repo (6 direct callers); every call through it waits for the I/O (impact -0.00)

### `internal/gateway/gateway.go` — 4 Funde, Abzug 0.45.

- [ ] [medium] Run - a database call is reached once per loop iteration through Run -> checkTokens -> validTokens (cross-function N+1 query) (impact -0.00)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/update/semver.go` — 3 Funde, Abzug 0.45.

- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `web/src/composables/useSortableDrag.ts` — 3 Funde, Abzug 0.45.

- [ ] [low] - caught exception is swallowed without any handling (impact -0.15)
- [ ] [low] - caught exception is swallowed without any handling (impact -0.15)
- [ ] [low] - caught exception is swallowed without any handling (impact -0.15)

### `internal/auth/native_transaction.go` — 1 Funde, Abzug 0.40.

- [ ] [low] WithAuthorizedTransaction - if condition combines 3 boolean operators (impact -0.40)

### `internal/perm/store_assignments.go` — 3 Funde, Abzug 0.40.

- [ ] [medium] syncRoles - a database call is reached once per loop iteration through syncRoles -> grantRole (cross-function N+1 query) (impact -0.00)
- [ ] [medium] syncRoles - a database call is reached once per loop iteration through syncRoles -> revokeRole (cross-function N+1 query) (impact -0.00)
- [ ] [low] grantRole - if condition combines 3 boolean operators (impact -0.40)

### `internal/webbundle/version.go` — 1 Funde, Abzug 0.40.

- [ ] [low] ParseVersion - if condition combines 3 boolean operators (impact -0.40)

### `internal/ws/handle.go` — 1 Funde, Abzug 0.40.

- [ ] [low] handleTyping - if condition combines 3 boolean operators (impact -0.40)

### `web/src/components/AdminUploadLimit.vue` — 1 Funde, Abzug 0.40.

- [ ] [low] computed callback - ternary condition combines 3 boolean operators (impact -0.40)

### `web/src/components/ChatArea.vue` — 1 Funde, Abzug 0.40.

- [ ] [low] handleSend - if condition combines 3 boolean operators (impact -0.40)

### `web/src/components/MarkdownContent.vue` — 1 Funde, Abzug 0.40.

- [ ] [low] onKeydown - if condition combines 3 boolean operators (impact -0.40)

### `web/src/components/ParticipantTile.vue` — 1 Funde, Abzug 0.40.

- [ ] [low] onDblclick - if condition combines 3 boolean operators (impact -0.40)

### `web/src/components/ThreadSidebar.vue` — 1 Funde, Abzug 0.40.

- [ ] [low] handleSendReply - if condition combines 3 boolean operators (impact -0.40)

### `web/src/composables/webrtc/voiceAudio.ts` — 1 Funde, Abzug 0.40.

- [ ] [low] watchForNoAudio - if condition combines 3 boolean operators (impact -0.40)

### `web/src/lib/appVersion.ts` — 1 Funde, Abzug 0.40.

- [ ] [low] isNewServerVersion - if condition combines 3 boolean operators (impact -0.40)

### `web/src/composables/useAppRouting.ts` — 1 Funde, Abzug 0.38.

- [ ] [low] useAppRouting - useAppRouting is 62 lines long (impact -0.38)

### `web/src/composables/useMessageActions.ts` — 1 Funde, Abzug 0.38.

- [ ] [low] useMessageActions - useMessageActions is 65 lines long (impact -0.38)

### `web/src/composables/useScreenLayerMenu.ts` — 1 Funde, Abzug 0.38.

- [ ] [low] useScreenLayerMenu - useScreenLayerMenu is 62 lines long (impact -0.38)

### `web/src/composables/useSlashCommands.ts` — 1 Funde, Abzug 0.38.

- [ ] [low] useSlashCommands - useSlashCommands is 66 lines long (impact -0.38)

### `internal/auth/handler_account.go` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 33% of file duplicated in other files; worst clone shares 35 lines with internal/auth/handler_profile.go (impact -0.35)

### `internal/auth/handler_profile.go` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 51% of file duplicated in other files; worst clone shares 35 lines with internal/auth/handler_account.go (impact -0.35)

### `internal/auth/moderation.go` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 37% of file duplicated in other files; worst clone shares 32 lines with internal/auth/users_admin_handlers.go (impact -0.35)

### `internal/bot/commands_bot.go` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 65% of file duplicated in other files; worst clone shares 34 lines with internal/bot/interactions_respond.go (impact -0.35)

### `internal/bot/handler_access.go` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 42% of file duplicated in other files; worst clone shares 32 lines with internal/bot/handler.go (impact -0.35)

### `internal/bot/handler_commands.go` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 71% of file duplicated in other files; worst clone shares 39 lines with internal/webhook/outgoing_handler.go (impact -0.35)

### `internal/bot/handler_tokens.go` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 47% of file duplicated in other files; worst clone shares 34 lines with internal/bot/handler_commands.go (impact -0.35)

### `internal/chat/botapi_members.go` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 30% of file duplicated in other files; worst clone shares 31 lines with internal/bot/handler_commands.go (impact -0.35)

### `internal/chat/handler_channels.go` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 56% of file duplicated in other files; worst clone shares 35 lines with internal/roles/handler.go (impact -0.35)

### `internal/chat/handler_readstate.go` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 39% of file duplicated in other files; worst clone shares 33 lines with internal/bot/handler.go (impact -0.35)

### `internal/roles/handler.go` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 53% of file duplicated in other files; worst clone shares 45 lines with internal/roles/handler_permissions.go (impact -0.35)

### `internal/roles/handler_permissions.go` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 33% of file duplicated in other files; worst clone shares 45 lines with internal/roles/handler.go (impact -0.35)

### `internal/server/system_update.go` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 37% of file duplicated in other files; worst clone shares 21 lines with internal/server/system.go (co-changed 2x) (impact -0.35)

### `internal/webhook/handler.go` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 51% of file duplicated in other files; worst clone shares 37 lines with internal/webhook/outgoing_handler.go (impact -0.35)

### `loadtest/reload.mjs` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 30% of file duplicated in other files; worst clone shares 7 lines with loadtest/browsers.mjs (impact -0.35)

### `web/src/components/AdminResetPasswordDialog.vue` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 29% of file duplicated in other files; worst clone shares 23 lines with web/src/components/MemberRolesDialog.vue (impact -0.35)

### `web/src/components/BotCommandsPanel.vue` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 36% of file duplicated in other files; worst clone shares 17 lines with web/src/components/IncomingWebhooksPanel.vue (impact -0.35)

### `web/src/components/ChatComposer.vue` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 46% of file duplicated in other files; worst clone shares 37 lines with web/src/components/ThreadComposer.vue (impact -0.35)

### `web/src/components/IncomingWebhooksPanel.vue` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 54% of file duplicated in other files; worst clone shares 37 lines with web/src/components/OutgoingWebhooksPanel.vue (impact -0.35)

### `web/src/components/MemberRolesDialog.vue` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 25% of file duplicated in other files; worst clone shares 23 lines with web/src/components/AdminResetPasswordDialog.vue (impact -0.35)

### `web/src/components/MentionSuggestions.vue` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 53% of file duplicated in other files; worst clone shares 24 lines with web/src/components/SlashSuggestions.vue (impact -0.35)

### `web/src/components/OutgoingWebhooksPanel.vue` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 32% of file duplicated in other files; worst clone shares 37 lines with web/src/components/IncomingWebhooksPanel.vue (impact -0.35)

### `web/src/components/SlashSuggestions.vue` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 70% of file duplicated in other files; worst clone shares 24 lines with web/src/components/MentionSuggestions.vue (impact -0.35)

### `web/src/components/ThreadComposer.vue` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 56% of file duplicated in other files; worst clone shares 37 lines with web/src/components/ChatComposer.vue (impact -0.35)

### `web/src/composables/useChannelAdminActions.ts` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 26% of file duplicated in other files; worst clone shares 14 lines with web/src/components/AdminLayoutTab.vue (impact -0.35)

### `web/src/lib/media/capability.ts` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 29% of file duplicated in other files; worst clone shares 10 lines with web/src/lib/media/heartbeat.ts (impact -0.35)

### `web/src/lib/media/heartbeat.ts` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 33% of file duplicated in other files; worst clone shares 10 lines with web/src/lib/media/capability.ts (impact -0.35)

### `web/src/lib/media/replay.ts` — 1 Funde, Abzug 0.35.

- [ ] [medium] ReplayWindow - ReplayWindow has low cohesion (LCOM4=2): its 5 methods split into 2 groups that share no fields or calls (impact -0.35)

### `web/src/lib/talkLayout.ts` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 39% of file duplicated in other files; worst clone shares 18 lines with web/src/lib/voiceChatPanel.ts (impact -0.35)

### `web/src/lib/voiceChatPanel.ts` — 1 Funde, Abzug 0.35.

- [ ] [medium] - 38% of file duplicated in other files; worst clone shares 18 lines with web/src/lib/talkLayout.ts (impact -0.35)

### `web/src/media-test.fixture.ts` — 1 Funde, Abzug 0.35.

- [ ] [medium] TestStream - TestStream has low cohesion (LCOM4=2): its 8 methods split into 2 groups that share no fields or calls (impact -0.35)

### `web/src/third_party/deepfilternet3/worker-glue.js` — 1 Funde, Abzug 0.35. Vendor, nicht editieren.

- [ ] [medium] - 100% of file duplicated in other files; worst clone shares 242 lines with web/src/third_party/deepfilternet3/worklet.js (impact -0.35)

### `internal/bot/store.go` — 2 Funde, Abzug 0.30.

- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/gateway/session.go` — 2 Funde, Abzug 0.30.

- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/server/public.go` — 2 Funde, Abzug 0.30.

- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `loadtest/viewer/main.go` — 3 Funde, Abzug 0.30.

- [ ] [medium] runBounded - a goroutine is spawned per loop element with no concurrency bound; use a worker pool or a bounded errgroup/semaphore (impact -0.00)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `cmd/server/storage.go` — 1 Funde, Abzug 0.15.

- [ ] [low] Upload - long parameter list: Upload takes 5 parameters, 3 of 5 typed ones scalars or strings (impact -0.15)

### `internal/audit/audit.go` — 1 Funde, Abzug 0.15.

- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/auth/service.go` — 1 Funde, Abzug 0.15.

- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/auth/users_admin.go` — 1 Funde, Abzug 0.15.

- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/chat/channel_layout.go` — 3 Funde, Abzug 0.15.

- [ ] [medium] ApplyLayoutTx - a database call runs once per loop iteration (N+1 query) (impact -0.00)
- [ ] [medium] ApplyLayoutTx - a database call runs once per loop iteration (N+1 query) (impact -0.00)
- [ ] [low] - 8% of file duplicated in other files; worst clone shares 16 lines with internal/media/admin.go (impact -0.15)

### `internal/gateway/serve_read.go` — 1 Funde, Abzug 0.15.

- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/httpx/discord.go` — 1 Funde, Abzug 0.15.

- [ ] [low] - 15% of file duplicated in other files; worst clone shares 12 lines with internal/httpx/response.go (impact -0.15)

### `internal/httpx/response.go` — 1 Funde, Abzug 0.15.

- [ ] [low] - 9% of file duplicated in other files; worst clone shares 12 lines with internal/httpx/discord.go (impact -0.15)

### `internal/linkpreview/handler.go` — 1 Funde, Abzug 0.15.

- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/media/handler.go` — 1 Funde, Abzug 0.15.

- [ ] [low] - 24% of file duplicated in other files; worst clone shares 31 lines with internal/media/handler_admin.go (impact -0.15)

### `internal/media/store.go` — 1 Funde, Abzug 0.15.

- [ ] [low] Upload - long parameter list: Upload takes 5 parameters, 3 of 5 typed ones scalars or strings (impact -0.15)

### `internal/s3/s3.go` — 1 Funde, Abzug 0.15.

- [ ] [low] Upload - long parameter list: Upload takes 5 parameters, 3 of 5 typed ones scalars or strings (impact -0.15)

### `internal/server/metrics.go` — 1 Funde, Abzug 0.15.

- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/server/system_ops.go` — 1 Funde, Abzug 0.15.

- [ ] [low] - 21% of file duplicated in other files; worst clone shares 21 lines with internal/server/system.go (co-changed 2x) (impact -0.15)

### `internal/sfu/layer_viewers.go` — 1 Funde, Abzug 0.15.

- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/webhook/outgoing_store.go` — 1 Funde, Abzug 0.15.

- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/ws/layers.go` — 1 Funde, Abzug 0.15.

- [ ] [low] - 24% of file duplicated in other files; worst clone shares 13 lines with internal/ws/signal.go (co-changed 2x) (impact -0.15)

### `internal/ws/lifecycle_pumps.go` — 1 Funde, Abzug 0.15.

- [ ] [low] - 14% of file duplicated in other files; worst clone shares 19 lines with internal/ws/native_metadata_admission.go (co-changed 2x) (impact -0.15)

### `internal/ws/presence.go` — 1 Funde, Abzug 0.15.

- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `internal/ws/voice_mute.go` — 1 Funde, Abzug 0.15.

- [ ] [low] - 15% of file duplicated in other files; worst clone shares 9 lines with internal/ws/layers.go (impact -0.15)

### `loadtest/lib.mjs` — 3 Funde, Abzug 0.15.

- [ ] [low] - 9% of file duplicated in other files; worst clone shares 7 lines with e2e/tests/screenshare.spec.ts (impact -0.15)
- [ ] [low] loadTokens - a blocking filesystem call in one of the most-called functions in this repo (3 direct callers); every call through it waits for the I/O (impact -0.00)
- [ ] [low] writeJSON - a blocking filesystem call in one of the most-called functions in this repo (4 direct callers); every call through it waits for the I/O (impact -0.00)

### `loadtest/viewer/summary.go` — 2 Funde, Abzug 0.15.

- [ ] [medium] diagnoseAll - a goroutine is spawned per loop element with no concurrency bound; use a worker pool or a bounded errgroup/semaphore (impact -0.00)
- [ ] [low] - error value is checked then ignored, or discarded via the blank identifier (impact -0.15)

### `web/src/components/AdminBotsTab.vue` — 1 Funde, Abzug 0.15.

- [ ] [low] - 22% of file duplicated in other files; worst clone shares 15 lines with web/src/components/IncomingWebhooksPanel.vue (impact -0.15)

### `web/src/components/AdminCategoryHeader.vue` — 1 Funde, Abzug 0.15.

- [ ] [low] - 9% of file duplicated in other files; worst clone shares 7 lines with web/src/components/AdminChannelRow.vue (impact -0.15)

### `web/src/components/AdminChannelRow.vue` — 1 Funde, Abzug 0.15.

- [ ] [low] - 8% of file duplicated in other files; worst clone shares 7 lines with web/src/components/AdminCategoryHeader.vue (impact -0.15)

### `web/src/components/AdminInvitesTab.vue` — 1 Funde, Abzug 0.15.

- [ ] [low] - 15% of file duplicated in other files; worst clone shares 11 lines with web/src/components/AdminLayoutTab.vue (impact -0.15)

### `web/src/components/AdminLayoutTab.vue` — 1 Funde, Abzug 0.15.

- [ ] [low] - 10% of file duplicated in other files; worst clone shares 33 lines with web/src/components/IncomingWebhooksPanel.vue (impact -0.15)

### `web/src/components/AdminSystemTab.vue` — 1 Funde, Abzug 0.15.

- [ ] [low] - caught exception is swallowed without any handling (impact -0.15)

### `web/src/components/BotTokensPanel.vue` — 1 Funde, Abzug 0.15.

- [ ] [low] - 10% of file duplicated in other files; worst clone shares 13 lines with web/src/components/BotCommandsPanel.vue (impact -0.15)

### `web/src/components/ModerationDialog.vue` — 1 Funde, Abzug 0.15.

- [ ] [low] - 16% of file duplicated in other files; worst clone shares 14 lines with web/src/components/MessageModerationDialog.vue (impact -0.15)

### `web/src/components/ProfileBio.vue` — 1 Funde, Abzug 0.15.

- [ ] [low] - 11% of file duplicated in other files; worst clone shares 16 lines with web/src/components/ProfilePassword.vue (impact -0.15)

### `web/src/components/ProfilePassword.vue` — 1 Funde, Abzug 0.15.

- [ ] [low] - 16% of file duplicated in other files; worst clone shares 16 lines with web/src/components/ProfileBio.vue (impact -0.15)

### `web/src/components/SidebarCategoryHeader.vue` — 1 Funde, Abzug 0.15.

- [ ] [low] - 17% of file duplicated in other files; worst clone shares 9 lines with web/src/components/SidebarChannelRow.vue (impact -0.15)

### `web/src/components/SidebarChannelRow.vue` — 1 Funde, Abzug 0.15.

- [ ] [low] - 15% of file duplicated in other files; worst clone shares 12 lines with web/src/components/TalkParticipants.vue (impact -0.15)

### `web/src/components/TalkControlBar.vue` — 1 Funde, Abzug 0.15.

- [ ] [low] - caught exception is swallowed without any handling (impact -0.15)

### `web/src/components/TalkParticipants.vue` — 1 Funde, Abzug 0.15.

- [ ] [low] - 17% of file duplicated in other files; worst clone shares 12 lines with web/src/components/SidebarChannelRow.vue (impact -0.15)

### `web/src/composables/useNotificationHint.ts` — 1 Funde, Abzug 0.15.

- [ ] [low] - caught exception is swallowed without any handling (impact -0.15)

### `web/src/composables/useStreamQuality.ts` — 1 Funde, Abzug 0.15.

- [ ] [low] radioItem - long parameter list: radioItem takes 5 parameters, 4 of 5 typed ones scalars or strings (impact -0.15)

### `web/src/composables/webrtc/audioMath.ts` — 1 Funde, Abzug 0.15.

- [ ] [low] - caught exception is swallowed without any handling (impact -0.15)

### `web/src/composables/webrtc/statsPolling.ts` — 1 Funde, Abzug 0.15.

- [ ] [low] - caught exception is swallowed without any handling (impact -0.15)

### `web/src/directives/tooltip.ts` — 1 Funde, Abzug 0.15.

- [ ] [low] - caught exception is swallowed without any handling (impact -0.15)

### `web/src/lib/channelTree.ts` — 1 Funde, Abzug 0.15.

- [ ] [low] - caught exception is swallowed without any handling (impact -0.15)

### `web/src/lib/media/api.ts` — 1 Funde, Abzug 0.15.

- [ ] [low] - 14% of file duplicated in other files; worst clone shares 9 lines with web/src/composables/useWebRTC.harness.fixture.ts (impact -0.15)

### `web/src/lib/voicePlayback.ts` — 1 Funde, Abzug 0.15.

- [ ] [low] - caught exception is swallowed without any handling (impact -0.15)

### `web/src/stores/appVersion.ts` — 1 Funde, Abzug 0.15.

- [ ] [low] - caught exception is swallowed without any handling (impact -0.15)

### `web/src/stores/auth.ts` — 1 Funde, Abzug 0.15.

- [ ] [low] - caught exception is swallowed without any handling (impact -0.15)

### `web/src/stores/voice/controls.ts` — 1 Funde, Abzug 0.15.

- [ ] [low] - caught exception is swallowed without any handling (impact -0.15)

### `web/src/stores/voice/state.ts` — 1 Funde, Abzug 0.15.

- [ ] [low] - caught exception is swallowed without any handling (impact -0.15)

### `internal/chat/post.go` — 2 Funde, Abzug 0.00.

- [ ] [medium] RunSlowModePrune - a database call is reached once per loop iteration through RunSlowModePrune -> PruneSlowMode (cross-function N+1 query) (impact -0.00)
- [ ] [medium] queryIDs - a database call runs once per loop iteration (N+1 query) (impact -0.00)

### `internal/httpx/middleware_clientip.go` — 2 Funde, Abzug 0.00.

- [ ] [low] ParseCIDRs - string built by += in a loop; use a buffer / join for linear cost (impact -0.00)
- [ ] [low] ParseCIDRs - string built by += in a loop; use a buffer / join for linear cost (impact -0.00)

### `internal/server/ops.go` — 1 Funde, Abzug 0.00.

- [ ] [medium] run - a filesystem call is reached once per loop iteration through run -> sample -> read -> Process (cross-function I/O call inside a loop) (impact -0.00)

### `internal/sfu/ordered.go` — 1 Funde, Abzug 0.00.

- [ ] [low] send - a lock is acquired every loop iteration; hoist the lock outside the loop or batch the critical section (impact -0.00)

### `scripts/check-native-test-vectors.mjs` — 2 Funde, Abzug 0.00.

- [ ] [medium] - a filesystem call runs once per loop iteration (I/O call inside a loop) (impact -0.00)
- [ ] [medium] - a subprocess spawn runs once per loop iteration (I/O call inside a loop) (impact -0.00)

### `web/vite.config.ts` — 3 Funde, Abzug 0.00.

- [ ] [medium] closeBundle - a filesystem call runs once per loop iteration (I/O call inside a loop) (impact -0.00)
- [ ] [medium] closeBundle - a filesystem call runs once per loop iteration (I/O call inside a loop) (impact -0.00)
- [ ] [medium] closeBundle - a filesystem call runs once per loop iteration (I/O call inside a loop) (impact -0.00)

## Live Dead Code (24, dieser Branch)

Alle Confidence 0.4, keines `safe_to_delete`. Pfad und Symbol:

| kind | Datei | Symbol | Zeilen | Grund |
| --- | --- | --- | ---: | --- |
| unused_export | `internal/chat/botapi.go` | `BotCreateMessageRequest` | 13 | 'BotCreateMessageRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `internal/webhook/execute.go` | `ExecutePayload` | 14 | 'ExecutePayload' is not imported, but is named elsewhere in the repo |
| unused_export | `loadtest/lib.mjs` | `BASE_URL` | 1 | Public symbol 'BASE_URL' has no importers |
| unused_export | `loadtest/lib.mjs` | `CHANNEL_NAME` | 1 | Public symbol 'CHANNEL_NAME' has no importers |
| unused_export | `loadtest/lib.mjs` | `host` | 1 | Public symbol 'host' has no importers |
| unused_export | `loadtest/lib.mjs` | `secure` | 1 | Public symbol 'secure' has no importers |
| unused_export | `loadtest/lib.mjs` | `fakeDisplayMedia` | 17 | Public symbol 'fakeDisplayMedia' has no importers |
| unused_export | `loadtest/lib.mjs` | `trackPeerConnections` | 10 | Public symbol 'trackPeerConnections' has no importers |
| unused_export | `web/src/composables/useNavMenus.ts` | `NOTIFY_LEVELS` | 1 | Public symbol 'NOTIFY_LEVELS' has no importers |
| unused_export | `web/src/composables/useWebRTC.harness.fixture.ts` | `permissiveMediaCall` | 14 | Public symbol 'permissiveMediaCall' has no importers |
| unused_export | `web/src/lib/media/bounds.ts` | `ROTATE_MS` | 1 | Public symbol 'ROTATE_MS' has no importers |
| unused_export | `web/src/lib/media/bounds.ts` | `ROTATE_JITTER_MS` | 1 | Public symbol 'ROTATE_JITTER_MS' has no importers |
| unused_export | `web/src/lib/media/bounds.ts` | `ABSENT_MS` | 1 | Public symbol 'ABSENT_MS' has no importers |
| unused_export | `web/src/lib/media/bounds.ts` | `HEARTBEAT_MS` | 1 | Public symbol 'HEARTBEAT_MS' has no importers |
| unused_export | `web/src/lib/media/bounds.ts` | `MAINTENANCE_MS` | 1 | Public symbol 'MAINTENANCE_MS' has no importers |
| unused_export | `web/src/lib/media/keys.fixture.ts` | `freshWindow` | 3 | Public symbol 'freshWindow' has no importers |
| unused_export | `web/src/lib/mentionQuery.ts` | `GROUP_MENTIONS` | 1 | Public symbol 'GROUP_MENTIONS' has no importers |
| unused_export | `web/src/lib/native/nativeChatView.ts` | `NativeTextSnapshot` | 1 | Public symbol 'NativeTextSnapshot' has no importers |
| unused_export | `web/src/lib/presence.ts` | `STATUSES` | 1 | Public symbol 'STATUSES' has no importers |
| unused_export | `web/src/lib/rtcStats.ts` | `ReceivedVideo` | 1 | Public symbol 'ReceivedVideo' has no importers |
| unused_export | `web/src/lib/screenLayers.ts` | `SCREEN_LAYER_HEIGHTS` | 1 | Public symbol 'SCREEN_LAYER_HEIGHTS' has no importers |
| unused_export | `web/src/lib/screenLayers.ts` | `LOWER_LAYER_FPS` | 1 | Public symbol 'LOWER_LAYER_FPS' has no importers |
| unused_export | `web/src/lib/streamQuality.ts` | `STREAM_MODES` | 1 | Public symbol 'STREAM_MODES' has no importers |
| unused_export | `web/src/test-fixtures.fixture.ts` | `attachmentFixture` | 7 | Public symbol 'attachmentFixture' has no importers |

Löschen nur, wenn kein Test, kein OpenAPI-Dokument und kein Fixture den Namen braucht. Sonst den Export privat machen, wenn der Compiler das erlaubt, oder stehen lassen und den Grund in dieser Tabelle ergänzen.

## Security (14, Index 2026-10-05)

Snippets stehen nicht hier.

| # | Schwere | Art | Datei | Zeile | Quelle | Aktion |
| ---: | --- | --- | --- | ---: | --- | --- |
| 1 | high | hardcoded_secret | `docker-compose.yml` | 145 | working-tree | Falsch positiv auf dem heutigen Baum prüfen. In v0.8.14 steht in dieser Region der PostgreSQL-17-Upgrade-Marker, kein Passwort. Wert steht absichtlich nicht in dieser Datei. |
| 2 | low | hardcoded_password | `e2e/run.sh` | 21 | working-tree | Test- oder E2E-Platzhalter. Wert nicht kopiert. Nur behalten, wenn es ein dokumentierter Dummy ist und .gitleaks.toml ihn schon erlaubt. Kein Produktionssecret einsetzen. |
| 3 | low | hardcoded_secret | `e2e/run.sh` | 23 | working-tree | Test- oder E2E-Platzhalter. Wert nicht kopiert. Nur behalten, wenn es ein dokumentierter Dummy ist und .gitleaks.toml ihn schon erlaubt. Kein Produktionssecret einsetzen. |
| 4 | low | hardcoded_secret | `e2e/run.sh` | 66 | working-tree | Test- oder E2E-Platzhalter. Wert nicht kopiert. Nur behalten, wenn es ein dokumentierter Dummy ist und .gitleaks.toml ihn schon erlaubt. Kein Produktionssecret einsetzen. |
| 5 | low | hardcoded_password | `e2e/run.sh` | 67 | working-tree | Test- oder E2E-Platzhalter. Wert nicht kopiert. Nur behalten, wenn es ein dokumentierter Dummy ist und .gitleaks.toml ihn schon erlaubt. Kein Produktionssecret einsetzen. |
| 6 | low | hardcoded_password | `e2e/run.sh` | 86 | working-tree | Test- oder E2E-Platzhalter. Wert nicht kopiert. Nur behalten, wenn es ein dokumentierter Dummy ist und .gitleaks.toml ihn schon erlaubt. Kein Produktionssecret einsetzen. |
| 7 | low | hardcoded_secret | `internal/auth/token_test.go` | 12 | working-tree | Test- oder E2E-Platzhalter. Wert nicht kopiert. Nur behalten, wenn es ein dokumentierter Dummy ist und .gitleaks.toml ihn schon erlaubt. Kein Produktionssecret einsetzen. |
| 8 | low | hardcoded_secret | `internal/server/auth_integration_test.go` | 254 | working-tree | Test- oder E2E-Platzhalter. Wert nicht kopiert. Nur behalten, wenn es ein dokumentierter Dummy ist und .gitleaks.toml ihn schon erlaubt. Kein Produktionssecret einsetzen. |
| 9 | low | hardcoded_secret | `internal/server/metrics_test.go` | 34 | working-tree | Test- oder E2E-Platzhalter. Wert nicht kopiert. Nur behalten, wenn es ein dokumentierter Dummy ist und .gitleaks.toml ihn schon erlaubt. Kein Produktionssecret einsetzen. |
| 10 | low | weak_hash | `internal/server/public.go` | 5 | working-tree | HMAC-SHA1 ist das von coturn verlangte REST-Format. Nicht auf einen anderen Hash umstellen. Das Secret verlässt den Server nicht. |
| 11 | low | weak_hash | `internal/server/public.go` | 98 | working-tree | HMAC-SHA1 ist das von coturn verlangte REST-Format. Nicht auf einen anderen Hash umstellen. Das Secret verlässt den Server nicht. |
| 12 | low | weak_hash | `internal/server/turn_test.go` | 5 | working-tree | Test des coturn-HMAC-SHA1. Der Hash bleibt, weil der Produktionscode ihn braucht. |
| 13 | low | weak_hash | `internal/server/turn_test.go` | 28 | working-tree | Test des coturn-HMAC-SHA1. Der Hash bleibt, weil der Produktionscode ihn braucht. |
| 14 | low | security_sensitive_symbol | `web/src/components/LoginModal.vue` | 21 | working-tree | Nur der Feldname des Login-Formulars. Kein Geheimnis. |

## Code health, die 447 der Overview

Offen, keine Testdatei, Typ nicht versteckt. `heute` ist der Zustand auf diesem Branch, nicht die Index-Zeile.

| # | Schwere | Biomarker | Datei im Index | Symbol | Zeilen | Impact | public_id | Grund | heute |
| ---: | --- | --- | --- | --- | --- | ---: | --- | --- | --- |
| 1 | high | brain_method | `internal/auth/service.go` | Register | 210-293 | 0.60 | `finding_4b6351c682f469b02176` | Brain Method: Register is 76 lines, CCN 22, in a file imported by 14 others | `internal/auth/service.go` 265 Zeilen |
| 2 | medium | brain_method | `internal/config/config.go` | FromEnv | 84-168 | 0.35 | `finding_a238671bb51239f6e800` | Brain Method: FromEnv is 81 lines, CCN 11, in a file imported by 7 others | `internal/config/config.go` 110 Zeilen |
| 3 | high | brain_method | `internal/sfu/sfu.go` | JoinPeer | 282-410 | 0.60 | `finding_70eb329df21e70834498` | Brain Method: JoinPeer is 105 lines, CCN 22, in a file imported by 10 others | `internal/sfu/sfu.go` 185 Zeilen |
| 4 | high | brain_method | `web/src/stores/chat.js` | defineStore callback | 33-1259 | 0.60 | `finding_07580a40e808b47725c7` | Brain Method: defineStore callback is 1026 lines, CCN 11, in a file imported by 34 others | `web/src/stores/chat.ts (was .js)` 137 Zeilen |
| 5 | high | brain_method | `web/src/stores/voice.js` | defineStore callback | 64-625 | 0.60 | `finding_1b53b3bf164b379b8b50` | Brain Method: defineStore callback is 470 lines, CCN 15, in a file imported by 28 others | `web/src/stores/voice.ts (was .js)` 134 Zeilen |
| 6 | low | bumpy_road | `web/src/components/AdminDashboard.vue` | onChannelDrop | 311-340 | 0.15 | `finding_c86bd0aa5d03a9e987c6` | onChannelDrop has 3 nested blocks at the same level (bumpy road) | `web/src/components/AdminDashboard.vue` 112 Zeilen |
| 7 | low | bumpy_road | `web/src/components/ChatArea.vue` | openMessageContextMenu | 422-516 | 0.12 | `finding_02ffd70192433651466e` | openMessageContextMenu has 3 nested blocks at the same level (bumpy road) | `web/src/components/ChatArea.vue` 346 Zeilen |
| 8 | medium | bumpy_road | `web/src/lib/rtcStats.js` | summarizeStats | 10-91 | 0.35 | `finding_15ac0dc7d16cf7bdba9f` | summarizeStats has 4 nested blocks at the same level (bumpy road) | `web/src/lib/rtcStats.ts (was .js)` 125 Zeilen |
| 9 | low | bumpy_road | `web/src/lib/soundEffects.js` | playSound | 69-183 | 0.15 | `finding_4dbaf3967f9a97c17442` | playSound has 3 nested blocks at the same level (bumpy road) | `web/src/lib/soundEffects.ts (was .js)` 174 Zeilen |
| 10 | critical | change_entropy | `cmd/server/main.go` |  |  | 0.55 | `finding_48113b378b16d1844296` | changes are scattered across noisy commits (top 4.3% among files eligible for change-entropy ranking); a strong history-based fault predictor | `cmd/server/main.go` 179 Zeilen |
| 11 | medium | change_entropy | `internal/auth/handler.go` |  |  | 0.89 | `finding_0e23a1cd1e2d580a33ad` | changes are scattered across noisy commits (top 18% among files eligible for change-entropy ranking); a strong history-based fault predictor | `internal/auth/handler.go` 207 Zeilen |
| 12 | medium | change_entropy | `internal/auth/service.go` |  |  | 1.00 | `finding_1c9c9c502968a935fc0a` | changes are scattered across noisy commits (top 17% among files eligible for change-entropy ranking); a strong history-based fault predictor | `internal/auth/service.go` 265 Zeilen |
| 13 | medium | change_entropy | `internal/auth/users_admin.go` |  |  | 0.75 | `finding_0d2ba7127310a169eedf` | changes are scattered across noisy commits (top 19% among files eligible for change-entropy ranking); a strong history-based fault predictor | `internal/auth/users_admin.go` 208 Zeilen |
| 14 | medium | change_entropy | `internal/chat/channels.go` |  |  | 1.06 | `finding_3c57fcbdeb585a618604` | changes are scattered across noisy commits (top 12% among files eligible for change-entropy ranking); a strong history-based fault predictor | `internal/chat/channels.go` 303 Zeilen |
| 15 | medium | change_entropy | `internal/chat/handler.go` |  |  | 0.93 | `finding_86bd1608191451be301d` | changes are scattered across noisy commits (top 12% among files eligible for change-entropy ranking); a strong history-based fault predictor | `internal/chat/handler.go` 177 Zeilen |
| 16 | medium | change_entropy | `internal/chat/messages.go` |  |  | 0.37 | `finding_5320169c387856a3f9a2` | changes are scattered across noisy commits (top 10% among files eligible for change-entropy ranking); a strong history-based fault predictor | `internal/chat/messages.go` 341 Zeilen |
| 17 | high | change_entropy | `internal/config/config.go` |  |  | 0.61 | `finding_cb5c3c37b0490c7cd187` | changes are scattered across noisy commits (top 5.8% among files eligible for change-entropy ranking); a strong history-based fault predictor | `internal/config/config.go` 110 Zeilen |
| 18 | medium | change_entropy | `internal/db/db.go` |  |  | 0.43 | `finding_771fe177d8360a33d86c` | changes are scattered across noisy commits (top 14% among files eligible for change-entropy ranking); a strong history-based fault predictor | `internal/db/db.go` 297 Zeilen |
| 19 | medium | change_entropy | `internal/httpx/middleware.go` |  |  | 0.41 | `finding_a049c0e0bed4ec485319` | changes are scattered across noisy commits (top 20% among files eligible for change-entropy ranking); a strong history-based fault predictor | `internal/httpx/middleware.go` 201 Zeilen |
| 20 | high | change_entropy | `internal/server/server.go` |  |  | 0.62 | `finding_ded713d663e90846510f` | changes are scattered across noisy commits (top 6.5% among files eligible for change-entropy ranking); a strong history-based fault predictor | `internal/server/server.go` 144 Zeilen |
| 21 | high | change_entropy | `internal/sfu/sfu.go` |  |  | 0.66 | `finding_5747defb418e10fb9595` | changes are scattered across noisy commits (top 8% among files eligible for change-entropy ranking); a strong history-based fault predictor | `internal/sfu/sfu.go` 185 Zeilen |
| 22 | critical | change_entropy | `internal/ws/hub.go` |  |  | 0.73 | `finding_1d30c0d950cd4c4f302e` | changes are scattered across noisy commits (top 0.7% among files eligible for change-entropy ranking); a strong history-based fault predictor | `internal/ws/hub.go` 165 Zeilen |
| 23 | high | change_entropy | `web/src/App.vue` |  |  | 0.80 | `finding_5fbf2c56b6983b79304b` | changes are scattered across noisy commits (top 5.1% among files eligible for change-entropy ranking); a strong history-based fault predictor | `web/src/App.vue` 300 Zeilen |
| 24 | medium | change_entropy | `web/src/components/AdminDashboard.vue` |  |  | 0.47 | `finding_dbe03110d7bea1a44750` | changes are scattered across noisy commits (top 11% among files eligible for change-entropy ranking); a strong history-based fault predictor | `web/src/components/AdminDashboard.vue` 112 Zeilen |
| 25 | high | change_entropy | `web/src/components/AudioSettingsModal.vue` |  |  | 0.35 | `finding_a3e40f37e529c96935bf` | changes are scattered across noisy commits (top 9.4% among files eligible for change-entropy ranking); a strong history-based fault predictor | `web/src/components/AudioSettingsModal.vue` 46 Zeilen |
| 26 | critical | change_entropy | `web/src/components/ChatArea.vue` |  |  | 0.91 | `finding_c902986b2e836af2a351` | changes are scattered across noisy commits (top 2.2% among files eligible for change-entropy ranking); a strong history-based fault predictor | `web/src/components/ChatArea.vue` 346 Zeilen |
| 27 | high | change_entropy | `web/src/components/Sidebar.vue` |  |  | 0.33 | `finding_4ed565b6c1684af06455` | changes are scattered across noisy commits (top 7.2% among files eligible for change-entropy ranking); a strong history-based fault predictor | `web/src/components/Sidebar.vue` 362 Zeilen |
| 28 | medium | change_entropy | `web/src/components/ThreadSidebar.vue` |  |  | 0.29 | `finding_d99dcf49719efc3ab110` | changes are scattered across noisy commits (top 15% among files eligible for change-entropy ranking); a strong history-based fault predictor | `web/src/components/ThreadSidebar.vue` 321 Zeilen |
| 29 | medium | change_entropy | `web/src/components/UserBar.vue` |  |  | 0.28 | `finding_aadbdfefd6ff400a8e01` | changes are scattered across noisy commits (top 14% among files eligible for change-entropy ranking); a strong history-based fault predictor | `web/src/components/UserBar.vue` 152 Zeilen |
| 30 | medium | change_entropy | `web/src/components/UserProfileModal.vue` |  |  | 0.24 | `finding_3641d8cd8466e474637a` | changes are scattered across noisy commits (top 16% among files eligible for change-entropy ranking); a strong history-based fault predictor | `web/src/components/UserProfileModal.vue` 286 Zeilen |
| 31 | critical | change_entropy | `web/src/components/VoiceStage.vue` |  |  | 0.77 | `finding_709a5cb88ad5801f259b` | changes are scattered across noisy commits (top 3.6% among files eligible for change-entropy ranking); a strong history-based fault predictor | `web/src/components/VoiceStage.vue` 389 Zeilen |
| 32 | critical | change_entropy | `web/src/composables/useWebRTC.js` |  |  | 0.64 | `finding_46a777cd7316b4f7e753` | changes are scattered across noisy commits (top 1.4% among files eligible for change-entropy ranking); a strong history-based fault predictor | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 33 | critical | change_entropy | `web/src/stores/chat.js` |  |  | 1.15 | `finding_7c6c83afd9b86588a81b` | changes are scattered across noisy commits (top 2.9% among files eligible for change-entropy ranking); a strong history-based fault predictor | `web/src/stores/chat.ts (was .js)` 137 Zeilen |
| 34 | high | change_entropy | `web/src/stores/voice.js` |  |  | 1.31 | `finding_cbfb7ec52cd50e2136c0` | changes are scattered across noisy commits (top 8.7% among files eligible for change-entropy ranking); a strong history-based fault predictor | `web/src/stores/voice.ts (was .js)` 134 Zeilen |
| 35 | critical | churn_risk | `cmd/server/main.go` |  |  | 0.44 | `finding_b19f534866e09e3f53e9` | 90-day churn rewrote 28.7x the file's size (2156 lines over 75 NLOC, top 2.4% among repository files eligible for churn ranking) | `cmd/server/main.go` 179 Zeilen |
| 36 | medium | churn_risk | `internal/auth/handler.go` |  |  | 0.71 | `finding_ddaee97b5bebf8080190` | 90-day churn rewrote 2.3x the file's size (573 lines over 246 NLOC, top 11% among repository files eligible for churn ranking) | `internal/auth/handler.go` 207 Zeilen |
| 37 | low | churn_risk | `internal/auth/service.go` |  |  | 0.34 | `finding_ed32acb33fa041ba50d0` | 90-day churn rewrote 1.4x the file's size (437 lines over 308 NLOC, top 14% among repository files eligible for churn ranking) | `internal/auth/service.go` 265 Zeilen |
| 38 | medium | churn_risk | `internal/auth/users_admin.go` |  |  | 0.60 | `finding_1f9895c44e05ca8b4fe3` | 90-day churn rewrote 2.4x the file's size (556 lines over 231 NLOC, top 12% among repository files eligible for churn ranking) | `internal/auth/users_admin.go` 208 Zeilen |
| 39 | medium | churn_risk | `internal/chat/channels.go` |  |  | 0.84 | `finding_43643d79f54639d62d80` | 90-day churn rewrote 1.6x the file's size (381 lines over 235 NLOC, top 15% among repository files eligible for churn ranking) | `internal/chat/channels.go` 303 Zeilen |
| 40 | medium | churn_risk | `internal/chat/handler.go` |  |  | 0.74 | `finding_2033b821cf216b15535e` | 90-day churn rewrote 2.0x the file's size (1112 lines over 551 NLOC, top 6.8% among repository files eligible for churn ranking) | `internal/chat/handler.go` 177 Zeilen |
| 41 | high | churn_risk | `internal/chat/messages.go` |  |  | 0.51 | `finding_4ff9427910ba7858e827` | 90-day churn rewrote 3.0x the file's size (1191 lines over 401 NLOC, top 5.9% among repository files eligible for churn ranking) | `internal/chat/messages.go` 341 Zeilen |
| 42 | medium | churn_risk | `internal/config/config.go` |  |  | 0.28 | `finding_c2654f102a9be509fa92` | 90-day churn rewrote 2.0x the file's size (422 lines over 213 NLOC, top 14% among repository files eligible for churn ranking) | `internal/config/config.go` 110 Zeilen |
| 43 | high | churn_risk | `internal/db/db.go` |  |  | 0.58 | `finding_85092bbe981423bf373e` | 90-day churn rewrote 2.7x the file's size (459 lines over 167 NLOC, top 13% among repository files eligible for churn ranking) | `internal/db/db.go` 297 Zeilen |
| 44 | low | churn_risk | `internal/httpx/middleware.go` |  |  | 0.14 | `finding_468cdd95efb959096279` | 90-day churn rewrote 1.3x the file's size (384 lines over 286 NLOC, top 18% among repository files eligible for churn ranking) | `internal/httpx/middleware.go` 201 Zeilen |
| 45 | medium | churn_risk | `internal/media/handler.go` |  |  | 0.78 | `finding_b906dfc3e91bac9a047a` | 90-day churn rewrote 1.6x the file's size (542 lines over 330 NLOC, top 12% among repository files eligible for churn ranking) | `internal/media/handler.go` 146 Zeilen |
| 46 | low | churn_risk | `internal/server/server.go` |  |  | 0.12 | `finding_eb50369c96d44d1f7c86` | 90-day churn rewrote 1.4x the file's size (267 lines over 196 NLOC, top 23% among repository files eligible for churn ranking) | `internal/server/server.go` 144 Zeilen |
| 47 | medium | churn_risk | `internal/sfu/sfu.go` |  |  | 0.30 | `finding_04c154233eba8b37a58a` | 90-day churn rewrote 2.1x the file's size (1017 lines over 479 NLOC, top 4.9% among repository files eligible for churn ranking) | `internal/sfu/sfu.go` 185 Zeilen |
| 48 | medium | churn_risk | `internal/ws/hub.go` |  |  | 0.20 | `finding_c8da24b0e0c9f0312e27` | 90-day churn rewrote 2.3x the file's size (1763 lines over 764 NLOC, top 2.9% among repository files eligible for churn ranking) | `internal/ws/hub.go` 165 Zeilen |
| 49 | high | churn_risk | `web/src/App.vue` |  |  | 0.64 | `finding_791aba2cb549e693451e` | 90-day churn rewrote 2.7x the file's size (623 lines over 235 NLOC, top 9.3% among repository files eligible for churn ranking) | `web/src/App.vue` 300 Zeilen |
| 50 | high | churn_risk | `web/src/components/AdminDashboard.vue` |  |  | 0.64 | `finding_a81041168bc83bd29e7f` | 90-day churn rewrote 2.8x the file's size (2215 lines over 781 NLOC, top 3.9% among repository files eligible for churn ranking) | `web/src/components/AdminDashboard.vue` 112 Zeilen |
| 51 | high | churn_risk | `web/src/components/AudioSettingsModal.vue` |  |  | 0.28 | `finding_04b1ba23d6305dc86249` | 90-day churn rewrote 3.6x the file's size (925 lines over 260 NLOC, top 6.3% among repository files eligible for churn ranking) | `web/src/components/AudioSettingsModal.vue` 46 Zeilen |
| 52 | high | churn_risk | `web/src/components/ChatArea.vue` |  |  | 0.43 | `finding_c139a23bfe06b06cc1b4` | 90-day churn rewrote 2.9x the file's size (2190 lines over 743 NLOC, top 1.5% among repository files eligible for churn ranking) | `web/src/components/ChatArea.vue` 346 Zeilen |
| 53 | critical | churn_risk | `web/src/components/ConnectionStatsModal.vue` |  |  | 0.83 | `finding_8660ee092b033e15658e` | 90-day churn rewrote 9.3x the file's size (631 lines over 68 NLOC, top 8.8% among repository files eligible for churn ranking) | `web/src/components/ConnectionStatsModal.vue` 188 Zeilen |
| 54 | critical | churn_risk | `web/src/components/CreateChannelModal.vue` |  |  | 0.83 | `finding_929469e3c0f434ec9b24` | 90-day churn rewrote 5.2x the file's size (604 lines over 116 NLOC, top 11% among repository files eligible for churn ranking) | `web/src/components/CreateChannelModal.vue` 232 Zeilen |
| 55 | critical | churn_risk | `web/src/components/LegalModal.vue` |  |  | 1.26 | `finding_9cce732860673ad5fc70` | 90-day churn rewrote 8.2x the file's size (930 lines over 113 NLOC, top 7.3% among repository files eligible for churn ranking) | `web/src/components/LegalModal.vue` 285 Zeilen |
| 56 | critical | churn_risk | `web/src/components/LoginModal.vue` |  |  | 0.75 | `finding_0ce3b6d04e5f1c1b5010` | 90-day churn rewrote 4.0x the file's size (342 lines over 85 NLOC, top 17% among repository files eligible for churn ranking) | `web/src/components/LoginModal.vue` 219 Zeilen |
| 57 | critical | churn_risk | `web/src/components/MarkdownContent.vue` |  |  | 1.00 | `finding_a8ae971055710459a7d8` | 90-day churn rewrote 5.8x the file's size (295 lines over 51 NLOC, top 20% among repository files eligible for churn ranking) | `web/src/components/MarkdownContent.vue` 143 Zeilen |
| 58 | critical | churn_risk | `web/src/components/MemberList.vue` |  |  | 1.71 | `finding_6a812d3748dfb9067d09` | 90-day churn rewrote 10.0x the file's size (613 lines over 61 NLOC, top 10% among repository files eligible for churn ranking) | `web/src/components/MemberList.vue` 103 Zeilen |
| 59 | critical | churn_risk | `web/src/components/Sidebar.vue` |  |  | 0.44 | `finding_54c04d2bffbedf20258a` | 90-day churn rewrote 4.5x the file's size (1557 lines over 346 NLOC, top 3.4% among repository files eligible for churn ranking) | `web/src/components/Sidebar.vue` 362 Zeilen |
| 60 | high | churn_risk | `web/src/components/ThreadSidebar.vue` |  |  | 0.40 | `finding_438460f338a6ffd1adf9` | 90-day churn rewrote 3.0x the file's size (1072 lines over 355 NLOC, top 4.4% among repository files eligible for churn ranking) | `web/src/components/ThreadSidebar.vue` 321 Zeilen |
| 61 | critical | churn_risk | `web/src/components/UserBar.vue` |  |  | 0.62 | `finding_e63ad4983ca3ee303f6b` | 90-day churn rewrote 8.7x the file's size (616 lines over 71 NLOC, top 9.8% among repository files eligible for churn ranking) | `web/src/components/UserBar.vue` 152 Zeilen |
| 62 | high | churn_risk | `web/src/components/UserProfileModal.vue` |  |  | 0.32 | `finding_b874aef8ec30dc13eeb8` | 90-day churn rewrote 3.5x the file's size (1003 lines over 285 NLOC, top 5.4% among repository files eligible for churn ranking) | `web/src/components/UserProfileModal.vue` 286 Zeilen |
| 63 | high | churn_risk | `web/src/components/VoiceStage.vue` |  |  | 0.37 | `finding_99120baba829732cdc14` | 90-day churn rewrote 3.7x the file's size (1757 lines over 473 NLOC, top 2% among repository files eligible for churn ranking) | `web/src/components/VoiceStage.vue` 389 Zeilen |
| 64 | medium | churn_risk | `web/src/composables/useWebRTC.js` |  |  | 0.18 | `finding_e84e4c5279f2402807f7` | 90-day churn rewrote 1.9x the file's size (2117 lines over 1120 NLOC, top 0.5% among repository files eligible for churn ranking) | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 65 | high | churn_risk | `web/src/stores/auth.js` |  |  | 0.48 | `finding_ead148d95e21132bc8c1` | 90-day churn rewrote 3.0x the file's size (329 lines over 110 NLOC, top 17% among repository files eligible for churn ranking) | `web/src/stores/auth.ts (was .js)` 61 Zeilen |
| 66 | medium | churn_risk | `web/src/stores/chat.js` |  |  | 0.32 | `finding_74fb1de3064596a6264e` | 90-day churn rewrote 2.4x the file's size (2559 lines over 1051 NLOC, top 1% among repository files eligible for churn ranking) | `web/src/stores/chat.ts (was .js)` 137 Zeilen |
| 67 | low | churn_risk | `web/src/stores/voice.js` |  |  | 0.26 | `finding_bb761de7e7c6128ea928` | 90-day churn rewrote 1.4x the file's size (745 lines over 523 NLOC, top 7.8% among repository files eligible for churn ranking) | `web/src/stores/voice.ts (was .js)` 134 Zeilen |
| 68 | medium | co_change_scatter | `cmd/server/main.go` |  |  | 0.23 | `finding_0aba639d6e525c9192b9` | co-changes with 42 distinct files (top 7.1% among files with any co-change history) — editing this file tends to ripple across the codebase (shotgun surgery) | `cmd/server/main.go` 179 Zeilen |
| 69 | medium | co_change_scatter | `internal/chat/messages.go` |  |  | 0.45 | `finding_5e1399d82dcdd8e02e04` | co-changes with 46 distinct files (top 13% among files with any co-change history) — editing this file tends to ripple across the codebase (shotgun surgery) | `internal/chat/messages.go` 341 Zeilen |
| 70 | medium | co_change_scatter | `internal/config/config.go` |  |  | 0.42 | `finding_f569631d5e4fc64b4bee` | co-changes with 44 distinct files (top 10% among files with any co-change history) — editing this file tends to ripple across the codebase (shotgun surgery) | `internal/config/config.go` 110 Zeilen |
| 71 | medium | co_change_scatter | `internal/db/db.go` |  |  | 0.51 | `finding_35b2a751157de29945ea` | co-changes with 33 distinct files (top 19% among files with any co-change history) — editing this file tends to ripple across the codebase (shotgun surgery) | `internal/db/db.go` 297 Zeilen |
| 72 | medium | co_change_scatter | `internal/server/server.go` |  |  | 0.43 | `finding_4c37d8df9df7b86cd59d` | co-changes with 47 distinct files (top 9.2% among files with any co-change history) — editing this file tends to ripple across the codebase (shotgun surgery) | `internal/server/server.go` 144 Zeilen |
| 73 | medium | co_change_scatter | `internal/sfu/sfu.go` |  |  | 0.46 | `finding_b03ca88641c9e3f25030` | co-changes with 30 distinct files (top 14% among files with any co-change history) — editing this file tends to ripple across the codebase (shotgun surgery) | `internal/sfu/sfu.go` 185 Zeilen |
| 74 | high | co_change_scatter | `internal/ws/hub.go` |  |  | 0.52 | `finding_04e2e1f89b6ea7f38ab8` | co-changes with 52 distinct files (top 1% among files with any co-change history) — editing this file tends to ripple across the codebase (shotgun surgery) | `internal/ws/hub.go` 165 Zeilen |
| 75 | medium | co_change_scatter | `web/src/App.vue` |  |  | 0.56 | `finding_d8310a50650d5a18e07d` | co-changes with 62 distinct files (top 6.1% among files with any co-change history) — editing this file tends to ripple across the codebase (shotgun surgery) | `web/src/App.vue` 300 Zeilen |
| 76 | medium | co_change_scatter | `web/src/components/AdminDashboard.vue` |  |  | 0.56 | `finding_3ff5e81b16aa2b33ac5e` | co-changes with 58 distinct files (top 15% among files with any co-change history) — editing this file tends to ripple across the codebase (shotgun surgery) | `web/src/components/AdminDashboard.vue` 112 Zeilen |
| 77 | medium | co_change_scatter | `web/src/components/AudioSettingsModal.vue` |  |  | 0.24 | `finding_183295443251512c837a` | co-changes with 60 distinct files (top 12% among files with any co-change history) — editing this file tends to ripple across the codebase (shotgun surgery) | `web/src/components/AudioSettingsModal.vue` 46 Zeilen |
| 78 | high | co_change_scatter | `web/src/components/ChatArea.vue` |  |  | 0.65 | `finding_a078c8fa50a043a88607` | co-changes with 63 distinct files (top 3.1% among files with any co-change history) — editing this file tends to ripple across the codebase (shotgun surgery) | `web/src/components/ChatArea.vue` 346 Zeilen |
| 79 | medium | co_change_scatter | `web/src/components/Sidebar.vue` |  |  | 0.23 | `finding_24a918a80e6ed97e2a62` | co-changes with 71 distinct files (top 8.2% among files with any co-change history) — editing this file tends to ripple across the codebase (shotgun surgery) | `web/src/components/Sidebar.vue` 362 Zeilen |
| 80 | medium | co_change_scatter | `web/src/components/ThreadSidebar.vue` |  |  | 0.35 | `finding_8d61af54d629f3f7625b` | co-changes with 58 distinct files (top 18% among files with any co-change history) — editing this file tends to ripple across the codebase (shotgun surgery) | `web/src/components/ThreadSidebar.vue` 321 Zeilen |
| 81 | medium | co_change_scatter | `web/src/components/UserBar.vue` |  |  | 0.33 | `finding_cc4d6e62e03c3e726c73` | co-changes with 59 distinct files (top 17% among files with any co-change history) — editing this file tends to ripple across the codebase (shotgun surgery) | `web/src/components/UserBar.vue` 152 Zeilen |
| 82 | medium | co_change_scatter | `web/src/components/UserProfileModal.vue` |  |  | 0.28 | `finding_97d1f1ffd64ac29ae373` | co-changes with 67 distinct files (top 16% among files with any co-change history) — editing this file tends to ripple across the codebase (shotgun surgery) | `web/src/components/UserProfileModal.vue` 286 Zeilen |
| 83 | high | co_change_scatter | `web/src/components/VoiceStage.vue` |  |  | 0.55 | `finding_d57bc5bc4da85e943537` | co-changes with 72 distinct files (top 2% among files with any co-change history) — editing this file tends to ripple across the codebase (shotgun surgery) | `web/src/components/VoiceStage.vue` 389 Zeilen |
| 84 | high | co_change_scatter | `web/src/composables/useWebRTC.js` |  |  | 0.46 | `finding_221e6ed5d637d97c83a8` | co-changes with 50 distinct files (top 4.1% among files with any co-change history) — editing this file tends to ripple across the codebase (shotgun surgery) | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 85 | medium | co_change_scatter | `web/src/stores/chat.js` |  |  | 0.48 | `finding_0759a62ae43dff23c444` | co-changes with 66 distinct files (top 5.1% among files with any co-change history) — editing this file tends to ripple across the codebase (shotgun surgery) | `web/src/stores/chat.ts (was .js)` 137 Zeilen |
| 86 | medium | co_change_scatter | `web/src/stores/voice.js` |  |  | 0.91 | `finding_7ddc929a14666db9673e` | co-changes with 45 distinct files (top 11% among files with any co-change history) — editing this file tends to ripple across the codebase (shotgun surgery) | `web/src/stores/voice.ts (was .js)` 134 Zeilen |
| 87 | low | complex_conditional | `internal/chat/messages.go` | ValidateEmoji | 91-91 | 0.40 | `finding_bad8d79b534a72e1da57` | if condition combines 3 boolean operators | `internal/chat/messages.go` 341 Zeilen |
| 88 | low | complex_conditional | `internal/chat/search.go` | Search | 47-47 | 0.40 | `finding_58021323ba344eeb5f3c` | if condition combines 3 boolean operators | `internal/chat/search.go` 197 Zeilen |
| 89 | low | complex_conditional | `internal/config/config.go` | validate | 178-178 | 0.40 | `finding_e3d6fd52c425e90125f3` | if condition combines 3 boolean operators | `internal/config/config.go` 110 Zeilen |
| 90 | medium | complex_conditional | `internal/linkpreview/linkpreview.go` | publicIP | 148-148 | 0.58 | `finding_0c28655306f54434581c` | if condition combines 4 boolean operators | `internal/linkpreview/linkpreview.go` 291 Zeilen |
| 91 | low | complex_conditional | `internal/linkpreview/linkpreview.go` | checkURL | 202-202 | 0.25 | `finding_9272c7c329bf1fe7c0ae` | if condition combines 3 boolean operators | `internal/linkpreview/linkpreview.go` 291 Zeilen |
| 92 | medium | complex_conditional | `internal/sfu/subscriptions.go` | Subscribe | 150-150 | 0.93 | `finding_a93d76bdbf522160d8a8` | if condition combines 4 boolean operators | `internal/sfu/subscriptions.go` 182 Zeilen |
| 93 | low | complex_conditional | `web/src/App.vue` | applyRoute | 117-117 | 0.40 | `finding_ca12a117924ce112eeeb` | if condition combines 3 boolean operators | `web/src/App.vue` 300 Zeilen |
| 94 | critical | complex_conditional | `web/src/components/ChatArea.vue` | handleMessageKeydown | 563-563 | 2.07 | `finding_328dc27d048b9201c974` | if condition combines 6 boolean operators | `web/src/components/ChatArea.vue` 346 Zeilen |
| 95 | low | complex_conditional | `web/src/components/ChatArea.vue` | onGlobalKeydown | 601-601 | 0.31 | `finding_c9203f80649b127085d1` | if condition combines 3 boolean operators | `web/src/components/ChatArea.vue` 346 Zeilen |
| 96 | medium | complex_conditional | `web/src/components/ContextMenu.vue` | handleKeydown | 140-140 | 0.93 | `finding_c34a41b8336d453d52fb` | if condition combines 4 boolean operators | `web/src/components/ContextMenu.vue` 390 Zeilen |
| 97 | medium | complex_conditional | `web/src/lib/globalSearch.js` | onKeydown | 27-27 | 0.93 | `finding_c362d6a4204033160e7c` | if condition combines 4 boolean operators | `web/src/lib/globalSearch.ts (was .js)` 43 Zeilen |
| 98 | low | complex_conditional | `web/src/lib/markdown.js` | renderMarkdown | 56-56 | 0.40 | `finding_4eb4db7324a056bb4dad` | if condition combines 3 boolean operators | `web/src/lib/markdown.ts (was .js)` 88 Zeilen |
| 99 | low | complex_conditional | `web/src/lib/rtcStats.js` | summarizeStats | 16-16 | 0.40 | `finding_92de68ed168a3fb55088` | if condition combines 3 boolean operators | `web/src/lib/rtcStats.ts (was .js)` 125 Zeilen |
| 100 | medium | complex_method | `cmd/server/main.go` | run | 32-92 | 0.85 | `finding_c110c597294368f8585d` | run has cyclomatic complexity 14 | `cmd/server/main.go` 179 Zeilen |
| 101 | medium | complex_method | `internal/auth/invites.go` | CreateInvite | 67-101 | 0.85 | `finding_acf995dab6b355b08a1a` | CreateInvite has cyclomatic complexity 13 | `internal/auth/invites.go` 219 Zeilen |
| 102 | high | complex_method | `internal/auth/service.go` | Register | 210-293 | 1.19 | `finding_8d750ecfbecc570b0ef9` | Register has cyclomatic complexity 22 | `internal/auth/service.go` 265 Zeilen |
| 103 | medium | complex_method | `internal/chat/channels.go` | GetServerHierarchy | 45-89 | 0.62 | `finding_0abe58689ad25bf95f23` | GetServerHierarchy has cyclomatic complexity 10 | `internal/chat/channels.go` 303 Zeilen |
| 104 | medium | complex_method | `internal/chat/channels.go` | CreateChannel | 107-138 | 0.62 | `finding_213c7239ffedac3ffd23` | CreateChannel has cyclomatic complexity 9 | `internal/chat/channels.go` 303 Zeilen |
| 105 | medium | complex_method | `internal/chat/handler.go` | listMessages | 137-173 | 0.50 | `finding_9886965698816fcff83a` | listMessages has cyclomatic complexity 11 | `internal/chat/handler.go` 177 Zeilen |
| 106 | medium | complex_method | `internal/chat/handler.go` | createMessage | 195-242 | 0.50 | `finding_120d5c369c891b295494` | createMessage has cyclomatic complexity 11 | `internal/chat/handler.go` 177 Zeilen |
| 107 | medium | complex_method | `internal/chat/handler.go` | getThread | 416-457 | 0.50 | `finding_f409a819f006a2bd50d7` | getThread has cyclomatic complexity 12 | `internal/chat/handler.go` 177 Zeilen |
| 108 | medium | complex_method | `internal/chat/mentions.go` | ResolveMentions | 49-94 | 0.85 | `finding_251f96a0374487d863f6` | ResolveMentions has cyclomatic complexity 12 | `internal/chat/mentions.go` 179 Zeilen |
| 109 | medium | complex_method | `internal/chat/messages.go` | ValidateEmoji | 85-96 | 0.40 | `finding_75a3e278578d1c10a40b` | ValidateEmoji has cyclomatic complexity 9 | `internal/chat/messages.go` 341 Zeilen |
| 110 | medium | complex_method | `internal/chat/messages.go` | enrich | 153-219 | 0.40 | `finding_6d67ac91798d0bc098fb` | enrich has cyclomatic complexity 14 | `internal/chat/messages.go` 341 Zeilen |
| 111 | medium | complex_method | `internal/chat/messages.go` | ValidateReplyTarget | 312-327 | 0.40 | `finding_552b8bced981cf124380` | ValidateReplyTarget has cyclomatic complexity 12 | `internal/chat/messages.go` 341 Zeilen |
| 112 | high | complex_method | `internal/chat/search.go` | Search | 38-103 | 1.19 | `finding_c4b1352374c8b78ef458` | Search has cyclomatic complexity 16 | `internal/chat/search.go` 197 Zeilen |
| 113 | medium | complex_method | `internal/config/config.go` | FromEnv | 84-168 | 0.35 | `finding_8a3a807f3e6b39a11873` | FromEnv has cyclomatic complexity 11 | `internal/config/config.go` 110 Zeilen |
| 114 | critical | complex_method | `internal/config/config.go` | validate | 170-231 | 1.00 | `finding_a1504951a55b8908bf03` | validate has cyclomatic complexity 27 | `internal/config/config.go` 110 Zeilen |
| 115 | medium | complex_method | `internal/db/db.go` | Migrate | 67-132 | 0.61 | `finding_f78237071039e0019a49` | Migrate has cyclomatic complexity 14 | `internal/db/db.go` 297 Zeilen |
| 116 | medium | complex_method | `internal/db/db.go` | EnsureAdminUser | 152-193 | 0.61 | `finding_84e77f9170c6f7ee5100` | EnsureAdminUser has cyclomatic complexity 9 | `internal/db/db.go` 297 Zeilen |
| 117 | medium | complex_method | `internal/httpx/request.go` | SanitizeFilename | 83-107 | 0.85 | `finding_f6abbd553ec753976ab2` | SanitizeFilename has cyclomatic complexity 11 | `internal/httpx/request.go` 107 Zeilen |
| 118 | medium | complex_method | `internal/linkpreview/linkpreview.go` | publicIP | 137-157 | 0.40 | `finding_c9aae3d216161be04d0e` | publicIP has cyclomatic complexity 11 | `internal/linkpreview/linkpreview.go` 291 Zeilen |
| 119 | medium | complex_method | `internal/linkpreview/linkpreview.go` | checkURL | 195-206 | 0.40 | `finding_950842e90e957db679d6` | checkURL has cyclomatic complexity 9 | `internal/linkpreview/linkpreview.go` 291 Zeilen |
| 120 | high | complex_method | `internal/linkpreview/linkpreview.go` | parseHTML | 281-333 | 0.69 | `finding_1d8170ebfe88b69ab00d` | parseHTML has cyclomatic complexity 20 | `internal/linkpreview/linkpreview.go` 291 Zeilen |
| 121 | medium | complex_method | `internal/media/admin.go` | PruneOlderThan | 103-142 | 0.85 | `finding_3d7c67c054cdc3ee74ba` | PruneOlderThan has cyclomatic complexity 9 | `internal/media/admin.go` 207 Zeilen |
| 122 | medium | complex_method | `internal/media/handler.go` | readFile | 80-122 | 0.35 | `finding_3a8f5e42684f23f3e524` | readFile has cyclomatic complexity 11 | `internal/media/handler.go` 146 Zeilen |
| 123 | high | complex_method | `internal/media/handler.go` | upload | 155-243 | 0.59 | `finding_ae8c7513b177d29c5587` | upload has cyclomatic complexity 19 | `internal/media/handler.go` 146 Zeilen |
| 124 | medium | complex_method | `internal/media/handler.go` | uploadAvatar | 266-318 | 0.35 | `finding_942f0bcfa20cb1619c01` | uploadAvatar has cyclomatic complexity 11 | `internal/media/handler.go` 146 Zeilen |
| 125 | medium | complex_method | `internal/server/server.go` | NewRouter | 48-181 | 0.74 | `finding_81e18c16dd981fbf69c2` | NewRouter has cyclomatic complexity 11 | `internal/server/server.go` 144 Zeilen |
| 126 | high | complex_method | `internal/sfu/sfu.go` | JoinPeer | 282-410 | 0.58 | `finding_ceaecf1f32bf191b73d5` | JoinPeer has cyclomatic complexity 22 | `internal/sfu/sfu.go` 185 Zeilen |
| 127 | high | complex_method | `internal/sfu/sfu.go` | SignalPeerConnections | 423-485 | 0.58 | `finding_eae68435121b48811893` | SignalPeerConnections has cyclomatic complexity 19 | `internal/sfu/sfu.go` 185 Zeilen |
| 128 | medium | complex_method | `internal/sfu/subscriptions.go` | Subscribe | 132-164 | 0.85 | `finding_35d9de9d121d684e5c08` | Subscribe has cyclomatic complexity 13 | `internal/sfu/subscriptions.go` 182 Zeilen |
| 129 | medium | complex_method | `internal/tools/openapifix/main.go` | run | 39-74 | 0.50 | `finding_bfcba202b48f8046b6ec` | run has cyclomatic complexity 13 | `internal/tools/openapifix/main.go` 206 Zeilen |
| 130 | medium | complex_method | `internal/tools/openapifix/main.go` | walk | 77-102 | 0.50 | `finding_4a909ef6e7f067d9a164` | walk has cyclomatic complexity 9 | `internal/tools/openapifix/main.go` 206 Zeilen |
| 131 | medium | complex_method | `internal/tools/openapifix/main.go` | fixOperation | 104-141 | 0.50 | `finding_ec9264b57a4699ca6004` | fixOperation has cyclomatic complexity 13 | `internal/tools/openapifix/main.go` 206 Zeilen |
| 132 | medium | complex_method | `internal/ws/hub.go` | unregister | 372-414 | 0.19 | `finding_26b244b0b6ba8a81e521` | unregister has cyclomatic complexity 9 | `internal/ws/hub.go` 165 Zeilen |
| 133 | high | complex_method | `internal/ws/hub.go` | joinVoice | 553-614 | 0.32 | `finding_5cfff6e0d47d5ad8b734` | joinVoice has cyclomatic complexity 16 | `internal/ws/hub.go` 165 Zeilen |
| 134 | critical | complex_method | `internal/ws/hub.go` | handle | 794-889 | 0.54 | `finding_3f55dcade53b3659685a` | handle has cyclomatic complexity 38 | `internal/ws/hub.go` 165 Zeilen |
| 135 | medium | complex_method | `internal/ws/hub.go` | handleSubscribe | 894-930 | 0.19 | `finding_0c2bfb50745908010bb2` | handleSubscribe has cyclomatic complexity 14 | `internal/ws/hub.go` 165 Zeilen |
| 136 | medium | complex_method | `internal/ws/hub.go` | writePump | 938-977 | 0.19 | `finding_12b65872c8212c3b616e` | writePump has cyclomatic complexity 9 | `internal/ws/hub.go` 165 Zeilen |
| 137 | critical | complex_method | `web/src/App.vue` | applyRoute | 79-154 | 1.30 | `finding_8fc1ee15510154cc3ee1` | applyRoute has cyclomatic complexity 30 | `web/src/App.vue` 300 Zeilen |
| 138 | medium | complex_method | `web/src/components/AccountMenu.vue` | onKeydown | 57-84 | 0.85 | `finding_93f3c72a96b3f24b67e5` | onKeydown has cyclomatic complexity 11 | `web/src/components/AccountMenu.vue` 220 Zeilen |
| 139 | medium | complex_method | `web/src/components/AdminDashboard.vue` | onChannelDrop | 311-340 | 0.85 | `finding_5906ba13512399332751` | onChannelDrop has cyclomatic complexity 9 | `web/src/components/AdminDashboard.vue` 112 Zeilen |
| 140 | medium | complex_method | `web/src/components/ChatArea.vue` | maybeLoadMore | 190-199 | 0.20 | `finding_77f6f52287a1ba1a001a` | maybeLoadMore has cyclomatic complexity 10 | `web/src/components/ChatArea.vue` 346 Zeilen |
| 141 | medium | complex_method | `web/src/components/ChatArea.vue` | watch callback | 242-267 | 0.20 | `finding_29164c776a6afff4cd8f` | watch callback has cyclomatic complexity 11 | `web/src/components/ChatArea.vue` 346 Zeilen |
| 142 | medium | complex_method | `web/src/components/ChatArea.vue` | openMessageContextMenu | 422-516 | 0.20 | `finding_b0005952086ce225d512` | openMessageContextMenu has cyclomatic complexity 12 | `web/src/components/ChatArea.vue` 346 Zeilen |
| 143 | critical | complex_method | `web/src/components/ChatArea.vue` | handleMessageKeydown | 555-594 | 0.59 | `finding_aa3d51eb024a3a2b79b4` | handleMessageKeydown has cyclomatic complexity 26 | `web/src/components/ChatArea.vue` 346 Zeilen |
| 144 | medium | complex_method | `web/src/components/ChatArea.vue` | onGlobalKeydown | 596-606 | 0.20 | `finding_aae723ac447144c2b083` | onGlobalKeydown has cyclomatic complexity 14 | `web/src/components/ChatArea.vue` 346 Zeilen |
| 145 | high | complex_method | `web/src/components/ContextMenu.vue` | handleKeydown | 111-152 | 1.45 | `finding_6a91adb02bc3823f0dd3` | handleKeydown has cyclomatic complexity 17 | `web/src/components/ContextMenu.vue` 390 Zeilen |
| 146 | medium | complex_method | `web/src/components/LegalModal.vue` | onMounted callback | 27-46 | 0.85 | `finding_f2964dce7245d640dd77` | onMounted callback has cyclomatic complexity 11 | `web/src/components/LegalModal.vue` 285 Zeilen |
| 147 | medium | complex_method | `web/src/components/SearchModal.vue` | handleKeydown | 123-144 | 0.85 | `finding_42f1e4ba3ae4e4f113b5` | handleKeydown has cyclomatic complexity 10 | `web/src/components/SearchModal.vue` 340 Zeilen |
| 148 | medium | complex_method | `web/src/components/VoiceStage.vue` | computed callback | 124-136 | 0.85 | `finding_e2dd80c87b29cd7adb1f` | computed callback has cyclomatic complexity 9 | `web/src/components/VoiceStage.vue` 389 Zeilen |
| 149 | medium | complex_method | `web/src/composables/useNavMenus.js` | buildChannelItems | 63-123 | 0.61 | `finding_be7ab9f5dcb4b29f3df0` | buildChannelItems has cyclomatic complexity 9 | `web/src/composables/useNavMenus.ts (was .js)` 367 Zeilen |
| 150 | medium | complex_method | `web/src/composables/useNavMenus.js` | buildMemberItems | 186-262 | 0.61 | `finding_b16d09b58849e31ad591` | buildMemberItems has cyclomatic complexity 13 | `web/src/composables/useNavMenus.ts (was .js)` 367 Zeilen |
| 151 | medium | complex_method | `web/src/composables/useWebRTC.js` | createBoost | 125-145 | 0.18 | `finding_9f1310fec7f2bd79c540` | createBoost has cyclomatic complexity 9 | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 152 | medium | complex_method | `web/src/composables/useWebRTC.js` | buildScreenAudioMix | 226-262 | 0.18 | `finding_678363e98cbffa17aa58` | buildScreenAudioMix has cyclomatic complexity 12 | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 153 | high | complex_method | `web/src/composables/useWebRTC.js` | setupPeerConnection | 265-347 | 0.32 | `finding_40d1d700df0f54fa784e` | setupPeerConnection has cyclomatic complexity 19 | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 154 | medium | complex_method | `web/src/composables/useWebRTC.js` | handlePttKeyDown | 389-402 | 0.18 | `finding_409917a9a86a90441bcd` | handlePttKeyDown has cyclomatic complexity 10 | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 155 | medium | complex_method | `web/src/i18n/index.js` | flattenKeys | 111-124 | 0.85 | `finding_80f74ff729e4dd8df040` | flattenKeys has cyclomatic complexity 9 | `web/src/i18n/index.ts (was .js)` 136 Zeilen |
| 156 | medium | complex_method | `web/src/lib/api.js` | errorMessage | 41-50 | 0.75 | `finding_e633f4e09254c43bae01` | errorMessage has cyclomatic complexity 9 | `web/src/lib/api.ts (was .js)` 195 Zeilen |
| 157 | medium | complex_method | `web/src/lib/api.js` | api | 64-92 | 0.75 | `finding_454301608c5d02027b8b` | api has cyclomatic complexity 10 | `web/src/lib/api.ts (was .js)` 195 Zeilen |
| 158 | medium | complex_method | `web/src/lib/linkPreview.js` | fetchLinkPreview | 8-22 | 0.85 | `finding_2e588449453292242f0a` | fetchLinkPreview has cyclomatic complexity 9 | `web/src/lib/linkPreview.ts (was .js)` 31 Zeilen |
| 159 | medium | complex_method | `web/src/lib/markdown.js` | renderMarkdown | 21-67 | 0.85 | `finding_230f515db694e2c72019` | renderMarkdown has cyclomatic complexity 10 | `web/src/lib/markdown.ts (was .js)` 88 Zeilen |
| 160 | medium | complex_method | `web/src/lib/mentionQuery.js` | findMentionQuery | 11-20 | 0.55 | `finding_5469eb3efc282559b94f` | findMentionQuery has cyclomatic complexity 11 | `web/src/lib/mentionQuery.ts (was .js)` 66 Zeilen |
| 161 | high | complex_method | `web/src/lib/mentionQuery.js` | suggestMentions | 30-49 | 0.95 | `finding_da86e280f4b70b1f1ca7` | suggestMentions has cyclomatic complexity 22 | `web/src/lib/mentionQuery.ts (was .js)` 66 Zeilen |
| 162 | medium | complex_method | `web/src/lib/messageGrouping.js` | isContinuation | 10-20 | 0.85 | `finding_d55a5ed0b2f6640fda33` | isContinuation has cyclomatic complexity 10 | `web/src/lib/messageGrouping.ts (was .js)` 33 Zeilen |
| 163 | medium | complex_method | `web/src/lib/router.js` | parseRoute | 5-32 | 0.50 | `finding_61ce7b32a027f7e3f723` | parseRoute has cyclomatic complexity 12 | `web/src/lib/router.ts (was .js)` 126 Zeilen |
| 164 | medium | complex_method | `web/src/lib/router.js` | buildRoute | 34-48 | 0.50 | `finding_b525e8732342b5593e2f` | buildRoute has cyclomatic complexity 10 | `web/src/lib/router.ts (was .js)` 126 Zeilen |
| 165 | medium | complex_method | `web/src/lib/router.js` | resolveRoute | 89-99 | 0.50 | `finding_0b280a551a64d0f99e22` | resolveRoute has cyclomatic complexity 11 | `web/src/lib/router.ts (was .js)` 126 Zeilen |
| 166 | critical | complex_method | `web/src/lib/rtcStats.js` | summarizeStats | 10-91 | 1.30 | `finding_c80d4d9bb9b49a6a5bc1` | summarizeStats has cyclomatic complexity 41 | `web/src/lib/rtcStats.ts (was .js)` 125 Zeilen |
| 167 | medium | complex_method | `web/src/lib/soundEffects.js` | playTone | 27-63 | 0.35 | `finding_85b150f629dfb8c3e86d` | playTone has cyclomatic complexity 13 | `web/src/lib/soundEffects.ts (was .js)` 174 Zeilen |
| 168 | critical | complex_method | `web/src/lib/soundEffects.js` | playSound | 69-183 | 1.00 | `finding_5608a087c788797ee07d` | playSound has cyclomatic complexity 26 | `web/src/lib/soundEffects.ts (was .js)` 174 Zeilen |
| 169 | medium | complex_method | `web/src/stores/chat.js` | defineStore callback | 33-1259 | 0.38 | `finding_3797127b7311d554a452` | defineStore callback has cyclomatic complexity 11 | `web/src/stores/chat.ts (was .js)` 137 Zeilen |
| 170 | high | complex_method | `web/src/stores/voice.js` | defineStore callback | 64-625 | 0.55 | `finding_13aaff1544769f47f389` | defineStore callback has cyclomatic complexity 15 | `web/src/stores/voice.ts (was .js)` 134 Zeilen |
| 171 | medium | complex_method | `web/web.go` | newHandler | 40-77 | 0.85 | `finding_451fa98f9ff05432015e` | newHandler has cyclomatic complexity 11 | `web/web.go` 137 Zeilen |
| 172 | medium | defer_in_loop | `web/web.go` | serveCompressed | 98-98 | 0.00 | `finding_c4098856f7a64524cdbe` | a deferred call inside a loop runs only when the enclosing function returns, so the resource stays held across every iteration; close it in the loop body (or wrap the body in a function) instead | `web/web.go` 137 Zeilen |
| 173 | low | error_handling | `internal/db/db.go` |  | 78-78 | 0.15 | `finding_b507462c076b84d2b654` | error value is checked then ignored, or discarded via the blank identifier | `internal/db/db.go` 297 Zeilen |
| 174 | low | error_handling | `internal/linkpreview/handler.go` |  | 82-82 | 0.15 | `finding_60defe5a83c1c0873eb4` | error value is checked then ignored, or discarded via the blank identifier | `internal/linkpreview/handler.go` 104 Zeilen |
| 175 | low | error_handling | `internal/media/handler.go` |  | 427-427 | 0.15 | `finding_04a65367d91035197ce5` | error value is checked then ignored, or discarded via the blank identifier | `internal/media/handler.go` 146 Zeilen |
| 176 | low | error_handling | `internal/server/metrics.go` |  | 86-86 | 0.15 | `finding_4d8e723bddf24406844b` | error value is checked then ignored, or discarded via the blank identifier | `internal/server/metrics.go` 108 Zeilen |
| 177 | low | error_handling | `internal/server/public.go` |  | 149-149 | 0.15 | `finding_72a047560cf0d9608d91` | error value is checked then ignored, or discarded via the blank identifier | `internal/server/public.go` 188 Zeilen |
| 178 | low | error_handling | `internal/server/public.go` |  | 179-179 | 0.15 | `finding_c75efd68cc078dffb82a` | error value is checked then ignored, or discarded via the blank identifier | `internal/server/public.go` 188 Zeilen |
| 179 | low | error_handling | `internal/sfu/sfu.go` |  | 305-305 | 0.15 | `finding_dfd21433e3977f8f7584` | error value is checked then ignored, or discarded via the blank identifier | `internal/sfu/sfu.go` 185 Zeilen |
| 180 | low | error_handling | `internal/sfu/sfu.go` |  | 306-306 | 0.15 | `finding_387b3a828418a12c44da` | error value is checked then ignored, or discarded via the blank identifier | `internal/sfu/sfu.go` 185 Zeilen |
| 181 | low | error_handling | `internal/sfu/sfu.go` |  | 307-307 | 0.15 | `finding_7d05826697696e813984` | error value is checked then ignored, or discarded via the blank identifier | `internal/sfu/sfu.go` 185 Zeilen |
| 182 | low | error_handling | `web/src/components/ChatArea.vue` |  | 329-329 | 0.15 | `finding_98efcdb000e25f70f3e9` | caught exception is swallowed without any handling | `web/src/components/ChatArea.vue` 346 Zeilen |
| 183 | low | error_handling | `web/src/components/LegalModal.vue` |  | 43-43 | 0.15 | `finding_8bf1e69bb77bc7727557` | caught exception is swallowed without any handling | `web/src/components/LegalModal.vue` 285 Zeilen |
| 184 | low | error_handling | `web/src/composables/useResizable.js` |  | 154-154 | 0.15 | `finding_eb294786c84c0f6a7e82` | caught exception is swallowed without any handling | `web/src/composables/useResizable.ts (was .js)` 322 Zeilen |
| 185 | low | error_handling | `web/src/composables/useResizable.js` |  | 197-197 | 0.15 | `finding_c8cb358734f94f5d25f4` | caught exception is swallowed without any handling | `web/src/composables/useResizable.ts (was .js)` 322 Zeilen |
| 186 | low | error_handling | `web/src/composables/useWebRTC.js` |  | 97-97 | 0.04 | `finding_09f15a0bc768e879e584` | caught exception is swallowed without any handling | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 187 | low | error_handling | `web/src/composables/useWebRTC.js` |  | 164-164 | 0.04 | `finding_ccb7552b5890c520a31e` | caught exception is swallowed without any handling | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 188 | low | error_handling | `web/src/composables/useWebRTC.js` |  | 177-177 | 0.04 | `finding_b9afc3dab234afa2b8fa` | caught exception is swallowed without any handling | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 189 | low | error_handling | `web/src/composables/useWebRTC.js` |  | 185-185 | 0.04 | `finding_b4ad4ca33fc004887b9d` | caught exception is swallowed without any handling | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 190 | low | error_handling | `web/src/composables/useWebRTC.js` |  | 217-217 | 0.04 | `finding_380f9439f61dfc50d409` | caught exception is swallowed without any handling | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 191 | low | error_handling | `web/src/composables/useWebRTC.js` |  | 219-219 | 0.04 | `finding_abe1c0c90ad4bc90bf0f` | caught exception is swallowed without any handling | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 192 | low | error_handling | `web/src/composables/useWebRTC.js` |  | 311-311 | 0.04 | `finding_b0f942a8c2bb792b9df7` | caught exception is swallowed without any handling | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 193 | low | error_handling | `web/src/composables/useWebRTC.js` |  | 490-490 | 0.04 | `finding_8b972a1f079f0bb3efa4` | caught exception is swallowed without any handling | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 194 | low | error_handling | `web/src/composables/useWebRTC.js` |  | 669-669 | 0.04 | `finding_fb6f855f4a6d5699822a` | caught exception is swallowed without any handling | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 195 | low | error_handling | `web/src/composables/useWebRTC.js` |  | 992-992 | 0.04 | `finding_4c02877147ad57645558` | caught exception is swallowed without any handling | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 196 | low | error_handling | `web/src/composables/useWebRTC.js` |  | 999-999 | 0.04 | `finding_39177753be5117b33c39` | caught exception is swallowed without any handling | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 197 | low | error_handling | `web/src/directives/tooltip.js` |  | 109-109 | 0.15 | `finding_7d7651e6bf6cc5a45e4f` | caught exception is swallowed without any handling | `web/src/directives/tooltip.ts (was .js)` 154 Zeilen |
| 198 | low | error_handling | `web/src/i18n/index.js` |  | 20-20 | 0.15 | `finding_076f4d7a50a48a857f7d` | caught exception is swallowed without any handling | `web/src/i18n/index.ts (was .js)` 136 Zeilen |
| 199 | low | error_handling | `web/src/lib/channelTree.js` |  | 31-31 | 0.15 | `finding_6a0c1f573482fffccdc3` | caught exception is swallowed without any handling | `web/src/lib/channelTree.ts (was .js)` 39 Zeilen |
| 200 | low | error_handling | `web/src/lib/soundEffects.js` |  | 60-60 | 0.15 | `finding_ea48222d2d8c3bde6482` | caught exception is swallowed without any handling | `web/src/lib/soundEffects.ts (was .js)` 174 Zeilen |
| 201 | low | error_handling | `web/src/lib/soundEffects.js` |  | 73-73 | 0.15 | `finding_184fd8064923ba49c973` | caught exception is swallowed without any handling | `web/src/lib/soundEffects.ts (was .js)` 174 Zeilen |
| 202 | low | error_handling | `web/src/lib/voiceSession.js` |  | 21-21 | 0.15 | `finding_8afe627d2072dda09e21` | caught exception is swallowed without any handling | `web/src/lib/voiceSession.ts (was .js)` 79 Zeilen |
| 203 | low | error_handling | `web/src/lib/voiceSession.js` |  | 29-29 | 0.15 | `finding_fb35507aadab86a18ef6` | caught exception is swallowed without any handling | `web/src/lib/voiceSession.ts (was .js)` 79 Zeilen |
| 204 | low | error_handling | `web/src/stores/auth.js` |  | 21-21 | 0.15 | `finding_a74af3e5811edd586dcf` | caught exception is swallowed without any handling | `web/src/stores/auth.ts (was .js)` 61 Zeilen |
| 205 | low | error_handling | `web/src/stores/voice.js` |  | 31-31 | 0.15 | `finding_87aeae5330faf9432cef` | caught exception is swallowed without any handling | `web/src/stores/voice.ts (was .js)` 134 Zeilen |
| 206 | low | error_handling | `web/src/stores/voice.js` |  | 411-411 | 0.15 | `finding_c4284f20494b02eeb940` | caught exception is swallowed without any handling | `web/src/stores/voice.ts (was .js)` 134 Zeilen |
| 207 | low | error_handling | `web/web.go` |  | 36-36 | 0.12 | `finding_eb37ace0f235d1481271` | error value is checked then ignored, or discarded via the blank identifier | `web/web.go` 137 Zeilen |
| 208 | low | error_handling | `web/web.go` |  | 41-41 | 0.12 | `finding_6cc689c15c44df69027a` | error value is checked then ignored, or discarded via the blank identifier | `web/web.go` 137 Zeilen |
| 209 | low | error_handling | `web/web.go` |  | 51-51 | 0.12 | `finding_0829b8c46beafc497fcd` | error value is checked then ignored, or discarded via the blank identifier | `web/web.go` 137 Zeilen |
| 210 | low | error_handling | `web/web.go` |  | 120-120 | 0.12 | `finding_c3885d4faadcf2a511bc` | error value is checked then ignored, or discarded via the blank identifier | `web/web.go` 137 Zeilen |
| 211 | high | function_hotspot | `cmd/server/main.go` | run | 32-92 | 0.26 | `finding_75f35e519b85da16b9f8` | run has been modified across 6 commits (repo p80=2) and carries CCN=14 / nesting=1 | `cmd/server/main.go` 179 Zeilen |
| 212 | high | function_hotspot | `internal/auth/service.go` | Register | 210-293 | 1.31 | `finding_aca2f8d1cb6aa70e340c` | Register has been modified across 3 commits (repo p80=2) and carries CCN=22 / nesting=2 | `internal/auth/service.go` 265 Zeilen |
| 213 | medium | function_hotspot | `internal/chat/channels.go` | GetServerHierarchy | 45-89 | 0.81 | `finding_67928344e74d47383539` | GetServerHierarchy has been modified across 3 commits (repo p80=2) and carries CCN=10 / nesting=3 | `internal/chat/channels.go` 303 Zeilen |
| 214 | medium | function_hotspot | `internal/chat/handler.go` | createMessage | 195-242 | 0.71 | `finding_83230a9921f4293068a5` | createMessage has been modified across 3 commits (repo p80=2) and carries CCN=11 / nesting=1 | `internal/chat/handler.go` 177 Zeilen |
| 215 | medium | function_hotspot | `internal/chat/handler.go` | getThread | 416-457 | 0.71 | `finding_368398a0f218d3305faa` | getThread has been modified across 3 commits (repo p80=2) and carries CCN=12 / nesting=2 | `internal/chat/handler.go` 177 Zeilen |
| 216 | medium | function_hotspot | `internal/chat/messages.go` | scanMessages | 114-143 | 0.29 | `finding_f1fec05376b205742d99` | scanMessages has been modified across 3 commits (repo p80=2) and carries CCN=5 / nesting=3 | `internal/chat/messages.go` 341 Zeilen |
| 217 | medium | function_hotspot | `internal/chat/messages.go` | enrich | 153-219 | 0.29 | `finding_ec192f14e321e4ab644b` | enrich has been modified across 3 commits (repo p80=2) and carries CCN=14 / nesting=2 | `internal/chat/messages.go` 341 Zeilen |
| 218 | medium | function_hotspot | `internal/chat/messages.go` | GetChannelMessages | 249-276 | 0.29 | `finding_ae69735218f744535202` | GetChannelMessages has been modified across 3 commits (repo p80=2) and carries CCN=7 / nesting=3 | `internal/chat/messages.go` 341 Zeilen |
| 219 | high | function_hotspot | `internal/config/config.go` | FromEnv | 84-168 | 0.47 | `finding_bf89e31cb4600b5f0bec` | FromEnv has been modified across 10 commits (repo p80=2) and carries CCN=11 / nesting=1 | `internal/config/config.go` 110 Zeilen |
| 220 | critical | function_hotspot | `internal/config/config.go` | validate | 170-231 | 0.78 | `finding_c680c97d61c9d37ac8d8` | validate has been modified across 7 commits (repo p80=2) and carries CCN=27 / nesting=2 | `internal/config/config.go` 110 Zeilen |
| 221 | medium | function_hotspot | `internal/db/db.go` | Migrate | 67-132 | 0.33 | `finding_33180b18a06fa0974ac6` | Migrate has been modified across 3 commits (repo p80=2) and carries CCN=14 / nesting=2 | `internal/db/db.go` 297 Zeilen |
| 222 | high | function_hotspot | `internal/media/handler.go` | upload | 155-243 | 1.28 | `finding_256de084b52023318ff7` | upload has been modified across 2 commits (repo p80=2) and carries CCN=19 / nesting=2 | `internal/media/handler.go` 146 Zeilen |
| 223 | medium | function_hotspot | `internal/media/handler.go` | uploadAvatar | 266-318 | 0.75 | `finding_d7373211460801306200` | uploadAvatar has been modified across 2 commits (repo p80=2) and carries CCN=11 / nesting=3 | `internal/media/handler.go` 146 Zeilen |
| 224 | high | function_hotspot | `internal/server/server.go` | NewRouter | 48-181 | 0.48 | `finding_cae344077d9c97ca1841` | NewRouter has been modified across 10 commits (repo p80=2) and carries CCN=11 / nesting=3 | `internal/server/server.go` 144 Zeilen |
| 225 | critical | function_hotspot | `internal/sfu/sfu.go` | JoinPeer | 282-410 | 0.84 | `finding_6fcc8c131b9075159261` | JoinPeer has been modified across 7 commits (repo p80=2) and carries CCN=22 / nesting=3 | `internal/sfu/sfu.go` 185 Zeilen |
| 226 | high | function_hotspot | `internal/sfu/sfu.go` | SignalPeerConnections | 423-485 | 0.51 | `finding_afd18d07f98256e0a0da` | SignalPeerConnections has been modified across 5 commits (repo p80=2) and carries CCN=19 / nesting=4 | `internal/sfu/sfu.go` 185 Zeilen |
| 227 | high | function_hotspot | `internal/ws/hub.go` | register | 344-370 | 0.34 | `finding_7f9a358634699ccd3148` | register has been modified across 4 commits (repo p80=2) and carries CCN=6 / nesting=3 | `internal/ws/hub.go` 165 Zeilen |
| 228 | high | function_hotspot | `internal/ws/hub.go` | unregister | 372-414 | 0.34 | `finding_bee71b21e014a3bbd29a` | unregister has been modified across 6 commits (repo p80=2) and carries CCN=9 / nesting=3 | `internal/ws/hub.go` 165 Zeilen |
| 229 | high | function_hotspot | `internal/ws/hub.go` | joinVoice | 553-614 | 0.34 | `finding_1f21a7e5ba8c91f88f50` | joinVoice has been modified across 6 commits (repo p80=2) and carries CCN=16 / nesting=4 | `internal/ws/hub.go` 165 Zeilen |
| 230 | critical | function_hotspot | `internal/ws/hub.go` | handle | 794-889 | 0.56 | `finding_d5c9e2b8d9bcd0cfa0f4` | handle has been modified across 13 commits (repo p80=2) and carries CCN=38 / nesting=5 | `internal/ws/hub.go` 165 Zeilen |
| 231 | high | function_hotspot | `web/src/App.vue` | applyRoute | 79-154 | 0.62 | `finding_60e36235d12805e95232` | applyRoute has been modified across 2 commits (repo p80=2) and carries CCN=30 / nesting=5 | `web/src/App.vue` 300 Zeilen |
| 232 | medium | function_hotspot | `web/src/components/AdminDashboard.vue` | toggleDisableUser | 183-209 | 0.36 | `finding_86cc9b12399ef04a99ed` | toggleDisableUser has been modified across 2 commits (repo p80=2) and carries CCN=6 / nesting=3 | `web/src/components/AdminDashboard.vue` 112 Zeilen |
| 233 | medium | function_hotspot | `web/src/components/ChatArea.vue` | openMessageContextMenu | 422-516 | 0.24 | `finding_1d2eb7c0f8767be576ef` | openMessageContextMenu has been modified across 2 commits (repo p80=2) and carries CCN=12 / nesting=2 | `web/src/components/ChatArea.vue` 346 Zeilen |
| 234 | high | function_hotspot | `web/src/components/ChatArea.vue` | handleMessageKeydown | 555-594 | 0.42 | `finding_0a35a022beb2060f3593` | handleMessageKeydown has been modified across 2 commits (repo p80=2) and carries CCN=26 / nesting=2 | `web/src/components/ChatArea.vue` 346 Zeilen |
| 235 | medium | function_hotspot | `web/src/components/ChatArea.vue` | onGlobalKeydown | 596-606 | 0.24 | `finding_0b8d0f714dbeba15e537` | onGlobalKeydown has been modified across 2 commits (repo p80=2) and carries CCN=14 / nesting=1 | `web/src/components/ChatArea.vue` 346 Zeilen |
| 236 | high | function_hotspot | `web/src/components/LegalModal.vue` | onMounted callback | 27-46 | 0.73 | `finding_986a195309ebb154551c` | onMounted callback has been modified across 4 commits (repo p80=2) and carries CCN=11 / nesting=3 | `web/src/components/LegalModal.vue` 285 Zeilen |
| 237 | medium | function_hotspot | `web/src/components/LoginModal.vue` | handleSubmit | 37-56 | 0.25 | `finding_4fb1d72f55d3168922e5` | handleSubmit has been modified across 2 commits (repo p80=2) and carries CCN=6 / nesting=3 | `web/src/components/LoginModal.vue` 219 Zeilen |
| 238 | medium | function_hotspot | `web/src/components/MemberList.vue` | getUserVoiceChannel | 33-46 | 0.58 | `finding_286466b1c9c29740f72e` | getUserVoiceChannel has been modified across 2 commits (repo p80=2) and carries CCN=7 / nesting=4 | `web/src/components/MemberList.vue` 103 Zeilen |
| 239 | high | function_hotspot | `web/src/composables/useWebRTC.js` | cleanupPeerConnection | 172-198 | 0.29 | `finding_e789082fb4226b8c3d79` | cleanupPeerConnection has been modified across 5 commits (repo p80=2) and carries CCN=5 / nesting=3 | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 240 | medium | function_hotspot | `web/src/composables/useWebRTC.js` | teardownScreenAudioMix | 210-222 | 0.17 | `finding_6003752730232ae4121c` | teardownScreenAudioMix has been modified across 3 commits (repo p80=2) and carries CCN=7 / nesting=3 | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 241 | medium | function_hotspot | `web/src/composables/useWebRTC.js` | buildScreenAudioMix | 226-262 | 0.17 | `finding_2f70068509301b38b4ba` | buildScreenAudioMix has been modified across 2 commits (repo p80=2) and carries CCN=12 / nesting=3 | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 242 | high | function_hotspot | `web/src/composables/useWebRTC.js` | setupPeerConnection | 265-347 | 0.29 | `finding_93e83f79a90d7ca44135` | setupPeerConnection has been modified across 6 commits (repo p80=2) and carries CCN=19 / nesting=3 | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 243 | medium | function_hotspot | `web/src/composables/useWebRTC.js` | handlePttKeyDown | 389-402 | 0.17 | `finding_a75b4c49a73207dee4eb` | handlePttKeyDown has been modified across 2 commits (repo p80=2) and carries CCN=10 / nesting=3 | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 244 | medium | function_hotspot | `web/src/composables/useWebRTC.js` | handlePttKeyUp | 404-415 | 0.17 | `finding_4bda9880255a2446baa6` | handlePttKeyUp has been modified across 2 commits (repo p80=2) and carries CCN=8 / nesting=3 | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 245 | medium | function_hotspot | `web/src/composables/useWebRTC.js` | onVisibilityChange | 423-436 | 0.17 | `finding_79c0b72c53cac73e5404` | onVisibilityChange has been modified across 2 commits (repo p80=2) and carries CCN=8 / nesting=3 | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 246 | high | function_hotspot | `web/src/stores/chat.js` | defineStore callback | 33-1259 | 0.53 | `finding_4a1bd0182373ce50ca94` | defineStore callback has been modified across 16 commits (repo p80=2) and carries CCN=11 / nesting=3 | `web/src/stores/chat.ts (was .js)` 137 Zeilen |
| 247 | high | function_hotspot | `web/src/stores/voice.js` | defineStore callback | 64-625 | 1.01 | `finding_fe375783fe17f14073f6` | defineStore callback has been modified across 12 commits (repo p80=2) and carries CCN=15 / nesting=2 | `web/src/stores/voice.ts (was .js)` 134 Zeilen |
| 248 | medium | goroutine_in_unbounded_loop | `internal/sfu/sfu.go` | SignalPeerConnections | 463-463 | 0.00 | `finding_7113347640358b45cb30` | a goroutine is spawned per loop element with no concurrency bound; use a worker pool or a bounded errgroup/semaphore | `internal/sfu/sfu.go` 185 Zeilen |
| 249 | medium | hidden_coupling | `cmd/server/main.go` |  |  | 0.00 | `finding_4c9b21fa370f7bd4bae2` | internal/db/migrations/0001_init.sql changed with this file in 6 of its 7 commits (86%) but no static dependency exists | `cmd/server/main.go` 179 Zeilen |
| 250 | medium | hidden_coupling | `cmd/server/main.go` |  |  | 0.00 | `finding_80ddcb0306ef55fd0e1d` | web/src/stores/auth.js changed with this file in 5 of its 7 commits (71%) but no static dependency exists | `cmd/server/main.go` 179 Zeilen |
| 251 | medium | hidden_coupling | `cmd/server/main.go` |  |  | 0.00 | `finding_e0739e53da848c09041c` | web/src/components/ChatArea.vue changed with this file in 8 of this file's 14 commits (57%) but no static dependency exists | `cmd/server/main.go` 179 Zeilen |
| 252 | medium | hidden_coupling | `internal/chat/channels.go` |  |  | 0.00 | `finding_8fdc1da0f76b978c571c` | internal/chat/messages.go changed with this file in 5 of this file's 8 commits (62%) but no static dependency exists | `internal/chat/channels.go` 303 Zeilen |
| 253 | medium | hidden_coupling | `internal/chat/channels.go` |  |  | 0.00 | `finding_af3ffecc3932f499fc66` | web/src/components/ChatArea.vue changed with this file in 4 of this file's 8 commits (50%) but no static dependency exists | `internal/chat/channels.go` 303 Zeilen |
| 254 | medium | hidden_coupling | `internal/chat/channels.go` |  |  | 0.00 | `finding_86e486ef49fcc36f3b57` | web/src/stores/chat.js changed with this file in 4 of this file's 8 commits (50%) but no static dependency exists | `internal/chat/channels.go` 303 Zeilen |
| 255 | medium | hidden_coupling | `internal/chat/members.go` |  |  | 0.00 | `finding_ff007eb645703c3fc062` | web/src/components/VoiceStage.vue changed with this file in 4 of this file's 5 commits (80%) but no static dependency exists | `internal/chat/members.go` 56 Zeilen |
| 256 | medium | hidden_coupling | `internal/chat/members.go` |  |  | 0.00 | `finding_2486101345b6ea98e338` | web/src/stores/chat.js changed with this file in 4 of this file's 5 commits (80%) but no static dependency exists | `internal/chat/members.go` 56 Zeilen |
| 257 | medium | hidden_coupling | `internal/chat/members.go` |  |  | 0.00 | `finding_d993f860925d541c7684` | web/src/stores/voice.js changed with this file in 4 of this file's 5 commits (80%) but no static dependency exists | `internal/chat/members.go` 56 Zeilen |
| 258 | medium | hidden_coupling | `internal/chat/messages.go` |  |  | 0.00 | `finding_c714b5eebc781ded56df` | internal/chat/channels.go changed with this file in 5 of its 8 commits (62%) but no static dependency exists | `internal/chat/messages.go` 341 Zeilen |
| 259 | medium | hidden_coupling | `internal/chat/messages.go` |  |  | 0.00 | `finding_a9ad0a05765fa5caf7f7` | internal/chat/members.go changed with this file in 3 of its 5 commits (60%) but no static dependency exists | `internal/chat/messages.go` 341 Zeilen |
| 260 | medium | hidden_coupling | `internal/chat/messages.go` |  |  | 0.00 | `finding_c65953e87f1d9d5369c1` | web/src/stores/chat.js changed with this file in 5 of this file's 10 commits (50%) but no static dependency exists | `internal/chat/messages.go` 341 Zeilen |
| 261 | medium | hidden_coupling | `internal/config/config.go` |  |  | 0.00 | `finding_c7cfbf0c4636f718ac48` | web/src/components/LegalModal.vue changed with this file in 3 of its 6 commits (50%) but no static dependency exists | `internal/config/config.go` 110 Zeilen |
| 262 | medium | hidden_coupling | `internal/db/db.go` |  |  | 0.00 | `finding_7c480d345a9f5295cf94` | internal/db/migrations/0001_init.sql changed with this file in 6 of its 7 commits (86%) but no static dependency exists | `internal/db/db.go` 297 Zeilen |
| 263 | medium | hidden_coupling | `internal/db/db.go` |  |  | 0.00 | `finding_8c0510057fcd80a1e266` | web/src/components/ChatArea.vue changed with this file in 5 of this file's 8 commits (62%) but no static dependency exists | `internal/db/db.go` 297 Zeilen |
| 264 | medium | hidden_coupling | `internal/db/db.go` |  |  | 0.00 | `finding_3a201fe164f04eb60cbf` | web/src/stores/chat.js changed with this file in 5 of this file's 8 commits (62%) but no static dependency exists | `internal/db/db.go` 297 Zeilen |
| 265 | medium | hidden_coupling | `internal/media/handler.go` |  |  | 0.00 | `finding_68d75df081bbe3a6c015` | internal/ws/hub.go changed with this file in 3 of this file's 5 commits (60%) but no static dependency exists | `internal/media/handler.go` 146 Zeilen |
| 266 | medium | hidden_coupling | `internal/ws/hub.go` |  |  | 0.00 | `finding_bb173f1169b4590572d7` | cmd/server/main.go changed with this file in 7 of its 14 commits (50%) but no static dependency exists | `internal/ws/hub.go` 165 Zeilen |
| 267 | medium | hidden_coupling | `web/src/App.vue` |  |  | 0.00 | `finding_4d67eda27432a24ab6d8` | web/src/components/MarkdownContent.vue changed with this file in 5 of its 6 commits (83%) but no static dependency exists | `web/src/App.vue` 300 Zeilen |
| 268 | medium | hidden_coupling | `web/src/App.vue` |  |  | 0.00 | `finding_35d1249ae18dd9bbb867` | cmd/server/main.go changed with this file in 7 of its 14 commits (50%) but no static dependency exists | `web/src/App.vue` 300 Zeilen |
| 269 | medium | hidden_coupling | `web/src/components/AdminDashboard.vue` |  |  | 0.00 | `finding_17a936d4d7d4f5167531` | web/src/components/LoginModal.vue changed with this file in 6 of its 7 commits (86%) but no static dependency exists | `web/src/components/AdminDashboard.vue` 112 Zeilen |
| 270 | medium | hidden_coupling | `web/src/components/AdminDashboard.vue` |  |  | 0.00 | `finding_607d96e4a9fb22f824fd` | web/src/components/CreateChannelModal.vue changed with this file in 4 of its 5 commits (80%) but no static dependency exists | `web/src/components/AdminDashboard.vue` 112 Zeilen |
| 271 | medium | hidden_coupling | `web/src/components/AdminDashboard.vue` |  |  | 0.00 | `finding_8f2015eff422e4dcea6f` | web/src/components/ConnectionStatsModal.vue changed with this file in 4 of its 5 commits (80%) but no static dependency exists | `web/src/components/AdminDashboard.vue` 112 Zeilen |
| 272 | medium | hidden_coupling | `web/src/components/AudioSettingsModal.vue` |  |  | 0.00 | `finding_017f54702b93be7e220e` | web/src/components/ConnectionStatsModal.vue changed with this file in 4 of its 5 commits (80%) but no static dependency exists | `web/src/components/AudioSettingsModal.vue` 46 Zeilen |
| 273 | medium | hidden_coupling | `web/src/components/AudioSettingsModal.vue` |  |  | 0.00 | `finding_c98d28ce69676c82830c` | web/src/components/CreateChannelModal.vue changed with this file in 4 of its 5 commits (80%) but no static dependency exists | `web/src/components/AudioSettingsModal.vue` 46 Zeilen |
| 274 | medium | hidden_coupling | `web/src/components/AudioSettingsModal.vue` |  |  | 0.00 | `finding_69d440b868a2336d20d4` | web/src/components/VoiceStage.vue changed with this file in 7 of this file's 11 commits (64%) but no static dependency exists | `web/src/components/AudioSettingsModal.vue` 46 Zeilen |
| 275 | critical | hidden_coupling | `web/src/components/ChatArea.vue` |  |  | 0.00 | `finding_ca2615b7fa537ddf818d` | web/src/components/ThreadSidebar.vue changed with this file in 9 of its 9 commits (100%) but no static dependency exists | `web/src/components/ChatArea.vue` 346 Zeilen |
| 276 | medium | hidden_coupling | `web/src/components/ChatArea.vue` |  |  | 0.00 | `finding_0b37bea7bd043eac30b5` | web/src/components/CreateChannelModal.vue changed with this file in 5 of its 5 commits (100%) but no static dependency exists | `web/src/components/ChatArea.vue` 346 Zeilen |
| 277 | medium | hidden_coupling | `web/src/components/ChatArea.vue` |  |  | 0.00 | `finding_f5387d0c812bfd4aaa01` | web/src/components/ConnectionStatsModal.vue changed with this file in 5 of its 5 commits (100%) but no static dependency exists | `web/src/components/ChatArea.vue` 346 Zeilen |
| 278 | medium | hidden_coupling | `web/src/components/ConnectionStatsModal.vue` |  |  | 0.00 | `finding_ecf3cdb13e2b69cd0190` | web/src/components/UserBar.vue changed with this file in 5 of this file's 5 commits (100%) but no static dependency exists | `web/src/components/ConnectionStatsModal.vue` 188 Zeilen |
| 279 | medium | hidden_coupling | `web/src/components/ConnectionStatsModal.vue` |  |  | 0.00 | `finding_b1c7df13eb8998313645` | web/src/components/ChatArea.vue changed with this file in 5 of this file's 5 commits (100%) but no static dependency exists | `web/src/components/ConnectionStatsModal.vue` 188 Zeilen |
| 280 | medium | hidden_coupling | `web/src/components/ConnectionStatsModal.vue` |  |  | 0.00 | `finding_cf615f6222a08827cb6e` | web/src/components/VoiceStage.vue changed with this file in 5 of this file's 5 commits (100%) but no static dependency exists | `web/src/components/ConnectionStatsModal.vue` 188 Zeilen |
| 281 | medium | hidden_coupling | `web/src/components/CreateChannelModal.vue` |  |  | 0.00 | `finding_369bf68c783cdfd4b5c8` | web/src/components/ChatArea.vue changed with this file in 5 of this file's 5 commits (100%) but no static dependency exists | `web/src/components/CreateChannelModal.vue` 232 Zeilen |
| 282 | medium | hidden_coupling | `web/src/components/CreateChannelModal.vue` |  |  | 0.00 | `finding_59fe3c26f19991bb6e77` | web/src/components/LoginModal.vue changed with this file in 4 of this file's 5 commits (80%) but no static dependency exists | `web/src/components/CreateChannelModal.vue` 232 Zeilen |
| 283 | medium | hidden_coupling | `web/src/components/CreateChannelModal.vue` |  |  | 0.00 | `finding_f6909e35fac1743a2103` | web/src/components/UserBar.vue changed with this file in 4 of this file's 5 commits (80%) but no static dependency exists | `web/src/components/CreateChannelModal.vue` 232 Zeilen |
| 284 | medium | hidden_coupling | `web/src/components/LegalModal.vue` |  |  | 0.00 | `finding_7cd2cf2966e45d3cbf79` | web/src/components/Sidebar.vue changed with this file in 4 of this file's 6 commits (67%) but no static dependency exists | `web/src/components/LegalModal.vue` 285 Zeilen |
| 285 | medium | hidden_coupling | `web/src/components/LegalModal.vue` |  |  | 0.00 | `finding_2c6165ade493eb36db1c` | web/src/components/UserProfileModal.vue changed with this file in 4 of this file's 6 commits (67%) but no static dependency exists | `web/src/components/LegalModal.vue` 285 Zeilen |
| 286 | medium | hidden_coupling | `web/src/components/LegalModal.vue` |  |  | 0.00 | `finding_0a185f0e56aec82fbbb1` | web/src/components/AudioSettingsModal.vue changed with this file in 4 of this file's 6 commits (67%) but no static dependency exists | `web/src/components/LegalModal.vue` 285 Zeilen |
| 287 | medium | hidden_coupling | `web/src/components/LoginModal.vue` |  |  | 0.00 | `finding_dcb355cf78a376e183bc` | web/src/components/Sidebar.vue changed with this file in 7 of this file's 7 commits (100%) but no static dependency exists | `web/src/components/LoginModal.vue` 219 Zeilen |
| 288 | medium | hidden_coupling | `web/src/components/LoginModal.vue` |  |  | 0.00 | `finding_9f2ec0f4325c24989385` | web/src/components/UserBar.vue changed with this file in 6 of this file's 7 commits (86%) but no static dependency exists | `web/src/components/LoginModal.vue` 219 Zeilen |
| 289 | medium | hidden_coupling | `web/src/components/LoginModal.vue` |  |  | 0.00 | `finding_3670e5c9879ac5bbfd32` | web/src/components/ChatArea.vue changed with this file in 6 of this file's 7 commits (86%) but no static dependency exists | `web/src/components/LoginModal.vue` 219 Zeilen |
| 290 | medium | hidden_coupling | `web/src/components/MarkdownContent.vue` |  |  | 0.00 | `finding_a9c4b4b74467dd23d068` | web/src/App.vue changed with this file in 5 of this file's 6 commits (83%) but no static dependency exists | `web/src/components/MarkdownContent.vue` 143 Zeilen |
| 291 | medium | hidden_coupling | `web/src/components/MarkdownContent.vue` |  |  | 0.00 | `finding_0b6c99520c27c1f1ba0a` | web/src/components/Sidebar.vue changed with this file in 4 of this file's 6 commits (67%) but no static dependency exists | `web/src/components/MarkdownContent.vue` 143 Zeilen |
| 292 | medium | hidden_coupling | `web/src/components/MarkdownContent.vue` |  |  | 0.00 | `finding_aaf5a2c48309001f2e35` | web/src/components/UserBar.vue changed with this file in 4 of this file's 6 commits (67%) but no static dependency exists | `web/src/components/MarkdownContent.vue` 143 Zeilen |
| 293 | medium | hidden_coupling | `web/src/components/MemberList.vue` |  |  | 0.00 | `finding_cd1537256ed7cb6e5cc4` | web/src/components/ConnectionStatsModal.vue changed with this file in 4 of its 5 commits (80%) but no static dependency exists | `web/src/components/MemberList.vue` 103 Zeilen |
| 294 | medium | hidden_coupling | `web/src/components/MemberList.vue` |  |  | 0.00 | `finding_5976608decbef5492804` | web/src/components/VoiceStage.vue changed with this file in 6 of this file's 8 commits (75%) but no static dependency exists | `web/src/components/MemberList.vue` 103 Zeilen |
| 295 | medium | hidden_coupling | `web/src/components/MemberList.vue` |  |  | 0.00 | `finding_8c6a6f47534938ced0f6` | web/src/components/ChatArea.vue changed with this file in 6 of this file's 8 commits (75%) but no static dependency exists | `web/src/components/MemberList.vue` 103 Zeilen |
| 296 | medium | hidden_coupling | `web/src/components/ParticipantTile.vue` |  |  | 0.00 | `finding_51df33803eff5b4e50c3` | web/src/stores/voice.js changed with this file in 3 of this file's 5 commits (60%) but no static dependency exists | `web/src/components/ParticipantTile.vue` 305 Zeilen |
| 297 | medium | hidden_coupling | `web/src/components/ParticipantTile.vue` |  |  | 0.00 | `finding_76b873b5ebe617db5915` | web/src/stores/chat.js changed with this file in 3 of this file's 5 commits (60%) but no static dependency exists | `web/src/components/ParticipantTile.vue` 305 Zeilen |
| 298 | medium | hidden_coupling | `web/src/components/ParticipantTile.vue` |  |  | 0.00 | `finding_43a8aeacbebfeb35f98d` | web/src/components/Sidebar.vue changed with this file in 3 of this file's 5 commits (60%) but no static dependency exists | `web/src/components/ParticipantTile.vue` 305 Zeilen |
| 299 | medium | hidden_coupling | `web/src/components/Sidebar.vue` |  |  | 0.00 | `finding_70388cee4b67860755f1` | web/src/components/LoginModal.vue changed with this file in 7 of its 7 commits (100%) but no static dependency exists | `web/src/components/Sidebar.vue` 362 Zeilen |
| 300 | high | hidden_coupling | `web/src/components/Sidebar.vue` |  |  | 0.00 | `finding_ddb02ad7f31d634a9132` | web/src/components/ChatArea.vue changed with this file in 11 of this file's 15 commits (73%) but no static dependency exists | `web/src/components/Sidebar.vue` 362 Zeilen |
| 301 | medium | hidden_coupling | `web/src/components/Sidebar.vue` |  |  | 0.00 | `finding_7f0399f74f2cccd9b905` | web/src/components/AdminDashboard.vue changed with this file in 7 of its 10 commits (70%) but no static dependency exists | `web/src/components/Sidebar.vue` 362 Zeilen |
| 302 | critical | hidden_coupling | `web/src/components/ThreadSidebar.vue` |  |  | 0.00 | `finding_9d81005cd51fa0e78a0d` | web/src/components/ChatArea.vue changed with this file in 9 of this file's 9 commits (100%) but no static dependency exists | `web/src/components/ThreadSidebar.vue` 321 Zeilen |
| 303 | critical | hidden_coupling | `web/src/components/ThreadSidebar.vue` |  |  | 0.00 | `finding_a3cbb9e8208decfb32fd` | web/src/components/VoiceStage.vue changed with this file in 8 of this file's 9 commits (89%) but no static dependency exists | `web/src/components/ThreadSidebar.vue` 321 Zeilen |
| 304 | medium | hidden_coupling | `web/src/components/ThreadSidebar.vue` |  |  | 0.00 | `finding_6bb3e3013478007cd10a` | web/src/components/ConnectionStatsModal.vue changed with this file in 4 of its 5 commits (80%) but no static dependency exists | `web/src/components/ThreadSidebar.vue` 321 Zeilen |
| 305 | medium | hidden_coupling | `web/src/components/UserBar.vue` |  |  | 0.00 | `finding_8a589eec1d0ac59be24f` | web/src/components/ConnectionStatsModal.vue changed with this file in 5 of its 5 commits (100%) but no static dependency exists | `web/src/components/UserBar.vue` 152 Zeilen |
| 306 | critical | hidden_coupling | `web/src/components/UserBar.vue` |  |  | 0.00 | `finding_12b39848d112dbfc2ad8` | web/src/components/ChatArea.vue changed with this file in 9 of this file's 10 commits (90%) but no static dependency exists | `web/src/components/UserBar.vue` 152 Zeilen |
| 307 | medium | hidden_coupling | `web/src/components/UserBar.vue` |  |  | 0.00 | `finding_81adf16739838eab9fc0` | web/src/components/LoginModal.vue changed with this file in 6 of its 7 commits (86%) but no static dependency exists | `web/src/components/UserBar.vue` 152 Zeilen |
| 308 | critical | hidden_coupling | `web/src/components/UserProfileModal.vue` |  |  | 0.00 | `finding_7e0c688f43dbb3b10621` | web/src/components/ChatArea.vue changed with this file in 8 of this file's 10 commits (80%) but no static dependency exists | `web/src/components/UserProfileModal.vue` 286 Zeilen |
| 309 | critical | hidden_coupling | `web/src/components/UserProfileModal.vue` |  |  | 0.00 | `finding_9f2410231bc91b8f1580` | web/src/components/VoiceStage.vue changed with this file in 8 of this file's 10 commits (80%) but no static dependency exists | `web/src/components/UserProfileModal.vue` 286 Zeilen |
| 310 | medium | hidden_coupling | `web/src/components/UserProfileModal.vue` |  |  | 0.00 | `finding_d8e73440a8a4ef2c3daa` | web/src/components/ConnectionStatsModal.vue changed with this file in 4 of its 5 commits (80%) but no static dependency exists | `web/src/components/UserProfileModal.vue` 286 Zeilen |
| 311 | medium | hidden_coupling | `web/src/components/VoiceStage.vue` |  |  | 0.00 | `finding_80ee783d14374732396f` | web/src/components/ConnectionStatsModal.vue changed with this file in 5 of its 5 commits (100%) but no static dependency exists | `web/src/components/VoiceStage.vue` 389 Zeilen |
| 312 | critical | hidden_coupling | `web/src/components/VoiceStage.vue` |  |  | 0.00 | `finding_7f63031ae770acfd2e6b` | web/src/components/ThreadSidebar.vue changed with this file in 8 of its 9 commits (89%) but no static dependency exists | `web/src/components/VoiceStage.vue` 389 Zeilen |
| 313 | critical | hidden_coupling | `web/src/components/VoiceStage.vue` |  |  | 0.00 | `finding_0f2f75b21e2082eb1b8f` | web/src/components/UserBar.vue changed with this file in 8 of its 10 commits (80%) but no static dependency exists | `web/src/components/VoiceStage.vue` 389 Zeilen |
| 314 | medium | hidden_coupling | `web/src/composables/useWebRTC.js` |  |  | 0.00 | `finding_00633a3410c27271207e` | web/src/stores/auth.js changed with this file in 5 of its 7 commits (71%) but no static dependency exists | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 315 | medium | hidden_coupling | `web/src/composables/useWebRTC.js` |  |  | 0.00 | `finding_b4d20e71664e6f4c67dc` | web/vite.config.js changed with this file in 4 of its 7 commits (57%) but no static dependency exists | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 316 | medium | hidden_coupling | `web/src/composables/useWebRTC.js` |  |  | 0.00 | `finding_55c86b9517c97d245cfd` | web/src/components/LegalModal.vue changed with this file in 3 of its 6 commits (50%) but no static dependency exists | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 317 | medium | hidden_coupling | `web/src/stores/auth.js` |  |  | 0.00 | `finding_26d77563ef3f43096171` | web/src/composables/useWebRTC.js changed with this file in 5 of this file's 7 commits (71%) but no static dependency exists | `web/src/stores/auth.ts (was .js)` 61 Zeilen |
| 318 | medium | hidden_coupling | `web/src/stores/auth.js` |  |  | 0.00 | `finding_7d3ec19e77c6e44a4909` | cmd/server/main.go changed with this file in 5 of this file's 7 commits (71%) but no static dependency exists | `web/src/stores/auth.ts (was .js)` 61 Zeilen |
| 319 | medium | hidden_coupling | `web/src/stores/auth.js` |  |  | 0.00 | `finding_71e4396b170b75bfb788` | web/vite.config.js changed with this file in 4 of this file's 7 commits (57%) but no static dependency exists | `web/src/stores/auth.ts (was .js)` 61 Zeilen |
| 320 | medium | hidden_coupling | `web/src/stores/chat.js` |  |  | 0.00 | `finding_a7b9f9872f1cc14b1b32` | internal/chat/members.go changed with this file in 4 of its 5 commits (80%) but no static dependency exists | `web/src/stores/chat.ts (was .js)` 137 Zeilen |
| 321 | medium | hidden_coupling | `web/src/stores/chat.js` |  |  | 0.00 | `finding_e92281bb187381e16a33` | internal/db/migrations/0001_init.sql changed with this file in 5 of its 7 commits (71%) but no static dependency exists | `web/src/stores/chat.ts (was .js)` 137 Zeilen |
| 322 | medium | hidden_coupling | `web/src/stores/chat.js` |  |  | 0.00 | `finding_6d1399bbc65fce2368bf` | internal/db/db.go changed with this file in 5 of its 8 commits (62%) but no static dependency exists | `web/src/stores/chat.ts (was .js)` 137 Zeilen |
| 323 | medium | hidden_coupling | `web/src/stores/voice.js` |  |  | 0.00 | `finding_9749b5c4d823dcc33fa5` | internal/chat/members.go changed with this file in 4 of its 5 commits (80%) but no static dependency exists | `web/src/stores/voice.ts (was .js)` 134 Zeilen |
| 324 | medium | hidden_coupling | `web/src/stores/voice.js` |  |  | 0.00 | `finding_a02df59f66a77054ea2f` | web/src/components/ParticipantTile.vue changed with this file in 3 of its 5 commits (60%) but no static dependency exists | `web/src/stores/voice.ts (was .js)` 134 Zeilen |
| 325 | medium | hidden_coupling | `web/src/stores/voice.js` |  |  | 0.00 | `finding_8677945e1ba7a11885e2` | web/vite.config.js changed with this file in 4 of its 7 commits (57%) but no static dependency exists | `web/src/stores/voice.ts (was .js)` 134 Zeilen |
| 326 | medium | hidden_coupling | `web/vite.config.js` |  |  | 0.00 | `finding_9a7199a2718f20110a2c` | web/src/components/AudioSettingsModal.vue changed with this file in 4 of this file's 7 commits (57%) but no static dependency exists | `web/vite.config.ts (was .js)` 113 Zeilen |
| 327 | medium | hidden_coupling | `web/vite.config.js` |  |  | 0.00 | `finding_82a9fd7313e08cb518fb` | web/src/composables/useWebRTC.js changed with this file in 4 of this file's 7 commits (57%) but no static dependency exists | `web/vite.config.ts (was .js)` 113 Zeilen |
| 328 | medium | hidden_coupling | `web/vite.config.js` |  |  | 0.00 | `finding_af34ac337cb05acd7bf8` | web/src/stores/voice.js changed with this file in 4 of this file's 7 commits (57%) but no static dependency exists | `web/vite.config.ts (was .js)` 113 Zeilen |
| 329 | medium | io_in_loop | `internal/chat/channels.go` | ApplyLayout | 243-243 | 0.00 | `finding_3da43c33566d226a32ce` | a database call runs once per loop iteration (N+1 query) | `internal/chat/channels.go` 303 Zeilen |
| 330 | medium | io_in_loop | `internal/chat/channels.go` | ApplyLayout | 252-252 | 0.00 | `finding_ac52e2a269df5bdd07f8` | a database call runs once per loop iteration (N+1 query) | `internal/chat/channels.go` 303 Zeilen |
| 331 | medium | io_in_loop | `internal/chat/handler.go` | listMessages | 148-148 | 0.00 | `finding_aa98e0a2d0326ddceb0e` | a database call runs once per loop iteration (N+1 query) | `internal/chat/handler.go` 177 Zeilen |
| 332 | medium | io_in_loop | `internal/chat/handler.go` | listMessages | 159-159 | 0.00 | `finding_70d05c039e33cc59b80b` | a database call is reached once per loop iteration through listMessages -> loadMessageRef (cross-function N+1 query) | `internal/chat/handler.go` 177 Zeilen |
| 333 | medium | io_in_loop | `internal/chat/handler.go` | getThread | 432-432 | 0.00 | `finding_8a37ecc027dcccd32470` | a database call runs once per loop iteration (N+1 query) | `internal/chat/handler.go` 177 Zeilen |
| 334 | medium | io_in_loop | `internal/chat/handler.go` | getThread | 442-442 | 0.00 | `finding_1c919a60acf0794e6e3a` | a database call is reached once per loop iteration through getThread -> loadMessageRef (cross-function N+1 query) | `internal/chat/handler.go` 177 Zeilen |
| 335 | medium | io_in_loop | `internal/media/admin.go` | StartRetentionWorker | 160-160 | 0.00 | `finding_43273aafddc9d2087079` | a database call is reached once per loop iteration through StartRetentionWorker -> PruneOlderThan (cross-function N+1 query) | `internal/media/admin.go` 207 Zeilen |
| 336 | medium | io_in_loop | `internal/ws/hub.go` | KickFromVoice | 632-632 | 0.00 | `finding_6da00823df76758919f2` | a database call is reached once per loop iteration through KickFromVoice -> leaveVoice -> removePresence -> addVoiceTime (cross-function N+1 query) | `internal/ws/hub.go` 165 Zeilen |
| 337 | medium | io_in_loop | `internal/ws/hub.go` | readPump | 790-790 | 0.00 | `finding_927673474580d9902037` | a database call is reached once per loop iteration through readPump -> handle -> LoadChannel (cross-function N+1 query) | `internal/ws/hub.go` 165 Zeilen |
| 338 | medium | io_in_loop | `web/vite.config.js` | closeBundle | 23-23 | 0.00 | `finding_288a3b2910d3098e54aa` | a filesystem call runs once per loop iteration (I/O call inside a loop) | `web/vite.config.ts (was .js)` 113 Zeilen |
| 339 | medium | io_in_loop | `web/vite.config.js` | closeBundle | 25-25 | 0.00 | `finding_1469f233c3d85132358d` | a filesystem call runs once per loop iteration (I/O call inside a loop) | `web/vite.config.ts (was .js)` 113 Zeilen |
| 340 | medium | io_in_loop | `web/vite.config.js` | closeBundle | 26-26 | 0.00 | `finding_03351b89856110d4fc2c` | a filesystem call runs once per loop iteration (I/O call inside a loop) | `web/vite.config.ts (was .js)` 113 Zeilen |
| 341 | high | knowledge_loss | `internal/auth/service.go` |  |  | 0.45 | `finding_aee1c8a1ec6da0ba2977` | Primary owner Marius no longer the recent owner | `internal/auth/service.go` 265 Zeilen |
| 342 | high | knowledge_loss | `internal/httpx/middleware.go` |  |  | 0.18 | `finding_41f67d5a869dd6300af6` | Primary owner Marius no longer the recent owner | `internal/httpx/middleware.go` 201 Zeilen |
| 343 | high | knowledge_loss | `internal/media/handler.go` |  |  | 0.44 | `finding_dc170fe11c97dc9b2fae` | Primary owner Marius no longer the recent owner | `internal/media/handler.go` 146 Zeilen |
| 344 | high | knowledge_loss | `internal/server/server.go` |  |  | 0.16 | `finding_9907a4d242c6f7c861db` | Primary owner Marius no longer the recent owner | `internal/server/server.go` 144 Zeilen |
| 345 | high | knowledge_loss | `web/src/components/ConnectionStatsModal.vue` |  |  | 0.17 | `finding_96f39836a38bed1e2e81` | Primary owner Marius Schröder no longer the recent owner | `web/src/components/ConnectionStatsModal.vue` 188 Zeilen |
| 346 | high | knowledge_loss | `web/src/components/CreateChannelModal.vue` |  |  | 0.17 | `finding_0efc7d587c096401e48b` | Primary owner Marius Schröder no longer the recent owner | `web/src/components/CreateChannelModal.vue` 232 Zeilen |
| 347 | high | knowledge_loss | `web/src/components/UserBar.vue` |  |  | 0.12 | `finding_4fb26150348bcfcd49e3` | Primary owner Marius Schröder no longer the recent owner | `web/src/components/UserBar.vue` 152 Zeilen |
| 348 | low | large_method | `internal/auth/service.go` | Register | 210-293 | 0.31 | `finding_30ca898a6eb9826658c8` | Register is 76 lines long | `internal/auth/service.go` 265 Zeilen |
| 349 | low | large_method | `internal/chat/messages.go` | enrich | 153-219 | 0.17 | `finding_87b7003f282300629a77` | enrich is 64 lines long | `internal/chat/messages.go` 341 Zeilen |
| 350 | low | large_method | `internal/chat/search.go` | Search | 38-103 | 0.31 | `finding_4a2c7eda9fb90c8f46ab` | Search is 63 lines long | `internal/chat/search.go` 197 Zeilen |
| 351 | low | large_method | `internal/config/config.go` | FromEnv | 84-168 | 0.15 | `finding_777797fe89fe9b3b4c94` | FromEnv is 81 lines long | `internal/config/config.go` 110 Zeilen |
| 352 | low | large_method | `internal/db/db.go` | Migrate | 67-132 | 0.27 | `finding_356cc8fdcb5a7f4c1f5b` | Migrate is 62 lines long | `internal/db/db.go` 297 Zeilen |
| 353 | low | large_method | `internal/media/handler.go` | upload | 155-243 | 0.15 | `finding_232d71864b62fa4bb80a` | upload is 83 lines long | `internal/media/handler.go` 146 Zeilen |
| 354 | medium | large_method | `internal/server/server.go` | NewRouter | 48-181 | 0.76 | `finding_b5663173eb86d8c60e48` | NewRouter is 114 lines long | `internal/server/server.go` 144 Zeilen |
| 355 | medium | large_method | `internal/sfu/sfu.go` | JoinPeer | 282-410 | 0.35 | `finding_db4e8a65593ae25ccb91` | JoinPeer is 105 lines long | `internal/sfu/sfu.go` 185 Zeilen |
| 356 | low | large_method | `internal/ws/hub.go` | handle | 794-889 | 0.08 | `finding_fb891c52dee25757e553` | handle is 82 lines long | `internal/ws/hub.go` 165 Zeilen |
| 357 | low | large_method | `web/src/App.vue` | applyRoute | 79-154 | 0.20 | `finding_58b76af2a569c31a6e31` | applyRoute is 69 lines long | `web/src/App.vue` 300 Zeilen |
| 358 | low | large_method | `web/src/components/ChatArea.vue` | openMessageContextMenu | 422-516 | 0.09 | `finding_f9319c69cb09612a0359` | openMessageContextMenu is 85 lines long | `web/src/components/ChatArea.vue` 346 Zeilen |
| 359 | low | large_method | `web/src/composables/useNavMenus.js` | buildMemberItems | 186-262 | 0.27 | `finding_7643370e5433cf082556` | buildMemberItems is 73 lines long | `web/src/composables/useNavMenus.ts (was .js)` 367 Zeilen |
| 360 | high | large_method | `web/src/composables/useResizable.js` | useResizable | 105-238 | 1.36 | `finding_c4686d15621ffb8a970b` | useResizable is 120 lines long | `web/src/composables/useResizable.ts (was .js)` 322 Zeilen |
| 361 | low | large_method | `web/src/composables/useWebRTC.js` | setupPeerConnection | 265-347 | 0.08 | `finding_96fe496c12d93da3eb4c` | setupPeerConnection is 69 lines long | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 362 | critical | large_method | `web/src/composables/useWebRTC.js` | useWebRTC | 496-1351 | 0.55 | `finding_ec0ee028ddd4e82e342d` | useWebRTC is 728 lines long | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 363 | low | large_method | `web/src/lib/rtcStats.js` | summarizeStats | 10-91 | 0.20 | `finding_84334f875bc1c33fead6` | summarizeStats is 74 lines long | `web/src/lib/rtcStats.ts (was .js)` 125 Zeilen |
| 364 | low | large_method | `web/src/lib/soundEffects.js` | playSound | 69-183 | 0.15 | `finding_f6b490968e40cd2601d8` | playSound is 88 lines long | `web/src/lib/soundEffects.ts (was .js)` 174 Zeilen |
| 365 | critical | large_method | `web/src/stores/chat.js` | defineStore callback | 33-1259 | 1.12 | `finding_4c0a7e7d3745012190ed` | defineStore callback is 1026 lines long | `web/src/stores/chat.ts (was .js)` 137 Zeilen |
| 366 | critical | large_method | `web/src/stores/voice.js` | defineStore callback | 64-625 | 0.95 | `finding_429517bc994363d75ddc` | defineStore callback is 470 lines long | `web/src/stores/voice.ts (was .js)` 134 Zeilen |
| 367 | critical | large_method | `web/src/third_party/deepfilternet3/worklet.js` | <anonymous> | 8-379 | 1.50 | `finding_a2cf4dade4a271697dc2` | <anonymous> is 292 lines long | `web/src/third_party/deepfilternet3/worklet.js` 379 Zeilen |
| 368 | critical | nested_complexity | `internal/linkpreview/linkpreview.go` | parseHTML | 281-333 | 1.67 | `finding_5f9e53c3f86b660c44ee` | parseHTML nests 8 levels deep | `internal/linkpreview/linkpreview.go` 291 Zeilen |
| 369 | medium | nested_complexity | `internal/sfu/sfu.go` | SignalPeerConnections | 423-485 | 0.94 | `finding_0c408a0d30a6430f2143` | SignalPeerConnections nests 4 levels deep | `internal/sfu/sfu.go` 185 Zeilen |
| 370 | medium | nested_complexity | `internal/tools/openapifix/main.go` | run | 39-74 | 0.83 | `finding_eefb3ebdd5190aa7e4b1` | run nests 4 levels deep | `internal/tools/openapifix/main.go` 206 Zeilen |
| 371 | medium | nested_complexity | `internal/tools/openapifix/main.go` | walk | 77-102 | 0.83 | `finding_4fd5f3f36a9f82d29bae` | walk nests 4 levels deep | `internal/tools/openapifix/main.go` 206 Zeilen |
| 372 | medium | nested_complexity | `internal/tools/openapifix/main.go` | fixOperation | 104-141 | 0.83 | `finding_2198b55f723d336935f3` | fixOperation nests 4 levels deep | `internal/tools/openapifix/main.go` 206 Zeilen |
| 373 | medium | nested_complexity | `internal/ws/hub.go` | joinVoice | 553-614 | 0.92 | `finding_2af847c746a5d56f7151` | joinVoice nests 4 levels deep | `internal/ws/hub.go` 165 Zeilen |
| 374 | high | nested_complexity | `internal/ws/hub.go` | handle | 794-889 | 1.58 | `finding_fcb5bce5c5027e96ee58` | handle nests 5 levels deep | `internal/ws/hub.go` 165 Zeilen |
| 375 | high | nested_complexity | `web/src/App.vue` | applyRoute | 79-154 | 1.61 | `finding_498476ea95ca2ddca766` | applyRoute nests 5 levels deep | `web/src/App.vue` 300 Zeilen |
| 376 | medium | nested_complexity | `web/src/components/MemberList.vue` | getUserVoiceChannel | 33-46 | 0.94 | `finding_854150f7b0048036634f` | getUserVoiceChannel nests 4 levels deep | `web/src/components/MemberList.vue` 103 Zeilen |
| 377 | medium | nested_complexity | `web/src/lib/soundEffects.js` | playSound | 69-183 | 0.94 | `finding_cb31d1aa2dee99752895` | playSound nests 4 levels deep | `web/src/lib/soundEffects.ts (was .js)` 174 Zeilen |
| 378 | low | primitive_obsession | `internal/auth/invites.go` | CreateInvite | 67-101 | 0.15 | `finding_529e17de9c09d267c0fa` | long parameter list: CreateInvite takes 6 parameters | `internal/auth/invites.go` 219 Zeilen |
| 379 | medium | primitive_obsession | `internal/chat/channels.go` | CreateChannel | 107-138 | 0.26 | `finding_417cb70e721dbd7f952e` | long parameter list: CreateChannel takes 7 parameters | `internal/chat/channels.go` 303 Zeilen |
| 380 | low | primitive_obsession | `internal/chat/mentions.go` | ResolveMentions | 49-94 | 0.15 | `finding_882b47b9dbe58a494ff8` | long parameter list: ResolveMentions takes 5 parameters | `internal/chat/mentions.go` 179 Zeilen |
| 381 | low | primitive_obsession | `internal/chat/mentions.go` | RecordMentions | 115-125 | 0.15 | `finding_48a2922520345c193577` | long parameter list: RecordMentions takes 5 parameters | `internal/chat/mentions.go` 179 Zeilen |
| 382 | low | primitive_obsession | `internal/chat/messages.go` | olderThan | 278-292 | 0.07 | `finding_d87bbee902b448200dfc` | long parameter list: olderThan takes 5 parameters | `internal/chat/messages.go` 341 Zeilen |
| 383 | low | primitive_obsession | `internal/chat/messages.go` | collectKeys | 433-444 | 0.07 | `finding_7b3ac5e97e97a5dd39bf` | long parameter list: collectKeys takes 5 parameters | `internal/chat/messages.go` 341 Zeilen |
| 384 | low | primitive_obsession | `internal/media/handler.go` | readFile | 80-122 | 0.06 | `finding_77a50dcde3f387ab37b5` | long parameter list: readFile takes 5 parameters | `internal/media/handler.go` 146 Zeilen |
| 385 | low | primitive_obsession | `internal/media/store.go` | Upload | 38-47 | 0.15 | `finding_8924d45373dd774fe557` | long parameter list: Upload takes 5 parameters | `internal/media/store.go` 127 Zeilen |
| 386 | low | primitive_obsession | `internal/s3/s3.go` | Upload | 61-73 | 0.15 | `finding_4ccceba9d93988c8ad35` | long parameter list: Upload takes 5 parameters | `internal/s3/s3.go` 198 Zeilen |
| 387 | low | primitive_obsession | `web/src/composables/useResizable.js` | availableMax | 76-79 | 0.14 | `finding_de8112f5a350b427db13` | long parameter list: availableMax takes 5 parameters | `web/src/composables/useResizable.ts (was .js)` 322 Zeilen |
| 388 | critical | prior_defect | `cmd/server/main.go` |  |  | 0.37 | `finding_cdc5ba97a59e3a8f1171` | 4 bug-fixes touched this file in the last ~6 months (top 3.9% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | `cmd/server/main.go` 179 Zeilen |
| 389 | medium | prior_defect | `internal/chat/channels.go` |  |  | 0.70 | `finding_a4fea12ac6ebba789695` | 2 bug-fixes touched this file in the last ~6 months (top 12% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | `internal/chat/channels.go` 303 Zeilen |
| 390 | critical | prior_defect | `internal/chat/messages.go` |  |  | 0.71 | `finding_068fb7af3c8a2cce492b` | 4 bug-fixes touched this file in the last ~6 months (top 3.9% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | `internal/chat/messages.go` 341 Zeilen |
| 391 | critical | prior_defect | `internal/config/config.go` |  |  | 0.68 | `finding_0e623ad24dc36c30e747` | 5 bug-fixes touched this file in the last ~6 months (top 1.5% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | `internal/config/config.go` 110 Zeilen |
| 392 | critical | prior_defect | `internal/db/db.go` |  |  | 0.81 | `finding_15ea9f7bf6363dd33ede` | 3 bug-fixes touched this file in the last ~6 months (top 6.4% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | `internal/db/db.go` 297 Zeilen |
| 393 | medium | prior_defect | `internal/httpx/middleware.go` |  |  | 0.27 | `finding_5981fe2fc4b25479b391` | 2 bug-fixes touched this file in the last ~6 months (top 12% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | `internal/httpx/middleware.go` 201 Zeilen |
| 394 | medium | prior_defect | `internal/s3/s3.go` |  |  | 0.70 | `finding_5d41f34868d6c11c0c49` | 2 bug-fixes touched this file in the last ~6 months (top 12% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | `internal/s3/s3.go` 198 Zeilen |
| 395 | critical | prior_defect | `internal/server/server.go` |  |  | 0.69 | `finding_a6e7cbbaf982cd4778e2` | 5 bug-fixes touched this file in the last ~6 months (top 1.5% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | `internal/server/server.go` 144 Zeilen |
| 396 | critical | prior_defect | `internal/sfu/sfu.go` |  |  | 0.73 | `finding_536f56735e838f122d8d` | 4 bug-fixes touched this file in the last ~6 months (top 3.9% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | `internal/sfu/sfu.go` 185 Zeilen |
| 397 | critical | prior_defect | `internal/ws/hub.go` |  |  | 0.48 | `finding_d22e198204159d2dcb7a` | 4 bug-fixes touched this file in the last ~6 months (top 3.9% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | `internal/ws/hub.go` 165 Zeilen |
| 398 | critical | prior_defect | `web/src/App.vue` |  |  | 0.89 | `finding_01bc4790ae91c90bcfca` | 3 bug-fixes touched this file in the last ~6 months (top 6.4% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | `web/src/App.vue` 300 Zeilen |
| 399 | medium | prior_defect | `web/src/components/AdminDashboard.vue` |  |  | 0.31 | `finding_271f987af0d4f9b7aa68` | 2 bug-fixes touched this file in the last ~6 months (top 12% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | `web/src/components/AdminDashboard.vue` 112 Zeilen |
| 400 | medium | prior_defect | `web/src/components/AudioSettingsModal.vue` |  |  | 0.13 | `finding_8d0603290243daf7cc3a` | 2 bug-fixes touched this file in the last ~6 months (top 12% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | `web/src/components/AudioSettingsModal.vue` 46 Zeilen |
| 401 | critical | prior_defect | `web/src/components/ChatArea.vue` |  |  | 0.60 | `finding_4614eb662492e170c7f2` | 4 bug-fixes touched this file in the last ~6 months (top 3.9% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | `web/src/components/ChatArea.vue` 346 Zeilen |
| 402 | critical | prior_defect | `web/src/components/ThreadSidebar.vue` |  |  | 0.56 | `finding_d153b0d81973eea587cf` | 3 bug-fixes touched this file in the last ~6 months (top 6.4% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | `web/src/components/ThreadSidebar.vue` 321 Zeilen |
| 403 | medium | prior_defect | `web/src/components/UserProfileModal.vue` |  |  | 0.16 | `finding_5202f3544237e179befe` | 2 bug-fixes touched this file in the last ~6 months (top 12% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | `web/src/components/UserProfileModal.vue` 286 Zeilen |
| 404 | critical | prior_defect | `web/src/components/VoiceStage.vue` |  |  | 0.51 | `finding_625051c68089a536fb82` | 3 bug-fixes touched this file in the last ~6 months (top 6.4% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | `web/src/components/VoiceStage.vue` 389 Zeilen |
| 405 | critical | prior_defect | `web/src/composables/useWebRTC.js` |  |  | 0.42 | `finding_a6b2d5e1eb803e7d045c` | 8 bug-fixes touched this file in the last ~6 months (top <0.1% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 406 | critical | prior_defect | `web/src/stores/auth.js` |  |  | 0.67 | `finding_ce58da29407e5954ed8b` | 3 bug-fixes touched this file in the last ~6 months (top 6.4% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | `web/src/stores/auth.ts (was .js)` 61 Zeilen |
| 407 | critical | prior_defect | `web/src/stores/chat.js` |  |  | 0.76 | `finding_59f7461e4883b2510bac` | 5 bug-fixes touched this file in the last ~6 months (top 1.5% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | `web/src/stores/chat.ts (was .js)` 137 Zeilen |
| 408 | low | string_concat_in_loop | `internal/httpx/middleware.go` | ParseCIDRs | 180-180 | 0.00 | `finding_4a10506cc11c5b5e0c3b` | string built by += in a loop; use a buffer / join for linear cost | `internal/httpx/middleware.go` 201 Zeilen |
| 409 | low | string_concat_in_loop | `internal/httpx/middleware.go` | ParseCIDRs | 182-182 | 0.00 | `finding_f5aeca8e20c113b1d245` | string built by += in a loop; use a buffer / join for linear cost | `internal/httpx/middleware.go` 201 Zeilen |
| 410 | medium | ungoverned_hotspot | `cmd/server/main.go` |  |  | 0.00 | `finding_2daf364ccab61362ffcc` | Churn hotspot with no governing architectural decision. | `cmd/server/main.go` 179 Zeilen |
| 411 | medium | ungoverned_hotspot | `internal/auth/handler.go` |  |  | 0.00 | `finding_d7b6e870c44fe35a1eaf` | Churn hotspot with no governing architectural decision. | `internal/auth/handler.go` 207 Zeilen |
| 412 | medium | ungoverned_hotspot | `internal/auth/service.go` |  |  | 0.00 | `finding_544e9126342e24634ab2` | Churn hotspot with no governing architectural decision. | `internal/auth/service.go` 265 Zeilen |
| 413 | medium | ungoverned_hotspot | `internal/auth/users_admin.go` |  |  | 0.00 | `finding_6fd71a80f0573821b8a3` | Churn hotspot with no governing architectural decision. | `internal/auth/users_admin.go` 208 Zeilen |
| 414 | medium | ungoverned_hotspot | `internal/chat/channels.go` |  |  | 0.00 | `finding_1ed512350b52aa5a4b76` | Churn hotspot with no governing architectural decision. | `internal/chat/channels.go` 303 Zeilen |
| 415 | medium | ungoverned_hotspot | `internal/chat/handler.go` |  |  | 0.00 | `finding_2658e05473699ba21290` | Churn hotspot with no governing architectural decision. | `internal/chat/handler.go` 177 Zeilen |
| 416 | medium | ungoverned_hotspot | `internal/chat/messages.go` |  |  | 0.00 | `finding_bb59dea2919227b8d614` | Churn hotspot with no governing architectural decision. | `internal/chat/messages.go` 341 Zeilen |
| 417 | medium | ungoverned_hotspot | `internal/config/config.go` |  |  | 0.00 | `finding_0f6aa9886c2d2eefa458` | Churn hotspot with no governing architectural decision. | `internal/config/config.go` 110 Zeilen |
| 418 | medium | ungoverned_hotspot | `internal/db/db.go` |  |  | 0.00 | `finding_9d5a456a0cbbf9a50ba3` | Churn hotspot with no governing architectural decision. | `internal/db/db.go` 297 Zeilen |
| 419 | medium | ungoverned_hotspot | `internal/httpx/middleware.go` |  |  | 0.00 | `finding_29ae510e6d76d4e18663` | Churn hotspot with no governing architectural decision. | `internal/httpx/middleware.go` 201 Zeilen |
| 420 | medium | ungoverned_hotspot | `internal/linkpreview/linkpreview.go` |  |  | 0.00 | `finding_918806c16e3989d1b0ae` | Churn hotspot with no governing architectural decision. | `internal/linkpreview/linkpreview.go` 291 Zeilen |
| 421 | medium | ungoverned_hotspot | `internal/media/handler.go` |  |  | 0.00 | `finding_fcb4e9315ee6f8ded4de` | Churn hotspot with no governing architectural decision. | `internal/media/handler.go` 146 Zeilen |
| 422 | medium | ungoverned_hotspot | `internal/s3/s3.go` |  |  | 0.00 | `finding_7c66cd20acc1aab4a111` | Churn hotspot with no governing architectural decision. | `internal/s3/s3.go` 198 Zeilen |
| 423 | medium | ungoverned_hotspot | `internal/server/server.go` |  |  | 0.00 | `finding_8f8d4fe04bc420cddc16` | Churn hotspot with no governing architectural decision. | `internal/server/server.go` 144 Zeilen |
| 424 | medium | ungoverned_hotspot | `internal/sfu/sfu.go` |  |  | 0.00 | `finding_e976c6469d686a650d36` | Churn hotspot with no governing architectural decision. | `internal/sfu/sfu.go` 185 Zeilen |
| 425 | medium | ungoverned_hotspot | `internal/ws/hub.go` |  |  | 0.00 | `finding_912b57c8f2c304801d7e` | Churn hotspot with no governing architectural decision. | `internal/ws/hub.go` 165 Zeilen |
| 426 | medium | ungoverned_hotspot | `web/src/App.vue` |  |  | 0.00 | `finding_3421e0ee36aac95d38f1` | Churn hotspot with no governing architectural decision. | `web/src/App.vue` 300 Zeilen |
| 427 | medium | ungoverned_hotspot | `web/src/components/AdminDashboard.vue` |  |  | 0.00 | `finding_f99f03748d8076524173` | Churn hotspot with no governing architectural decision. | `web/src/components/AdminDashboard.vue` 112 Zeilen |
| 428 | medium | ungoverned_hotspot | `web/src/components/AudioSettingsModal.vue` |  |  | 0.00 | `finding_01633fcd50b8fb1b6384` | Churn hotspot with no governing architectural decision. | `web/src/components/AudioSettingsModal.vue` 46 Zeilen |
| 429 | medium | ungoverned_hotspot | `web/src/components/ChatArea.vue` |  |  | 0.00 | `finding_f0fd1d8c573ad2708cf7` | Churn hotspot with no governing architectural decision. | `web/src/components/ChatArea.vue` 346 Zeilen |
| 430 | medium | ungoverned_hotspot | `web/src/components/ConnectionStatsModal.vue` |  |  | 0.00 | `finding_7112a74e1e659716e712` | Churn hotspot with no governing architectural decision. | `web/src/components/ConnectionStatsModal.vue` 188 Zeilen |
| 431 | medium | ungoverned_hotspot | `web/src/components/CreateChannelModal.vue` |  |  | 0.00 | `finding_1700324d99864f6883b8` | Churn hotspot with no governing architectural decision. | `web/src/components/CreateChannelModal.vue` 232 Zeilen |
| 432 | medium | ungoverned_hotspot | `web/src/components/LegalModal.vue` |  |  | 0.00 | `finding_2e885befda412e7a3b4d` | Churn hotspot with no governing architectural decision. | `web/src/components/LegalModal.vue` 285 Zeilen |
| 433 | medium | ungoverned_hotspot | `web/src/components/LoginModal.vue` |  |  | 0.00 | `finding_ccd73b73f52383326214` | Churn hotspot with no governing architectural decision. | `web/src/components/LoginModal.vue` 219 Zeilen |
| 434 | medium | ungoverned_hotspot | `web/src/components/MarkdownContent.vue` |  |  | 0.00 | `finding_a3e406b3b19d4022b55b` | Churn hotspot with no governing architectural decision. | `web/src/components/MarkdownContent.vue` 143 Zeilen |
| 435 | medium | ungoverned_hotspot | `web/src/components/MemberList.vue` |  |  | 0.00 | `finding_84ca25d262207d61a1b4` | Churn hotspot with no governing architectural decision. | `web/src/components/MemberList.vue` 103 Zeilen |
| 436 | medium | ungoverned_hotspot | `web/src/components/SearchModal.vue` |  |  | 0.00 | `finding_c4879803450790349a65` | Churn hotspot with no governing architectural decision. | `web/src/components/SearchModal.vue` 340 Zeilen |
| 437 | medium | ungoverned_hotspot | `web/src/components/Sidebar.vue` |  |  | 0.00 | `finding_26cf81e4b24d31b92409` | Churn hotspot with no governing architectural decision. | `web/src/components/Sidebar.vue` 362 Zeilen |
| 438 | medium | ungoverned_hotspot | `web/src/components/ThreadSidebar.vue` |  |  | 0.00 | `finding_c720597fc59ab5c09af5` | Churn hotspot with no governing architectural decision. | `web/src/components/ThreadSidebar.vue` 321 Zeilen |
| 439 | medium | ungoverned_hotspot | `web/src/components/UserBar.vue` |  |  | 0.00 | `finding_b3a8f83cb7ae0c33507b` | Churn hotspot with no governing architectural decision. | `web/src/components/UserBar.vue` 152 Zeilen |
| 440 | medium | ungoverned_hotspot | `web/src/components/UserProfileModal.vue` |  |  | 0.00 | `finding_3f3ef933ea29c34a65da` | Churn hotspot with no governing architectural decision. | `web/src/components/UserProfileModal.vue` 286 Zeilen |
| 441 | medium | ungoverned_hotspot | `web/src/components/VoiceStage.vue` |  |  | 0.00 | `finding_9c71801e9e34e907b0ca` | Churn hotspot with no governing architectural decision. | `web/src/components/VoiceStage.vue` 389 Zeilen |
| 442 | medium | ungoverned_hotspot | `web/src/composables/useNavMenus.js` |  |  | 0.00 | `finding_e3ef7367dee1c9877486` | Churn hotspot with no governing architectural decision. | `web/src/composables/useNavMenus.ts (was .js)` 367 Zeilen |
| 443 | medium | ungoverned_hotspot | `web/src/composables/useWebRTC.js` |  |  | 0.00 | `finding_3d85d404bec6499a3349` | Churn hotspot with no governing architectural decision. | `web/src/composables/useWebRTC.ts (was .js)` 68 Zeilen |
| 444 | medium | ungoverned_hotspot | `web/src/stores/auth.js` |  |  | 0.00 | `finding_1c0f838a607597c33f13` | Churn hotspot with no governing architectural decision. | `web/src/stores/auth.ts (was .js)` 61 Zeilen |
| 445 | medium | ungoverned_hotspot | `web/src/stores/chat.js` |  |  | 0.00 | `finding_6bd82774c4935a4ff1e7` | Churn hotspot with no governing architectural decision. | `web/src/stores/chat.ts (was .js)` 137 Zeilen |
| 446 | medium | ungoverned_hotspot | `web/src/stores/voice.js` |  |  | 0.00 | `finding_50d7a926401890b81574` | Churn hotspot with no governing architectural decision. | `web/src/stores/voice.ts (was .js)` 134 Zeilen |
| 447 | high | untested_hotspot | `internal/chat/channels.go` |  |  | 1.56 | `finding_65cbc83075d73de3818c` | Hotspot with no paired test file and no coverage data — 5 dependents | `internal/chat/channels.go` 303 Zeilen |

## Code health, nicht in den 731 (146 Zeilen)

Testdateien und verstecktes `dry_violation`. Dieselbe Erledigungsregel: auf diesem Branch neu prüfen. Test-Duplikate nur anfassen, wenn der Live-Lauf sie noch nennt.

| # | Schwere | Biomarker | Datei | Symbol | Zeilen | Impact | public_id | Grund | warum nicht in 731 |
| ---: | --- | --- | --- | --- | --- | ---: | --- | --- | --- |
| 1 | medium | change_entropy | `internal/sfu/sfu_test.go` |  |  | 1.06 | `finding_c9db62b060aad40558b3` | changes are scattered across noisy commits (top 17% among files eligible for change-entropy ranking); a strong history-based fault predictor | Testdatei |
| 2 | medium | change_entropy | `web/src/composables/useWebRTC.test.js` |  |  | 1.06 | `finding_569831ec98154550a151` | changes are scattered across noisy commits (top 13% among files eligible for change-entropy ranking); a strong history-based fault predictor | Testdatei |
| 3 | low | churn_risk | `internal/sfu/sfu_test.go` |  |  | 0.36 | `finding_c5335c151fc63a17b362` | 90-day churn rewrote 1.2x the file's size (346 lines over 292 NLOC, top 16% among repository files eligible for churn ranking) | Testdatei |
| 4 | low | churn_risk | `web/src/composables/useWebRTC.test.js` |  |  | 0.36 | `finding_b53150ade7bac65f938e` | 90-day churn rewrote 1.2x the file's size (741 lines over 611 NLOC, top 8.3% among repository files eligible for churn ranking) | Testdatei |
| 5 | low | complex_conditional | `internal/media/policy_test.go` | TestDangerousTypesAreNeverInline | 60-60 | 0.40 | `finding_f4366db0604eba95fd66` | if condition combines 3 boolean operators | Testdatei |
| 6 | low | complex_conditional | `internal/server/auth_integration_test.go` | TestWebRTCConfigNeedsSessionAndCarriesTURN | 268-268 | 0.40 | `finding_b74b0ef99e5e67e2ae07` | if condition combines 3 boolean operators | Testdatei |
| 7 | low | complex_conditional | `internal/server/chat_integration_test.go` | TestRepliesQuoteTheOriginal | 269-269 | 0.40 | `finding_c08f7f52a63daa7a2221` | if condition combines 3 boolean operators | Testdatei |
| 8 | low | complex_conditional | `web/src/components/AdminDashboard.test.js` | vi.fn callback | 79-79 | 0.40 | `finding_c514c622bb07b0bdb2e1` | if condition combines 3 boolean operators | Testdatei |
| 9 | medium | complex_method | `internal/httpx/middleware_test.go` | TestSecurityHeaders | 106-172 | 0.85 | `finding_3934db39f210c5dd377f` | TestSecurityHeaders has cyclomatic complexity 13 | Testdatei |
| 10 | medium | complex_method | `internal/linkpreview/linkpreview_test.go` | TestParsesOpenGraphAndFallbacks | 48-90 | 0.85 | `finding_f536e8c61162c765c838` | TestParsesOpenGraphAndFallbacks has cyclomatic complexity 11 | Testdatei |
| 11 | high | complex_method | `internal/server/chat_integration_test.go` | TestMessagesAreScopedAndValidated | 15-92 | 0.36 | `finding_deff2117c8d5d233bcc6` | TestMessagesAreScopedAndValidated has cyclomatic complexity 16 | Testdatei |
| 12 | critical | complex_method | `internal/server/chat_integration_test.go` | TestHistoryPagingAt15kMessages | 148-253 | 0.61 | `finding_57c4409c2b89d0418c76` | TestHistoryPagingAt15kMessages has cyclomatic complexity 30 | Testdatei |
| 13 | medium | complex_method | `internal/server/chat_integration_test.go` | TestRepliesQuoteTheOriginal | 255-292 | 0.21 | `finding_f00f82d49eed9175cb73` | TestRepliesQuoteTheOriginal has cyclomatic complexity 13 | Testdatei |
| 14 | high | complex_method | `internal/server/layout_integration_test.go` | TestAdminRenamesAndReordersChannels | 28-79 | 1.45 | `finding_1ca1875dd8752bfbf090` | TestAdminRenamesAndReordersChannels has cyclomatic complexity 15 | Testdatei |
| 15 | medium | complex_method | `internal/server/media_integration_test.go` | TestUploadedMediaIsServedSafely | 44-80 | 0.85 | `finding_4bbb8e211d01dee78c78` | TestUploadedMediaIsServedSafely has cyclomatic complexity 11 | Testdatei |
| 16 | medium | complex_method | `internal/server/presence_integration_test.go` | TestStatusTextOnlyBySelfOrAdmin | 24-64 | 0.28 | `finding_47b4be79b9cf8d641bc8` | TestStatusTextOnlyBySelfOrAdmin has cyclomatic complexity 11 | Testdatei |
| 17 | high | complex_method | `internal/server/presence_integration_test.go` | TestMentionsResolveUsersAllAndHere | 103-164 | 0.47 | `finding_3b2a964c144fb07ae891` | TestMentionsResolveUsersAllAndHere has cyclomatic complexity 17 | Testdatei |
| 18 | medium | complex_method | `internal/server/presence_integration_test.go` | TestPresenceOverWebSocket | 166-217 | 0.28 | `finding_5d978a580d34ee7a4939` | TestPresenceOverWebSocket has cyclomatic complexity 10 | Testdatei |
| 19 | high | complex_method | `internal/server/presence_integration_test.go` | TestVoiceTimeAndMessageCounts | 219-279 | 0.47 | `finding_ce79d215b7227c8d2813` | TestVoiceTimeAndMessageCounts has cyclomatic complexity 15 | Testdatei |
| 20 | medium | complex_method | `internal/server/readstate_integration_test.go` | TestUnreadAndMentionCounts | 47-84 | 0.85 | `finding_6b9df85027b89bdfcda9` | TestUnreadAndMentionCounts has cyclomatic complexity 9 | Testdatei |
| 21 | high | complex_method | `internal/server/search_integration_test.go` | TestSearchFindsMessagesWithFilters | 37-97 | 1.45 | `finding_c4ce9c844b5f6e323951` | TestSearchFindsMessagesWithFilters has cyclomatic complexity 18 | Testdatei |
| 22 | medium | complex_method | `internal/server/users_admin_integration_test.go` | TestAdminDisablesAndEnablesUsers | 37-71 | 0.75 | `finding_f65ff0de763eca61639e` | TestAdminDisablesAndEnablesUsers has cyclomatic complexity 9 | Testdatei |
| 23 | medium | complex_method | `internal/server/users_admin_integration_test.go` | TestAdminKicksFromVoiceAndLastSeen | 103-137 | 0.75 | `finding_ed6e73c44d37ae716012` | TestAdminKicksFromVoiceAndLastSeen has cyclomatic complexity 9 | Testdatei |
| 24 | medium | complex_method | `internal/server/ws_integration_test.go` | TestVoiceJoinOnlyForVoiceChannels | 97-130 | 0.50 | `finding_f7703580976d1b88264e` | TestVoiceJoinOnlyForVoiceChannels has cyclomatic complexity 9 | Testdatei |
| 25 | medium | complex_method | `internal/server/ws_integration_test.go` | TestVoiceSurvivesReconnectWithinGrace | 134-176 | 0.50 | `finding_bc3b842ff771ed22f0d1` | TestVoiceSurvivesReconnectWithinGrace has cyclomatic complexity 10 | Testdatei |
| 26 | medium | complex_method | `internal/server/ws_integration_test.go` | TestTypingIsRelayedToOthersAndThrottled | 214-251 | 0.50 | `finding_f943ac88caeb984226bd` | TestTypingIsRelayedToOthersAndThrottled has cyclomatic complexity 9 | Testdatei |
| 27 | medium | complex_method | `internal/sfu/sfu_test.go` | publishVideo | 113-160 | 0.85 | `finding_4bed96d2722d1c0beb0b` | publishVideo has cyclomatic complexity 9 | Testdatei |
| 28 | medium | complex_method | `internal/sfu/subscriptions_test.go` | TestSubscriptionsOptOutAndIn | 26-59 | 0.75 | `finding_31e17e7e3715c3148b9e` | TestSubscriptionsOptOutAndIn has cyclomatic complexity 10 | Testdatei |
| 29 | medium | complex_method | `internal/sfu/subscriptions_test.go` | TestRoomSubscribeAndFiltering | 111-153 | 0.75 | `finding_8fc07b45609d9348bb5a` | TestRoomSubscribeAndFiltering has cyclomatic complexity 13 | Testdatei |
| 30 | medium | complex_method | `internal/ws/hub_test.go` | TestHubBroadcastAndSendToUsers | 67-132 | 0.75 | `finding_1bbc4d54bb1b2d73a68a` | TestHubBroadcastAndSendToUsers has cyclomatic complexity 10 | Testdatei |
| 31 | medium | complex_method | `internal/ws/hub_test.go` | TestHubSubscribeValidation | 256-315 | 0.75 | `finding_07b630f053d9950ab8e3` | TestHubSubscribeValidation has cyclomatic complexity 11 | Testdatei |
| 32 | medium | complex_method | `internal/ws/presence_test.go` | TestPresenceFollowsChoiceAndIdle | 37-86 | 0.85 | `finding_44480bfe25f679379c3d` | TestPresenceFollowsChoiceAndIdle has cyclomatic complexity 14 | Testdatei |
| 33 | medium | complex_method | `web/src/components/AdminDashboard.test.js` | vi.fn callback | 73-83 | 0.85 | `finding_b2d982ffc9a5b7b5f87e` | vi.fn callback has cyclomatic complexity 10 | Testdatei |
| 34 | medium | complex_method | `web/src/lib/messageWindow.test.js` | it callback | 142-171 | 0.85 | `finding_c723cb21941218c3eb17` | it callback has cyclomatic complexity 10 | Testdatei |
| 35 | low | dry_violation | `e2e/tests/smoke.spec.js` |  | 66-71 | 0.15 | `finding_f34ab957b201f630e3a9` | 9% of file duplicated; worst clone shares 6 lines with e2e/tests/smoke.spec.js | Testdatei, Typ versteckt |
| 36 | high | dry_violation | `internal/auth/handler.go` |  | 189-238 | 0.60 | `finding_05862398cb3f98ff5a3e` | 36% of file duplicated; worst clone shares 56 lines with internal/auth/invites.go (co-changed 3x) | Typ versteckt |
| 37 | high | dry_violation | `internal/auth/invites.go` |  | 142-197 | 0.60 | `finding_699b632130eac09b1f4a` | 41% of file duplicated; worst clone shares 56 lines with internal/auth/handler.go (co-changed 3x) | Typ versteckt |
| 38 | medium | dry_violation | `internal/auth/users_admin.go` |  | 135-180 | 0.35 | `finding_f734b6ae91c16b77aca5` | 63% of file duplicated; worst clone shares 47 lines with internal/auth/invites.go (co-changed 2x) | Typ versteckt |
| 39 | high | dry_violation | `internal/chat/handler.go` |  | 69-108 | 0.60 | `finding_152620b51d759775a3a8` | 45% of file duplicated; worst clone shares 45 lines with internal/media/handler.go (co-changed 4x) | Typ versteckt |
| 40 | low | dry_violation | `internal/httpx/response.go` |  | 58-84 | 0.15 | `finding_44e191c868d7a0efa1ab` | 24% of file duplicated; worst clone shares 27 lines with internal/httpx/response.go | Typ versteckt |
| 41 | high | dry_violation | `internal/media/handler.go` |  | 383-427 | 0.60 | `finding_965a16d823541d3a0318` | 44% of file duplicated; worst clone shares 45 lines with internal/chat/handler.go (co-changed 4x) | Typ versteckt |
| 42 | medium | dry_violation | `internal/media/policy.go` |  | 43-56 | 0.35 | `finding_313ed4d10cdace773427` | 27% of file duplicated; worst clone shares 15 lines with internal/media/policy.go | Typ versteckt |
| 43 | low | dry_violation | `internal/server/auth_integration_test.go` |  | 216-225 | 0.15 | `finding_406e27b86426418a03d5` | 23% of file duplicated; worst clone shares 11 lines with internal/server/search_integration_test.go | Testdatei, Typ versteckt |
| 44 | low | dry_violation | `internal/server/chat_integration_test.go` |  | 251-259 | 0.15 | `finding_250f70119f6262e03ae1` | 11% of file duplicated; worst clone shares 11 lines with internal/server/search_integration_test.go | Testdatei, Typ versteckt |
| 45 | low | dry_violation | `internal/server/helpers_integration_test.go` |  | 209-218 | 0.15 | `finding_f4d14a099f24228ed3a6` | 9% of file duplicated; worst clone shares 10 lines with internal/server/helpers_integration_test.go | Testdatei, Typ versteckt |
| 46 | low | dry_violation | `internal/server/media_integration_test.go` |  | 40-49 | 0.15 | `finding_cee5c8c3876872bbb51f` | 16% of file duplicated; worst clone shares 10 lines with internal/server/media_integration_test.go | Testdatei, Typ versteckt |
| 47 | low | dry_violation | `internal/server/presence_integration_test.go` |  | 226-237 | 0.15 | `finding_1e7917221f55d60f3426` | 15% of file duplicated; worst clone shares 12 lines with internal/server/ws_integration_test.go | Testdatei, Typ versteckt |
| 48 | low | dry_violation | `internal/server/search_integration_test.go` |  | 95-107 | 0.15 | `finding_d716b941f84645a30603` | 19% of file duplicated; worst clone shares 13 lines with internal/server/ws_integration_test.go | Testdatei, Typ versteckt |
| 49 | medium | dry_violation | `internal/server/users_admin_integration_test.go` |  | 106-119 | 0.35 | `finding_d7e2b233bd3cc5f4cb77` | 29% of file duplicated; worst clone shares 14 lines with internal/server/ws_integration_test.go | Testdatei, Typ versteckt |
| 50 | medium | dry_violation | `internal/server/ws_integration_test.go` |  | 93-114 | 0.35 | `finding_20731a3993e2634d767b` | 35% of file duplicated; worst clone shares 22 lines with internal/server/ws_integration_test.go | Testdatei, Typ versteckt |
| 51 | low | dry_violation | `internal/sfu/subscriptions.go` |  | 50-65 | 0.15 | `finding_f7929a05ca8a15a9ce02` | 15% of file duplicated; worst clone shares 16 lines with internal/sfu/subscriptions.go | Typ versteckt |
| 52 | low | dry_violation | `internal/sfu/subscriptions_test.go` |  | 62-70 | 0.15 | `finding_d8764b002535199499f8` | 10% of file duplicated; worst clone shares 9 lines with internal/sfu/subscriptions_test.go | Testdatei, Typ versteckt |
| 53 | low | dry_violation | `internal/ws/hub_test.go` |  | 31-42 | 0.15 | `finding_1d11a0f5b1068d4a7a25` | 17% of file duplicated; worst clone shares 12 lines with internal/ws/hub_test.go | Testdatei, Typ versteckt |
| 54 | low | dry_violation | `web/src/components/AccountMenu.vue` |  | 24-33 | 0.15 | `finding_a9293e185cacf902a22e` | 16% of file duplicated; worst clone shares 10 lines with web/src/components/PresenceMenu.vue | Typ versteckt |
| 55 | medium | dry_violation | `web/src/components/AdminDashboard.test.js` |  | 210-253 | 0.35 | `finding_7a8a6cd017f581ed2088` | 46% of file duplicated; worst clone shares 44 lines with web/src/components/AdminDashboard.test.js | Testdatei, Typ versteckt |
| 56 | medium | dry_violation | `web/src/components/AdminDashboard.vue` |  | 435-448 | 0.35 | `finding_2086a3b6ef2d0168c66c` | 19% of file duplicated; worst clone shares 14 lines with web/src/components/Sidebar.vue (co-changed 7x) | Typ versteckt |
| 57 | low | dry_violation | `web/src/components/AudioSettingsModal.test.js` |  | 105-112 | 0.15 | `finding_06c12eaa0ee8c63f3708` | 11% of file duplicated; worst clone shares 8 lines with web/src/components/AudioSettingsModal.test.js | Testdatei, Typ versteckt |
| 58 | low | dry_violation | `web/src/components/ChatArea.test.js` |  | 69-75 | 0.15 | `finding_7642079443053db9ce96` | 18% of file duplicated; worst clone shares 7 lines with web/src/components/ChatArea.test.js | Testdatei, Typ versteckt |
| 59 | high | dry_violation | `web/src/components/ChatArea.vue` |  | 961-984 | 0.60 | `finding_68bac1e9836680a8249e` | 33% of file duplicated; worst clone shares 24 lines with web/src/components/VoiceStage.vue (co-changed 10x) | Typ versteckt |
| 60 | low | dry_violation | `web/src/components/ConfirmDialog.test.js` |  | 40-46 | 0.15 | `finding_ff7627bc1a616e474fe8` | 25% of file duplicated; worst clone shares 7 lines with web/src/components/ConfirmDialog.test.js | Testdatei, Typ versteckt |
| 61 | low | dry_violation | `web/src/components/ContextMenu.test.js` |  | 51-61 | 0.15 | `finding_d36606958ea7ab4bfbd3` | 13% of file duplicated; worst clone shares 11 lines with web/src/components/ContextMenu.test.js | Testdatei, Typ versteckt |
| 62 | medium | dry_violation | `web/src/components/MemberList.vue` |  | 58-88 | 0.35 | `finding_ecf627e9b875d72379cc` | 44% of file duplicated; worst clone shares 31 lines with web/src/components/MemberList.vue | Typ versteckt |
| 63 | low | dry_violation | `web/src/components/ParticipantTile.vue` |  | 10-24 | 0.15 | `finding_de720d40afa86b051c50` | 10% of file duplicated; worst clone shares 15 lines with web/src/components/ParticipantTile.vue | Typ versteckt |
| 64 | medium | dry_violation | `web/src/components/PresenceMenu.vue` |  | 47-58 | 0.35 | `finding_86abb18d28381ebf5776` | 32% of file duplicated; worst clone shares 12 lines with web/src/components/TalkParticipants.vue | Typ versteckt |
| 65 | low | dry_violation | `web/src/components/TalkParticipants.vue` |  | 44-55 | 0.15 | `finding_b85e29def9b697fd965f` | 12% of file duplicated; worst clone shares 12 lines with web/src/components/PresenceMenu.vue | Typ versteckt |
| 66 | high | dry_violation | `web/src/components/ThreadSidebar.vue` |  | 572-630 | 0.60 | `finding_114ec2678a607d810346` | 62% of file duplicated; worst clone shares 59 lines with web/src/components/ChatArea.vue (co-changed 9x) | Typ versteckt |
| 67 | low | dry_violation | `web/src/components/UserBar.test.js` |  | 39-48 | 0.15 | `finding_ac7b0fa9851f476c56a7` | 8% of file duplicated; worst clone shares 10 lines with web/src/components/AdminDashboard.test.js | Testdatei, Typ versteckt |
| 68 | medium | dry_violation | `web/src/components/UserBar.vue` |  | 87-105 | 0.35 | `finding_4814fddb4505fe93f4a7` | 26% of file duplicated; worst clone shares 23 lines with web/src/components/UserBar.vue | Typ versteckt |
| 69 | medium | dry_violation | `web/src/components/VoiceStage.test.js` |  | 185-198 | 0.35 | `finding_8db52829c4470a30df84` | 31% of file duplicated; worst clone shares 14 lines with web/src/components/VoiceStage.test.js | Testdatei, Typ versteckt |
| 70 | medium | dry_violation | `web/src/components/VoiceStage.vue` |  | 763-783 | 0.35 | `finding_e38ed163ee6416223ca5` | 18% of file duplicated; worst clone shares 24 lines with web/src/components/ChatArea.vue (co-changed 10x) | Typ versteckt |
| 71 | low | dry_violation | `web/src/components/smoke.test.js` |  | 58-68 | 0.15 | `finding_334a482fffc255f0e7a2` | 12% of file duplicated; worst clone shares 11 lines with web/src/components/smoke.test.js | Testdatei, Typ versteckt |
| 72 | low | dry_violation | `web/src/composables/useComposerAssist.test.js` |  | 47-54 | 0.15 | `finding_49bd839d213726d9a837` | 23% of file duplicated; worst clone shares 8 lines with web/src/composables/useComposerAssist.test.js | Testdatei, Typ versteckt |
| 73 | low | dry_violation | `web/src/composables/useNavMenus.js` |  | 232-250 | 0.15 | `finding_8fb305440f197abbdb23` | 17% of file duplicated; worst clone shares 19 lines with web/src/composables/useNavMenus.js | Typ versteckt |
| 74 | low | dry_violation | `web/src/composables/useWebRTC.js` |  | 573-591 | 0.15 | `finding_10bb9ea05d8c336f2b2d` | 12% of file duplicated; worst clone shares 19 lines with web/src/composables/useWebRTC.js | Typ versteckt |
| 75 | low | dry_violation | `web/src/directives/tooltip.js` |  | 115-121 | 0.15 | `finding_34fd0b1e99cc965ff11e` | 11% of file duplicated; worst clone shares 7 lines with web/src/directives/tooltip.js | Typ versteckt |
| 76 | low | dry_violation | `web/src/i18n/i18n.test.js` |  | 48-53 | 0.15 | `finding_59616a7ac8e997ad091b` | 9% of file duplicated; worst clone shares 7 lines with web/src/lib/replies.test.js | Testdatei, Typ versteckt |
| 77 | medium | dry_violation | `web/src/lib/clock.js` |  | 28-34 | 0.35 | `finding_c25635cc9839d38b1cd6` | 34% of file duplicated; worst clone shares 7 lines with web/src/lib/clock.js | Typ versteckt |
| 78 | medium | dry_violation | `web/src/lib/clock.test.js` |  | 4-10 | 0.35 | `finding_7967bb92cccb3af3b3ba` | 35% of file duplicated; worst clone shares 9 lines with web/src/lib/ping.test.js | Testdatei, Typ versteckt |
| 79 | medium | dry_violation | `web/src/lib/levelMeter.test.js` |  | 18-25 | 0.35 | `finding_ca7fc78d1e09dc2a389e` | 50% of file duplicated; worst clone shares 8 lines with web/src/lib/levelMeter.test.js | Testdatei, Typ versteckt |
| 80 | low | dry_violation | `web/src/lib/messageWindow.test.js` |  | 63-68 | 0.15 | `finding_998fd7653684c1465a03` | 14% of file duplicated; worst clone shares 6 lines with web/src/lib/messageWindow.test.js | Testdatei, Typ versteckt |
| 81 | medium | dry_violation | `web/src/lib/ping.test.js` |  | 4-12 | 0.35 | `finding_39448c03de7157d28350` | 56% of file duplicated; worst clone shares 9 lines with web/src/lib/clock.test.js | Testdatei, Typ versteckt |
| 82 | low | dry_violation | `web/src/lib/replies.test.js` |  | 14-20 | 0.15 | `finding_fbd3122d3eaf864faaa5` | 18% of file duplicated; worst clone shares 7 lines with web/src/i18n/i18n.test.js | Testdatei, Typ versteckt |
| 83 | medium | dry_violation | `web/src/lib/router.test.js` |  | 6-31 | 0.35 | `finding_80c101bad6d00b323f0a` | 46% of file duplicated; worst clone shares 26 lines with web/src/lib/router.test.js | Testdatei, Typ versteckt |
| 84 | low | dry_violation | `web/src/lib/rtcStats.test.js` |  | 19-26 | 0.15 | `finding_4ba33f975a74791699f1` | 18% of file duplicated; worst clone shares 8 lines with web/src/lib/rtcStats.test.js | Testdatei, Typ versteckt |
| 85 | medium | dry_violation | `web/src/lib/soundEffects.js` |  | 103-132 | 0.35 | `finding_1be52f8ad165087ad8bf` | 40% of file duplicated; worst clone shares 30 lines with web/src/lib/soundEffects.js | Typ versteckt |
| 86 | low | dry_violation | `web/src/lib/soundEffects.test.js` |  | 80-89 | 0.15 | `finding_9d97d5f35d6f88d2dba5` | 24% of file duplicated; worst clone shares 11 lines with web/src/lib/soundEffects.test.js | Testdatei, Typ versteckt |
| 87 | low | dry_violation | `web/src/stores/chat.js` |  | 1178-1256 | 0.15 | `finding_4a2a24eeaddcefb8588f` | 10% of file duplicated; worst clone shares 79 lines with web/src/stores/chat.js | Typ versteckt |
| 88 | low | dry_violation | `web/src/stores/chatComfort.test.js` |  | 118-127 | 0.15 | `finding_c5cfee4c744a46c07a80` | 23% of file duplicated; worst clone shares 10 lines with web/src/stores/chatComfort.test.js | Testdatei, Typ versteckt |
| 89 | low | dry_violation | `web/src/stores/voice.js` |  | 529-622 | 0.15 | `finding_38f53861298ae9665c8a` | 23% of file duplicated; worst clone shares 94 lines with web/src/stores/voice.js | Typ versteckt |
| 90 | low | dry_violation | `web/src/stores/voice.test.js` |  | 12-20 | 0.15 | `finding_33468dc3cb8f45f69e80` | 10% of file duplicated; worst clone shares 9 lines with web/src/stores/voice.test.js | Testdatei, Typ versteckt |
| 91 | low | error_handling | `internal/auth/token_test.go` |  | 31-31 | 0.10 | `finding_f477d39c55151cf72f77` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 92 | low | error_handling | `internal/auth/token_test.go` |  | 32-32 | 0.10 | `finding_8c56358fe1f42a72a767` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 93 | low | error_handling | `internal/auth/token_test.go` |  | 37-37 | 0.10 | `finding_e4a8ece5b2f292e76531` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 94 | low | error_handling | `internal/auth/token_test.go` |  | 42-42 | 0.10 | `finding_58937bba67a6a4e0619b` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 95 | low | error_handling | `internal/auth/token_test.go` |  | 45-45 | 0.10 | `finding_bf2b2fe84bea9e86085c` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 96 | low | error_handling | `internal/config/config_test.go` |  | 42-42 | 0.15 | `finding_f6b554a50c59d305e6b4` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 97 | low | error_handling | `internal/config/config_test.go` |  | 43-43 | 0.15 | `finding_db417b20df5493d3c30b` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 98 | low | error_handling | `internal/httpx/middleware_test.go` |  | 107-107 | 0.15 | `finding_e6cb4225382567c952ed` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 99 | low | error_handling | `internal/httpx/middleware_test.go` |  | 175-175 | 0.15 | `finding_f1a37726d08abaa25614` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 100 | low | error_handling | `internal/httpx/request_test.go` |  | 107-107 | 0.08 | `finding_51e02561269ca060b9ac` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 101 | low | error_handling | `internal/httpx/request_test.go` |  | 114-114 | 0.08 | `finding_90e04f968dd8cefa4b5e` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 102 | low | error_handling | `internal/httpx/request_test.go` |  | 118-118 | 0.08 | `finding_fb0918b825b22d669bd5` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 103 | low | error_handling | `internal/httpx/request_test.go` |  | 134-134 | 0.08 | `finding_503c79c8db96145cd55e` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 104 | low | error_handling | `internal/httpx/request_test.go` |  | 144-144 | 0.08 | `finding_48e4292ff15b452f7426` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 105 | low | error_handling | `internal/httpx/request_test.go` |  | 153-153 | 0.08 | `finding_01ff864be0e0015a8c2c` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 106 | low | error_handling | `internal/linkpreview/linkpreview_test.go` |  | 37-37 | 0.07 | `finding_8416443880f802de61ea` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 107 | low | error_handling | `internal/linkpreview/linkpreview_test.go` |  | 53-53 | 0.07 | `finding_6a9dfac351b59ee1f476` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 108 | low | error_handling | `internal/linkpreview/linkpreview_test.go` |  | 60-60 | 0.07 | `finding_0d687230d2dfa0190899` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 109 | low | error_handling | `internal/linkpreview/linkpreview_test.go` |  | 64-64 | 0.07 | `finding_03015187cff3c3fceb6f` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 110 | low | error_handling | `internal/linkpreview/linkpreview_test.go` |  | 96-96 | 0.07 | `finding_ea6d8cbba86942c72076` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 111 | low | error_handling | `internal/linkpreview/linkpreview_test.go` |  | 100-100 | 0.07 | `finding_08c6ff3695937285f9aa` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 112 | low | error_handling | `internal/linkpreview/linkpreview_test.go` |  | 120-120 | 0.07 | `finding_dc22ca6f9bde94299894` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 113 | low | error_handling | `internal/server/helpers_integration_test.go` |  | 110-110 | 0.08 | `finding_229d9818ba993de9af0b` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 114 | low | error_handling | `internal/server/helpers_integration_test.go` |  | 145-145 | 0.08 | `finding_ad984751c0eedbf9cb39` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 115 | low | error_handling | `internal/server/helpers_integration_test.go` |  | 149-149 | 0.08 | `finding_4959b3308d7761bcdd38` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 116 | low | error_handling | `internal/server/helpers_integration_test.go` |  | 166-166 | 0.08 | `finding_b8afa04cd947db00caee` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 117 | low | error_handling | `internal/server/helpers_integration_test.go` |  | 189-189 | 0.08 | `finding_53940787e8d64029c3bf` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 118 | low | error_handling | `internal/server/helpers_integration_test.go` |  | 190-190 | 0.08 | `finding_54c7d93b4e84517dfedb` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 119 | low | error_handling | `internal/server/search_integration_test.go` |  | 53-53 | 0.06 | `finding_96581ab36a43a5d7e5b3` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 120 | low | error_handling | `internal/server/search_integration_test.go` |  | 61-61 | 0.06 | `finding_b5899ce7b86e108bd06b` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 121 | low | error_handling | `internal/server/search_integration_test.go` |  | 65-65 | 0.06 | `finding_0e375af987a0d192dfcd` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 122 | low | error_handling | `internal/server/search_integration_test.go` |  | 69-69 | 0.06 | `finding_ba36a4977c87949491e2` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 123 | low | error_handling | `internal/server/search_integration_test.go` |  | 73-73 | 0.06 | `finding_b6aadbf48b0b9474bf3c` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 124 | low | error_handling | `internal/server/search_integration_test.go` |  | 77-77 | 0.06 | `finding_bf3858665bf077aa2c61` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 125 | low | error_handling | `internal/server/search_integration_test.go` |  | 82-82 | 0.06 | `finding_c21e932d3263af685256` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 126 | low | error_handling | `internal/server/search_integration_test.go` |  | 86-86 | 0.06 | `finding_0d9d1c88c61958b9c5b5` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 127 | low | error_handling | `internal/server/search_integration_test.go` |  | 110-110 | 0.06 | `finding_a84a52764e9777382bef` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 128 | low | error_handling | `internal/server/turn_test.go` |  | 24-24 | 0.15 | `finding_728616b61b4d03b6d189` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 129 | low | error_handling | `internal/sfu/subscriptions_test.go` |  | 87-87 | 0.15 | `finding_0bc6ada8186028513063` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 130 | low | error_handling | `internal/sfu/subscriptions_test.go` |  | 156-156 | 0.15 | `finding_aedc4309d5afc37d939b` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 131 | low | error_handling | `web/web_test.go` |  | 38-38 | 0.15 | `finding_2cc037bb9be05b33e1fe` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 132 | low | error_handling | `web/web_test.go` |  | 66-66 | 0.15 | `finding_cb818d86be7a827c34df` | error value is checked then ignored, or discarded via the blank identifier | Testdatei |
| 133 | medium | io_in_loop | `internal/server/chat_integration_test.go` | TestHistoryPagingAt15kMessages | 176-176 | 0.00 | `finding_13301dfd526d96746e2c` | a network call is reached once per loop iteration through TestHistoryPagingAt15kMessages -> page -> get -> do (cross-function I/O call inside a loop) | Testdatei |
| 134 | medium | io_in_loop | `internal/server/presence_integration_test.go` | TestPresenceChoiceAndNames | 94-94 | 0.00 | `finding_629cbd27de62a3484515` | a network call is reached once per loop iteration through TestPresenceChoiceAndNames -> post -> do (cross-function I/O call inside a loop) | Testdatei |
| 135 | low | large_method | `internal/server/chat_integration_test.go` | TestMessagesAreScopedAndValidated | 15-92 | 0.09 | `finding_93efead463b09e347bd0` | TestMessagesAreScopedAndValidated is 68 lines long | Testdatei |
| 136 | medium | large_method | `internal/server/chat_integration_test.go` | TestHistoryPagingAt15kMessages | 148-253 | 0.22 | `finding_dab232a8bea2f62ac996` | TestHistoryPagingAt15kMessages is 95 lines long | Testdatei |
| 137 | high | low_cohesion | `web/src/composables/useWebRTC.test.js` | FakePC | 47-70 | 0.60 | `finding_cca2ff5043c501cfeddd` | FakePC has low cohesion (LCOM4=4): its 10 methods split into 4 groups that share no fields or calls | Testdatei |
| 138 | high | low_cohesion | `web/src/composables/useWebRTC.test.js` | FakeAudioContext | 72-103 | 0.60 | `finding_7826d2b4e3455736f48f` | FakeAudioContext has low cohesion (LCOM4=8): its 8 methods split into 8 groups that share no fields or calls | Testdatei |
| 139 | high | low_cohesion | `web/src/stores/connection.test.js` | FakeSocket | 8-19 | 0.60 | `finding_6c0079ffe87cf83918db` | FakeSocket has low cohesion (LCOM4=3): its 5 methods split into 3 groups that share no fields or calls | Testdatei |
| 140 | medium | nested_complexity | `internal/sfu/sfu_test.go` | publishVideo | 113-160 | 0.94 | `finding_e92ee5b81e8140a00cac` | publishVideo nests 4 levels deep | Testdatei |
| 141 | low | primitive_obsession | `internal/sfu/sfu_test.go` | waitSources | 297-307 | 0.15 | `finding_62641e3d262fd12b556a` | long parameter list: waitSources takes 5 parameters | Testdatei |
| 142 | medium | prior_defect | `internal/config/config_test.go` |  |  | 0.70 | `finding_2edd745c7541876e7b80` | 2 bug-fixes touched this file in the last ~6 months (top 12% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | Testdatei |
| 143 | medium | prior_defect | `internal/httpx/middleware_test.go` |  |  | 0.70 | `finding_a983a0709319df1aab88` | 2 bug-fixes touched this file in the last ~6 months (top 12% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | Testdatei |
| 144 | medium | prior_defect | `internal/sfu/sfu_test.go` |  |  | 0.70 | `finding_ba46612f46dd6a94444b` | 2 bug-fixes touched this file in the last ~6 months (top 12% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | Testdatei |
| 145 | medium | prior_defect | `web/src/components/AudioSettingsModal.test.js` |  |  | 0.70 | `finding_ff142dd37103778867ad` | 2 bug-fixes touched this file in the last ~6 months (top 12% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | Testdatei |
| 146 | medium | prior_defect | `web/src/composables/useWebRTC.test.js` |  |  | 0.70 | `finding_a95509f8f5f1288ad37e` | 2 bug-fixes touched this file in the last ~6 months (top 12% among files in this repository); recent defect history is the strongest cost-effective predictor of further defects | Testdatei |

## Refactoring-Pläne, offen

In der Overview: 44. Offen insgesamt: 102.

| # | in 731 | Typ | Datei | Symbol | Zeilen | Aufwand | Confidence | Impact | Biomarker | public_id |
| ---: | --- | --- | --- | --- | --- | --- | --- | ---: | --- | --- |
| 1 | ja | extract_method | `web/src/components/AdminDashboard.vue` | `onChannelDrop` | 311-340 | S | high | 1.69 | complex_method | `refac2_15aa609fbe3ef23778c0` |
| 2 | ja | extract_method | `web/src/components/LegalModal.vue` | `onMounted callback` | 27-46 | S | high | 1.69 | complex_method | `refac2_562ec031edfb94727cd1` |
| 3 | ja | extract_method | `internal/server/server.go` | `NewRouter` | 48-181 | M | high | 1.48 | complex_method | `refac2_f849423c33ceb4e0e8d2` |
| 4 | nein | extract_class | `web/src/composables/useWebRTC.test.js` | `FakeAudioContext` | 72-103 | S | high | 1.12 | low_cohesion | `refac2_67d90b418603a25a8cd9` |
| 5 | nein | extract_method | `internal/sfu/sfu_test.go` | `publishVideo` | 113-160 | M | high | 0.88 | complex_method | `refac2_07d9decccd164a355110` |
| 6 | ja | extract_method | `web/src/i18n/index.js` | `flattenKeys` | 111-124 | S | high | 0.85 | complex_method | `refac2_31fd2472ccfebc5c82ca` |
| 7 | ja | extract_method | `web/src/lib/linkPreview.js` | `fetchLinkPreview` | 8-22 | S | medium | 0.85 | complex_method | `refac2_2417835dd8301168a580` |
| 8 | ja | extract_method | `web/src/components/AccountMenu.vue` | `onKeydown` | 57-84 | S | high | 0.85 | complex_method | `refac2_242a93d774c5270e4587` |
| 9 | nein | extract_method | `internal/linkpreview/linkpreview_test.go` | `TestParsesOpenGraphAndFallbacks` | 48-90 | S | high | 0.85 | complex_method | `refac2_c2fb431c85aa2ccdec13` |
| 10 | nein | extract_method | `internal/server/media_integration_test.go` | `TestUploadedMediaIsServedSafely` | 44-80 | S | high | 0.85 | complex_method | `refac2_acf9946657fdb94cced0` |
| 11 | nein | extract_method | `internal/server/readstate_integration_test.go` | `TestUnreadAndMentionCounts` | 47-84 | S | high | 0.85 | complex_method | `refac2_4b1b999ef5b27b421c16` |
| 12 | nein | extract_method | `web/src/lib/messageWindow.test.js` | `it callback` | 142-171 | S | high | 0.85 | complex_method | `refac2_793b03770fec27fd297d` |
| 13 | ja | extract_method | `web/src/lib/api.js` | `api` | 64-92 | S | high | 0.75 | complex_method | `refac2_735b99d28ac83fe76cf4` |
| 14 | nein | extract_method | `internal/server/users_admin_integration_test.go` | `TestAdminKicksFromVoiceAndLastSeen` | 103-137 | S | high | 0.75 | complex_method | `refac2_5eb13a2faba49ab83208` |
| 15 | nein | extract_method | `internal/sfu/subscriptions_test.go` | `TestSubscriptionsOptOutAndIn` | 26-59 | S | high | 0.75 | complex_method | `refac2_b6cbc605a9b517ce14dd` |
| 16 | nein | extract_method | `internal/ws/hub_test.go` | `TestHubBroadcastAndSendToUsers` | 67-132 | S | medium | 0.75 | complex_method | `refac2_a2ea3dbc9e44a109a612` |
| 17 | ja | extract_method | `internal/media/handler.go` | `uploadAvatar` | 266-318 | S | high | 0.69 | complex_method | `refac2_55551a4ca9aab27ba6d1` |
| 18 | ja | extract_method | `web/src/composables/useNavMenus.js` | `buildChannelItems` | 63-123 | M | high | 0.61 | complex_method | `refac2_35502a9a2749aa8b0ab9` |
| 19 | nein | extract_method | `internal/server/layout_integration_test.go` | `TestAdminRenamesAndReordersChannels` | 28-79 | M | high | 0.60 | complex_method | `refac2_bff68dc217b88da953b9` |
| 20 | nein | extract_method | `internal/server/search_integration_test.go` | `TestSearchFindsMessagesWithFilters` | 37-97 | M | high | 0.60 | complex_method | `refac2_dce57eb4f36796f0b54e` |
| 21 | nein | extract_class | `web/src/stores/connection.test.js` | `FakeSocket` | 8-19 | S | high | 0.60 | low_cohesion | `refac2_5f66926ab2680d3eeb27` |
| 22 | nein | extract_helper | `web/src/components/ChatArea.vue` | `ChatArea.vue:961-984` | 961-984 | S | high | 0.60 | dry_violation | `refac2_dd8560cf70bf007766ca` |
| 23 | nein | extract_method | `internal/server/ws_integration_test.go` | `TestTypingIsRelayedToOthersAndThrottled` | 214-251 | S | high | 0.50 | complex_method | `refac2_94b884678d10f5ecf541` |
| 24 | nein | extract_method | `internal/server/ws_integration_test.go` | `TestVoiceJoinOnlyForVoiceChannels` | 97-130 | S | high | 0.50 | complex_method | `refac2_e43fd4a9c76a89c56e63` |
| 25 | nein | extract_method | `internal/server/ws_integration_test.go` | `TestVoiceSurvivesReconnectWithinGrace` | 134-176 | S | high | 0.50 | complex_method | `refac2_1aafea322e7c3169fa08` |
| 26 | ja | extract_method | `web/src/lib/soundEffects.js` | `playSound` | 69-183 | S | high | 0.40 | complex_method | `refac2_8673e03347691ee0eb17` |
| 27 | ja | extract_method | `web/src/composables/useWebRTC.js` | `createBoost` | 125-145 | S | high | 0.38 | complex_method | `refac2_7fe73a835443536387bc` |
| 28 | ja | extract_method | `web/src/composables/useWebRTC.js` | `buildScreenAudioMix` | 226-262 | S | high | 0.38 | complex_method | `refac2_b272a3280dd4d034501e` |
| 29 | nein | extract_helper | `web/src/lib/soundEffects.js` | `soundEffects.js:103-132` | 103-132 | S | medium | 0.35 | dry_violation | `refac2_1ea88e8353220ef63808` |
| 30 | ja | extract_method | `web/src/lib/soundEffects.js` | `playTone` | 27-63 | S | high | 0.35 | complex_method | `refac2_8f45de44b3a29eb860b2` |
| 31 | ja | extract_method | `internal/chat/search.go` | `Search` | 38-103 | S | medium | 0.31 | large_method | `refac2_748e7170b1901054fec2` |
| 32 | nein | extract_helper | `web/src/composables/useWebRTC.js` | `useWebRTC.js:573-591` | 573-591 | S | medium | 0.30 | dry_violation | `refac2_791b38ffa352a901a02a` |
| 33 | nein | extract_method | `internal/server/presence_integration_test.go` | `TestPresenceOverWebSocket` | 166-217 | M | high | 0.28 | complex_method | `refac2_03b612cd152837f10877` |
| 34 | nein | extract_method | `internal/server/chat_integration_test.go` | `TestHistoryPagingAt15kMessages` | 148-253 | M | medium | 0.22 | large_method | `refac2_58bfa4c28e583c2b2624` |
| 35 | nein | extract_method | `internal/server/chat_integration_test.go` | `TestRepliesQuoteTheOriginal` | 255-292 | S | medium | 0.21 | complex_method | `refac2_c74002cc9224f7e7e278` |
| 36 | ja | extract_method | `web/src/components/ChatArea.vue` | `maybeLoadMore` | 190-199 | S | high | 0.20 | complex_method | `refac2_370210dca4eab55f53b8` |
| 37 | ja | extract_method | `web/src/App.vue` | `applyRoute` | 79-154 | M | high | 0.20 | large_method | `refac2_c2b1abcb360e7854fac4` |
| 38 | nein | extract_method | `internal/server/presence_integration_test.go` | `TestMentionsResolveUsersAllAndHere` | 103-164 | M | high | 0.20 | complex_method | `refac2_0cbaf2d9835d0ce63773` |
| 39 | nein | extract_method | `internal/server/presence_integration_test.go` | `TestVoiceTimeAndMessageCounts` | 219-279 | M | high | 0.20 | complex_method | `refac2_976bb2730a12c1f894c3` |
| 40 | ja | extract_method | `internal/ws/hub.go` | `unregister` | 372-414 | S | high | 0.19 | complex_method | `refac2_0a7a9bc1f7de2a5dde12` |
| 41 | nein | extract_method | `internal/server/chat_integration_test.go` | `TestMessagesAreScopedAndValidated` | 15-92 | M | high | 0.15 | complex_method | `refac2_1e99dd7b237c96a0be45` |
| 42 | ja | break_cycle | `web/src/components/GlobalSearch.vue` | `cycle[4]: globalSearch.js->GlobalSearch.vue` |  | M | high | 0.00 |  | `refac2_25d40905538d9787e99f` |
| 43 | nein | extract_helper | `internal/chat/handler.go` | `handler.go:142-160` | 142-160 | S | medium | 0.00 | dry_violation | `refac2_f6882cb32fc216da232a` |
| 44 | nein | extract_helper | `internal/chat/messages.go` | `messages.go:178-185` | 178-185 | S | medium | 0.00 | dry_violation | `refac2_8e20883997b149a3f7d2` |
| 45 | nein | extract_helper | `web/src/components/AdminDashboard.vue` | `AdminDashboard.vue:185-200` | 185-200 | S | medium | 0.00 | dry_violation | `refac2_6b1d7bf897b37a7772c9` |
| 46 | nein | extract_helper | `web/src/components/AdminDashboard.vue` | `AdminDashboard.vue:897-946` | 897-946 | M | medium | 0.00 | dry_violation | `refac2_13187b3c5349cf3e46bb` |
| 47 | nein | extract_helper | `web/src/components/ChatArea.vue` | `ChatArea.vue:465-477` | 465-477 | S | medium | 0.00 | dry_violation | `refac2_7069125e52112036dce1` |
| 48 | nein | extract_helper | `web/src/components/ChatArea.vue` | `ChatArea.vue:908-924` | 908-924 | S | high | 0.00 | dry_violation | `refac2_fd3af1b4e754e92d77fa` |
| 49 | nein | extract_helper | `web/src/components/ThreadSidebar.vue` | `ThreadSidebar.vue:307-336` | 307-336 | S | medium | 0.00 | dry_violation | `refac2_f225ec96df42738ea3a3` |
| 50 | nein | extract_helper | `web/src/components/ThreadSidebar.vue` | `ThreadSidebar.vue:340-356` | 340-356 | S | medium | 0.00 | dry_violation | `refac2_4d1620b542662d935d3d` |
| 51 | nein | extract_helper | `web/src/stores/chat.js` | `chat.js:251-258` | 251-258 | S | medium | 0.00 | dry_violation | `refac2_c5746c34f8c349a91816` |
| 52 | nein | extract_helper | `web/src/stores/voice.js` | `voice.js:346-353` | 346-353 | S | medium | 0.00 | dry_violation | `refac2_860f3fa13f468ce648c4` |
| 53 | ja | extract_method | `internal/chat/mentions.go` | `ResolveMentions` | 49-94 | S | high | 0.00 | complex_method | `refac2_08fe75a97cf1aefffb6c` |
| 54 | nein | extract_method | `internal/httpx/middleware_test.go` | `TestSecurityHeaders` | 106-172 | M | high | 0.00 | complex_method | `refac2_b65b2446fb11a505207a` |
| 55 | ja | extract_method | `internal/httpx/request.go` | `SanitizeFilename` | 83-107 | S | medium | 0.00 | complex_method | `refac2_721b9cd0704ca08ec9bf` |
| 56 | nein | extract_method | `internal/server/users_admin_integration_test.go` | `TestAdminDisablesAndEnablesUsers` | 37-71 | S | high | 0.00 | complex_method | `refac2_4c6469cfe6bc297a5f3f` |
| 57 | ja | extract_method | `internal/sfu/subscriptions.go` | `Subscribe` | 132-164 | S | medium | 0.00 | complex_method | `refac2_0b4bc61fcc97d99aaabd` |
| 58 | nein | extract_method | `internal/sfu/subscriptions_test.go` | `TestRoomSubscribeAndFiltering` | 111-153 | S | high | 0.00 | complex_method | `refac2_4b8d7ec6dddeb95c191b` |
| 59 | nein | extract_method | `internal/ws/hub_test.go` | `TestHubSubscribeValidation` | 256-315 | M | high | 0.00 | complex_method | `refac2_c17d164eb7f89e69fb8c` |
| 60 | nein | extract_method | `internal/ws/presence_test.go` | `TestPresenceFollowsChoiceAndIdle` | 37-86 | M | high | 0.00 | complex_method | `refac2_0b35dd54dd89a082e468` |
| 61 | ja | extract_method | `web/src/components/ChatArea.vue` | `openMessageContextMenu` | 422-516 | M | high | 0.00 | complex_method | `refac2_b08f97c692bdce38179d` |
| 62 | ja | extract_method | `web/src/components/ChatArea.vue` | `watch callback` | 242-267 | S | medium | 0.00 | complex_method | `refac2_61db61e774b8540a90ba` |
| 63 | ja | extract_method | `web/src/composables/useNavMenus.js` | `buildMemberItems` | 186-262 | M | high | 0.00 | complex_method | `refac2_d37b3c96872a0b1b7de4` |
| 64 | nein | extract_helper | `web/src/components/ChatArea.vue` | `ChatArea.vue:1059-1116` | 1059-1116 | M | high | 0.00 | dry_violation | `refac2_d888eb4001e4c2498b6f` |
| 65 | nein | extract_helper | `web/src/components/ChatArea.vue` | `ChatArea.vue:822-878` | 822-878 | M | high | 0.00 | dry_violation | `refac2_022699117edc493896c8` |
| 66 | nein | extract_helper | `web/src/components/ChatArea.vue` | `ChatArea.vue:991-1038` | 991-1038 | M | high | 0.00 | dry_violation | `refac2_d97226e574eef2696c1b` |
| 67 | nein | extract_method | `internal/server/presence_integration_test.go` | `TestStatusTextOnlyBySelfOrAdmin` | 24-64 | S | high | 0.00 | complex_method | `refac2_c233de3cc67b2c729e14` |
| 68 | ja | extract_method | `web/src/lib/mentionQuery.js` | `suggestMentions` | 30-49 | S | high | 0.00 | complex_method | `refac2_4952b5acdd1a92ffc2c8` |
| 69 | ja | break_cycle | `web/src/lib/soundEffects.js` | `cycle[2]: voice.js->soundEffects.js` |  | S | high | 0.00 |  | `refac2_7755147073af143b3851` |
| 70 | nein | extract_helper | `web/src/components/AccountMenu.vue` | `AccountMenu.vue:67-74` | 67-74 | S | medium | 0.00 | dry_violation | `refac2_a762eb57ba67fd578e38` |
| 71 | nein | extract_helper | `web/src/composables/useWebRTC.js` | `useWebRTC.js:1052-1063` | 1052-1063 | S | medium | 0.00 | dry_violation | `refac2_45480d56e3dc6ff63346` |
| 72 | nein | extract_helper | `web/src/composables/useWebRTC.js` | `useWebRTC.js:1065-1078` | 1065-1078 | S | medium | 0.00 | dry_violation | `refac2_5b923996a8cd7d8ed48d` |
| 73 | nein | extract_helper | `web/src/composables/useWebRTC.js` | `useWebRTC.js:601-618` | 601-618 | S | medium | 0.00 | dry_violation | `refac2_e6bdf5ecc65699ba7c19` |
| 74 | nein | extract_helper | `web/src/composables/useWebRTC.js` | `useWebRTC.js:634-642` | 634-642 | S | medium | 0.00 | dry_violation | `refac2_0e11c76b6834af4b8d79` |
| 75 | nein | extract_helper | `web/src/composables/useWebRTC.js` | `useWebRTC.js:654-664` | 654-664 | S | medium | 0.00 | dry_violation | `refac2_f3d117bd5849ae41f52c` |
| 76 | ja | extract_method | `internal/tools/openapifix/main.go` | `run` | 39-74 | S | high | 0.00 | complex_method | `refac2_18fd562a5be22b2c7f1b` |
| 77 | ja | extract_method | `web/src/composables/useWebRTC.js` | `setupPeerConnection` | 265-347 | M | high | 0.00 | complex_method | `refac2_90ccf3b2428f668205ef` |
| 78 | nein | extract_helper | `internal/ws/hub.go` | `hub.go:858-865` | 858-865 | S | medium | 0.00 | dry_violation | `refac2_cc87e2e75748888bcc2f` |
| 79 | nein | extract_helper | `web/src/components/VoiceStage.vue` | `VoiceStage.vue:579-587` | 579-587 | S | medium | 0.00 | dry_violation | `refac2_1e91fa715437fbe14058` |
| 80 | nein | extract_helper | `web/src/components/VoiceStage.vue` | `VoiceStage.vue:610-617` | 610-617 | S | medium | 0.00 | dry_violation | `refac2_617d818b82302311d88e` |
| 81 | nein | extract_helper | `web/src/components/VoiceStage.vue` | `VoiceStage.vue:627-635` | 627-635 | S | medium | 0.00 | dry_violation | `refac2_a6f62148a63319e3eae1` |
| 82 | nein | extract_helper | `web/src/components/VoiceStage.vue` | `VoiceStage.vue:643-652` | 643-652 | S | medium | 0.00 | dry_violation | `refac2_a068660e474fcec9898f` |
| 83 | nein | extract_helper | `web/src/composables/useWebRTC.js` | `useWebRTC.js:1170-1179` | 1170-1179 | S | medium | 0.00 | dry_violation | `refac2_3f0a381abb21d5f8fd88` |
| 84 | nein | extract_helper | `web/src/lib/soundEffects.js` | `soundEffects.js:145-163` | 145-163 | S | medium | 0.00 | dry_violation | `refac2_d9a4d706c5d8c2cf02b5` |
| 85 | ja | extract_method | `internal/sfu/sfu.go` | `JoinPeer` | 282-410 | S | medium | 0.00 | brain_method | `refac2_8a8d5d553fcd5302e099` |
| 86 | ja | extract_method | `internal/ws/hub.go` | `joinVoice` | 553-614 | M | high | 0.00 | complex_method | `refac2_5177b1d8f20aa3321c54` |
| 87 | ja | extract_method | `cmd/server/main.go` | `run` | 32-92 | S | high | 0.00 | complex_method | `refac2_7641cf06c944b631d597` |
| 88 | ja | extract_method | `internal/ws/hub.go` | `handle` | 794-889 | S | high | 0.00 | complex_method | `refac2_864ec1128633ae961fc9` |
| 89 | ja | split_file | `web/src/composables/useWebRTC.js` | `useWebRTC.js -> 3 files` |  | XL | medium | 0.00 |  | `refac2_b0280d2b1670452cf595` |
| 90 | ja | performance_fix | `internal/media/admin.go` | `internal/media/admin.go::StartRetentionWorker` | 160-160 | M | medium | 0.00 | io_in_loop | `refac2_e08724d79b8775856161` |
| 91 | ja | performance_fix | `internal/chat/handler.go` | `internal/chat/handler.go::Handler::listMessages` | 159-159 | M | medium | 0.00 | io_in_loop | `refac2_b9b441c50687edf349db` |
| 92 | ja | performance_fix | `internal/chat/handler.go` | `listMessages` | 148-148 | M | medium | 0.00 | io_in_loop | `refac2_8cab083a2ad2b4a330e1` |
| 93 | ja | performance_fix | `internal/ws/hub.go` | `internal/ws/hub.go::Hub::removePresence` | 632-632 | M | medium | 0.00 | io_in_loop | `refac2_4fe69b3bbeaa4601f238` |
| 94 | ja | performance_fix | `internal/chat/channels.go` | `ApplyLayout` | 252-252 | M | medium | 0.00 | io_in_loop | `refac2_6fe93355aa00ff831b5a` |
| 95 | ja | performance_fix | `internal/ws/hub.go` | `internal/ws/hub.go::Client::handle` | 790-790 | M | medium | 0.00 | io_in_loop | `refac2_3632d1a0b1b4dae5e1e9` |
| 96 | ja | performance_fix | `internal/chat/handler.go` | `getThread` | 432-432 | M | medium | 0.00 | io_in_loop | `refac2_a9858c14c52ed189f4d8` |
| 97 | ja | performance_fix | `internal/chat/handler.go` | `internal/chat/handler.go::Handler::getThread` | 442-442 | M | medium | 0.00 | io_in_loop | `refac2_8d81620c76cc75fedf6a` |
| 98 | ja | performance_fix | `internal/chat/channels.go` | `ApplyLayout` | 243-243 | M | medium | 0.00 | io_in_loop | `refac2_af9d53327c46c968f1f8` |
| 99 | nein | performance_fix | `internal/server/helpers_integration_test.go` | `internal/server/helpers_integration_test.go::client::post` |  | M | medium | 0.00 | io_in_loop | `refac2_f3c56c0c882a776a3243` |
| 100 | nein | performance_fix | `internal/server/helpers_integration_test.go` | `internal/server/helpers_integration_test.go::client::get` |  | M | medium | 0.00 | io_in_loop | `refac2_3adb6d21a54f3ef8c5d7` |
| 101 | ja | performance_fix | `internal/httpx/middleware.go` | `ParseCIDRs` | 182-182 | S | medium | 0.00 | string_concat_in_loop | `refac2_707c7b1051686771d074` |
| 102 | ja | performance_fix | `internal/httpx/middleware.go` | `ParseCIDRs` | 180-180 | S | medium | 0.00 | string_concat_in_loop | `refac2_ca924e8bef0328f5fd83` |

## Performance-Opportunities

| Status | Biomarker | Aktion | Datei | Symbol | Score | Beobachtungen | id |
| --- | --- | --- | --- | --- | ---: | ---: | --- |
| open | io_in_loop | advisory | `internal/media/admin.go` | internal/media/admin.go::StartRetentionWorker | 20 | 1 | `perf2_b1212f02a856707e343e` |
| open | io_in_loop | advisory | `internal/chat/handler.go` | internal/chat/handler.go::Handler::listMessages | 17 | 1 | `perf2_0265bb6fab7fd7891b94` |
| open | io_in_loop | advisory | `internal/chat/handler.go` |  | 17 | 1 | `perf2_061ed8c6382978369c08` |
| open | io_in_loop | advisory | `internal/ws/hub.go` | internal/ws/hub.go::Hub::removePresence | 17 | 1 | `perf2_2226410f45abebe9eb9b` |
| open | io_in_loop | advisory | `internal/chat/channels.go` |  | 17 | 1 | `perf2_7f2432e7cd92cfea262d` |
| open | io_in_loop | advisory | `internal/ws/hub.go` | internal/ws/hub.go::Client::handle | 17 | 1 | `perf2_80f1a02953b88da8e734` |
| open | io_in_loop | advisory | `internal/chat/handler.go` |  | 17 | 1 | `perf2_c22d3e97f264f5b2154b` |
| open | io_in_loop | advisory | `internal/chat/handler.go` | internal/chat/handler.go::Handler::getThread | 17 | 1 | `perf2_d3aadd0b2b98b0543c0b` |
| open | io_in_loop | advisory | `internal/chat/channels.go` |  | 17 | 1 | `perf2_db6b81e1fad3006afb5e` |
| open | io_in_loop | advisory | `internal/server/helpers_integration_test.go` | internal/server/helpers_integration_test.go::client::post | 15 | 1 | `perf2_aaa3dc4970334e9e9d34` |
| open | io_in_loop | advisory | `internal/server/helpers_integration_test.go` | internal/server/helpers_integration_test.go::client::get | 15 | 1 | `perf2_b343f6965bd4d75f85a2` |
| open | io_in_loop | expected | `web/vite.config.js` |  | 15 | 1 | `perf2_2270823bbd70035af8bd` |
| open | io_in_loop | expected | `web/vite.config.js` |  | 15 | 1 | `perf2_a5b9de1854b0a5848f8b` |
| open | io_in_loop | expected | `web/vite.config.js` |  | 15 | 1 | `perf2_ebb4795f82d856ff2bb6` |
| open | goroutine_in_unbounded_loop | investigate | `internal/sfu/sfu.go` |  | 13 | 1 | `perf2_632d00e59b0edc158aa5` |
| open | string_concat_in_loop | advisory | `internal/httpx/middleware.go` |  | 12 | 1 | `perf2_02293c5d9a85b1d3d348` |
| open | string_concat_in_loop | advisory | `internal/httpx/middleware.go` |  | 12 | 1 | `perf2_e5972cb759fbb220d29e` |
| open | defer_in_loop | investigate | `web/web.go` |  | 12 | 1 | `perf2_17d4c442f2cbfb65ec90` |
| resolved | io_in_loop | advisory | `internal/chat/handler.go` |  | 17 | 1 | `perf2_5441dec8e58085ff1ea6` |
| resolved | io_in_loop | advisory | `internal/chat/handler.go` |  | 17 | 1 | `perf2_f9f875c5d8243d69d351` |
| resolved | io_in_loop | expected | `web/vite.config.js` |  | 15 | 1 | `perf2_05ad4e2bd836b5f48f4f` |
| resolved | io_in_loop | expected | `web/vite.config.js` |  | 15 | 1 | `perf2_4674de30a4d703c9397f` |
| resolved | goroutine_in_unbounded_loop | investigate | `internal/sfu/sfu.go` |  | 13 | 1 | `perf2_40f5e42c2b19cc5d058f` |
| resolved | defer_in_loop | investigate | `web/web.go` |  | 12 | 1 | `perf2_6c2a390c3fa69edf05bc` |

## Dead Code im Index (31)

| kind | Datei | Symbol | Zeilen | Confidence | safe | Grund |
| --- | --- | --- | --- | ---: | --- | --- |
| unreachable_file | `scripts/git-hooks/pre-commit` |  |  | 0.40 | False | File has no importers (in_degree=0) |
| unreachable_file | `web/src/third_party/deepfilternet3/worklet.js` |  |  | 0.40 | False | File has no importers (in_degree=0) |
| unused_export | `internal/auth/handler.go` | LoginRequest | 436-440 | 0.40 | False | 'LoginRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `internal/auth/handler.go` | RegisterRequest | 443-451 | 0.40 | False | 'RegisterRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `internal/auth/handler.go` | ChangePasswordRequest | 459-462 | 0.40 | False | 'ChangePasswordRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `internal/auth/handler.go` | UpdateProfileRequest | 465-469 | 0.40 | False | 'UpdateProfileRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `internal/auth/handler.go` | SetLocaleRequest | 472-474 | 0.40 | False | 'SetLocaleRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `internal/auth/handler.go` | SetPresenceRequest | 477-481 | 0.40 | False | 'SetPresenceRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `internal/auth/handler.go` | SetStatusRequest | 484-487 | 0.40 | False | 'SetStatusRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `internal/auth/invites.go` | CreateInviteRequest | 209-216 | 0.40 | False | 'CreateInviteRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `internal/auth/users_admin.go` | SetPasswordRequest | 404-406 | 0.40 | False | 'SetPasswordRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `internal/chat/handler.go` | CreateMessageRequest | 909-915 | 0.40 | False | 'CreateMessageRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `internal/chat/handler.go` | EditMessageRequest | 918-920 | 0.40 | False | 'EditMessageRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `internal/chat/handler.go` | ReactionRequest | 923-926 | 0.40 | False | 'ReactionRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `internal/chat/handler.go` | MarkReadRequest | 953-956 | 0.40 | False | 'MarkReadRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `internal/chat/handler.go` | MarkUnreadRequest | 959-961 | 0.40 | False | 'MarkUnreadRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `internal/chat/handler.go` | SetNotifyLevelRequest | 964-966 | 0.40 | False | 'SetNotifyLevelRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `internal/chat/handler.go` | CreateCategoryRequest | 969-972 | 0.40 | False | 'CreateCategoryRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `internal/chat/handler.go` | RenameCategoryRequest | 975-977 | 0.40 | False | 'RenameCategoryRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `internal/chat/handler.go` | CreateChannelRequest | 986-992 | 0.40 | False | 'CreateChannelRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `internal/chat/handler.go` | UpdateChannelRequest | 996-999 | 0.40 | False | 'UpdateChannelRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `internal/chat/handler.go` | LayoutRequest | 1003-1006 | 0.40 | False | 'LayoutRequest' is not imported, but is named elsewhere in the repo |
| unused_export | `web/src/composables/useNavMenus.js` | NOTIFY_LEVELS | 17-17 | 0.40 | False | Public symbol 'NOTIFY_LEVELS' has no importers |
| unused_export | `web/src/lib/clock.js` | serverOffset | 6-6 | 0.40 | False | Public symbol 'serverOffset' has no importers |
| unused_export | `web/src/lib/mentionQuery.js` | GROUP_MENTIONS | 23-23 | 0.40 | False | Public symbol 'GROUP_MENTIONS' has no importers |
| unused_export | `web/src/lib/presence.js` | STATUSES | 9-9 | 0.40 | False | Public symbol 'STATUSES' has no importers |
| unused_export | `web/src/lib/router.js` | navigateTo | 64-73 | 0.40 | False | Public symbol 'navigateTo' has no importers |
| unused_export | `web/src/stores/voice.js` | USER_VOLUME_MIN | 15-15 | 0.40 | False | Public symbol 'USER_VOLUME_MIN' has no importers |
| unused_export | `web/src/stores/voice.js` | USER_VOLUME_MAX | 16-16 | 0.40 | False | Public symbol 'USER_VOLUME_MAX' has no importers |
| unused_export | `web/src/stores/voice.js` | USER_VOLUME_DEFAULT | 17-17 | 0.40 | False | Public symbol 'USER_VOLUME_DEFAULT' has no importers |
| zombie_package | `e2e` |  |  | 0.50 | False | Package 'e2e' has no importers from other packages |

## Decisions (47 proposed)

Keine ist akzeptiert. Zusammen mit den 37 Hotspots sind das die 84 der Overview.

### D1. Use cryptographic library for e2e password generation

- id: `9d6fab5e6075eed055a5e56cfa017efa`
- status: proposed, kind: architectural, confidence: 0.711
- affected: `e2e/tests/smoke.spec.js`
- decision: Generate e2e passwords using node:crypto instead of Math.random

### D2. Anchor path prefixes

- id: `c10dd0b80271a63a942dd3c5097a6db2`
- status: proposed, kind: architectural, confidence: 0.633
- decision: Anchor path prefixes

### D3. Anchor path prefixes in i18n scanner

- id: `effea097f85d9cd6bad1e5b1ab669e7d`
- status: proposed, kind: architectural, confidence: 0.633
- affected: `web/src/i18n/noHardcodedText.test.js`
- decision: Anchor path prefixes

### D4. Enhanced HTML and script tag parsing in i18n scanner

- id: `f85d1ddc30e0ce65c189b9620f4c6fa4`
- status: proposed, kind: architectural, confidence: 0.633
- decision: Strip HTML comments to fixpoint, anchor path prefixes, match script tags case-insensitively in i18n scanner, replace stripped comments with space

### D5. Match script tags case-insensitively in i18n scanner

- id: `7a33d1de0db18e8b56105aa11d939431`
- status: proposed, kind: architectural, confidence: 0.633
- affected: `web/src/i18n/noHardcodedText.test.js`
- decision: Match script tags case-insensitively in the i18n scanner

### D6. Strip HTML comments to fixpoint and replace with space in i18n scanner

- id: `70b99fc118e156bfa5bd46aaad77ebb0`
- status: proposed, kind: architectural, confidence: 0.633
- affected: `web/src/i18n/noHardcodedText.test.js`
- decision: Strip HTML comments to a fixpoint and replace stripped HTML comments with a space

### D7. Strip HTML comments to fixpoint in i18n scanner

- id: `88d9c66a1cd4c32c00ff544a76ae6446`
- status: proposed, kind: architectural, confidence: 0.633
- affected: `web/src/i18n/noHardcodedText.test.js`
- decision: Strip HTML comments to a fixpoint and replace with a space

### D8. Strip and space-replace HTML comments in i18n scanner

- id: `c4a9fe430b67b8a95724ebb3d884b918`
- status: proposed, kind: architectural, confidence: 0.633
- affected: `web/src/i18n/noHardcodedText.test.js`
- decision: Strip HTML comments to a fixpoint and replace with space

### D9. Message search with pg_trgm indexing for performance

- id: `b1c03d154a1a0f9b84e4b24e43d3261b`
- status: proposed, kind: architectural, confidence: 0.567
- affected: `internal/chat/search.go`
- decision: Use pg_trgm GIN index for substring search

### D10. Message search with pg_trgm text index optimization

- id: `fc65dc9a15569bc964d298eb2a8e1c2a`
- status: proposed, kind: architectural, confidence: 0.567
- affected: `internal/chat/search.go`
- decision: Implement server-side message search using pg_trgm GIN index for fast case-insensitive substring matching

### D11. Never trust authorization data in token

- id: `8a28541bd988f9a07ead7373a5286f2a`
- status: proposed, kind: architectural, confidence: 0.567
- evidence: `internal/auth/token.go`
- affected: `internal/auth/token.go`
- decision: Authorization data (role, names) deliberately not trusted from token; middleware reloads user on every request

### D12. Substring search with pg_trgm GIN index

- id: `e15c7fc77b6d6fb382e83b3ea86e9d51`
- status: proposed, kind: architectural, confidence: 0.567
- affected: `internal/chat/search.go`

### D13. Authorization Data Validation on Every Request

- id: `a2cc19b5b66d4d0c7ff8410e7bbaeefe`
- status: proposed, kind: architectural, confidence: 0.533
- evidence: `internal/auth/token.go`
- affected: `internal/auth/token.go`
- decision: Do not trust authorization data (role, names) from the token; reload user information on every request

### D14. Differentiate stream IDs by track type

- id: `dbe9332a3530bd813060e4939b1d3ec6`
- status: proposed, kind: architectural, confidence: 0.533
- evidence: `internal/sfu/sfu.go`
- affected: `internal/sfu/sfu.go`
- decision: Audio and screen tracks use user ID as stream ID; camera tracks use CameraStreamPrefix + user ID

### D15. Prevent stopped video re-publishing from delayed packets

- id: `e85005ad86cdbce83fe86f6d478a1080`
- status: proposed, kind: architectural, confidence: 0.533
- evidence: `internal/sfu/sfu.go`
- affected: `internal/sfu/sfu.go`
- decision: Implement resumeHoldoff mechanism to prevent re-publishing from late packets

### D16. Rate-limit keyframe requests per forwarded track

- id: `01b2e356bf82a650e041603a6574d3a9`
- status: proposed, kind: architectural, confidence: 0.533
- evidence: `internal/sfu/sfu.go`
- affected: `internal/sfu/sfu.go`
- decision: keyframeMinInterval rate-limits keyframe requests per forwarded track

### D17. Rate-limit keyframe requests per track

- id: `d270668ad9570ab48dee58b65afec8e3`
- status: proposed, kind: architectural, confidence: 0.533
- evidence: `internal/sfu/sfu.go`
- affected: `internal/sfu/sfu.go`
- decision: Implement keyframeMinInterval to rate-limit keyframe requests per forwarded track

### D18. Backup and restore with safe verification mode

- id: `eea6bb456f100239a7c532809c76f1d4`
- status: proposed, kind: architectural, confidence: 0.51
- affected: `scripts/backup.sh`, `scripts/restore.sh`
- decision: Implement backup.sh and restore.sh with --verify mode that restores to scratch database without touching live data

### D19. Dependency-free i18n with per-account language

- id: `32bda2c8fb4913039a3c14698b6d009f`
- status: proposed, kind: architectural, confidence: 0.51
- affected: `web/src/App.vue`
- decision: Implement dependency-free internationalization system with per-account language preference, switcher in account menu and login

### D20. HTTP range requests and cascade deletion for media

- id: `8fc1270ea5a3a6f1bc330c9b42a6d388`
- status: proposed, kind: architectural, confidence: 0.51
- affected: `internal/chat/channels.go`, `internal/chat/messages.go`, `internal/media/handler.go`, `internal/media/objectreader.go`, `internal/media/store.go`
- decision: Serve media through http.ServeContent with lazily fetched, seekable object readers supporting HTTP range requests (206 responses) and ETags for conditional requests; cascade delete attached media objects from storage when messages or channels are deleted

### D21. Mandatory Explicit Secrets and Rate Limiting

- id: `4633a67485d86da8e931dd3a257d7c77`
- status: proposed, kind: architectural, confidence: 0.51
- affected: `cmd/server/main.go`, `internal/config/config.go`, `internal/db/db.go`, `internal/sfu/sfu.go`
- decision: Remove all hardcoded secret defaults; production requires explicit DATABASE_URL, JWT_SECRET, ADMIN_INITIAL_PASSWORD, and S3 keys; implement in-memory rate limits on login (per IP and username), registration, and password change; configure STUN servers via WEBRTC_STUN_URLS (defaults to none for privacy)

### D22. Message search with pg_trgm GIN index strategy

- id: `e7983ae5962f4d2efd792f411f1e87bf`
- status: proposed, kind: architectural, confidence: 0.51
- affected: `internal/chat/search.go`
- decision: Implement server-side message search using pg_trgm GIN index for fast substring matching

### D23. Production secrets security: remove hardcoded defaults, enforce configuration

- id: `29f6bc870ca80ccc007887aec91417c7`
- status: proposed, kind: architectural, confidence: 0.51
- affected: `cmd/server/main.go`, `internal/config/config.go`
- decision: Remove hardcoded secret defaults; production refuses to start unless JWT_SECRET, ADMIN_INITIAL_PASSWORD, DATABASE_URL, and S3 keys are properly configured; empty ADMIN_INITIAL_PASSWORD generates a random value on first seed

### D24. Publisher-Initiated Keyframe Requests

- id: `10ba965c598804ad2f47ca1f9f724137`
- status: proposed, kind: architectural, confidence: 0.51
- affected: `internal/sfu/sfu.go`
- decision: Forward subscriber PLI/FIR keyframe requests to publisher; request keyframe immediately when viewer subscribes instead of waiting

### D25. Security architecture: HttpOnly cookies, CSRF protection, rate limiting, strict parsing

- id: `470077e86924b4bedf422173e2bd4677`
- status: proposed, kind: architectural, confidence: 0.51
- affected: `internal/auth/handler.go`, `internal/auth/service.go`, `internal/auth/session.go`, `internal/auth/token.go`, `internal/config/config.go`, `internal/httpx/middleware.go`, `internal/httpx/ratelimit.go`
- decision: Implement HttpOnly session cookies with token_version revocation (no tokens in localStorage or WebSocket URL); CSRF origin checking and exact CORS allowlist; security headers and CSP; request IDs and trusted-proxy client IP; rate limiting with escalating login lockout; strict JSON decoding; MIME type validation for uploads; authenticated upload serving; inline only for raster images/video/audio

### D26. WebRTC SFU Peer Connection Ownership and Track Scoping

- id: `5837feb6f341e2183dfdfcaf78ea31fb`
- status: proposed, kind: architectural, confidence: 0.51
- affected: `internal/sfu/sfu.go`, `internal/ws/hub.go`
- decision: Restructure so each WebRTC connection owns its peer independently; forward keyframe requests (PLI/FIR) to publishers; scope forwarded track IDs to publishing user; register Pion default interceptors (NACK, RTCP, TWCC) for packet recovery

### D27. Enable bulk session revocation via TokenVersion

- id: `12bf3e17cac7e9b60d928f002ba4c7fc`
- status: proposed, kind: architectural, confidence: 0.5
- evidence: `internal/auth/token.go`
- affected: `internal/auth/token.go`

### D28. Local Track ID Scoping to Publisher

- id: `eb30b5dfd14bca1241e5994080086eea`
- status: proposed, kind: architectural, confidence: 0.5
- evidence: `internal/sfu/sfu.go`
- affected: `internal/sfu/sfu.go`
- decision: Key trackLocals by the local track ID scoped to the publishing user

### D29. Session revocation via TokenVersion

- id: `11242046ad17245464182e1db395c5ae`
- status: proposed, kind: architectural, confidence: 0.5
- evidence: `internal/auth/token.go`
- affected: `internal/auth/token.go`

### D30. Improve i18n scanner HTML and script tag handling

- id: `e4dddc4a49bd6db1099b06a52bfebac4`
- status: proposed, kind: architectural, confidence: 0.472
- decision: Strip HTML comments to a fixpoint and replace with space; anchor path prefixes; match script tags case-insensitively in i18n scanner

### D31. Prevent late packets from restarting stopped video

- id: `6d5dc9d129af094824e8de9df07efc38`
- status: proposed, kind: architectural, confidence: 0.467
- evidence: `internal/sfu/sfu.go`
- affected: `internal/sfu/sfu.go`

### D32. Trigger renegotiation when tracks change during pending offer

- id: `94b11de5304ff6eec8fb77c6559cf7e6`
- status: proposed, kind: architectural, confidence: 0.467
- evidence: `internal/sfu/sfu.go`
- affected: `internal/sfu/sfu.go`

### D33. Architectural restructure with security hardening and package organization

- id: `b270f1bee090e741ed5ee307ca558d3a`
- status: proposed, kind: architectural, confidence: 0.453
- affected: `cmd/server/main.go`, `internal/auth/handler.go`, `internal/auth/invites.go`, `internal/auth/service.go`, `internal/auth/session.go`, `internal/auth/token.go`, `internal/chat/access.go`, `internal/chat/channels.go`, `internal/chat/handler.go`, `internal/chat/members.go`, `internal/chat/messages.go`, `internal/config/config.go` (+4)
- decision: Security: HttpOnly session cookies with token_version revocation (no token in localStorage or WebSocket URL), CSRF origin check, strict CORS allowlist, security headers with CSP, request IDs, client IP from trusted proxies only, rate limits and escalating login lockout, strict JSON decoding, MIME sniffing prevention with allowlist for uploads, authenticated upload serving, no hardcoded secrets (production refuses placeholder values), media retention opt-in. Structure: New packages (httpx, auth, chat, media, events, server) modelled on mnema.xyz, embedded versioned migrations with advisory lock. Features: Discord-style replies with quote and jump-to-message, cursor-based history paging (before/after/around at 15k scale), resizable panes. Honest UI: real RTC stats, no E2E claims, self-hosted fonts.

### D34. Connection-Owned Peer Lifecycle Management

- id: `6a6ecfa59843e6f359748ed73865d8a1`
- status: proposed, kind: architectural, confidence: 0.453
- affected: `internal/sfu/sfu.go`, `internal/ws/hub.go`
- decision: Each WebSocket connection owns its peer; rejoin or late unregister of old socket does not affect other connections

### D35. Eliminate hardcoded secrets and add authentication rate limiting

- id: `ff571b93d7edadfa9e3583ee6b3910c5`
- status: proposed, kind: architectural, confidence: 0.453
- affected: `cmd/server/main.go`, `internal/config/config.go`, `internal/db/db.go`, `internal/sfu/sfu.go`, `web/src/components/LegalModal.vue`, `web/src/components/UserProfileModal.vue`, `web/src/composables/useWebRTC.js`, `web/src/stores/auth.js`
- decision: Production refuses to start with missing or placeholder values for JWT_SECRET, ADMIN_INITIAL_PASSWORD, DATABASE_URL, S3 keys. Empty ADMIN_INITIAL_PASSWORD generates random password on first seed. In-memory rate limits on login (per IP and per username), registration, and password change. Registration no longer leaks internal errors. PUT /api/auth/password endpoint for user password changes. WEBRTC_STUN_URLS configurable with default none (no Google STUN). Privacy policy template configured with retention/session periods and DSGVO Art. 21/7(3)/77 rights disclosure.

### D36. HTTP range request support for media streaming

- id: `de42e95588d27405df1892cf1a8d005e`
- status: proposed, kind: architectural, confidence: 0.453
- affected: `internal/media/handler.go`, `internal/media/objectreader.go`, `internal/s3/s3.go`
- decision: Serve media via http.ServeContent with lazily fetched, seekable object reader; implement HTTP 206 range requests that fetch only requested bytes from S3; use ETag for conditional requests

### D37. Implement cascading deletion of media objects with parent messages and channels

- id: `990b224ccf3112aa6cf4f6da54b7a5cd`
- status: proposed, kind: architectural, confidence: 0.453
- affected: `internal/chat/channels.go`, `internal/chat/messages.go`, `internal/media/store.go`
- decision: When a message (or thread reply) or channel is deleted, cascade the deletion to remove all attached media objects from storage instead of orphaning them

### D38. No Built-In Defaults for Sensitive Configuration

- id: `f41544b6a98b87547a9397a9b6a2b865`
- status: proposed, kind: architectural, confidence: 0.453
- affected: `internal/config/config.go`
- decision: Production refuses to start with missing or example values for JWT_SECRET, ADMIN_INITIAL_PASSWORD, DATABASE_URL, S3 keys

### D39. Privacy-First WebRTC STUN Configuration

- id: `44dfbb238bbc67a21dbf904d0bba7391`
- status: proposed, kind: architectural, confidence: 0.453
- affected: `internal/config/config.go`, `internal/sfu/sfu.go`
- decision: STUN servers configurable via WEBRTC_STUN_URLS environment variable; defaults to none (no Google STUN)

### D40. RTC Packet Recovery and Bandwidth Estimation via Interceptors

- id: `5cd19f83de5474cfc66a75097c9f4ed2`
- status: proposed, kind: architectural, confidence: 0.453
- affected: `internal/sfu/sfu.go`
- decision: Register Pion's default interceptors: NACK both directions, RTCP reports, TWCC feedback

### D41. Request Security Middleware Stack

- id: `2ced42074c1046e1b6c7f90e5bcd1eaf`
- status: proposed, kind: architectural, confidence: 0.453
- affected: `internal/config/config.go`, `internal/httpx/middleware.go`, `internal/httpx/ratelimit.go`
- decision: Security headers, CSP, CORS exact allowlist, client IP from trusted proxies, rate limits, and strict JSON decoding at middleware layer

### D42. SFU peer and connection lifecycle restructuring

- id: `2b62d71aac7207e020e2b4c5b80ae718`
- status: proposed, kind: architectural, confidence: 0.453
- affected: `internal/sfu/sfu.go`, `internal/ws/hub.go`
- decision: Register Pion's RTCP interceptors (NACK, TWCC, reports) for packet recovery and bandwidth estimation; forward keyframe requests (PLI/FIR) from subscribers to publishers; request keyframe when new viewer subscribes; scope forwarded track IDs to publishing user ID; manage peers per connection instead of per rejoin; join rooms under SFU lock; renegotiate only from stable signaling state

### D43. SFU peer management and keyframe routing restructure

- id: `488fc355677f7b1a2d95c432ce887923`
- status: proposed, kind: architectural, confidence: 0.453
- affected: `internal/server/ws_integration_test.go`, `internal/sfu/sfu.go`, `internal/sfu/sfu_test.go`, `internal/ws/hub.go`
- decision: Registered Pion default interceptors for NACK, RTCP, TWCC feedback. Forward PLI/FIR keyframe requests to publishers. Request keyframe when viewer subscribes. Route webrtc_request_keyframe to publishers instead of back to viewer. Each connection owns its peer. Scope track IDs to publishing user (stream ID = user ID). Join rooms under SFU lock. Renegotiate only from stable signaling state.

### D44. Screen audio mixing, per-user volume control, and camera stream architecture for WebRTC voice

- id: `c3de05f74b2fae23480467f37bbb3f87`
- status: proposed, kind: architectural, confidence: 0.453
- affected: `web/src/composables/useWebRTC.js`, `web/src/stores/voice.js`
- decision: Screen audio is mixed with microphone input into a single sent track using replaceTrack (avoiding renegotiation); gate and mute affect only the microphone portion of the mix. Per-user volume implemented with gain nodes (0-200% range) persisted in localStorage. Cameras use separate senders; remote cameras mapped to publishers via SFU stream IDs.

### D45. Session Token Security Architecture

- id: `16097858f291b70a41574858621577a8`
- status: proposed, kind: architectural, confidence: 0.453
- affected: `internal/auth/session.go`, `internal/auth/token.go`, `internal/httpx/middleware.go`
- decision: HttpOnly session cookies with token_version revocation instead of localStorage or WebSocket URL tokens

### D46. WebRTC media architecture: unified audio mixing via replaceTrack with per-user remote volume and separate camera sender

- id: `010262cfdfeff974c9edcb536cb67632`
- status: proposed, kind: architectural, confidence: 0.453
- affected: `web/src/composables/useWebRTC.js`, `web/src/stores/voice.js`
- decision: Screen audio and processed microphone are mixed into a single track using replaceTrack (no renegotiation); gate and mute control only microphone input to the mix; remote audio uses gain nodes for per-user volume control (0-200%, persisted in localStorage); camera operates as a separate sender alongside screen share; remote cameras are mapped to publishers via SFU stream IDs

### D47. WebRTC media stream architecture: unified audio mixing with per-user volume and separated camera sender

- id: `8d6619b120028491bfca468e867d8589`
- status: proposed, kind: architectural, confidence: 0.453
- affected: `web/src/composables/useWebRTC.js`, `web/src/stores/voice.js`
- decision: Mix screen audio with processed microphone into single track using replaceTrack (no renegotiation); implement per-user remote audio volume control (0-200%, gain node above 100%) with local mute, persisted in localStorage; separate camera sender alongside screen share; map remote cameras to publishers via SFU stream IDs

## Ungoverned Hotspots (37)

Produktions-Hotspots ohne akzeptierte Decision. Dieselben Dateien tragen im Health-Index `ungoverned_hotspot`, wo der Typ dort feuert.

- `cmd/server/main.go`
- `internal/auth/handler.go`
- `internal/auth/service.go`
- `internal/auth/users_admin.go`
- `internal/chat/channels.go`
- `internal/chat/handler.go`
- `internal/chat/messages.go`
- `internal/config/config.go`
- `internal/db/db.go`
- `internal/httpx/middleware.go`
- `internal/linkpreview/linkpreview.go`
- `internal/media/handler.go`
- `internal/s3/s3.go`
- `internal/server/server.go`
- `internal/sfu/sfu.go`
- `internal/ws/hub.go`
- `web/src/App.vue`
- `web/src/components/AdminDashboard.vue`
- `web/src/components/AudioSettingsModal.vue`
- `web/src/components/ChatArea.vue`
- `web/src/components/ConnectionStatsModal.vue`
- `web/src/components/CreateChannelModal.vue`
- `web/src/components/LegalModal.vue`
- `web/src/components/LoginModal.vue`
- `web/src/components/MarkdownContent.vue`
- `web/src/components/MemberList.vue`
- `web/src/components/SearchModal.vue`
- `web/src/components/Sidebar.vue`
- `web/src/components/ThreadSidebar.vue`
- `web/src/components/UserBar.vue`
- `web/src/components/UserProfileModal.vue`
- `web/src/components/VoiceStage.vue`
- `web/src/composables/useNavMenus.js`
- `web/src/composables/useWebRTC.js`
- `web/src/stores/auth.js`
- `web/src/stores/chat.js`
- `web/src/stores/voice.js`

Gezählt: 37.

## Ownership (142)

Codedateien, nicht `history_only`, Primär-Autor über 80 % der Commits. E-Mail steht absichtlich nicht hier.

| # | Datei | Anteil | Commits 90 Tage | Hotspot |
| ---: | --- | ---: | ---: | --- |
| 1 | `cmd/server/main.go` | 86% | 14 | ja |
| 2 | `internal/chat/handler.go` | 88% | 8 | ja |
| 3 | `internal/db/db.go` | 88% | 8 | ja |
| 4 | `internal/auth/handler.go` | 86% | 7 | ja |
| 5 | `web/src/components/LoginModal.vue` | 86% | 7 | ja |
| 6 | `web/src/composables/useWebRTC.test.js` | 100% | 7 | ja |
| 7 | `internal/sfu/sfu_test.go` | 100% | 6 | ja |
| 8 | `internal/auth/users_admin.go` | 100% | 5 | ja |
| 9 | `internal/linkpreview/linkpreview.go` | 100% | 3 | ja |
| 10 | `internal/ws/hub_test.go` | 100% | 3 | ja |
| 11 | `web/src/components/SearchModal.vue` | 100% | 3 | ja |
| 12 | `web/src/components/VoiceStage.test.js` | 100% | 3 | ja |
| 13 | `web/src/composables/useNavMenus.js` | 100% | 3 | ja |
| 14 | `web/src/components/ParticipantTile.vue` | 100% | 5 |  |
| 15 | `web/src/components/AudioSettingsModal.test.js` | 100% | 4 |  |
| 16 | `web/src/components/VoiceStatusPanel.vue` | 100% | 4 |  |
| 17 | `internal/chat/readstate.go` | 100% | 3 |  |
| 18 | `internal/server/metrics.go` | 100% | 3 |  |
| 19 | `web/src/components/UserBar.test.js` | 100% | 3 |  |
| 20 | `web/src/components/smoke.test.js` | 100% | 3 |  |
| 21 | `web/src/lib/thirdParty.js` | 100% | 3 |  |
| 22 | `web/src/stores/voice.test.js` | 100% | 3 |  |
| 23 | `api/api.go` | 100% | 2 |  |
| 24 | `internal/linkpreview/handler.go` | 100% | 2 |  |
| 25 | `internal/linkpreview/linkpreview_test.go` | 100% | 2 |  |
| 26 | `internal/server/metrics_test.go` | 100% | 2 |  |
| 27 | `internal/server/openapi_test.go` | 100% | 2 |  |
| 28 | `internal/server/presence_integration_test.go` | 100% | 2 |  |
| 29 | `internal/server/search_integration_test.go` | 100% | 2 |  |
| 30 | `internal/server/users_admin_integration_test.go` | 100% | 2 |  |
| 31 | `internal/testutil/db.go` | 100% | 2 |  |
| 32 | `web/src/components/AdminDashboard.test.js` | 100% | 2 |  |
| 33 | `web/src/components/ContextMenu.vue` | 100% | 2 |  |
| 34 | `web/src/components/LinkPreviewCard.vue` | 100% | 2 |  |
| 35 | `web/src/components/MemberRow.vue` | 100% | 2 |  |
| 36 | `web/src/composables/useNavMenus.test.js` | 100% | 2 |  |
| 37 | `web/src/lib/router.js` | 100% | 2 |  |
| 38 | `web/src/lib/router.test.js` | 100% | 2 |  |
| 39 | `web/src/stores/chatComfort.test.js` | 100% | 2 |  |
| 40 | `web/src/stores/connection.test.js` | 100% | 2 |  |
| 41 | `web/web_test.go` | 100% | 2 |  |
| 42 | `e2e/playwright.config.js` | 100% | 1 |  |
| 43 | `e2e/run.sh` | 100% | 1 |  |
| 44 | `internal/auth/session.go` | 100% | 1 |  |
| 45 | `internal/auth/token.go` | 100% | 1 |  |
| 46 | `internal/auth/token_test.go` | 100% | 1 |  |
| 47 | `internal/chat/access.go` | 100% | 1 |  |
| 48 | `internal/chat/mentions.go` | 100% | 1 |  |
| 49 | `internal/chat/mentions_test.go` | 100% | 1 |  |
| 50 | `internal/chat/search.go` | 100% | 1 |  |
| 51 | `internal/chat/validate_test.go` | 100% | 1 |  |
| 52 | `internal/events/events.go` | 100% | 1 |  |
| 53 | `internal/httpx/ratelimit.go` | 100% | 1 |  |
| 54 | `internal/httpx/request.go` | 100% | 1 |  |
| 55 | `internal/httpx/request_test.go` | 100% | 1 |  |
| 56 | `internal/media/objectreader.go` | 100% | 1 |  |
| 57 | `internal/media/policy.go` | 100% | 1 |  |
| 58 | `internal/media/policy_test.go` | 100% | 1 |  |
| 59 | `internal/server/doc.go` | 100% | 1 |  |
| 60 | `internal/server/layout_integration_test.go` | 100% | 1 |  |
| 61 | `internal/server/readstate_integration_test.go` | 100% | 1 |  |
| 62 | `internal/server/turn_test.go` | 100% | 1 |  |
| 63 | `internal/sfu/announce.go` | 100% | 1 |  |
| 64 | `internal/sfu/announce_test.go` | 100% | 1 |  |
| 65 | `internal/sfu/subscriptions.go` | 100% | 1 |  |
| 66 | `internal/sfu/subscriptions_test.go` | 100% | 1 |  |
| 67 | `internal/tools/openapifix/main.go` | 100% | 1 |  |
| 68 | `internal/ws/presence_test.go` | 100% | 1 |  |
| 69 | `scripts/backup.sh` | 100% | 1 |  |
| 70 | `scripts/health-check.sh` | 100% | 1 |  |
| 71 | `scripts/restore.sh` | 100% | 1 |  |
| 72 | `scripts/scorecard.mjs` | 100% | 1 |  |
| 73 | `web/postcss.config.js` | 100% | 1 |  |
| 74 | `web/src/apiDocs.js` | 100% | 1 |  |
| 75 | `web/src/components/AccountMenu.test.js` | 100% | 1 |  |
| 76 | `web/src/components/AccountMenu.vue` | 100% | 1 |  |
| 77 | `web/src/components/ActivityStats.vue` | 100% | 1 |  |
| 78 | `web/src/components/BaseDialog.test.js` | 100% | 1 |  |
| 79 | `web/src/components/BaseDialog.vue` | 100% | 1 |  |
| 80 | `web/src/components/ChatArea.test.js` | 100% | 1 |  |
| 81 | `web/src/components/ConfirmDialog.test.js` | 100% | 1 |  |
| 82 | `web/src/components/ConfirmDialog.vue` | 100% | 1 |  |
| 83 | `web/src/components/ConnectionBanner.test.js` | 100% | 1 |  |
| 84 | `web/src/components/ConnectionBanner.vue` | 100% | 1 |  |
| 85 | `web/src/components/ContextMenu.test.js` | 100% | 1 |  |
| 86 | `web/src/components/EditNameDialog.vue` | 100% | 1 |  |
| 87 | `web/src/components/EmojiButton.vue` | 100% | 1 |  |
| 88 | `web/src/components/EmojiPicker.vue` | 100% | 1 |  |
| 89 | `web/src/components/GlobalSearch.vue` | 100% | 1 |  |
| 90 | `web/src/components/ImageLightbox.vue` | 100% | 1 |  |
| 91 | `web/src/components/MentionSuggestions.vue` | 100% | 1 |  |
| 92 | `web/src/components/PresenceDot.vue` | 100% | 1 |  |
| 93 | `web/src/components/PresenceMenu.vue` | 100% | 1 |  |
| 94 | `web/src/components/ReactionPalette.vue` | 100% | 1 |  |
| 95 | `web/src/components/TalkParticipants.vue` | 100% | 1 |  |
| 96 | `web/src/components/ToastHost.test.js` | 100% | 1 |  |
| 97 | `web/src/components/ToastHost.vue` | 100% | 1 |  |
| 98 | `web/src/components/VoiceTimer.vue` | 100% | 1 |  |
| 99 | `web/src/composables/useComposerAssist.js` | 100% | 1 |  |
| 100 | `web/src/composables/useComposerAssist.test.js` | 100% | 1 |  |
| 101 | `web/src/composables/useDialog.js` | 100% | 1 |  |
| 102 | `web/src/composables/useResizable.test.js` | 100% | 1 |  |
| 103 | `web/src/directives/tooltip.js` | 100% | 1 |  |
| 104 | `web/src/directives/tooltip.test.js` | 100% | 1 |  |
| 105 | `web/src/i18n/i18n.test.js` | 100% | 1 |  |
| 106 | `web/src/i18n/index.js` | 100% | 1 |  |
| 107 | `web/src/lib/api.test.js` | 100% | 1 |  |
| 108 | `web/src/lib/channelTree.test.js` | 100% | 1 |  |
| 109 | `web/src/lib/chatLogic.js` | 100% | 1 |  |
| 110 | `web/src/lib/chatLogic.test.js` | 100% | 1 |  |
| 111 | `web/src/lib/clock.js` | 100% | 1 |  |
| 112 | `web/src/lib/clock.test.js` | 100% | 1 |  |
| 113 | `web/src/lib/confirm.js` | 100% | 1 |  |
| 114 | `web/src/lib/contextMenuPoint.js` | 100% | 1 |  |
| 115 | `web/src/lib/globalSearch.js` | 100% | 1 |  |
| 116 | `web/src/lib/levelMeter.js` | 100% | 1 |  |
| 117 | `web/src/lib/levelMeter.test.js` | 100% | 1 |  |
| 118 | `web/src/lib/linkPreview.js` | 100% | 1 |  |
| 119 | `web/src/lib/linkPreview.test.js` | 100% | 1 |  |
| 120 | `web/src/lib/mentionQuery.js` | 100% | 1 |  |
| 121 | `web/src/lib/mentionQuery.test.js` | 100% | 1 |  |
| 122 | `web/src/lib/messageGrouping.test.js` | 100% | 1 |  |
| 123 | `web/src/lib/messageWindow.js` | 100% | 1 |  |
| 124 | `web/src/lib/micMetering.js` | 100% | 1 |  |
| 125 | `web/src/lib/micMetering.test.js` | 100% | 1 |  |
| 126 | `web/src/lib/noiseSuppressor.js` | 100% | 1 |  |
| 127 | `web/src/lib/noiseSuppressor.test.js` | 100% | 1 |  |
| 128 | `web/src/lib/ping.js` | 100% | 1 |  |
| 129 | `web/src/lib/ping.test.js` | 100% | 1 |  |
| 130 | `web/src/lib/presence.js` | 100% | 1 |  |
| 131 | `web/src/lib/presence.test.js` | 100% | 1 |  |
| 132 | `web/src/lib/replies.test.js` | 100% | 1 |  |
| 133 | `web/src/lib/rtcStats.test.js` | 100% | 1 |  |
| 134 | `web/src/lib/soundEffects.js` | 100% | 1 |  |
| 135 | `web/src/lib/soundEffects.test.js` | 100% | 1 |  |
| 136 | `web/src/lib/thirdParty.test.js` | 100% | 1 |  |
| 137 | `web/src/lib/voiceSession.test.js` | 100% | 1 |  |
| 138 | `web/src/stores/auth.test.js` | 100% | 1 |  |
| 139 | `web/src/stores/toast.js` | 100% | 1 |  |
| 140 | `web/src/stores/toast.test.js` | 100% | 1 |  |
| 141 | `web/src/test-setup.js` | 100% | 1 |  |
| 142 | `web/src/third_party/deepfilternet3/worklet.js` | 100% | 1 |  |

## Commit-Health (299, historisch)

Zuschreibung eines Biomarkers an einen Commit im Index-Fenster. Keine zweite Queue. Severity-Reihenfolge des Speichers.

| sha | Art | Schwere | Biomarker | Datei | Symbol | Zeilen | Impact |
| --- | --- | --- | --- | --- | --- | --- | ---: |
| `8bf7982560` | worsened | critical | large_method | `cmd/server/main.go` | main | 29-478 | 0.76 |
| `8bf7982560` | worsened | critical | complex_method | `cmd/server/main.go` | main | 29-478 | 0.74 |
| `1335d476d6` | worsened | critical | complex_method | `internal/config/config.go` | validate | 156-213 | 1.0 |
| `a347d4eaf4` | introduced | critical | nested_complexity | `internal/linkpreview/linkpreview.go` | parseHTML | 215-267 | 2.18 |
| `379635c6ba` | introduced | critical | complex_method | `internal/server/chat_integration_test.go` | TestHistoryPagingAt15kMessages | 121-226 | 0.73 |
| `4ee831a5cc` | worsened | critical | complex_method | `internal/ws/hub.go` | readPump | 272-370 | 1.0 |
| `7fe7d37dce` | worsened | critical | complex_method | `internal/ws/hub.go` | handle | 553-626 | 0.68 |
| `1dbc7e0226` | worsened | critical | complex_method | `web/src/App.vue` | applyRoute | 79-154 | 1.3 |
| `4de94aaaf9` | introduced | critical | complex_conditional | `web/src/components/ChatArea.vue` | handleMessageKeydown | 559-559 | 2.07 |
| `4de94aaaf9` | worsened | critical | complex_method | `web/src/components/ChatArea.vue` | handleMessageKeydown | 551-590 | 0.59 |
| `5ab4760bd1` | introduced | critical | large_method | `web/src/composables/useWebRTC.js` | useWebRTC | 496-1351 | 0.55 |
| `379635c6ba` | introduced | critical | complex_method | `web/src/lib/rtcStats.js` | summarizeStats | 9-90 | 1.3 |
| `5ab4760bd1` | introduced | critical | complex_method | `web/src/lib/soundEffects.js` | playSound | 69-183 | 1.0 |
| `9d7ac2ee08` | introduced | critical | large_method | `web/src/stores/chat.js` | defineStore callback | 16-760 | 1.5 |
| `059942f6b0` | worsened | critical | large_method | `web/src/stores/voice.js` | defineStore callback | 4-235 | 0.95 |
| `56a7dd2b0f` | worsened | critical | large_method | `web/src/stores/voice.js` | defineStore callback | 30-323 | 1.12 |
| `45d82a5bcf` | introduced | critical | large_method | `web/src/third_party/deepfilternet3/worklet.js` | <anonymous> | 8-379 | 1.5 |
| `a83a15feeb` | introduced | high | large_method | `cmd/server/main.go` | main | 24-220 | 0.76 |
| `a83a15feeb` | introduced | high | complex_method | `cmd/server/main.go` | main | 24-220 | 0.74 |
| `379635c6ba` | introduced | high | complex_method | `internal/auth/service.go` | Register | 147-220 | 1.19 |
| `8bf7982560` | introduced | high | nested_complexity | `internal/chat/messages.go` | GetChannelMessages | 36-115 | 1.61 |
| `a212044284` | introduced | high | complex_method | `internal/chat/search.go` | Search | 38-103 | 1.19 |
| `437b6bbd67` | worsened | high | complex_method | `internal/chat/upload.go` | UploadHandler | 38-154 | 0.88 |
| `379635c6ba` | introduced | high | complex_method | `internal/config/config.go` | validate | 139-182 | 0.81 |
| `a347d4eaf4` | introduced | high | complex_method | `internal/linkpreview/linkpreview.go` | parseHTML | 215-267 | 0.69 |
| `379635c6ba` | introduced | high | complex_method | `internal/media/handler.go` | upload | 127-212 | 0.59 |
| `13617e8e1f` | worsened | high | complex_method | `internal/server/chat_integration_test.go` | TestMessagesAreScopedAndValidated | 15-92 | 0.36 |
| `a1d710fdc2` | introduced | high | complex_method | `internal/server/layout_integration_test.go` | TestAdminRenamesAndReordersChannels | 28-79 | 1.45 |
| `26a976f8e4` | introduced | high | complex_method | `internal/server/presence_integration_test.go` | TestVoiceTimeAndMessageCounts | 219-279 | 0.47 |
| `3d99c0de2d` | introduced | high | complex_method | `internal/server/presence_integration_test.go` | TestMentionsResolveUsersAllAndHere | 103-164 | 0.69 |
| `a212044284` | introduced | high | complex_method | `internal/server/search_integration_test.go` | TestSearchFindsMessagesWithFilters | 36-96 | 1.45 |
| `4ee831a5cc` | introduced | high | complex_method | `internal/sfu/sfu.go` | SignalPeerConnections | 179-228 | 0.81 |
| `802e451c9c` | introduced | high | brain_method | `internal/sfu/sfu.go` | JoinPeer | 216-331 | 0.6 |
| `8bf7982560` | introduced | high | complex_method | `internal/sfu/sfu.go` | JoinPeer | 82-177 | 1.19 |
| `bf2b774885` | worsened | high | complex_method | `internal/sfu/sfu.go` | JoinPeer | 198-313 | 0.58 |
| `26a976f8e4` | worsened | high | complex_method | `internal/ws/hub.go` | joinVoice | 500-560 | 0.32 |
| `379635c6ba` | introduced | high | nested_complexity | `internal/ws/hub.go` | handle | 435-491 | 1.61 |
| `379635c6ba` | introduced | high | complex_method | `internal/ws/hub.go` | handle | 435-491 | 0.95 |
| `8bf7982560` | introduced | high | nested_complexity | `internal/ws/hub.go` | readPump | 204-262 | 1.61 |
| `a1beba3fa1` | worsened | high | large_method | `internal/ws/hub.go` | readPump | 290-445 | 0.47 |
| `b0423e83f3` | worsened | high | complex_method | `internal/ws/hub.go` | readPump | 250-318 | 0.81 |
| `7c00a6041b` | introduced | high | nested_complexity | `web/src/App.vue` | applyRoute | 59-113 | 1.61 |
| `7c00a6041b` | introduced | high | complex_method | `web/src/App.vue` | applyRoute | 59-113 | 0.95 |
| `13dee3cbe1` | worsened | high | complex_method | `web/src/components/ContextMenu.vue` | handleKeydown | 111-152 | 1.45 |
| `379635c6ba` | introduced | high | large_method | `web/src/composables/useResizable.js` | useResizable | 105-238 | 1.36 |
| `56a7dd2b0f` | worsened | high | complex_method | `web/src/composables/useWebRTC.js` | setupPeerConnection | 252-336 | 0.81 |
| `aae66aac6e` | introduced | high | low_cohesion | `web/src/composables/useWebRTC.test.js` | FakePC | 19-36 | 0.6 |
| `aae66aac6e` | introduced | high | low_cohesion | `web/src/composables/useWebRTC.test.js` | FakeAudioContext | 38-45 | 0.6 |
| `aa34060a40` | introduced | high | complex_method | `web/src/lib/mentionQuery.js` | suggestMentions | 30-49 | 0.95 |
| `379635c6ba` | introduced | high | brain_method | `web/src/lib/rtcStats.js` | summarizeStats | 9-90 | 0.6 |
| `ca406a6ad4` | worsened | high | large_method | `web/src/stores/auth.js` | defineStore callback | 4-144 | 1.5 |
| `aae66aac6e` | introduced | high | low_cohesion | `web/src/stores/connection.test.js` | FakeSocket | 7-18 | 0.6 |
| `02bd61850e` | introduced | high | complex_method | `web/src/stores/voice.js` | defineStore callback | 4-228 | 0.74 |
| `02bd61850e` | introduced | high | brain_method | `web/src/stores/voice.js` | defineStore callback | 4-228 | 0.6 |
| `56a7dd2b0f` | introduced | high | brain_method | `web/src/stores/voice.js` | defineStore callback | 30-323 | 0.6 |
| `5ab4760bd1` | worsened | high | complex_method | `web/src/stores/voice.js` | defineStore callback | 64-625 | 0.55 |
| `b0423e83f3` | introduced | high | large_method | `web/src/stores/voice.js` | defineStore callback | 4-173 | 1.5 |
| `4ee831a5cc` | introduced | low | error_handling | `cmd/server/main.go` |  | 503-503 | 0.15 |
| `a83a15feeb` | introduced | low | error_handling | `cmd/server/main.go` |  | 86-86 | 0.15 |
| `c9dd53c5e1` | introduced | low | error_handling | `cmd/server/main.go` |  | 317-317 | 0.15 |
| `ca406a6ad4` | introduced | low | error_handling | `cmd/server/main.go` |  | 453-453 | 0.13 |
| `ca406a6ad4` | introduced | low | lock_in_loop | `internal/auth/ratelimit.go` | cleanup | 63-63 | 0.0 |
| `379635c6ba` | introduced | low | large_method | `internal/auth/service.go` | Register | 147-220 | 0.31 |
| `8bf7982560` | introduced | low | error_handling | `internal/chat/admin.go` |  | 174-174 | 0.15 |
| `a1beba3fa1` | introduced | low | error_handling | `internal/chat/dms.go` |  | 184-184 | 0.15 |
| `3d99c0de2d` | introduced | low | primitive_obsession | `internal/chat/mentions.go` | ResolveMentions | 49-94 | 0.15 |
| `3d99c0de2d` | introduced | low | primitive_obsession | `internal/chat/mentions.go` | RecordMentions | 115-125 | 0.15 |
| `379635c6ba` | introduced | low | complex_conditional | `internal/chat/messages.go` | ValidateEmoji | 88-88 | 0.4 |
| `3a6714ac19` | introduced | low | primitive_obsession | `internal/chat/messages.go` | collectKeys | 390-401 | 0.08 |
| `3d99c0de2d` | introduced | low | large_method | `internal/chat/messages.go` | enrich | 152-218 | 0.17 |
| `437b6bbd67` | introduced | low | primitive_obsession | `internal/chat/messages.go` | populateAttachments | 38-60 | 0.15 |
| `437b6bbd67` | introduced | low | primitive_obsession | `internal/chat/messages.go` | CreateMessage | 205-228 | 0.15 |
| `4ee831a5cc` | introduced | low | large_method | `internal/chat/messages.go` | GetChannelMessages | 116-185 | 0.31 |
| `4ee831a5cc` | introduced | low | primitive_obsession | `internal/chat/messages.go` | populateReactions | 71-94 | 0.12 |
| `8bf7982560` | introduced | low | large_method | `internal/chat/messages.go` | GetChannelMessages | 36-115 | 0.38 |
| `8bf7982560` | introduced | low | primitive_obsession | `internal/chat/messages.go` | GetChannelMessages | 36-115 | 0.15 |
| `a212044284` | introduced | low | complex_conditional | `internal/chat/search.go` | Search | 47-47 | 0.4 |
| `a212044284` | introduced | low | large_method | `internal/chat/search.go` | Search | 38-103 | 0.31 |
| `437b6bbd67` | introduced | low | primitive_obsession | `internal/chat/upload.go` | UploadHandler | 38-154 | 0.09 |
| `8bf7982560` | introduced | low | large_method | `internal/chat/upload.go` | UploadHandler | 36-112 | 0.38 |
| `8bf7982560` | introduced | low | error_handling | `internal/chat/upload.go` |  | 153-153 | 0.15 |
| `379635c6ba` | introduced | low | complex_conditional | `internal/config/config.go` | validate | 147-147 | 0.4 |
| `a83a15feeb` | introduced | low | error_handling | `internal/config/config.go` |  | 47-47 | 0.13 |
| `a83a15feeb` | introduced | low | error_handling | `internal/config/config.go` |  | 44-44 | 0.13 |
| `a83a15feeb` | introduced | low | error_handling | `internal/config/config.go` |  | 45-45 | 0.13 |
| `a83a15feeb` | introduced | low | error_handling | `internal/config/config.go` |  | 48-48 | 0.13 |
| `a347d4eaf4` | introduced | low | error_handling | `internal/linkpreview/handler.go` |  | 50-50 | 0.15 |
| `a347d4eaf4` | introduced | low | complex_conditional | `internal/linkpreview/linkpreview.go` | checkURL | 136-136 | 0.32 |
| `a347d4eaf4` | introduced | low | error_handling | `internal/linkpreview/linkpreview_test.go` |  | 44-44 | 0.07 |
| `a347d4eaf4` | introduced | low | error_handling | `internal/linkpreview/linkpreview_test.go` |  | 87-87 | 0.07 |
| `a347d4eaf4` | introduced | low | error_handling | `internal/linkpreview/linkpreview_test.go` |  | 111-111 | 0.07 |
| `a347d4eaf4` | introduced | low | error_handling | `internal/linkpreview/linkpreview_test.go` |  | 55-55 | 0.07 |
| `a347d4eaf4` | introduced | low | error_handling | `internal/linkpreview/linkpreview_test.go` |  | 91-91 | 0.07 |
| `a347d4eaf4` | introduced | low | error_handling | `internal/linkpreview/linkpreview_test.go` |  | 51-51 | 0.07 |
| `a347d4eaf4` | introduced | low | error_handling | `internal/linkpreview/linkpreview_test.go` |  | 28-28 | 0.07 |
| `379635c6ba` | introduced | low | complex_conditional | `internal/media/policy_test.go` | TestDangerousTypesAreNeverInline | 60-60 | 0.4 |
| `a83a15feeb` | introduced | low | primitive_obsession | `internal/s3/s3.go` | Upload | 79-91 | 0.15 |
| `1335d476d6` | introduced | low | complex_conditional | `internal/server/auth_integration_test.go` | TestWebRTCConfigNeedsSessionAndCarriesTURN | 268-268 | 0.4 |
| `13617e8e1f` | introduced | low | large_method | `internal/server/chat_integration_test.go` | TestMessagesAreScopedAndValidated | 15-92 | 0.09 |
| `379635c6ba` | introduced | low | complex_conditional | `internal/server/chat_integration_test.go` | TestRepliesQuoteTheOriginal | 242-242 | 0.4 |
| `d5e9d583b3` | introduced | low | error_handling | `internal/server/metrics.go` |  | 73-73 | 0.15 |
| `00b8ce0a6d` | introduced | low | error_handling | `internal/server/public.go` |  | 179-179 | 0.15 |
| `a5e9fb76b3` | introduced | low | error_handling | `internal/server/public.go` |  | 87-87 | 0.15 |
| `70da9edeee` | introduced | low | error_handling | `internal/server/search_integration_test.go` |  | 110-110 | 0.06 |
| `a212044284` | introduced | low | error_handling | `internal/server/search_integration_test.go` |  | 64-64 | 0.06 |
| `a212044284` | introduced | low | error_handling | `internal/server/search_integration_test.go` |  | 68-68 | 0.06 |
| `a212044284` | introduced | low | error_handling | `internal/server/search_integration_test.go` |  | 52-52 | 0.06 |
| `a212044284` | introduced | low | error_handling | `internal/server/search_integration_test.go` |  | 81-81 | 0.06 |
| `a212044284` | introduced | low | error_handling | `internal/server/search_integration_test.go` |  | 60-60 | 0.06 |
| `a212044284` | introduced | low | error_handling | `internal/server/search_integration_test.go` |  | 76-76 | 0.06 |
| `a212044284` | introduced | low | error_handling | `internal/server/search_integration_test.go` |  | 72-72 | 0.06 |
| `a212044284` | introduced | low | error_handling | `internal/server/search_integration_test.go` |  | 85-85 | 0.06 |
| `379635c6ba` | introduced | low | large_method | `internal/server/server.go` | NewRouter | 46-135 | 0.38 |
| `1335d476d6` | introduced | low | error_handling | `internal/server/turn_test.go` |  | 24-24 | 0.15 |
| `8bf7982560` | introduced | low | large_method | `internal/sfu/sfu.go` | JoinPeer | 82-177 | 0.31 |
| `8bf7982560` | introduced | low | error_handling | `internal/sfu/sfu.go` |  | 160-160 | 0.15 |
| `8bf7982560` | introduced | low | error_handling | `internal/sfu/sfu.go` |  | 158-158 | 0.15 |
| `8bf7982560` | introduced | low | lock_in_loop | `internal/sfu/sfu.go` | JoinPeer | 152-152 | 0.0 |
| `bf2b774885` | introduced | low | error_handling | `internal/sfu/sfu.go` |  | 223-223 | 0.15 |
| `bf2b774885` | introduced | low | primitive_obsession | `internal/sfu/sfu_test.go` | waitSources | 293-303 | 0.15 |
| `802e451c9c` | introduced | low | error_handling | `internal/sfu/subscriptions_test.go` |  | 87-87 | 0.15 |
| `802e451c9c` | introduced | low | error_handling | `internal/sfu/subscriptions_test.go` |  | 156-156 | 0.15 |
| `7fe7d37dce` | introduced | low | large_method | `internal/ws/hub.go` | handle | 553-626 | 0.1 |
| `8bf7982560` | introduced | low | error_handling | `internal/ws/hub.go` |  | 284-284 | 0.15 |
| `8bf7982560` | introduced | low | lock_in_loop | `internal/ws/hub.go` | Run | 71-71 | 0.0 |
| `8bf7982560` | introduced | low | lock_in_loop | `internal/ws/hub.go` | Run | 85-85 | 0.0 |
| `8bf7982560` | introduced | low | lock_in_loop | `internal/ws/hub.go` | Run | 62-62 | 0.0 |
| `b0423e83f3` | introduced | low | large_method | `internal/ws/hub.go` | readPump | 250-318 | 0.21 |
| `b0423e83f3` | introduced | low | lock_in_loop | `internal/ws/hub.go` | Run | 98-98 | 0.0 |
| `b0423e83f3` | introduced | low | lock_in_loop | `internal/ws/hub.go` | Run | 120-120 | 0.0 |
| `1dbc7e0226` | introduced | low | complex_conditional | `web/src/App.vue` | applyRoute | 117-117 | 0.4 |
| `1dbc7e0226` | introduced | low | large_method | `web/src/App.vue` | applyRoute | 79-154 | 0.2 |
| `5a24c9a3a0` | introduced | low | complex_conditional | `web/src/components/AdminDashboard.test.js` | vi.fn callback | 79-79 | 0.4 |
| `5a24c9a3a0` | introduced | low | bumpy_road | `web/src/components/AdminDashboard.vue` | onChannelDrop | 297-326 | 0.15 |
| `4de94aaaf9` | introduced | low | complex_conditional | `web/src/components/ChatArea.vue` | onGlobalKeydown | 597-597 | 0.31 |
| `4de94aaaf9` | introduced | low | error_handling | `web/src/components/ChatArea.vue` |  | 325-325 | 0.15 |
| `7c00a6041b` | introduced | low | bumpy_road | `web/src/components/ChatArea.vue` | openMessageContextMenu | 325-417 | 0.15 |
| `7c00a6041b` | introduced | low | large_method | `web/src/components/ChatArea.vue` | openMessageContextMenu | 325-417 | 0.15 |
| `a1beba3fa1` | introduced | low | error_handling | `web/src/components/IncomingCallModal.vue` |  | 29-29 | 0.15 |
| `28c4ede36e` | introduced | low | error_handling | `web/src/components/LegalModal.vue` |  | 28-28 | 0.15 |
| `7c00a6041b` | introduced | low | error_handling | `web/src/components/LinkPreviewCard.vue` |  | 35-35 | 0.15 |
| `4f8c2040a4` | introduced | low | large_method | `web/src/composables/useNavMenus.js` | buildMemberItems | 186-252 | 0.27 |
| `45d82a5bcf` | introduced | low | error_handling | `web/src/composables/useWebRTC.js` |  | 627-627 | 0.1 |
| `4ee831a5cc` | introduced | low | bumpy_road | `web/src/composables/useWebRTC.js` | setupPeerConnection | 41-97 | 0.15 |
| `4ee831a5cc` | introduced | low | error_handling | `web/src/composables/useWebRTC.js` |  | 33-33 | 0.15 |
| `4ee831a5cc` | introduced | low | error_handling | `web/src/composables/useWebRTC.js` |  | 26-26 | 0.15 |
| `56a7dd2b0f` | introduced | low | large_method | `web/src/composables/useWebRTC.js` | setupPeerConnection | 252-336 | 0.21 |
| `56a7dd2b0f` | introduced | low | error_handling | `web/src/composables/useWebRTC.js` |  | 217-217 | 0.06 |
| `56a7dd2b0f` | introduced | low | error_handling | `web/src/composables/useWebRTC.js` |  | 294-294 | 0.06 |
| `56a7dd2b0f` | introduced | low | error_handling | `web/src/composables/useWebRTC.js` |  | 215-215 | 0.06 |
| `5ab4760bd1` | introduced | low | error_handling | `web/src/composables/useWebRTC.js` |  | 999-999 | 0.04 |
| `5ab4760bd1` | introduced | low | error_handling | `web/src/composables/useWebRTC.js` |  | 490-490 | 0.04 |
| `5ab4760bd1` | introduced | low | error_handling | `web/src/composables/useWebRTC.js` |  | 992-992 | 0.04 |
| `9d7ac2ee08` | introduced | low | error_handling | `web/src/composables/useWebRTC.js` |  | 143-143 | 0.13 |
| `385880c747` | introduced | low | error_handling | `web/src/directives/tooltip.js` |  | 109-109 | 0.15 |
| `385880c747` | introduced | low | error_handling | `web/src/i18n/index.js` |  | 20-20 | 0.15 |
| `aa34060a40` | introduced | low | complex_conditional | `web/src/lib/markdown.js` | renderMarkdown | 56-56 | 0.4 |
| `379635c6ba` | introduced | low | complex_conditional | `web/src/lib/rtcStats.js` | summarizeStats | 15-15 | 0.4 |
| `5ab4760bd1` | introduced | low | large_method | `web/src/lib/soundEffects.js` | playSound | 69-183 | 0.15 |
| `5ab4760bd1` | introduced | low | error_handling | `web/src/lib/soundEffects.js` |  | 60-60 | 0.15 |
| `5ab4760bd1` | introduced | low | bumpy_road | `web/src/lib/soundEffects.js` | playSound | 69-183 | 0.15 |
| `5ab4760bd1` | introduced | low | error_handling | `web/src/lib/soundEffects.js` |  | 73-73 | 0.15 |
| `9ffbfdc15b` | introduced | low | large_method | `web/src/stores/auth.js` | defineStore callback | 4-82 | 0.38 |
| `a1beba3fa1` | introduced | low | error_handling | `web/src/stores/chat.js` |  | 45-45 | 0.15 |
| `56a7dd2b0f` | introduced | low | error_handling | `web/src/stores/voice.js` |  | 25-25 | 0.15 |
| `5ab4760bd1` | introduced | low | error_handling | `web/src/stores/voice.js` |  | 411-411 | 0.15 |
| `00b8ce0a6d` | introduced | low | error_handling | `web/web.go` |  | 51-51 | 0.13 |
| `48920816b9` | introduced | low | error_handling | `web/web.go` |  | 109-109 | 0.15 |
| `9ffbfdc15b` | introduced | low | error_handling | `web/web.go` |  | 48-48 | 0.15 |
| `9ffbfdc15b` | introduced | low | error_handling | `web/web.go` |  | 34-34 | 0.15 |
| `9ffbfdc15b` | introduced | low | error_handling | `web/web.go` |  | 20-20 | 0.15 |
| `00b8ce0a6d` | introduced | low | error_handling | `web/web_test.go` |  | 66-66 | 0.15 |
| `48920816b9` | introduced | low | error_handling | `web/web_test.go` |  | 38-38 | 0.15 |
| `379635c6ba` | introduced | medium | complex_method | `cmd/server/main.go` | run | 30-77 | 0.85 |
| `c9dd53c5e1` | introduced | medium | complex_conditional | `cmd/server/main.go` | main | 290-290 | 0.93 |
| `a83a15feeb` | introduced | medium | complex_method | `internal/auth/auth.go` | Register | 107-171 | 0.85 |
| `379635c6ba` | introduced | medium | complex_method | `internal/auth/invites.go` | CreateInvite | 67-101 | 0.85 |
| `5a24c9a3a0` | introduced | medium | complex_method | `internal/auth/users_admin.go` | setUserPassword | 224-268 | 0.85 |
| `379635c6ba` | introduced | medium | complex_method | `internal/chat/channels.go` | CreateChannel | 105-136 | 0.62 |
| `8bf7982560` | introduced | medium | complex_method | `internal/chat/channels.go` | GetServerHierarchy | 38-84 | 0.85 |
| `8bf7982560` | introduced | medium | primitive_obsession | `internal/chat/channels.go` | CreateChannel | 99-107 | 0.35 |
| `a1d710fdc2` | introduced | medium | io_in_loop | `internal/chat/channels.go` | ApplyLayout | 252-252 | 0.0 |
| `a1d710fdc2` | introduced | medium | io_in_loop | `internal/chat/channels.go` | ApplyLayout | 243-243 | 0.0 |
| `a1beba3fa1` | introduced | medium | io_in_loop | `internal/chat/dms.go` | GetUserDMs | 146-146 | 0.0 |
| `13617e8e1f` | introduced | medium | complex_method | `internal/chat/handler.go` | getThread | 262-303 | 0.5 |
| `379635c6ba` | introduced | medium | complex_method | `internal/chat/handler.go` | createMessage | 96-140 | 0.75 |
| `379635c6ba` | introduced | medium | complex_method | `internal/chat/handler.go` | listMessages | 58-94 | 0.75 |
| `379635c6ba` | introduced | medium | io_in_loop | `internal/chat/handler.go` | listMessages | 80-80 | 0.0 |
| `3d99c0de2d` | introduced | medium | io_in_loop | `internal/chat/handler.go` | listMessages | 100-100 | 0.0 |
| `3d99c0de2d` | introduced | medium | io_in_loop | `internal/chat/handler.go` | getThread | 292-292 | 0.0 |
| `3d99c0de2d` | introduced | medium | complex_method | `internal/chat/mentions.go` | ResolveMentions | 49-94 | 0.85 |
| `379635c6ba` | introduced | medium | complex_method | `internal/chat/messages.go` | ValidateReplyTarget | 292-307 | 0.47 |
| `379635c6ba` | introduced | medium | complex_method | `internal/chat/messages.go` | enrich | 150-199 | 0.47 |
| `379635c6ba` | introduced | medium | complex_method | `internal/chat/messages.go` | ValidateEmoji | 82-93 | 0.47 |
| `437b6bbd67` | introduced | medium | nested_complexity | `internal/chat/messages.go` | populateAttachments | 38-60 | 0.94 |
| `4ee831a5cc` | introduced | medium | nested_complexity | `internal/chat/messages.go` | populateReactions | 71-94 | 0.94 |
| `8bf7982560` | introduced | medium | complex_method | `internal/chat/messages.go` | GetChannelMessages | 36-115 | 0.85 |
| `c9dd53c5e1` | introduced | medium | complex_method | `internal/chat/messages.go` | GetChannelMessages | 63-130 | 0.85 |
| `437b6bbd67` | worsened | medium | large_method | `internal/chat/upload.go` | UploadHandler | 38-154 | 0.53 |
| `8bf7982560` | introduced | medium | complex_method | `internal/chat/upload.go` | UploadHandler | 36-112 | 0.85 |
| `379635c6ba` | introduced | medium | complex_method | `internal/config/config.go` | FromEnv | 67-137 | 0.47 |
| `ca406a6ad4` | introduced | medium | complex_method | `internal/config/config.go` | Load | 49-112 | 0.85 |
| `379635c6ba` | introduced | medium | complex_method | `internal/db/db.go` | EnsureAdminUser | 152-193 | 0.61 |
| `379635c6ba` | introduced | medium | complex_method | `internal/db/db.go` | Migrate | 67-132 | 0.61 |
| `379635c6ba` | introduced | medium | complex_method | `internal/httpx/middleware_test.go` | TestSecurityHeaders | 106-161 | 0.85 |
| `379635c6ba` | introduced | medium | complex_method | `internal/httpx/request.go` | SanitizeFilename | 83-107 | 0.85 |
| `a347d4eaf4` | introduced | medium | complex_method | `internal/linkpreview/linkpreview.go` | checkURL | 129-140 | 0.4 |
| `a347d4eaf4` | introduced | medium | complex_method | `internal/linkpreview/linkpreview.go` | publicIP | 113-126 | 0.4 |
| `c25a60f780` | introduced | medium | complex_conditional | `internal/linkpreview/linkpreview.go` | publicIP | 148-148 | 0.58 |
| `a347d4eaf4` | introduced | medium | complex_method | `internal/linkpreview/linkpreview_test.go` | TestParsesOpenGraphAndFallbacks | 39-81 | 0.85 |
| `379635c6ba` | introduced | medium | complex_method | `internal/media/admin.go` | PruneOlderThan | 103-142 | 0.85 |
| `379635c6ba` | introduced | medium | io_in_loop | `internal/media/admin.go` | StartRetentionWorker | 160-160 | 0.0 |
| `379635c6ba` | introduced | medium | complex_method | `internal/media/handler.go` | readFile | 78-120 | 0.35 |
| `379635c6ba` | introduced | medium | complex_method | `internal/media/handler.go` | uploadAvatar | 214-266 | 0.35 |
| `379635c6ba` | introduced | medium | large_method | `internal/server/chat_integration_test.go` | TestHistoryPagingAt15kMessages | 121-226 | 0.26 |
| `379635c6ba` | introduced | medium | complex_method | `internal/server/chat_integration_test.go` | TestMessagesAreScopedAndValidated | 15-65 | 0.26 |
| `379635c6ba` | introduced | medium | complex_method | `internal/server/chat_integration_test.go` | TestRepliesQuoteTheOriginal | 228-265 | 0.26 |
| `379635c6ba` | introduced | medium | complex_method | `internal/server/media_integration_test.go` | TestUploadedMediaIsServedSafely | 43-79 | 0.85 |
| `3d99c0de2d` | introduced | medium | complex_method | `internal/server/presence_integration_test.go` | TestStatusTextOnlyBySelfOrAdmin | 24-64 | 0.4 |
| `3d99c0de2d` | introduced | medium | complex_method | `internal/server/presence_integration_test.go` | TestPresenceOverWebSocket | 166-217 | 0.4 |
| `44771be856` | introduced | medium | complex_method | `internal/server/readstate_integration_test.go` | TestUnreadAndMentionCounts | 47-84 | 0.85 |
| `205a3833fa` | introduced | medium | complex_method | `internal/server/server.go` | NewRouter | 48-171 | 0.74 |
| `a347d4eaf4` | worsened | medium | large_method | `internal/server/server.go` | NewRouter | 47-153 | 0.88 |
| `e6605f0dab` | introduced | medium | complex_method | `internal/server/users_admin_integration_test.go` | TestAdminDisablesAndEnablesUsers | 37-71 | 0.75 |
| `e6605f0dab` | introduced | medium | complex_method | `internal/server/users_admin_integration_test.go` | TestAdminKicksFromVoiceAndLastSeen | 103-137 | 0.75 |
| `14d658ff86` | introduced | medium | complex_method | `internal/server/ws_integration_test.go` | TestTypingIsRelayedToOthersAndThrottled | 214-251 | 0.5 |
| `379635c6ba` | introduced | medium | complex_method | `internal/server/ws_integration_test.go` | TestVoiceSurvivesReconnectWithinGrace | 134-176 | 0.75 |
| `379635c6ba` | introduced | medium | complex_method | `internal/server/ws_integration_test.go` | TestVoiceJoinOnlyForVoiceChannels | 97-130 | 0.75 |
| `279cd19c98` | introduced | medium | goroutine_in_unbounded_loop | `internal/sfu/sfu.go` | SignalPeerConnections | 327-327 | 0.0 |
| `379635c6ba` | introduced | medium | brain_method | `internal/sfu/sfu.go` | JoinPeer | 121-206 | 0.35 |
| `4ee831a5cc` | introduced | medium | nested_complexity | `internal/sfu/sfu.go` | SignalPeerConnections | 179-228 | 0.94 |
| `bf2b774885` | worsened | medium | large_method | `internal/sfu/sfu.go` | JoinPeer | 198-313 | 0.35 |
| `279cd19c98` | introduced | medium | nested_complexity | `internal/sfu/sfu_test.go` | publishVideo | 102-149 | 0.94 |
| `279cd19c98` | introduced | medium | complex_method | `internal/sfu/sfu_test.go` | publishVideo | 102-149 | 0.85 |
| `802e451c9c` | introduced | medium | complex_conditional | `internal/sfu/subscriptions.go` | Subscribe | 150-150 | 0.93 |
| `802e451c9c` | introduced | medium | complex_method | `internal/sfu/subscriptions.go` | Subscribe | 132-164 | 0.85 |
| `802e451c9c` | introduced | medium | complex_method | `internal/sfu/subscriptions_test.go` | TestRoomSubscribeAndFiltering | 111-153 | 0.75 |
| `802e451c9c` | introduced | medium | complex_method | `internal/sfu/subscriptions_test.go` | TestSubscriptionsOptOutAndIn | 26-59 | 0.75 |
| `00b8ce0a6d` | introduced | medium | nested_complexity | `internal/tools/openapifix/main.go` | run | 39-74 | 0.83 |
| `00b8ce0a6d` | introduced | medium | nested_complexity | `internal/tools/openapifix/main.go` | walk | 77-102 | 0.83 |
| `00b8ce0a6d` | introduced | medium | nested_complexity | `internal/tools/openapifix/main.go` | fixOperation | 104-141 | 0.83 |
| `00b8ce0a6d` | introduced | medium | complex_method | `internal/tools/openapifix/main.go` | fixOperation | 104-141 | 0.5 |
| `00b8ce0a6d` | introduced | medium | complex_method | `internal/tools/openapifix/main.go` | run | 39-74 | 0.5 |
| `00b8ce0a6d` | introduced | medium | complex_method | `internal/tools/openapifix/main.go` | walk | 77-102 | 0.5 |
| `00b8ce0a6d` | introduced | medium | io_in_loop | `internal/ws/hub.go` | KickFromVoice | 630-630 | 0.0 |
| `379635c6ba` | introduced | medium | complex_method | `internal/ws/hub.go` | joinVoice | 298-337 | 0.55 |
| `7fe7d37dce` | introduced | medium | complex_method | `internal/ws/hub.go` | unregister | 280-319 | 0.24 |
| `802e451c9c` | introduced | medium | nested_complexity | `internal/ws/hub.go` | joinVoice | 384-435 | 0.92 |
| `802e451c9c` | introduced | medium | complex_method | `internal/ws/hub.go` | handleSubscribe | 675-711 | 0.2 |
| `8bf7982560` | introduced | medium | complex_method | `internal/ws/hub.go` | readPump | 204-262 | 0.85 |
| `9d7ac2ee08` | introduced | medium | complex_method | `internal/ws/hub.go` | writePump | 625-664 | 0.4 |
| `b0423e83f3` | introduced | medium | complex_method | `internal/ws/hub.go` | Run | 63-132 | 0.47 |
| `7fe7d37dce` | introduced | medium | complex_method | `internal/ws/hub_test.go` | TestHubBroadcastAndSendToUsers | 66-131 | 0.85 |
| `802e451c9c` | introduced | medium | complex_method | `internal/ws/hub_test.go` | TestHubSubscribeValidation | 256-315 | 0.75 |
| `3d99c0de2d` | introduced | medium | complex_method | `internal/ws/presence_test.go` | TestPresenceFollowsChoiceAndIdle | 37-86 | 0.85 |
| `7c00a6041b` | introduced | medium | complex_method | `web/src/App.vue` | syncCurrentStateToRoute | 115-138 | 0.55 |
| `385880c747` | introduced | medium | complex_method | `web/src/components/AccountMenu.vue` | onKeydown | 57-84 | 0.85 |
| `5a24c9a3a0` | introduced | medium | complex_method | `web/src/components/AdminDashboard.test.js` | vi.fn callback | 73-83 | 0.85 |
| `5a24c9a3a0` | introduced | medium | complex_method | `web/src/components/AdminDashboard.vue` | onChannelDrop | 297-326 | 0.85 |
| `379635c6ba` | introduced | medium | complex_method | `web/src/components/ChatArea.vue` | watch callback | 227-252 | 0.75 |
| `379635c6ba` | introduced | medium | complex_method | `web/src/components/ChatArea.vue` | maybeLoadMore | 175-184 | 0.75 |
| `4de94aaaf9` | introduced | medium | complex_method | `web/src/components/ChatArea.vue` | onGlobalKeydown | 592-602 | 0.2 |
| `7c00a6041b` | introduced | medium | complex_method | `web/src/components/ChatArea.vue` | handleMessageKeydown | 428-454 | 0.34 |
| `7c00a6041b` | introduced | medium | complex_method | `web/src/components/ChatArea.vue` | openMessageContextMenu | 325-417 | 0.34 |
| `13dee3cbe1` | introduced | medium | complex_conditional | `web/src/components/ContextMenu.vue` | handleKeydown | 140-140 | 0.93 |
| `7c00a6041b` | introduced | medium | complex_method | `web/src/components/ContextMenu.vue` | handleKeydown | 91-113 | 0.85 |
| `ca406a6ad4` | introduced | medium | complex_method | `web/src/components/LegalModal.vue` | onMounted callback | 20-39 | 0.85 |
| `b0423e83f3` | introduced | medium | nested_complexity | `web/src/components/MemberList.vue` | getUserVoiceChannel | 23-36 | 0.94 |
| `e1783a5755` | introduced | medium | complex_method | `web/src/components/SearchModal.vue` | handleKeydown | 123-144 | 0.85 |
| `d4c3f4c2c4` | introduced | medium | complex_method | `web/src/components/VoiceStage.vue` | computed callback | 96-108 | 0.85 |
| `4f8c2040a4` | introduced | medium | complex_method | `web/src/composables/useNavMenus.js` | buildChannelItems | 63-123 | 0.61 |
| `4f8c2040a4` | introduced | medium | complex_method | `web/src/composables/useNavMenus.js` | buildMemberItems | 186-252 | 0.61 |
| `4ee831a5cc` | introduced | medium | complex_method | `web/src/composables/useWebRTC.js` | setupPeerConnection | 41-97 | 0.85 |
| `56a7dd2b0f` | introduced | medium | complex_method | `web/src/composables/useWebRTC.js` | createBoost | 120-140 | 0.47 |
| `5ab4760bd1` | introduced | medium | complex_method | `web/src/composables/useWebRTC.js` | buildScreenAudioMix | 226-262 | 0.18 |
| `5ab4760bd1` | introduced | medium | complex_method | `web/src/composables/useWebRTC.js` | handlePttKeyDown | 389-402 | 0.18 |
| `385880c747` | introduced | medium | complex_method | `web/src/i18n/index.js` | flattenKeys | 111-124 | 0.85 |
| `379635c6ba` | introduced | medium | complex_method | `web/src/lib/api.js` | errorMessage | 46-54 | 0.75 |
| `379635c6ba` | introduced | medium | complex_method | `web/src/lib/api.js` | api | 68-96 | 0.75 |
| `fe6f05b3dc` | introduced | medium | complex_conditional | `web/src/lib/globalSearch.js` | onKeydown | 27-27 | 0.93 |
| `d5837bf8b0` | introduced | medium | complex_method | `web/src/lib/linkPreview.js` | fetchLinkPreview | 8-22 | 0.85 |
| `aa34060a40` | introduced | medium | complex_method | `web/src/lib/markdown.js` | renderMarkdown | 21-67 | 0.85 |
| `aa34060a40` | introduced | medium | complex_method | `web/src/lib/mentionQuery.js` | findMentionQuery | 11-20 | 0.55 |
| `379635c6ba` | introduced | medium | complex_method | `web/src/lib/messageGrouping.js` | isContinuation | 10-20 | 0.85 |
| `379635c6ba` | introduced | medium | complex_method | `web/src/lib/messageWindow.test.js` | it callback | 142-171 | 0.85 |
| `1dbc7e0226` | introduced | medium | complex_method | `web/src/lib/router.js` | resolveRoute | 89-99 | 0.5 |
| `7c00a6041b` | introduced | medium | complex_method | `web/src/lib/router.js` | buildRoute | 34-48 | 0.75 |
| `7c00a6041b` | introduced | medium | complex_method | `web/src/lib/router.js` | parseRoute | 5-32 | 0.75 |
| `379635c6ba` | introduced | medium | bumpy_road | `web/src/lib/rtcStats.js` | summarizeStats | 9-90 | 0.35 |
| `5ab4760bd1` | introduced | medium | nested_complexity | `web/src/lib/soundEffects.js` | playSound | 69-183 | 0.94 |
| `5ab4760bd1` | introduced | medium | complex_method | `web/src/lib/soundEffects.js` | playTone | 27-63 | 0.35 |
| `c9dd53c5e1` | worsened | medium | large_method | `web/src/stores/auth.js` | defineStore callback | 4-103 | 0.88 |
| `fe6f05b3dc` | introduced | medium | complex_method | `web/src/stores/chat.js` | defineStore callback | 32-1148 | 0.38 |
| `48920816b9` | introduced | medium | io_in_loop | `web/vite.config.js` | closeBundle | 25-25 | 0.0 |
| `48920816b9` | introduced | medium | io_in_loop | `web/vite.config.js` | closeBundle | 24-24 | 0.0 |
| `48920816b9` | introduced | medium | io_in_loop | `web/vite.config.js` | closeBundle | 22-22 | 0.0 |
| `379635c6ba` | introduced | medium | complex_method | `web/web.go` | Handler | 18-55 | 0.85 |
| `48920816b9` | introduced | medium | complex_method | `web/web.go` | newHandler | 29-66 | 0.85 |
| `48920816b9` | introduced | medium | defer_in_loop | `web/web.go` | serveCompressed | 87-87 | 0.0 |

## Zähler zur Kontrolle

- Overview-Health: 447 (soll 447)
- Withheld-Health: 146
- Health offen: 593 (soll 593)
- Suggestions in 731: 44 (soll 44)
- Suggestions offen: 102
- Security: 14 (soll 14)
- Silos: 142 (soll 142)
- Ungoverned: 37 (soll 37)
- Decisions: 47 (soll 47)
- Dead Index: 31 (soll 31)
- Dead live: 24 (soll 24)
- Commit-Health: 299 (soll 299)
- Live code-shape: 848 (soll 848)
- Performance-Zeilen: 24
