# Garmin EmpirBus Signals

## Purpose

This document is the working reference for what we currently know about the Garmin-to-EmpirBus websocket protocol in this repo.

It is intentionally evidence-driven:

- WDU-client source-confirmed protocol mechanics are authoritative unless a live-device test disproves them
- browser-confirmed commands are called out as vehicle-specific behaviour
- inferred commands are labeled as inferred
- unresolved or low-confidence areas are listed as open questions rather than treated as facts

## Sources

Local sources used for this version:

- various har dumps
- live testing
- extracted Garmin WDU web client source map: `/Users/rog/Library/Mobile Documents/com~apple~CloudDocs/Downloads/Xtura garmin software/Xtura Display_v3 (1)/web/index.16816f2f5a38188c2b13.js.map` (reviewed 2026-08-12)

External source requested by the project:

- Code Red module https://github.com/litti/node-red-contrib-garmin-empirbus.

### Evidence hierarchy

1. **WDU-client source-confirmed:** message families, fields, byte order, flag bits, and data-type conversions implemented by the copied Garmin client. This is the default authority for wire mechanics.
2. **Browser-confirmed / live-device:** Xtura-specific signal assignments, control availability, long-press duration, and accepted session behaviour. These observations can refine or disprove source-derived assumptions.
3. **Capture-derived inference:** retained when neither source nor an explicit control action proves the meaning.

The copied client is a WebSocket UI client; it documents the Garmin-facing WDU protocol, not the lower-level EmpirBus/CAN protocol behind the WDU.

## Frame Conventions

### JSON envelope and endpoint

WDU-client source-confirmed:

- The UI opens `ws://<current-host>/ws`; the deployed SERV is currently reached at `ws://172.16.11.7:8888/ws`.
- Every WebSocket text frame is JSON with `messagetype`, `messagecmd`, `size`, and `data`.
- Component/signal IDs in `data[0]` and `data[1]` are unsigned 16-bit little-endian values.

### MFD status updates

WDU-client source-confirmed incoming `messagetype=16` (`0x10`) forms:

| `messagecmd` | Meaning | Data layout |
| ---: | --- | --- |
| `0` | Toggle status | `[signal-id LE, flags]` |
| `1` | Momentary status | `[signal-id LE, flags]` |
| `3` | Dimmer status | `[signal-id LE, flags, dimmer-value LE]` |
| `5` | Scalar status | `[signal-id LE, flags, value-type, signed-int32-value LE]` |

For toggle, momentary, and dimmer status, `flags & 0x01` is the on bit; `0x02` is fault/error 1, `0x08` is fault/error 2, and `0x80` is unavailable. Consumers must test the bit, rather than require `flags == 1`.

The signal `105` target-temperature reports use scalar status with value type `22`: signed millikelvin. Convert with `C = value / 1000 - 273.15`; the Garmin UI renders this to its configured display precision. The service rounds this source-derived numeric value to the vehicle's observed 0.5 C setpoint grid for automation decisions.

### Overview scalar telemetry

WDU-client source-map confirmed from `web/index.16816f2f5a38188c2b13.js.map` (reviewed 2026-08-12): scalar status values are signed 32-bit little-endian values. The overview adapter accepts only received scalar status frames (`messagetype=16`, `messagecmd=5`) with the documented value type; malformed, short, or other frame families remain unknown rather than becoming zero.

| Signal | Meaning | Value type | Conversion | Evidence |
| ---: | --- | ---: | --- | --- |
| `106` | Actual Temp ALDE | `22` | `raw / 1000 - 273.15` C | WDU-client source map; local signal catalogue |
| `12` | Fresh Water Value | `14` | `raw / 1000` % | WDU-client source map; local signal catalogue |
| `13` | Grey Water Value | `14` | `raw / 1000` % | WDU-client source map; local signal catalogue |
| `212` | Board Battery Current | `6` | `raw / 1000` A | WDU-client source map; local signal catalogue |
| `213` | Board Battery State Of Charge | `14` | `raw / 1000` % | WDU-client source map; local signal catalogue |

