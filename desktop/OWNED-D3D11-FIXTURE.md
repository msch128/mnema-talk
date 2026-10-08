# Owned D3D11 swap-chain fixture

Explicit development source experiment, never a third-party game overlay. It creates its own
Win32 window, hardware D3D11 device and DXGI swap chain, obtains its own back
buffer/render-target view and draws its scene plus Forest HUD/overlay into that
same render target **before Present**. No Tauri/Electron top-level window is used
for the HUD. No foreign process/window, graphics hook, DLL injection, capture,
anti-cheat code, media key, microphone, network or real voice session exists.

Local source validation did not launch a GUI or link/run a Windows executable.
The separate hosted Windows workflow links artifacts without launching them.
Windows source Clippy checks the actual device/swap-chain/HUD/message-loop code including
the Windows entrypoint; portable tests check only draw policy. Typechecking is
not evidence of actual hardware mode, successful GPU draws or visible pixels.

Manual build/run on Windows **after review**:

```powershell
cargo build --manifest-path desktop/Cargo.toml --locked --release --features owned-d3d11-fixture --bin mnema-owned-d3d11-fixture
desktop/target/release/mnema-owned-d3d11-fixture.exe --owned-d3d11-fixture
```

It starts windowed, with synthetic voice off. F8 toggles synthetic voice; Alt+M
toggles the own overlay only when joined. F11 requests a mode change through
`SetFullscreenState`. Space confirms the fullscreen experiment; a 15-second
unconfirmed timeout returns windowed. The timeout is armed before the request;
request/query mismatch or failure aborts the render owner and attempts fullscreen
release rather than continuing an uncertain unconfirmed transition. Escape returns windowed. Closing/focus
loss/minimization clears the synthetic voice/overlay; restoration does not
automatically rejoin. The HUD uses authored tiny bitmap glyphs and colored fixture
status rows, never real users. Default DXGI Alt+Enter behavior is disabled so
explicit F11 is the controlled mode request.

The label **DXGI FULLSCREEN** comes from successful `GetFullscreenState` on our
actual swap chain; it is not inferred from requested mode or window geometry.
The code double-queries mode around successful S_OK Present and can count own HUD
submissions in reported fullscreen. Positive occlusion/status HRESULTs are not
counted as successful presentations. Device/resize/query/presentation failures
stop the fixture and release its own fullscreen state before window destruction.
Normal WM_CLOSE posts an owned quit request without default HWND destruction;
the render owner releases fullscreen before its explicit DestroyWindow. External
forced window/process destruction cannot promise that ordering. Logical focus
loss clears the HUD policy, but while rendering is paused old submitted pixels
may persist; immediate pixel removal during focus/GPU/session stalls is not
qualified by this experiment.
WM_SIZE releases old render-target/back-buffer references before ResizeBuffers
and reacquires them. Reentrant mode-change resize messages remain pending for the
next render iteration instead of being erased by older event consumption.

**Reported DXGI fullscreen is not physical exclusive-scanout qualification.**
Microsoft's [Fullscreen Optimizations description](https://devblogs.microsoft.com/directx/demystifying-full-screen-optimizations/)
explains that Windows may present an application's apparent exclusive mode using
an optimized borderless path. Real testing must record Windows/display settings,
driver/presentation mode and visible pixels. The own HUD route can be tested with
both optimized and actual exclusive presentation; this source does not silently
equate them or change Windows settings to force either. A separate Tauri widget
working over optimized fullscreen would not prove third-party true exclusive
fullscreen support.

Pending real Windows matrix: create hardware device, windowed HUD, F11 actual
query and confirmation/timeout, resize/DPI/monitor transitions, Alt-Tab/lock,
occlusion, device removal and exit restoration; screenshot/visible HUD plus
presentation evidence. No hardware/OS result has been manufactured in this repo.

This experiment demonstrates the source shape of **game-owned rendering**.
Turning it into a companion for games we do not own requires a separate native
integration route, game/API-specific render/state/device-loss support and
licensing/signing/anti-cheat qualification. No claim that Electron or this fixture
solves those requirements is made.

Primary API sources:
[D3D11CreateDeviceAndSwapChain](https://learn.microsoft.com/en-us/windows/win32/api/d3d11/nf-d3d11-d3d11createdeviceandswapchain),
[GetFullscreenState](https://learn.microsoft.com/en-us/windows/win32/api/dxgi/nf-dxgi-idxgiswapchain-getfullscreenstate),
[SetFullscreenState](https://learn.microsoft.com/en-us/windows/win32/api/dxgi/nf-dxgi-idxgiswapchain-setfullscreenstate),
[MakeWindowAssociation](https://learn.microsoft.com/en-us/windows/win32/api/dxgi/nf-dxgi-idxgifactory-makewindowassociation),
[ClearView](https://learn.microsoft.com/en-us/windows/win32/api/d3d11_1/nf-d3d11_1-id3d11devicecontext1-clearview).

The optional `owned-d3d11-fixture` feature uses pinned Microsoft `windows 0.62.2`
COM/API bindings already present in the lock and full dependency notice inventory.
The default probe and production media route remain inactive. Hosted Windows
fixture builds package both executables and their dependency notices; build
success does not qualify mode transitions or actual rendered pixels.
