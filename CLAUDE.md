# Notes for AI tools working on this project

Read `project-description.md` (the requirements) and `README.md` first.

## Rules

- Go, Win32 through `golang.org/x/sys/windows` and `syscall.NewCallback`. **No cgo.** libmpv is loaded at run time.
- Keep it small. No new dependency unless it clearly saves RAM, CPU or code.
- `cameras.conf` has real passwords. It is git-ignored. Never commit it. Never write passwords to the log
  (`camera.Redact`).
- `libmpv-2.dll` is not in git (100 MB). `scripts/fetch-libmpv.ps1` downloads it.
- This is a desktop app: follow the `desktop-app-rules` skill (single instance, centered window, tray menu with
  Exit, stop old exe -> build -> start new exe).
- All `.md` files stay in the project root. `reference-docs/` is the user's folder (do not add `.md` files there).

## Build / test cycle

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File "$HOME\.claude\skills\desktop-app-rules\stop-app.ps1" -Name tiny-cam-viewer
powershell -ExecutionPolicy Bypass -File scripts\build.ps1
Start-Process dist\tiny-cam-viewer.exe
```

Test streams without cameras: run `mediamtx` and push test patterns:
`ffmpeg -re -f lavfi -i testsrc2=size=640x360:rate=15 -c:v libx264 -preset ultrafast -tune zerolatency -f rtsp -rtsp_transport tcp rtsp://127.0.0.1:8554/t1`

## Design decisions (why the code looks like this)

- **Threads**: the Win32 message loop runs on the main OS thread (`runtime.LockOSThread`). libmpv players are created
  and destroyed in goroutines, never on the UI thread. `mpv_create` is slow, and libmpv needs the UI thread to keep
  pumping messages while it closes its video windows (otherwise: deadlock).
- **Events, not polling**: one goroutine per camera blocks in `mpv_wait_event(h, 3600)`. State changes are posted to
  the UI thread with `PostMessage` (`wmAppState`).
- **Mouse/keys**: libmpv makes its own child window (class `mpv`) inside our `TCVCell` window. Double-click and Esc are bound with `keybind` to `script-message tcv-toggle` / `tcv-back` and come back as
  `MPV_EVENT_CLIENT_MESSAGE`. `input-default-bindings=no` removes all other mpv keys.
- **Status text**: drawn with `osd-overlay` (ASS), because a sibling Win32 window can not be shown above a D3D11
  swap chain window (first try with a badge window: it was hidden behind the video). `TCVCell.paintCell` is only
  used while no player window exists.
- **Reconnect**: `Instance.fail` schedules exactly one `time.AfterFunc` (guarded by `timer != nil`). Backoff
  2,4,8,16,30,30... s. It is reset on the first video frame (`MPV_EVENT_PLAYBACK_RESTART`).
  `demuxer-lavf-o=timeout=8000000` makes a dead camera fail after about 8 s instead of 60+ s.
- **Hardware decoding check**: after the first frame, `hwdec-current` is read once (not polled) and shown as HW/SW.
- **Live view (`untimed=yes`)**: frames are shown as soon as they are decoded, not at their timestamp. With timed
  playback, each network stall added delay that never went away (no catch up for live streams). The demuxer buffer
  (2 MiB = about 30 s of a sub stream) then filled up, mpv stopped reading the socket, and the NVR dropped parts of
  frames ("Too short data for FU-A" in the log, last picture line repeated to the bottom with d3d11va). Do not remove
  it. Burst arrivals are shown fast instead of smooth; that is the price for always-live video.
- **`demuxer-lavf-o` replaces the whole list**, also the `fflags=+nobuffer` that `profile=low-latency` adds. So our
  value repeats it: `fflags=+nobuffer,timeout=8000000`.
- **Fullscreen**: moves/hides the `TCVCell` windows. The hidden players close their stream (`Instance.SetActive(false)`
  -> mpv `stop`, state Disconnected "paused") so the bandwidth goes to the fullscreen camera; back in the grid they
  `loadfile` again (about 1 s). `cmdMu` orders `connect`/`idle` from different goroutines: each one re-checks
  `paused` under the lock, so the last state wins. `fail` and `onPlaying` ignore a paused player.