The service's sensor history store (`service/history`, added 2026-08-16) appends `106` Actual Temp ALDE samples to the Alde sensor's NDJSON file and its trend; the sensor panel shows Alde alongside configured SwitchBot BLE sensors. Repo-local capture evidence, not inferred.

Repo smoke-check: use the on-demand Garmin recording feature to collect received status while viewing the relevant Garmin pages, then confirm the capture contains the expected signal id, value type, and at least eight data bytes before treating a live value as available. The decoder tests in `service/adapters/garmin/adapter_test.go` exercise these source-map conversions, including a negative current and rejected invalid/short frames. This is transport/conversion validation, not a claim that any vehicle reading is fresh.

### Session bootstrap

Observed session bootstrap traffic:

```json
{"messagetype":96,"messagecmd":0,"size":306,"data":[152,0,1,0,4,0,5,0,6,0,7,0,9,0,10,0,11,0,20,0,21,0,22,0,23,0,27,0,30,0,31,0,32,0,33,0,34,0,35,0,36,0,37,0,38,0,39,0,40,0,41,0,51,0,52,0,53,0,54,0,55,0,56,0,57,0,58,0,59,0,60,0,68,0,69,0,70,0,71,0,72,0,73,0,78,0,79,0,83,0,76,0,49,0,50,0,225,0,226,0,227,0,228,0,229,0,230,0,231,0,232,0,233,0,45,0,46,0,47,0,48,0,77,0,84,0,85,0,177,0,178,0,172,0,179,0,2,0,38,1,3,0,237,0,238,0,239,0,12,0,13,0,61,0,62,0,63,0,14,0,66,0,25,0,24,0,74,0,75,0,101,0,102,0,103,0,105,0,106,0,107,0,108,0,110,0,113,0,114,0,115,0,119,0,97,0,87,0,88,0,89,0,90,0,91,0,92,0,96,0,99,0,98,0,248,0,153,0,15,0,16,0,17,0,18,0,19,0,111,0,93,0,95,0,240,0,26,0,200,0,201,0,202,0,203,0,204,0,205,0,206,0,208,0,209,0,211,0,212,0,213,0,214,0,215,0,216,0,217,0,218,0,199,0,221,0,222,0,223,0,181,0,182,0,185,0,183,0,197,0,196,0,28,0,220,0,195,0,180,0,189,0,191,0,190,0]}
{"messagetype":96,"messagecmd":1,"size":2,"data":[0,0]}
```

Then, after the first receive frame:

```json
{"messagetype":49,"messagecmd":1,"size":3,"data":[0,0,0]}
{"messagetype":49,"messagecmd":1,"size":3,"data":[0,0,0]}
```

### WDU heartbeat acknowledgement

WDU-client source-confirmed: a received WDU heartbeat (`messagetype=48`, `messagecmd=5`) requires this acknowledgement:

```json
{"messagetype":128,"messagecmd":0,"size":1,"data":[0]}
```

Older capture evidence showed this frame sent periodically by the browser. That is retained as an observed compatibility behaviour, but it is not the canonical mechanism in the copied WDU client. The service now acknowledges actual WDU heartbeats rather than sending this frame on a timer; live SERV verification remains required.

### Common action frame shape

WDU-client source-confirmed control writes use `messagetype=17` (`0x11`):

- `messagetype=17`
- `messagecmd=0` for simple action writes
- `messagecmd=1` for button-like press/release interactions

For simple action writes, the source-confirmed shape is:

```text
data[0..1] = signal id, unsigned 16-bit little-endian
data[2] = action value
```

Source-confirmed action values:

- Toggle command: `3` is on/set and `5` is on/reset; the target component determines the user-visible action.
- `1`: button press for heater temp up/down
- `0`: button release for heater temp up/down

Dimmer command uses `messagecmd=3` and `[signal-id LE, 0, value LE]`.

### Non-signal transport frames (`messagetype=32`, `48`, `81`)

