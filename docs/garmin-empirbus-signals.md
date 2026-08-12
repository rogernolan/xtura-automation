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

## Code Red Module Data

This repo currently contains a request to include data from the Code Red module and cite its GitHub source.

What is present in the repo today:

- the local `signal-info.json` catalog, which provides names and labels for many signals
- browser-captured HAR evidence for lights and heating controls

What is not yet pinned down in this repo:

- the exact public GitHub repository for the Code Red module
- which parts of the current signal naming or command knowledge came directly from that module versus from browser capture and local reverse engineering

Until the exact Code Red source URL is added, treat this section as a placeholder for provenance rather than a verified attribution block.

Recommended follow-up once the repo is known:

1. add the exact GitHub repo URL here
2. record the commit or tag used
3. list the concrete signals or command semantics imported from that source
4. distinguish those imported facts from browser-confirmed facts in this repo

## Open Questions

- Confirm `All Interior Lights Off` via a real browser click HAR.
- Confirm whether water, power, and fuse controls use the same `type=17` action pattern.
- Decode `signal 105` for heater target temperature beyond the currently known sample values.
