# Tiny Cam Viewer

A small Windows tray app that shows up to **8 RTSP cameras** in a 4×2 grid.

- Written in Go. Native Win32 windows. No cgo, no framework, no Electron.
- Video is played by **libmpv** (FFmpeg inside), one independent player per camera.
- Hardware decoding (D3D11VA) when the PC supports it. The app checks that it is really active.
- RTSP over TCP, audio off, small buffers for low latency.
- One camera failing never stops the others. Each camera reconnects by itself.

## Quick start

1. Get the program (see "Build" below). You need two files in the same folder:
   - `tiny-cam-viewer.exe`
   - `libmpv-2.dll`
2. Start `tiny-cam-viewer.exe`. If `cameras.conf` is missing, the app creates an example file and tells you.
3. Edit `cameras.conf` (one RTSP URL per line, see below).
4. Right-click the tray icon and choose **Reload cameras**.

## cameras.conf

The file is in the **same folder as the exe**.

```text
# Lines that start with # are ignored. Empty lines are ignored.
rtsp://192.168.1.18:554/user=admin&password=YOUR_PASSWORD&channel=1&stream=1.sdp?real_stream
rtsp://admin:YOUR_PASSWORD@192.168.1.20:554/Streaming/Channels/101
```

- One URL per line. Maximum 8. Extra lines are ignored.
- The camera number is the order of the URLs in the file (1 = first URL).
- Only the configured cameras are shown. The other grid cells stay black.
- A line that is not a valid `rtsp://` URL shows an **Error** status in its cell. Other cameras are not affected.
- `cameras.conf` contains passwords. It is in `.gitignore`. Never commit it. Passwords are hidden in the log file.
- `cameras.conf.example` in the repository is only a sample.

## Using the app

| Action | Result |
|---|---|
| Double-click a camera | That camera fills the whole window. The other cameras keep running (hidden). The stream is **not** restarted. |
| Double-click again, or press `Esc` | Back to the 4×2 grid. |
| Minimize the window | The window hides. The app keeps running in the system tray. |
| Left-click tray icon | Show or hide the window. |
| Right-click tray icon | Menu: Show/Hide window, Reload cameras, Edit cameras.conf, **Exit**. |
| Close button (X) or tray **Exit** | Stops all streams, frees all resources and exits. |

Only one copy of the app can run. A second copy shows an error message and exits.

### Status in each cell

Top left of every cell: a colored dot, the camera number and the state.

| Dot | State | Meaning |
|---|---|---|
| gray | Disconnected | Not started |
| yellow | Connecting / Reconnecting | Trying to open the stream |
| green | Playing, `HW` or `SW` | Video is shown. `HW` = hardware decoding is active, `SW` = software decoding |
| red | Error | Last attempt failed. The text shows the reason and the time to the next try |

### Reconnect

When a stream fails or is lost, the camera retries after 2 s, then 4 s, 8 s, 16 s, and then every 30 s.
There is only one pending retry per camera. After a successful connection the delay starts at 2 s again.
A connection attempt that gets no answer ends after about 8 seconds.

## Build

Needs: Windows 10/11 64-bit and [Go](https://go.dev/dl/) 1.22 or newer (tested with 1.26). No C compiler.

```powershell
powershell -ExecutionPolicy Bypass -File scripts\build.ps1
```

This puts the result in `dist\`:

1. Builds `dist\tiny-cam-viewer.exe` (`CGO_ENABLED=0`, `-ldflags "-s -w -H=windowsgui"`).
2. Downloads `libmpv-2.dll` (LGPL build) with `scripts\fetch-libmpv.ps1`.
3. Copies `cameras.conf.example`, `LICENSE` and `THIRD-PARTY-NOTICES.md`.

If you already have the DLL, `go build` is enough:

```powershell
go build -trimpath -ldflags "-s -w -H=windowsgui" -o dist\tiny-cam-viewer.exe .
```

The exe icon and version info are in `rsrc_windows_amd64.syso`. To rebuild it after you change
`winres\winres.json` or the icon:

```powershell
go run ./tools/genicon          # redraws the icons
go install github.com/tc-hib/go-winres@latest
go-winres make --in winres/winres.json --out rsrc
del rsrc_windows_386.syso
```

## Runtime files

| File | Required | Notes |
|---|---|---|
| `tiny-cam-viewer.exe` | yes | The program (about 2.5 MB) |
| `libmpv-2.dll` | yes | About 100 MB. LGPL build of mpv with FFmpeg inside. Licenses: see `THIRD-PARTY-NOTICES.md` |
| `cameras.conf` | yes | Created on first start if missing |
| `tiny-cam-viewer.log` | no | Small log (restarted at 512 KB). Passwords are hidden |

No VLC and no other install is needed.

## How it works (short)

- `main.go`: single instance, finds the program folder, loads `libmpv-2.dll`, starts the UI.
- `internal/camera`: reads and validates `cameras.conf`; hides passwords for the log.
- `internal/player`: calls libmpv through `LoadLibrary` (no cgo). One `mpv_handle` and one goroutine per camera.
  The goroutine sleeps inside `mpv_wait_event` and wakes only for events. There is no polling.
  Video is decoded and rendered by libmpv on the GPU. No video frame is copied into Go memory.
- `internal/window`: Win32 main window, one child window (`TCVCell`) per camera that libmpv draws into
  (`--wid`), tray icon and menu. Grid layout and fullscreen are only `SetWindowPos` calls.
- Double-click and `Esc` in the video area are given to us by libmpv as client messages (`script-message`),
  because the video window belongs to libmpv. The status text is drawn by libmpv (`osd-overlay`), because a native
  window can not be drawn on top of a Direct3D video window.

## Measured resource use

Test PC: Windows 11, i7-13650HX (20 logical cores), RTX 4060. **8 streams** playing at the same time:
1 real camera (352×288, 6 fps, H.264) and 7 test streams (640×360 up to 1920×1080, 15 fps, H.264).
All used D3D11VA hardware decoding.

| Item | Value |
|---|---|
| CPU | about 0.3–0.4 of one core (1.6–2 % of the whole CPU) |
| RAM | about 430 MB working set, 750 MB private bytes (includes GPU driver memory per player) |
| Threads | about 460 (each libmpv instance starts its own FFmpeg, GPU and cache threads) |

The numbers depend on the cameras (resolution, bitrate) and the GPU driver.
Real cameras with low resolution (like the 352×288 substream) use much less than the test above.

## Not included on purpose

Recording, playback history, PTZ, cloud access, user accounts, remote management, audio.

## License

MIT for this program (`LICENSE`). `libmpv-2.dll` is LGPL (`THIRD-PARTY-NOTICES.md`).