Not all received frames carry an EmpirBus signal. The daemon's NDJSON recorder
derives `signal` as `data[0] | data[1]<<8` for every frame, which only makes
sense for the signal-bearing types `16` and `17`. For the frame families below
that heuristic produces meaningless "signal" IDs such as `5634`, `4866`, or
`39681`; these are SERV transport/UI frames, not EmpirBus signals.

Capture-derived inference (high confidence these are transport frames, not
signals; medium confidence on the exact per-field semantics below):

- `messagetype=81`, `messagecmd=1` — component name broadcast. `data` contains
  a UTF-16LE name string; e.g. `[3,18,200,200,...]` decodes to
  `"Use Trip Footstep"`. Broadcast about once per second in every capture,
  including idle sessions.
- `messagetype=32`, `messagecmd=64` — periodic telemetry broadcast, roughly
  every 1.5 s regardless of user activity, shaped `[2,22,1,X,Y,0,...]` with a
  continuously rotating `X` (observed values 3–255 across captures) and a
  mostly session-constant `Y` (2–6). Its exact meaning is unknown.
- `messagetype=32`, `messagecmd=48` — SERV touch/key event. A press is
  broadcast as `[2,19,17,0,...]` followed by a release `[2,19,1,0,...]`
  (~0.4 s later). A page or screen swap broadcasts a burst that enumerates
  every component (`[2,0,...]`, `[2,1,...]`, ... and `[1,0,...]`,
  `[1,1,...]`, ...). These also appear during idle navigation.
- `messagetype=48`, `messagecmd=0` — constant periodic descriptor broadcast
  every ~4 s, byte-identical payload `[1,155,78,19,227]` in every capture
  (2026-04 through 2026-08). Fixed signature, not an event.

Evidence: `Heating.har`, `Heating 13C-20C.har`, `Load with Heating on at 20C.har`,
and `192.168.1.1.har` (2026-04), plus
`/var/lib/xtura/recordings/garmin-ws-20260815T132037Z.ndjson` (2026-08-15),
which also shows that moving the fridge between night-mode settings emits no
signal of its own: the only discrete on/off event in that recording is
`signal 220` "AC IN Indicator" (`0`→`1`→`0`) coincident with a ~240 W input AC
load blip. The 205-entry `signal-info.json` catalogue contains no fridge,
cooling, or night-mode signal.

## Domain Summary

The current domain groupings come from `empirbus_filter.py` in the related tooling repo:

- `lights`
- `heating`
- `fuses`
- `water`
- `options`
- `power`

## Lights

### Command evidence

Browser-confirmed from `192.168.1.1.har`:

| Action | Confidence | Frame | Notes |
| --- | --- | --- | --- |
| All exterior lights ON | browser-confirmed | `{"messagetype":17,"messagecmd":0,"size":3,"data":[47,0,3]}` | This is the frame that works in `send_lights_on.py --lights exterior`. |
| All exterior lights OFF | browser-confirmed | `{"messagetype":17,"messagecmd":0,"size":3,"data":[48,0,3]}` | Confirmed by the browser click cycle in the HAR. |
| All interior lights ON | inferred, then live-confirmed by receive state | `{"messagetype":17,"messagecmd":0,"size":3,"data":[45,0,3]}` | Inferred from the 45/46 label pair in `signal-info.json`, then corroborated by live receive `signal=45 data=[45,0,1]`. |
| All interior lights OFF | inferred | `{"messagetype":17,"messagecmd":0,"size":3,"data":[46,0,3]}` | Strong inference from the signal naming pattern, but not yet browser-confirmed in a HAR. |

### Signal evidence

High-confidence signals:

- `45`: `All Interior Lights ON`
- `46`: `All Interior Lights Off`
- `47`: `All Exterior Lights On`
- `48`: `All Exterior Lights Off`

Other light-domain signals currently grouped as `lights`:

- `3`: `Optional Awning Light`
- `9`: `Roof Light Back`
- `10`: `Roof Light Front`
- `20`: `Pre-Tent Light`
- `21`: `Bathroom Light`
- `22`: `Shower Light`
- `23`: `Kitchen Light`
- `30`: `Slider: Ambient Light Front`
- `32`: `IND Ambient Light Front`
- `34`: `Slider: Ambient Light Back`
- `36`: `IND Ambient Light Back`
- `38`: `Slider: Awning Light`
- `40`: `Awning Light Indication`
- `76`: `Working Light Front`
- `227`: `Working Lights Left`
- `230`: `Working Lights Right`
- `233`: `Working Lights Back`

