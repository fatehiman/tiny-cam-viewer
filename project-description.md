# Project Description: Lightweight 8-Channel RTSP Viewer

## 1. Project Overview

Build a lightweight Windows desktop application using Go that displays up to 8 RTSP camera streams simultaneously in a 4×2 grid.

The application must prioritize:
- Minimum RAM consumption.
- Minimum CPU usage.
- Low latency.
- Fast startup.
- Reliable RTSP connections.
- Simple configuration.
- No unnecessary dependencies or background services.

The application is designed primarily for Windows 11.

## 2. Technology Stack

- **Programming language:** Go
- **GUI:** Native Windows API (Win32)
- **Video playback engine:** libmpv
- **Video decoding:** FFmpeg through libmpv
- **Hardware acceleration:** D3D11VA when supported
- **RTSP transport:** TCP
- **Build target:** Windows 64-bit

Avoid Electron, Chromium, WebView, Python, OpenCV, and unnecessary frameworks.

Use native Windows child windows (HWND) for displaying video streams.

## 3. Camera Configuration

The application must load camera configurations from a `.conf` file located in the same directory as the executable.

The configuration format must be extremely simple:

Each non-empty line represents one RTSP camera URL.

Example `cameras.conf`:

```text
rtsp://192.168.1.18:554/user=admin&password=YOUR_PASSWORD&channel=1&stream=1.sdp?real_stream
```

Configuration requirements:

- Support up to 8 camera URLs.
- One URL per line.
- Ignore empty lines.
- Ignore lines starting with `#`.
- Do not require JSON, YAML, XML, or any other configuration format.
- Do not require camera names or additional configuration fields.
- Automatically assign camera numbers based on line order.
- If fewer than 8 URLs are configured, display only the configured cameras.
- If the configuration file is missing, create an empty example configuration file and display a helpful message.
- Validate RTSP URLs before attempting playback.

The application must never hardcode camera credentials.

**Addition (grid and fullscreen streams):** a line can contain `{normal:X}` and `{full:X}` markers anywhere.
The grid uses the URL with `X` of `{normal:...}`; fullscreen uses the URL with `X` of `{full:...}` (for example the
high-resolution main stream). This is plain text replacement. For such a camera, entering or leaving fullscreen
restarts its stream (it switches to the other URL). Lines without markers behave as described in section 5.

## 4. Main User Interface

The main window must display camera streams in a 4×2 grid.

Layout requirements:

- 4 columns and 2 rows.
- All camera cells must have equal dimensions.
- Maintain the correct video aspect ratio.
- Avoid stretching or distorting video.
- Use black backgrounds for empty areas.
- Display a small camera number in each cell.
- Use minimal UI elements to maximize video display area.

Each camera must have its own independent playback instance.

## 5. Double-Click Fullscreen Behavior

Implement the following interaction:

### Grid Mode

- Display all configured camera streams simultaneously.
- Each stream occupies one grid cell.

### Fullscreen Mode

When the user double-clicks any camera:

- Expand that camera to occupy the entire application window.
- Hide the other camera windows.
- **Changed later (user request):** close the RTSP connections of the hidden cameras while one camera is
  fullscreen, so all network bandwidth goes to the fullscreen camera. They connect again when the grid comes back.
- Do not reconnect or restart the selected stream (except when it has a separate `{full:X}` stream).

When the user double-clicks the fullscreen camera again, restore the original 4×2 grid.

Also support the `Esc` key to return to grid mode.

## 6. Video Playback Requirements

Use libmpv as the embedded playback engine.

Create one independent libmpv player instance per configured camera.

Recommended initial settings:

```ini
vo=gpu
gpu-api=d3d11
hwdec=auto

rtsp-transport=tcp

audio=no
osc=no
osd-level=0
terminal=no

keep-open=no
```

Additional requirements:

- Disable audio decoding.
- Disable unnecessary on-screen controls.
- Minimize buffering while maintaining stable playback.
- Use hardware decoding when available and beneficial.
- Avoid unnecessary frame copying between CPU and GPU memory.
- Handle different camera resolutions and frame rates.
- Do not upscale video unnecessarily.
- Ensure one camera failure does not affect other cameras.

