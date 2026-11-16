# WRAITH

*Observe. Analyze. Understand.*

A local-first Wi-Fi and Bluetooth monitor: an **ESP32 sensor** listens, a
**Go core** stores and analyzes, and a **React dashboard** shows it live.
No cloud, no host monitor mode, passive by default.

## What you get

- **Access points**: security, PMF, channel, signal history.
- **Connected devices**: who's on which network, the maker, a device-type
  guess, and your own names for them ([why not "iPhone"?](docs/clients.md)).
- **Bluetooth**: knows AirPods (with battery), Apple devices, AirPlay
  speakers and trackers (AirTag, Tile, SmartTag…) from what they broadcast,
  and flags trackers away from their owner or lingering nearby ([how](docs/bluetooth/identity.md)).
  The ESP32's one radio switches to Bluetooth manually or on a schedule.
- **Detections**: deauth floods, security/PMF downgrades, rogue APs,
  identity changes. Each finding comes with evidence, confidence and
  limitations.
- **Dashboard**: live updates, global search, coverage timeline, CSV/JSON
  export.

## Quick start

```bash
cd firmware && pio run -e bt -t upload         # or wraith-nano (Wi-Fi only), tx
scripts/install-user-service.sh                # Core + dashboard → http://127.0.0.1:8088
scripts/check-all.sh                           # every test that runs without hardware
```

Needs an original ESP32 (2.4 GHz), Linux, Go ≥ 1.26, Node ≥ 22 and
PlatformIO. Full setup: [docs/installation.md](docs/installation.md).

## Docs

[Architecture](docs/architecture.md) · [Protocol](docs/protocol.md) ·
[Detection](docs/detection.md) · [Clients](docs/clients.md) ·
[Bluetooth](docs/bluetooth/architecture.md) · [Hardware](docs/hardware.md) ·
[Testing](docs/testing.md) · [Deauth test TX](docs/tx.md) ·
[Roadmap](docs/roadmap.md)

## Scope

For networks and devices you own or are authorized to assess. WRAITH reads
frame headers, never payloads, and never pairs or recovers credentials. Only
two things transmit, both opt-in:

- **Classic Bluetooth inquiry**, the same kind of scan a phone runs.
- **Deauth test burst**: compiled out, hidden and confirm-gated by default
  ([docs/tx.md](docs/tx.md)).

## License

MIT