### Argument notes

For all-lights commands observed so far:

- `data[0]`: target signal id
- `data[1]`: always `0`
- `data[2]`: `3`

Repo usage note:

- the Go service now relies on signals `47` and `48` as exterior-light command targets and exterior-light state indications for `GET /v1/lights/state` and `POST /v1/lights/external/flash`

## Heating

### Command evidence

Browser-confirmed from `Heating.har`:

| Action | Confidence | Frame | Notes |
| --- | --- | --- | --- |
| Heater power ON | browser-confirmed | `{"messagetype":17,"messagecmd":0,"size":3,"data":[101,0,3]}` | Observed during the captured heater-on action. |
| Heater power OFF | browser-confirmed | `{"messagetype":17,"messagecmd":0,"size":3,"data":[101,0,5]}` | Same signal as ON, different action value. |
| Heater temperature UP press | browser-confirmed | `{"messagetype":17,"messagecmd":1,"size":3,"data":[107,0,1]}` | Button-like interaction. |
| Heater temperature UP release | browser-confirmed | `{"messagetype":17,"messagecmd":1,"size":3,"data":[107,0,0]}` | Release following press. |
| Heater temperature DOWN press | browser-confirmed | `{"messagetype":17,"messagecmd":1,"size":3,"data":[108,0,1]}` | Button-like interaction. |
| Heater temperature DOWN release | browser-confirmed | `{"messagetype":17,"messagecmd":1,"size":3,"data":[108,0,0]}` | Release following press. |

### Signal evidence

High-confidence signals:

- `101`: `HeatingTurnON/OFF ALDE`
- `102`: `HeatingBusy`
- `103`: `HeatingError`
- `104`: `HeatingNotAvailable`
- `105`: `HeatingTargetTemp`
- `106`: `Actual Temp ALDE`
- `107`: `HeatingTempUP ALDE`
- `108`: `HeatingTempDWN ALDE`
- `109`: `HeatingShowerStep ALDE`
- `110`: `HeatingSettingGaz ALDE`
- `119`: `HeatingPumpRunning`

Power, hot-water, and energy-source related heating-domain signals:

- `87`: `Heating Elec. 2kW`
- `88`: `Heating Elec 1kW`
- `89`: `Heating Elec Off`
- `90`: `Hot Water Boost`
- `91`: `Hot Water Normal`
- `92`: `Hot Water Off`
- `95`: `Hot Water Auto-Boost`
- `97`: `Heating Elec. 3kW`
- `98`: `Priority: Electricity`
- `99`: `Priority: GAS`
- `111`: `Gas On Text Indication`
- `112`: `HeatingElecOff`
- `113`: `HeatingElec1KW`
- `114`: `HeatingElec2KW`
- `115`: `HEatingElec3KW`
- `116`: `HeatingHotWater off`
- `117`: `HeatingHotWaterNormal`
- `118`: `HeatingHotWaterBoost`

### Electric-setting capture, 2026-10-22

Source: `captures/garmin-ws-20261022T073848Z.ndjson`, recorded by the live
daemon while Rog selected electric heating, disconnected/reconnected shore
power, changed the Alde electric setting, and increased the MultiPlus input
limit. The raw capture is retained on the Pi under
`/var/lib/xtura/recordings/`; `captures/` is ignored by Git. See
[the capture analysis](alde-shore-power-capture-2026-10-22.md) for timestamps,
record numbers, integrity checks, and the limits of these observations.

The received `16/1` status frames support these selected-setting mappings:

| Selected electric setting | Signal | Observed evidence |
| --- | ---: | --- |
| Off | `89` | `[89,0,1]` while the heater was on, then `[89,0,0]` when 3 kW was selected. |
| 1 kW | `88` | Catalogue mapping only: no selected/on frame occurred in this capture. |
| 2 kW | `87` | `[87,0,1]` accompanied `[97,0,0]`; the later 3 kW selection reversed these bits. |
| 3 kW | `97` | `[97,0,1]` followed the Off setting, then alternated with the 2 kW indication. |