- **Grid / fullscreen streams**: `camera.Expand` turns `{normal:X}` / `{full:X}` in a config line into two URLs
  (`Entry.URL`, `Entry.FullURL`). Plain text replacement, no URL parsing. No `{full:` in the line -> `FullURL` is empty
  and nothing changes. With a `FullURL`, `toggleFull` calls `Instance.SetFull`, which cancels a pending retry and
  runs `loadfile ... replace` with the other URL (in a goroutine). One player per camera, so no extra RAM. Only the
  fullscreen camera uses the big stream; the hidden ones stay on their grid streams.
- **Minimize / X**: the window has no minimize button. X (`WM_CLOSE`) and any minimize (`wmSize`, Win+Down) call
  `hideWindow` (save state, `SW_HIDE`).
- **No taskbar button**: the main window is *owned* by a hidden window (class `TCVOwner`, ignores `WM_CLOSE`). An owned
  top-level window has no taskbar button, and it keeps the normal title bar (`WS_EX_TOOLWINDOW` would change it).
- **Resize**: never swallow `WM_WINDOWPOSCHANGED` in `mainProc`: `DefWindowProc` makes `WM_SIZE` from it, and `WM_SIZE`
  runs `layout()`. (Returning 0 there was the "content does not resize" bug.) `layout()` also resizes the libmpv child
  window to the cell size.
- **Start minimized**: `startedHidden()` reads `STARTUPINFO.wShowWindow`. Windows replaces the argument of the *first*
  `ShowWindow` call of a process with that value, so `Run` makes one dummy `ShowWindow(SW_HIDE)` right after creating
  the window. The first real show is `showWindow()` (it uses `SW_MAXIMIZE` once if `pendingMax`).
- **settings.conf** (`camera.Settings`): window rect (normal rect from `GetWindowPlacement`), maximized, monitor index,
  and brightness/contrast per camera number. Separate from `cameras.conf` on purpose (that file has passwords and is
  edited by hand). It is not saved at every `WM_SIZE`: only on `WM_EXITSIZEMOVE`, maximize/restore, hide and exit.
- **Brightness/contrast panel** (`panel.go`): opened by right-click on a cell, or by a left click on the contrast
  icon drawn in the OSD (`player.ButtonRect` is its hit area; `onButton` compares it with the cursor). The mpv child
  window is mouse-transparent (`WindowFromPoint` returns `TCVCell`), so `cellProc` gets `WM_LBUTTONUP`/`WM_RBUTTONUP`
  (and the double click) directly; the libmpv `keybind`s are only a backup. The OSD has 4 overlays: 1 dot+number,
  2+3 icon (libass puts two drawings of one event side by side, so the dark half is its own overlay), 4 status. The panel is a separate top-level tool window (a sibling window can not be
  shown above the D3D video), owned by the main window, positioned over the neighbour cell. It closes on
  `WM_ACTIVATE` inactive. Values go to libmpv with `set brightness/contrast` (they survive `loadfile`).
- **Exit**: `close()` hides the window, removes the tray icon, stops players in a goroutine
  (`quit` + `mpv_terminate_destroy`, in parallel), then `wmAppQuit` destroys the window.
- **Tray icon is added after the first config load**, so the tooltip is correct from the start.

## Optimization notes

- Tried `swapchain-depth=1`: no measurable gain, removed.
- Used: `profile=fast,low-latency`, `untimed=yes`, `cache=no`, `demuxer-max-bytes=2MiB`, `demuxer-max-back-bytes=0`, `aid=no`, `ao=null`.
- The thread count is high because each libmpv has its own FFmpeg and render threads. CPU use is still low.
- Idea for later: `hwdec=d3d11va-copy` if a driver has problems with zero-copy `d3d11va`.

## Auto start

Do not add "start with Windows" inside the app. The user starts apps with DelayedTaskRunner (skill `autostart-app`).
