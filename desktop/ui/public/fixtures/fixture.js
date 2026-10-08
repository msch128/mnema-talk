/** Only local projection rendering. No activation/media/native commands exist. */
export function bindFixtureRenderer(doc, events) {
    const root = doc.getElementById('fixture-root');
    const members = doc.getElementById('members');
    const overlayWidget = doc.getElementById('overlay-widget');
    const isOverlay = doc.body.classList.contains('native-overlay');
    const clear = () => {
        if (root)
            root.hidden = true;
        if (overlayWidget)
            overlayWidget.hidden = true;
        members?.replaceChildren();
    };
    clear();
    events?.listen('mnema:voice-fixture', ({ payload }) => {
        if (!root || !members)
            return;
        root.hidden = !(isOverlay ? payload.overlay : payload.passive_widget);
        if (overlayWidget)
            overlayWidget.hidden = !payload.overlay_widget;
        members.replaceChildren();
        if (root.hidden)
            return;
        for (const member of payload.members.slice(0, 8)) {
            const row = doc.createElement('li');
            row.className = `member${member.speaking ? ' speaking' : ''}${member.muted ? ' muted' : ''}`;
            const avatar = doc.createElement('span');
            avatar.className = 'avatar';
            avatar.textContent = member.name.slice(0, 2).toUpperCase();
            const name = doc.createElement('span');
            name.className = 'name';
            name.textContent = member.name;
            const state = doc.createElement('span');
            state.className = `state${member.sharing ? ' live' : ''}`;
            state.textContent = [member.muted ? 'stumm' : member.speaking ? 'spricht' : '', member.sharing ? 'teilt' : ''].filter(Boolean).join(' · ');
            row.append(avatar, name, state);
            members.append(row);
        }
    }).catch(clear);
}
bindFixtureRenderer(document, window.__TAURI__?.event);