These are user-correlated, capture-derived state mappings, not confirmations
of a command write shape or measurements of heater consumption. The sequence
was Off → 3 kW → 2 kW → 3 kW. Signals `113`–`115` did not assert their on bits:
`113` remained `128`, and `114`/`115` changed between `0` and `128`. They must
not be used as selected-setting indicators on the strength of their names.
Their exact UI role remains unconfirmed.

Signal `99` (gas priority) remained on while electric 3 kW and 2 kW were
selected, and gas-enable signal `110` was off after 08:43:45 BST. Thus
`98`/`99` priority alone does not identify whether electric heating is enabled.
Heater power signal `101` also reported `129` during setting changes; an
automation must account for transitions rather than equate every non-`1`
value with heater off.

### Target-temperature evidence

From `docs/superpowers/specs/2026-04-21-heating-go-client-design.md`, signal `105` is the strongest current candidate for target-temperature decoding.

Observed values:

| Target temp | Signal 105 payload |
| --- | --- |
| `8.0 C` | `[105,0,128,22,12,74,4,0]` |
| `8.5 C` | `[105,0,0,22,0,76,4,0]` |
| `9.0 C` | `[105,0,0,22,244,77,4,0]` |
| `9.5 C` | `[105,0,0,22,232,79,4,0]` |
| `10.0 C` | `[105,0,0,22,230,81,4,0]` |

The current implementation now decodes the full source-confirmed signed 32-bit millikelvin scalar, then rounds to the observed 0.5 C setpoint grid. The signal-to-setpoint interpretation and grid remain Xtura-specific evidence pending a live test.

### Argument notes

For heater power:

- `data[0]`: `101`
- `data[1]`: `0`
- `data[2]`: `3` for ON, `5` for OFF

Observed receive-state interpretations used by this repo's Go logger and state tracker:

- receive `data[2]=1`: heater power on
- receive `data[2]=0`: heater power off
- receive `data[2]=129`: heater transition / changing state

### Recording feature repo-usage note

This is a repo implementation dependency, not new signal evidence: the on-demand WebSocket recorder waits only for received on-frames after arming. It uses signal `11` as the engine-running indication, `101` as the heating-on indication, and `197` as the Victron-inverter-on indication. The confidence labels and evidence above remain unchanged: signal `11` is still inference in the habitation-step discussion, while `101` remains browser-confirmed for its heater command and has the documented observed receive-state interpretation; `197` remains listed as power-domain signal evidence.

Source files: `service/recording/manager.go` maps the wait conditions and validates received on-frames; `service/runtime/app.go` supplies the daemon WebSocket traffic; `web/static/app.js` exposes the conditions in the Settings UI.

For heater temp up/down:

- `data[0]`: `107` or `108`
- `data[1]`: `0`
- `data[2]`: `1` for press, `0` for release

For target temperature (`105`), the Go logger currently renders inferred interpretations like `set_temp=20.0C` from the payload using the decode logic in `heating/state.go`. This remains inference from repo-local captures such as `Heating.har` and `Heating 13C-20C.har`, not a browser-confirmed field specification.

## Fuses

### Command evidence

No browser-confirmed write commands are captured yet for fuse-domain controls.

### Signal evidence

Examples from `signal-info.json`:

- `2`: `Tripped Breaker`
- `70`: `Fuse Reset Heating`
- `71`: `Fuse Trip Heating`
- `72`: `Fuse Reset WaterPump`
- `73`: `Fuse Trip WaterPump`
- `177`: `Fuse Reset Switch Group A`
- `178`: `Fuse Reset Switch Group B`
- `179`: `Fuse Trip IND: Group A`
- `225`: `Fuse Reset: Working Lights Left`
- `226`: `Fuse Trip: Working Lights Left`
- `228`: `Fuse Reset: Working Lights Right`
- `229`: `Fuse Trip: Working Lights Right`
- `231`: `Fuse Reset: Working Lights Back`
- `232`: `Fuse Trip: Working Lights Back`

