<p align="center"><img src="docs/logo.svg" alt="kindle-ai-tracker" width="96"></p>

# kindle-ai-tracker

Turn a jailbroken Kindle into a desk display for your AI plan usage.

![Dashboards for several AI services](docs/dashboards.gif)

See your usage, limits and resets at a glance. Updates every 30 minutes.

## Supported services

- **Claude** (Pro / Max)
- **ChatGPT** (Codex on Plus / Pro / Business)
- **GitHub Copilot**
- **OpenRouter**
- **Z.ai GLM** (Coding Plan)
- **OpenCode Go**

## Requirements

A Kindle jailbroken with the KindleModding hotfix (scriptlets) and FBInk. Built and tested on a Paperwhite 5, firmware 5.19.2.

## Install

**With KPM**, on the Kindle (for example in kTerm):

```bash
kpm add-repo https://me.pratcode.dev/kindle-ai-tracker/manifest.json
kpm install ai_dashboard
```

KPM handles updates (`kpm upgrade`) and removal (`kpm uninstall ai_dashboard`).

**By hand:** download `kindle-ai-dashboard-<version>.zip` from [Releases](https://github.com/prat31/kindle-ai-tracker/releases), and unzip it onto the Kindle drive.

**From source** (needs Go): plug in the Kindle and run `make install`.

The dashboard opens from your library (**AI Dashboard**) or from KUAL. Run it on a wall charger, because a Kindle connected to a computer switches to USB drive mode.

## Usage

1. Open **AI Dashboard** from your Kindle library.
2. Hold a finger on the screen for 3 seconds to open the menu, then choose **Connectors** and pick a service.
3. Follow the QR codes on screen to sign in from your phone.

Once connected:

- **Tap** to refresh.
- **Swipe**, or use the arrows at the top, to switch between services.
- **Hold** for 3 seconds to open the menu: connectors, rotate, refresh, or exit.
- **Offline**, it keeps showing the last readings it fetched, marked **OFFLINE** with when they were fetched.

The dashboard updates the service on screen every 30 minutes, except overnight (23:00 to 07:00), and the others when you switch to them. To save battery, the Kindle sleeps with Wi-Fi off whenever you aren't using it, and wakes by itself only long enough to fetch each update. While it sleeps the touchscreen is off: press the power button to use it, and it stays awake for 5 minutes after your last touch. Pressing power while it's awake puts it back to sleep.

If nothing changes for 6 updates in a row (about 3 hours, including time offline), it stops updating and sleeps until you press the power button; the screen says **press power to wake**. The **Zz** button at the top turns that off (crossed out) and on again: with it off, updates never stop, but while nothing changes they slow down, up to every 2 hours, and go back to every 30 minutes as soon as something changes or you touch the screen. The sun button next to it sets the front light.

## Configuration

Optional settings live in `ai-dashboard/config.json` on the Kindle:

```json
{
  "refresh_minutes": 30,
  "quiet_hours": "23:00-07:00",
  "pause_after": 6,
  "timezone": "Asia/Kolkata",
  "providers": [
    { "id": "openrouter", "monthly_budget_usd": 20 }
  ]
}
```

| Key | Meaning |
|---|---|
| `refresh_minutes` | How often to fetch the service on screen (default `30`). |
| `quiet_hours` | No updates between these times (default `"23:00-07:00"`); the Kindle sleeps through them and updates when they end. `""` updates around the clock. |
| `max_refresh_minutes` | With **Zz** off, the longest wait between updates while nothing changes (default `120`; set it to `refresh_minutes` to always update on time). |
| `pause_after` | Stop updating and sleep after this many fetches in a row with no change (`0` = never). With **Zz** off, updates slow down after this many instead. |
| `sleep_between_updates` | Sleep until the power button once updates pause (default `true`); the **Zz** button overrides it. With it off, updates never pause. Either way the Kindle sleeps between updates. |
| `frontlight` | Front light level while running (default `0`, off; `-1` leaves it alone); the sun button overrides it. |
| `timezone` | Time zone for clock times, e.g. `Europe/London` (`make install` sets it to your Mac's). |
| `full_refresh_every` | Flash the screen every N updates to clear e-ink ghosting (default `6`). |
| `providers` | Per-service options, such as an OpenRouter budget, `"region": "cn"` for Z.ai, or `"plan_label"` to override the plan name Claude shows. |

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
make dist      # release zip and KPM repository into build/
```

To release, bump `VERSION` and push a matching tag (`v0.1.0`). GitHub Actions publishes the zip, the `.kpkg` and the KPM repository.

Logs from the last 24 hours are kept in `ai-dashboard/dash.log` on the Kindle.
