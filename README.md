# kindle-ai-tracker

Turn a jailbroken Kindle into a desk display for your AI plan usage.

![Dashboards for several AI services](docs/dashboards.gif)

See your usage, limits and resets at a glance. Updates every 5 minutes.

## Supported services

- **Claude** (Pro / Max)
- **ChatGPT** (Codex on Plus / Pro / Business)
- **GitHub Copilot**
- **OpenRouter**
- **Z.ai GLM** (Coding Plan)

## Requirements

- A Kindle jailbroken with the KindleModding hotfix (scriptlets) and FBInk. Built and tested on a Paperwhite 5, firmware 5.19.2.
- Go, to build.

## Install

Plug in the Kindle, then run:

```bash
make install
```

Eject it and keep it on a wall charger. When the Kindle is connected to a computer, it switches to USB drive mode and the dashboard can't run.

## Usage

1. Open **AI Dashboard** from your Kindle library.
2. Hold a finger on the screen for 3 seconds to open the menu, then choose **Connectors** and pick a service.
3. Follow the QR codes on screen to sign in from your phone.

Once connected:

- **Tap** to refresh.
- **Swipe**, or use the arrows at the top, to switch between services.
- **Hold** for 3 seconds to open the menu: connectors, rotate, refresh, or exit.

If nothing changes for about 30 minutes, auto-refresh pauses until you tap the screen.

## Configuration

Optional settings live in `ai-dashboard/config.json` on the Kindle:

```json
{
  "refresh_minutes": 5,
  "pause_after": 6,
  "timezone": "Asia/Kolkata",
  "providers": [
    { "id": "openrouter", "monthly_budget_usd": 20 }
  ]
}
```

| Key | Meaning |
|---|---|
| `refresh_minutes` | How often to fetch. |
| `pause_after` | Pause after this many fetches in a row with no change (`0` = never). |
| `timezone` | Time zone for clock times. `make install` sets it to your Mac's. |
| `full_refresh_every` | Flash the screen every N updates to clear e-ink ghosting (default `6`). |
| `providers` | Per-service options, such as an OpenRouter budget or `"region": "cn"` for Z.ai. |

## Adding a service

Each service is a small package in `internal/provider/` that logs in and returns usage as **meters**, each a percentage of a limit with an optional reset time. The rest of the app (charts, pacing, history, the Kindle screen) works for any service.

To add one:

1. Implement the `Provider` interface from [internal/provider/provider.go](internal/provider/provider.go). `openrouter` is the simplest example for an API key, and `copilot` for a device-code login.
2. Add a blank import for it in `cmd/ai-dash/main.go`.
3. Run `make install`. The service then appears under **Connectors**.

## Development

```bash
make test      # unit tests
make preview   # render every screen with sample data into build/preview/
```

Logs are written to `ai-dashboard/dash.log` on the Kindle.