Working assumption:

- these signals are mostly status, indication, or reset/trip semantics
- no write framing is confirmed yet from the browser captures in this repo

## Water

### Command evidence

No browser-confirmed outbound write frames are captured yet for water-domain controls.

Capture evidence from `captures/grey-water-test.ndjson` on 2026-05-05 shows two grey-water tank discharge open/close cycles as inbound Garmin state broadcasts while the Garmin UI was used:

- `4` (`Tank Discharge Open`) is asserted with `messagetype=16`, `messagecmd=1`, `data=[4,0,1]` while the open control is held, then released with `data=[4,0,0]`.
- `5` (`Tank Discharge Close`) is asserted with `messagetype=16`, `messagecmd=1`, `data=[5,0,1]` while the close control is held, then released with `data=[5,0,0]`.
- Observed hold durations in that capture were approximately 1.6s and 5.1s for open, and 2.6s and 5.9s for close.

User-observed behavior on 2026-05-05: the grey-water valve control requires a long press to activate the stepper motor.

Inferred write-command shape, not browser-confirmed: these controls likely use press/release writes matching other momentary buttons, `messagetype=17`, `messagecmd=1`, `data=[4,0,1]` then `[4,0,0]` for open and `data=[5,0,1]` then `[5,0,0]` for close. The required hold time should be treated as part of the command behavior rather than a simple click.

Repo implementation note: `service/adapters/garmin/adapter.go` and `heating/client.go` use the inferred `messagetype=17`, `messagecmd=1` press/release shape for grey-water open and close, with the runtime hold duration set to five seconds in `service/runtime/water.go`.

### Signal evidence

- `4`: `Tank Discharge Open`
- `5`: `Tank Discharge Close`
- `12`: `Fresh Water Value %`
- `13`: `Grey Water Value %`
- `24`: `Warning/Acknowledge high grey Water Warning`
- `25`: `Warning/Acknowledge Low Fresh Water Warning`
- `27`: `Waterpump`

Working assumption:

- `27` is a likely control candidate
- `4` and `5` are momentary open/close controls for the tank discharge valve
- no outbound browser write frames are confirmed yet from the captures in this repo

## Options

### Command evidence

No browser-confirmed write commands are captured yet for option-domain controls.

### Signal evidence

- `150`: `Unhide Engine Control`
- `151`: `Unhide Vehicle Voltage`
- `152`: `Optional Awning Movement`
- `153`: `Panel busy sua`
- `237`: `Working Lights left Installed?`
- `238`: `Working Lights Right Installed?`
- `239`: `Working Lights Back Installed?`
- `240`: `ALDE3030+ Installed?`
- `248`: `AC installed?`
- `294`: `Shower Light Availalbe?`

Working assumption:

- this domain mixes capability flags, presence detection, and UI-state toggles
- no write frames are confirmed yet from the captures in this repo

### Habitation-step inference

The labels `Engine Control extract` (`125`) and `Engine Contract retract` (`126`) in the local `signal-info.json` catalogue are likely mistranslated or generic names for the habitation entry step extending and retracting, rather than controls for the vehicle engine itself. This matches the related signals:

- `6`: `Footstep Open`
- `7`: `Footstep Close`
- `11`: `Engine Running Signal Indication`

Inference: the engine-running indication is probably an interlock which retracts, or prevents extension of, the habitation step while driving. This is not source-confirmed or capture-confirmed. Confirm it by capturing Garmin traffic while extending/retracting the step and while transitioning the engine-running state.

### GPS tracking feature repo-usage note

The GPS tracker also relies on signal `11` as the engine-running indication, independently of the WebSocket recorder: in engine-only mode it starts a session track file when a received signal-11 frame reports the engine on and finalizes it when the engine is reported off. It decodes the same received-toggle semantics as the rest of the repo — signal id in `data[0..1]` as an unsigned 16-bit little-endian value and the on bit in `data[2] & 0x01`. This is a repo implementation dependency, not new signal evidence; the `11` = `Engine Running Signal Indication` interpretation above remains inference.