Check whether hardware decoding is actually active through libmpv properties.

Do not assume that hardware decoding is always more efficient.

## 7. Connection Management

Each camera must have an independent lifecycle.

Implement the following states:

- Disconnected
- Connecting
- Playing
- Reconnecting
- Error

Requirements:

- Automatically reconnect when a stream disconnects.
- Use exponential backoff for reconnection attempts.
- Start with a short delay, such as 2 seconds.
- Apply a reasonable maximum retry delay.
- Do not allow multiple simultaneous reconnect attempts for the same camera.
- Display a connection status indicator in each grid cell.
- Keep healthy streams running if another camera disconnects.

## 8. Resource Management

Resource efficiency is a primary requirement.

Implementation guidelines:

- Use a single application process.
- Do not launch separate mpv processes for individual cameras.
- Create only the required number of player instances.
- Release all allocated resources when the application exits.
- Avoid unnecessary goroutines.
- Avoid continuously polling player state.
- Use event-driven communication where possible.
- Avoid copying video frames into Go-managed memory.
- Keep UI rendering and video decoding responsibilities separate.

Target hardware:

- Windows 11
- Intel Core i7-13650HX
- NVIDIA RTX 4060
- 16 GB RAM

## 9. Application Behavior

- Start all configured cameras automatically.
- Open in grid mode.
- Resize the grid automatically when the application window is resized.
- Support normal Windows minimize, maximize, and close operations.
- Closing the application must stop all streams and release all resources.
- No installation of VLC is required.
- Bundle or document all required runtime libraries.

## 10. Project Structure

Use a clean and maintainable Go project structure.

Suggested structure:

```text
camera-viewer/
│
├── main.go
├── go.mod
├── go.sum
│
├── cameras.conf
│
├── internal/
│   ├── camera/
│   │   ├── config.go
│   │   └── manager.go
│   │
│   ├── player/
│   │   ├── mpv.go
│   │   └── instance.go
│   │
│   ├── window/
│   │   ├── main.go
│   │   └── grid.go
│   │
│   └── lifecycle/
│       └── manager.go
│
└── README.md
```

You may adjust the structure if a simpler and more efficient implementation is possible.

## 11. Build and Distribution

The final application must:

- Compile as a 64-bit Windows executable.
- Run without requiring Go to be installed.
- Include all required libmpv and FFmpeg runtime dependencies.
- Include a sample `cameras.conf`.
- Have a straightforward build process.
- Avoid unnecessary external runtime dependencies.

Document the required DLLs and their licensing requirements.

## 12. Development Priorities

Implement the project incrementally:

1. Create a native Windows application window.
2. Embed one libmpv player into a native child window.
3. Verify RTSP playback using a camera URL from `cameras.conf`.
4. Add support for 8 independent player instances.
5. Implement the 4×2 grid layout.
6. Implement double-click fullscreen and restoration.
7. Implement independent reconnection logic.
8. Optimize RAM, CPU, GPU and network usage.
9. Test stability with 8 simultaneous streams.

Do not implement unnecessary features such as recording, playback history, PTZ controls, cloud access, user accounts, or remote management.

## 13. Acceptance Criteria

The project is considered complete when:

- It can load up to 8 RTSP URLs from `cameras.conf`.
- It displays all configured cameras in a 4×2 grid.
- Double-clicking a camera expands it to fill the application window.
- Double-clicking again or pressing Esc restores the grid.
- Other streams remain connected during fullscreen mode.
- A disconnected camera reconnects automatically.
- A failure in one stream does not interrupt other streams.
- The application runs without VLC installation.
- Resource consumption is measured with 8 simultaneous streams.
- All resources are released correctly when the application closes.

**Important:** Prioritize simplicity, reliability, and low resource consumption over architectural complexity. Do not introduce abstractions or dependencies unless they provide a measurable benefit.