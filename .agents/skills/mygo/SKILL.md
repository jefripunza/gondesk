---
name: mygo
description: >-
  Comprehensive guide, CLI workflow, and complete documentation reference for the MyGo desktop
  application framework (https://mygo.run / github.com/egoist/mygo). Covers installation,
  project initialization (Web Frontend and Native UI -template native), the mygo CLI, Native UI
  toolkit (github.com/egoist/mygo/ui: views, flexbox/grid layouts, styling, themes, buttons,
  inputs, tables, splits, sidebars, tabs, modals, toasts, custom drawing, and headless testing),
  Web Frontend typed IPC (mygo.Bind, mygo.NewEvent), window and application lifecycle, desktop APIs,
  configuration (mygo.json and mygo.config.ts), plugins, and cross-platform packaging.
  Use this skill whenever initializing, building, styling, testing, or debugging a MyGo application.
---

# MyGo Desktop Application Framework Skill (`mygo.run`)

MyGo (`github.com/egoist/mygo`) builds cross-platform desktop apps in pure Go (`CGO_ENABLED=0`). Each window shows one of two kinds of interface, and a single app can use either or both:

- **Native UI (`github.com/egoist/mygo/ui`)**: Drawn directly on the GPU by MyGo (Metal on macOS, Direct3D 11 on Windows, OpenGL on Linux, and WebGL 2 in browsers via WASM). Written entirely in Go with no HTML, no JavaScript, no Bun, and no webview to start — windows open instantly and use minimal memory.
- **Web Frontend**: Shown in the operating system's native webview (`WKWebView` on macOS, `WebKitGTK` on Linux, `WebView2` on Windows) with a typed TypeScript client auto-generated from your Go structs and methods (`mygo.Bind`).

---

## 1. Getting Started (`https://mygo.run/docs/getting-started#install`)

### Install

Install [Go](https://go.dev/dl/) 1.27 (`1.27.3`+) or later. A project with a web frontend also needs [Bun](https://bun.sh), which runs its tools, and apps with web pages need the system's webview:

- **Linux**: GTK 3 and WebKitGTK 4.1, e.g. `sudo apt install libwebkit2gtk-4.1-0` on Debian and Ubuntu;
- **Windows 10**: the [WebView2 Runtime](https://developer.microsoft.com/microsoft-edge/webview2/), which Windows 11 includes;
- **macOS 12+**: nothing extra (`WKWebView` is part of macOS, and Native UI draws with Metal).

**Native UI needs nothing more**: Go builds the app, and it runs on what the system has (GTK 3 alone on Linux; Direct3D 11 on Windows; Metal on macOS).

The `mygo` command line tool creates, runs, and packages apps. It is a Go program, which Go runs without installing it:

```sh
go run github.com/egoist/mygo/cmd/mygo@latest init my-app
```

Projects with a web frontend also depend on it through npm, as the `mygo-cli` package, so Bun or npm create one too:

```sh
bunx mygo-cli init my-app      # or: npx mygo-cli init my-app
```

You can also install the CLI with Go, which puts `mygo` on your `PATH`:

```sh
go install github.com/egoist/mygo/cmd/mygo@latest
mygo init my-app
```

`mygo doctor` checks that the machine has what MyGo needs.

---

### Create a Project

#### Option A: Native UI (`-template native`)

`mygo init -template native my-app` (or `go run github.com/egoist/mygo/cmd/mygo@latest init -template native my-app`) creates an app whose window shows [Native UI](./references/ui/README.md): a Go module alone, with no frontend and no Bun.

```text
my-app/
├── main.go          the app: its state, its view and its window
├── main_test.go     a test of the view, which runs without a window
├── go.mod           with the CLI as a tool
├── mygo.json        the app's name, identifier and version
└── resources/
    └── icon.png     the app icon, a 1024×1024 PNG
```

The module pins the CLI as a Go tool (`tool github.com/egoist/mygo/cmd/mygo` in `go.mod`):
- `go tool mygo` runs it at the version `go.mod` pins.
- `go get -tool github.com/egoist/mygo/cmd/mygo@latest` updates it with MyGo.

#### Option B: Web Frontend (Default)

`mygo init my-app` creates a Go module and a TypeScript frontend built with Vite, side by side, and installs their dependencies:

```text
my-app/
├── main.go          the app: its windows and the Go code the page calls
├── go.mod
├── mygo.config.ts   the app's name, identifier and version, and how to build it
├── package.json     the scripts, and the frontend's dependencies
├── index.html       the page
├── src/
│   ├── main.ts      the page's code
│   ├── style.css
│   └── mygo.ts      the typed client of the Go code, generated
├── vite.config.ts
├── tsconfig.json
└── resources/
    └── icon.png     the app icon, a 1024×1024 PNG
```

Builds go to `dist/` (the frontend) and `build/` (the packaged apps), and the development app to `.mygo/`.

---

### Develop

```sh
cd my-app
go tool mygo dev   # native UI
bun run dev        # a web frontend: runs mygo dev
```

`mygo dev` builds a development version of the app and starts it, then rebuilds and restarts it when you edit a `.go` file, the configuration, the icon, or a resource. A build that fails to compile keeps the previous one running. Quit the app, or press `Ctrl+C`, to stop.

- Interactive terminal shortcuts in `mygo dev`:
  - `r` + Enter: rebuild and restart the app
  - `c` + Enter: clear the console
  - `q` + Enter: quit
  - `h` + Enter: list shortcuts
- Browser mode for Native UI (`-web`):
  - `go tool mygo dev -web` compiles a Native UI app to WebAssembly (`GOOS=js GOARCH=wasm`) and serves it at `http://localhost:5174` with live reload.

---

### Call Go

#### From Native UI

Native UI is Go, so it calls your code directly, with no bindings. The view is a function of your app's state that MyGo calls to build each frame; it asks its elements what happened since the last one:

```go
func (a *app) view(c *ui.Context) {
	ui.Column(c).Fill().Center().Gap(16).Children(func() {
		ui.TextInput(c, &a.name).Placeholder("Your name").Width(240)
		if ui.PrimaryButton(c, "Greet").Clicked() {
			a.greeting = greet(a.name) // your Go code
		}
		ui.Text(c, a.greeting)
	})
}
```

`main_test.go` clicks and types into the view without a window, as fast as a unit test: run `go test`.

#### From a Web Page

`main.go` binds a Go value, whose exported methods the page can call:

```go
type Greeter struct{}

func (Greeter) Greet(name string) string {
	if name == "" {
		name = "stranger"
	}
	return "Hello, " + name + "! This message comes from Go."
}

var Tick = mygo.NewEvent[time.Time]("tick")

func main() {
	mygo.Bind(Greeter{})
	// ...
}
```

`src/mygo.ts`, which `mygo dev` keeps up to date, turns them into typed functions and events:

```ts
import { Greeter, events } from "./mygo";

const greeting = await Greeter.greet("Ada"); // Promise<string>
events.tick.on((time) => console.log(time)); // time: string
```

---

### Build & Package

```sh
go tool mygo build   # native UI
bun run build        # a web frontend: runs mygo build
```

`mygo build` compiles the app and packages it for the machine's platform in `build/<os>-<arch>/`:

| Platform | Output |
|---|---|
| **macOS** | `My App.app`, and a disk image `my-app-0.1.0-arm64.dmg` |
| **Windows** | `My App.exe`, and an installer `my-app-setup-0.1.0-amd64.exe` |
| **Linux** | the executable `my-app` with its desktop entry and icon, their archive with `install.sh`, and `.deb` and `.rpm` packages |

Cross-compiling for all platforms from any machine (since MyGo needs no `cgo`):

```sh
go tool mygo build -platform darwin/universal,windows/amd64,linux/amd64
```

---

## 2. Native UI Architecture & Idioms (`github.com/egoist/mygo/ui`)

### Core Rules for Native UI Views
1. **State Lives in Your Struct, Not in `ui.Element`**:
   - Never store `ui.Element` or `*ui.Context` in app structs, package variables, or goroutines. Each `ui.Element` is valid only for the single build pass that created it.
   - Store persistent model fields (`string`, `bool`, `int`, `float32`, slices) and widget states (`ui.ListState`, `ui.ScrollState`, `*ui.Router`) on your application struct.
2. **Immediate-Style Controls**:
   - Controls that edit a value take a pointer to it (`ui.TextInput(c, &app.query)`, `ui.Checkbox(c, &app.checked, "Enable")`, `ui.Switch(c, &app.enabled, "Active")`, `ui.Tabs(c, &app.tabIndex, "Home", "Settings")`).
   - User input is applied before the constructor returns, so the next line already sees the updated variable!
   - Query events inline (`.Clicked()`, `.Changed()`, `.Submitted()`, `.Hovered()`) or schedule post-build callbacks (`.OnClick(fn)`, `.OnChange(fn)`, `.OnSubmit(fn)`). Use `.OnClick(fn)` when mutating a slice that you are currently iterating over.
3. **Keys in Loops and Dynamic Views**:
   - When rendering controls inside loops or switching dynamic accounts/items, key them using `c.Key(id)` before constructing the element:
     ```go
     for _, item := range app.items {
         ui.Row(c.Key(item.ID)).Gap(8).Children(func() {
             ui.Text(c, item.Name).Grow(1)
             ui.Button(c, "Connect").OnClick(func() { app.connect(item.ID) })
         })
     }
     ```
4. **Updating State from Background Goroutines (Thread Safety)**:
   - The `view(c *ui.Context)` method always runs on the **main thread**.
   - To mutate UI state from a background goroutine (e.g., network server, WebSocket, TURN events) and trigger a redraw, use `win.Update(func() { ... })`:
     ```go
     win.Update(func() {
         app.status = "Connected"
     })
     ```
   - For high-frequency polling where state might not have changed, use `mygo.RunOnMain(func() { ... })` and call `win.Invalidate()` only when values actually change.

---

### Layout & Styling Cheat Sheet (`package ui`)

- **Containers**:
  - `ui.Column(c)`: Vertical flex stack (`align-items: stretch` by default).
  - `ui.Row(c)`: Horizontal flex stack (`align-items: center` by default).
  - `ui.Box(c)`: Container column for grouping, backgrounds, borders, or custom `Draw`.
  - `ui.Spacer(c)`: Expands to fill remaining space in a `Row` or `Column`.
  - `ui.Divider(c)`: Draws a separator line across a `Row` or `Column`.
  - `ui.Scroll(c)`, `ui.ScrollHorizontal(c)`, `ui.ScrollBoth(c)`: Scrollable containers.
  - `ui.Split(c, &width, leftFn, rightFn)` / `ui.SplitVertical(c, &height, topFn, bottomFn)`: Resizable split panes.
  - `ui.Grid(c, cols...)`: CSS-like grid container.
- **Sizing & Spacing Modifiers**:
  - `.Fill()`, `.FillWidth()`, `.FillHeight()`
  - `.Width(w)`, `.Height(h)`, `.Size(w, h)`, `.MinWidth(w)`, `.MaxWidth(w)`, `.MinHeight(h)`, `.MaxHeight(h)`
  - `.Grow(1)`, `.Shrink(0)`
  - `.Gap(g)`, `.GapX(gx)`, `.GapY(gy)`
  - `.Padding(all)` / `.Padding(v, h)` / `.Padding(t, r, b, l)` / `.PaddingX(h)` / `.PaddingY(v)`
  - `.Margin(all)` / `.Margin(v, h)` / `.Margin(t, r, b, l)`
- **Alignment & Visibility**:
  - `.Center()`, `.Justify(ui.Start | ui.Center | ui.End | ui.SpaceBetween)`, `.AlignItems(ui.Start | ui.Center | ui.End | ui.Stretch | ui.Baseline)`
  - `.Hidden(bool)` (like `display: none` while preserving internal state), `.Invisible()` (like `visibility: hidden`)
- **Visual Styling & Theme**:
  - `t := c.Theme()` gives access to `t.Background`, `t.Surface`, `t.SurfaceHover`, `t.Border`, `t.Text`, `t.TextMuted`, `t.Accent`, `t.AccentText`, `t.Danger`, `t.Radius`, `t.Spacing`.
  - Customize accent/theme:
    ```go
    t := *c.Theme().WithAccent(ui.Hex("#EF443B"))
    c.SetTheme(&t)
    ```
  - Box modifiers: `.Background(color)`, `.Border(width, color)`, `.BorderWidth(t, r, b, l)`, `.BorderColor(color)`, `.Radius(r)`, `.Shadow(x, y, blur, spread, color)`, `.Gradient(from, to, angle)`, `.Opacity(o)`, `.Clip()`, `.Cursor(ui.CursorPointer)`.
  - Colors: `ui.Hex("#EF443B")`, `ui.RGB(r, g, b)`, `ui.RGBA(r, g, b, a)`, `color.Alpha(0.15)`.
- **Common Widgets**:
  - **Text**: `ui.Text(c, "Label").FontSize(18).Bold().TextColor(t.Text).SingleLine()`
  - **Buttons**: `ui.Button(c, "Cancel")`, `ui.PrimaryButton(c, "Connect")`
  - **Inputs**: `ui.TextInput(c, &str).Placeholder("...")`, `ui.TextInput(c, &pwd).Password()`, `ui.TextArea(c, &notes)`, `ui.SearchField(c, &query)`
  - **Choices**: `ui.Checkbox(c, &flag, "Label")`, `ui.Switch(c, &flag, "Label")`, `ui.Select(c, &val, options...)`, `ui.Segmented(c, &val, segments...)`
  - **Navigation**: `ui.Tabs(c, &tabIdx, "Tab 1", "Tab 2")`, `ui.Sidebar(c, &selectedID, func() { ... })`
  - **Status & Overlays**: `ui.Badge(c, "Online")`, `ui.Progress(c, 0.5)`, `ui.Spinner(c)`, `ui.Modal(c, &open, func() { ... })`, `c.Toast("Copied to clipboard")`
  - **Data Collections**: `ui.List(c, &listState, count, rowFn)`, `ui.Table(c, &tableState, cols, count, cellFn)`
  - **Custom Painting**: `ui.Box(c).Draw(func(p *ui.Painter, r ui.Rect) { ... })`

---

## 3. Complete Documentation Reference Index (`./references/`)

All official Markdown documentation files from `mygo.run/docs` (`github.com/egoist/mygo/docs`) are stored locally in `./references/`. Read the specific reference file whenever you need deep details on any feature:

### Core Framework & CLI
- [Overview & Requirements](./references/README.md)
- [Getting Started](./references/getting-started.md)
- [The `mygo` CLI Reference](./references/cli.md)
- [Configuration (`mygo.config.ts` & `mygo.json`)](./references/configuration.md)
- [The Application Lifecycle (`mygo.App`)](./references/app.md)
- [Windows (`mygo.NewWindow`, `WindowOptions`)](./references/windows.md)
- [Menus, Context Menus, System Tray & Shortcuts](./references/menus.md)
- [Desktop APIs (Dialogs, Notifications, Clipboard, Displays, Power, Secrets)](./references/native.md)
- [Clipboard & Drag-and-Drop Data Transfer](./references/data-transfer.md)
- [Document-Based Apps](./references/documents.md)
- [Internationalization (`i18n`)](./references/i18n.md)
- [Audio Playback & Synthesis](./references/audio.md)
- [Spotlight/Raycast-Style Launchers](./references/launchers.md)
- [Building & Distributing Apps](./references/distribution.md)
- [Signed Auto-Updates](./references/updates.md)
- [GitHub Actions CI/CD Release Workflow](./references/github-actions.md)
- [Web Frontend Guide](./references/frontend.md)
- [Calling Go from Frontend (`mygo.Bind`, Events, Channels)](./references/bindings.md)
- [Plugins Architecture](./references/plugins.md)
- [Native Modules (Swift, Rust, Zig without CGO)](./references/native-modules.md)
- [Internal Architecture & Threading Model](./references/architecture.md)
- [Repository Agent Guidelines (`AGENTS.md`)](./references/AGENTS.md)

### Native UI Guides (`package ui`)
- [Native UI Overview](./references/ui/README.md)
- [Views, Lifetimes, Events, Keys & Goroutine Updates](./references/ui/views.md)
- [Layout (Rows, Columns, Flexbox, Sizes, Spacing, Alignment, Positioning)](./references/ui/layout.md)
- [Styling, Borders, Shadows, Gradients & Themes](./references/ui/styling.md)
- [Text, Rich Text & Fonts](./references/ui/text.md)
- [Pointer, Keyboard Focus & Shortcuts (`input.md`)](./references/ui/input.md)
- [Drag and Drop](./references/ui/drag-and-drop.md)
- [Pages & Router Navigation](./references/ui/navigation.md)
- [Overlays](./references/ui/overlays.md)
- [Windows with Native UI (Custom Title Bars, Zoom, Vibrancy)](./references/ui/windows.md)
- [Drawing, Paths, Canvases & Animation](./references/ui/drawing.md)
- [GPU Shaders in Go Syntax](./references/ui/shaders.md)
- [Transitions](./references/ui/transitions.md)
- [Motion, Springs & Timelines](./references/ui/motion.md)
- [Transforms](./references/ui/transforms.md)
- [Custom Widgets (`ButtonBase`, `TextInputBase`, `TabsBase`, etc.)](./references/ui/custom-widgets.md)
- [Accessibility](./references/ui/accessibility.md)
- [Right-to-Left (RTL) Layouts](./references/ui/right-to-left.md)
- [Headless View Unit Testing (`ui.Tester`)](./references/ui/testing.md)
- [Native UI Inspector](./references/ui/inspector.md)
- [GPU & CPU Rendering](./references/ui/rendering.md)
- [Running Native UI in Web Browsers (WASM / WebGL 2)](./references/ui/browsers.md)
- [Performance & Frame Costs](./references/ui/performance.md)
- [Migration Guide](./references/ui/migration.md)

### Native UI Widgets & Components (`package ui`)
- **Actions**: [Button](./references/ui/button.md), [Link](./references/ui/link.md), [Menu Button](./references/ui/menu-button.md), [Split Button](./references/ui/split-button.md), [Context Menu](./references/ui/context-menu.md), [Toggle](./references/ui/toggle.md), [Segmented Control](./references/ui/segmented.md), [Toolbar](./references/ui/toolbar.md)
- **Choices**: [Checkbox](./references/ui/checkbox.md), [Switch](./references/ui/switch.md), [Radio](./references/ui/radio.md), [Select](./references/ui/select.md), [Slider](./references/ui/slider.md), [Range Slider](./references/ui/range-slider.md), [Stepper](./references/ui/stepper.md), [Rating](./references/ui/rating.md)
- **Text Inputs**: [Text Input & TextArea](./references/ui/text-input.md), [Text-Input Primitives](./references/ui/text-input-client.md), [Number Input](./references/ui/number-input.md), [Decimal Input](./references/ui/decimal-input.md), [Search Field](./references/ui/search-field.md), [Combobox](./references/ui/combobox.md), [Autocomplete](./references/ui/autocomplete.md), [Token Field](./references/ui/token-field.md), [Editable Text](./references/ui/editable-text.md), [Find Bar](./references/ui/find-bar.md), [Rich Text Editor](./references/ui/rich-text-editor.md)
- **Code & Markdown**: [Code Editor](./references/ui/code-editor.md), [Diff Viewer](./references/ui/diff-viewer.md), [Markdown](./references/ui/markdown.md)
- **Dates & Colors**: [Calendar](./references/ui/calendar.md), [Date Input](./references/ui/date-input.md), [Time Input](./references/ui/time-input.md), [Color Picker](./references/ui/color-picker.md)
- **Collections**: [List](./references/ui/list.md), [Table](./references/ui/table.md), [Tree](./references/ui/tree.md), [Outline](./references/ui/outline.md), [Grid View](./references/ui/grid-view.md)
- **Containers**: [Scroll View](./references/ui/scroll.md), [Grid](./references/ui/grid.md), [Split View](./references/ui/split.md), [Collapsible](./references/ui/collapsible.md), [Accordion](./references/ui/accordion.md), [Form](./references/ui/form.md), [Web View in Native UI](./references/ui/web-view.md)
- **Navigation**: [Tabs](./references/ui/tabs.md), [Sidebar](./references/ui/sidebar.md), [Breadcrumbs](./references/ui/breadcrumbs.md), [History Buttons](./references/ui/history-buttons.md)
- **Dialogs & Messages**: [Dialog / Modal](./references/ui/dialog.md), [Alert Dialog](./references/ui/alert-dialog.md), [Popover](./references/ui/popover.md), [Tooltip](./references/ui/tooltip.md), [Toast](./references/ui/toast.md)
- **Status, Charts & Media**: [Progress Bar](./references/ui/progress.md), [Spinner](./references/ui/spinner.md), [Meter](./references/ui/meter.md), [Badge](./references/ui/badge.md), [Charts](./references/ui/chart.md), [Image](./references/ui/image.md), [Video](./references/ui/video.md), [Icon](./references/ui/icon.md), [Avatar](./references/ui/avatar.md)

### Official Plugins
- [Fetch](./references/plugins/fetch.md), [WebSocket](./references/plugins/websocket.md), [SQLite](./references/plugins/sqlite.md), [Watch](./references/plugins/watch.md), [Updater](./references/plugins/updater.md), [Terminal](./references/plugins/terminal.md), [Glass](./references/plugins/glass.md), [Game](./references/plugins/game.md), [Loading](./references/plugins/loading.md), [SF Symbols](./references/plugins/sfsymbols.md), [Automation](./references/plugins/automation.md), [PermissionFlow](./references/plugins/permissionflow.md)