Source files: `service/tracking/manager.go` implements the engine-state decoding (`engineSignalState`) and the session lifecycle (`ObserveFrame`); `service/runtime/app.go` wires the manager to the daemon frame stream.

## Power

### Command evidence

No browser-confirmed write commands are captured yet for power-domain controls.

Potential command-like signals from names only:

- `29`: `230V Button`
- `96`: `ACC Command On`
- `199`: `Power Control: Charger Only`
- `200`: `Power Control: Charger/Inverter ON`
- `201`: `Power Control: Charger/Inverter OFF`
- `202`: `Power Control: Increase AC Input Limit`
- `203`: `Power Control: Decrease AC Input Limit`

These names suggest control semantics, but there is not yet a confirming browser write frame in this repo.

### Signal evidence

- `28`: `Shore Power Indication`
- `61`: `Victron Silent Mode`
- `65`: `Vehicle Voltage`
- `180`: `Eco mode`
- `195`: `AC OUT Indicator`
- `196`: `Charger On`
- `197`: `Inverter On`
- `204`: `Power Status: Input AC Current Limit`
- `205`: `Power Status: Input AC Voltage`
- `206`: `Power Status: Input AC Current`
- `208`: `Power Status: Output AC Voltage`
- `209`: `Power Status: Output AC Current`
- `211`: `Power Status: Board Battery Voltage`
- `212`: `Power Status: Board Battery Current`
- `213`: `Power Status: Board Battery State Of Charge`
- `221`: `Power Status: Input AC Watts`
- `222`: `Power Status: Output AC Watts`
- `223`: `Power Status: Board Battery Watts`

### Shore-power and input-limit capture, 2026-10-22

Source: `captures/garmin-ws-20261022T073848Z.ndjson`, with Rog's reported
unplug/reconnect and current-limit changes. All times below are BST. Detailed
evidence is in [the capture analysis](alde-shore-power-capture-2026-10-22.md).

- Signal `28` changed `1 → 129 → 128` during loss of shore power and
  `128 → 129 → 1` during return. Its on bit was clear from 08:44:29.476 to
  08:45:35.474; the final stable `1` arrived at 08:45:53.667. Because `128`
  also carries the protocol's unavailable flag, this trace does not establish
  a general rule that an unavailable indication means unplugged.
- Scalar input voltage `205` carried a zero payload at 08:44:30.076 and
  `237 V` at 08:45:36.230, both with flag `128` (unavailable). Its availability
  flag cleared at 08:45:54.068 with `234 V`. The zero-payload interval was
  roughly 66 seconds; that is not a period of valid zero-voltage measurements.
  These received state changes correlate with Rog's reported unplug/reconnect.
  Output voltage `208` returned to `230 V` at 08:45:04.733 while input voltage
  remained zero: output voltage alone cannot identify shore availability.
- Signal `204` is the configured input-current limit. Received `16/5`
  frames used value type `6`, with signed little-endian raw values divided
  by `1000` giving `3.0`, `3.1`, `5`, `10`, `15`, then `16 A`. The increases
  to `5`–`16 A` followed received press/release status on `202`. These are
  correlated status observations, not browser-confirmed command writes.
- For this capture, `205`/`208` used value type `27` and raw/1000 volts;
  `206`/`209` used type `6` and raw/1000 amperes. Power signals `221`–`223`
  used type `0`; raw/1000 watts agrees with the corresponding voltage/current
  products. The power scaling is capture-derived corroboration.
- After input AC returned, a `3.1 A` limit, about `233 V`, and the selected
  2 kW setting coincided with about `699 W` input, `2330 W` output, and
  `-143.7 A` battery current. Raising the limit to `16 A` was followed by
  positive battery current, including with 3 kW selected.

The configured limit is not proof of actual shore draw or physical supply
capacity. Before the unplug, signal `199` (charger only) was on, `200` was
off, the configured limit was `3 A`, and the received input current was
`13 A`. Later, with `200` on, input current was about `3 A` until the limit
was raised. This capture does not establish why the reported limit was not
reflected in the earlier actual draw, or how every MultiPlus mode enforces it.
The unplugged state was communicated through unavailable/off indications
alongside zero input payloads while the daemon kept receiving other telemetry.
Consumers must distinguish that correlated source state from a lost session
or an arbitrary unavailable value; zero payloads alone do not prove absence.

### Alde shore-allowance warning repo usage

The warning in `service/notifications/alde_power.go` depends on the received
electric selection indications `89`/`88`/`87`/`97`, heater power `101`, shore
indication `28`, input-current limit `204` (type `6`), and input voltage
`205` (type `27`). `service/adapters/garmin/electric_supply.go` decodes them.
The 1 kW mapping (`88`) remains catalogue-derived; the other selections and
the paired unavailable/off shore plus unavailable zero input state are based
on the dated capture above. No new hardware write command is introduced.

The decoder retains delta-reported settings only within their current session
and requires a frame received within 15 seconds. Unknown/conflicting settings,
unsupported flags, malformed scalars, and incomplete reconnect snapshots do not
count as a shore shortfall. The paired unplug state gives zero allowance;
connected state requires available voltage and a valid current limit. The
runtime evaluates the selected watts against that allowance once per second,
with a 60-second delay and one warning until a known safe state re-arms it.

## Simulation note: `servsim` command echo

`cmd/servsim` is a fake SERV for local development. It replays a recorded
NDJSON capture for background state and echoes command state for a small set of
signals so command APIs complete offline. This is **simulation behavior, not
browser-confirmed evidence**; it exists so the repo can develop and test
without the motorhome.

| Command frame | Echoed frames |
| --- | --- |
| `messagetype=17, messagecmd=0, data=[101,0,3]` | `16/5` `[101,0,1]` then `[102,0,0]`, plus a seeded `[105,...,20.0C]` when no target is known |
| `messagetype=17, messagecmd=0, data=[101,0,5]` | `16/5` `[101,0,0]` |
| `messagetype=17, messagecmd=0, data=[47,0,3]` | `16/5` `[47,0,1]` |
| `messagetype=17, messagecmd=0, data=[48,0,3]` | `16/5` `[48,0,1]` |
| `messagetype=17, messagecmd=1, data=[107,0,0]` (temp up release) | `16/5` `[105,0,0,22,...]` at +0.5C |
| `messagetype=17, messagecmd=1, data=[108,0,0]` (temp down release) | `16/5` `[105,0,0,22,...]` at -0.5C |
| `messagetype=17, messagecmd=1, data=[4,0,v]` | `16/5` `[4,0,v]` |
| `messagetype=17, messagecmd=1, data=[5,0,v]` | `16/5` `[5,0,v]` |

Echo state frames use the observed SERV state shape (`messagetype=16,
messagecmd=5, size=8, data=[sigLo,sigHi,value,0,0,0,0,0]`) and the signal `105`
payload layout above, matching the replayed frames in `garmin-ws-*.ndjson` and
the `Heating*.har` captures. Source: `cmd/servsim/echo.go`. The 20.0C seed is a
convenience so `SetTargetTemp` can read a baseline; it is not an observed SERV
value.

## Code Red Module Data

The source URL already recorded under Sources is
[`litti/node-red-contrib-garmin-empirbus`](https://github.com/litti/node-red-contrib-garmin-empirbus).
No commit or tag has been recorded, and this document does not yet attribute
any specific signal mapping or command shape to that module. The local
`signal-info.json` catalogue, browser HARs, and dated NDJSON observations above
remain separate evidence. Any future import from the module must name its
revision and the concrete facts taken from it.

## Open Questions

- Confirm `All Interior Lights Off` via a real browser click HAR.
- Confirm whether water, power, and fuse controls use the same `type=17` action pattern.
- Decode `signal 105` for heater target temperature beyond the currently known sample values.
- Determine whether the fridge has any WDU-visible signal at all: the 2026-08-15 night-mode recording shows no fridge signal, but the toggles may have fallen outside the 18-second capture window. A longer recording with only fridge controls exercised would settle this.
