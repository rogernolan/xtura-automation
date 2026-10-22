# Alde and shore-power capture, 22 October 2026

The recording captured the power-loss test, the Alde electric-setting changes,
and the MultiPlus input-current limit increasing from about 3 A to 16 A. It
also captured substantial battery discharge after shore power returned while
the low input limit remained configured. This supports a warning based on
the selected electric setting exceeding the configured shore allowance.

## Evidence and scope

The live daemon recorded `garmin-ws-20261022T073848Z.ndjson` from
07:38:48.012 UTC to its automatic timeout at 07:58:48.013 UTC. The file contains
9,393 valid JSON records and is 2,816,735 bytes. The original is
`/var/lib/xtura/recordings/garmin-ws-20261022T073848Z.ndjson` on the Pi; the
local copy is `captures/garmin-ws-20261022T073848Z.ndjson`. Both copies have
SHA-256 `947d13f6e548b022f04362c4448df55959aa3efc011fd511fa771d5e6b534b06`.
The capture directory is ignored by Git.

Rog reported selecting electric heat, unplugging for about 90 seconds,
reconnecting, changing the Alde power setting, and increasing the shore limit.
That report corroborates the received signal changes. The recorder observes
the daemon's WebSocket session; it does not record the control writes from a
separate Garmin browser session. No hardware commands were sent during this
analysis.

Times in the following tables are Europe/London (BST, UTC+1). Record numbers
are one-based lines in the NDJSON file. Scalar values are decoded from received
`messagetype=16, messagecmd=5` frames, using signed little-endian bytes 4–7.
Tables combine the latest received readings at each snapshot; readings are
event-driven and need not have identical timestamps. Decoded payload values
are retained even when a signal is flagged unavailable; those flags are
described below and must be checked by an automation.

## Observed sequence

| Time, BST | Observation | Capture records |
| --- | --- | --- |
| 08:42:19 | Heater off; electric Off indication selected/unavailable; input limit 3.0 A; shore indication on; charger-only mode selected. | 1334–1378 |
| 08:43:24–08:43:30 | Heater entered its transition state, then on. Electric Off reported `[89,0,1]` while the heater was starting. | 1913–1968 |
| 08:43:33 | Electric Off cleared and 3 kW signal `97` asserted. | 2001–2003 |
| 08:43:45 | Gas-enable signal `110` cleared. Gas-priority signal `99` stayed on despite the electric selection. | 2095; compare 1925 and later snapshots |
| 08:44:29–08:44:30 | Shore indication became `[28,0,128]`; input voltage, input current, input power, and output voltage carried zero payloads with unavailable flag `128`. | 2445–2450 |
| 08:45:02–08:45:04 | Inverter-on and charger/inverter mode asserted; configured input limit became 3.1 A; output AC returned to 230 V while input AC was still zero. | 2614–2637 |
| 08:45:35–08:45:36 | Shore indication became `[28,0,129]`; input voltage payload returned at 237 V, still with unavailable flag `128`. | 2878–2881 |
| 08:45:53 | Shore indication settled to `[28,0,1]`. The low input limit remained configured. | 3008 |
| 08:45:54 | Input voltage/current/power availability flags cleared, reporting 234 V, 3 A, and 702 W. | 3010–3012 |
| 08:46:21 | 2 kW signal `87` asserted and 3 kW signal `97` cleared. | 3244–3247 |
| 08:46:58–08:47:01 | Input limit increased through 5, 10, 15, and 16 A. | 3489, 3507, 3523, 3550 |
| 08:47:04 | Battery current was positive, 12.6 A, after the limit increase. | 3616 |
| 08:47:10 | 3 kW selected again; 2 kW cleared. | 3671–3674 |

Input voltage had a zero payload for about 66.2 seconds, but the signal was
flagged unavailable during that interval. Shore indication settled to its
ordinary connected value about 84.2 seconds after its off-bit frame; input
voltage became available about 0.4 seconds later. These are different
telemetry boundaries; none measures exactly when Rog physically removed or
inserted the plug. The physical-event interpretation relies on Rog's report
as well as the correlated received states, not on treating an unavailable
zero payload as a valid voltage measurement.

## Electric-setting mapping

The selected electric setting followed Off → 3 kW → 2 kW → 3 kW. Received
momentary-status on bits identified `89` as Off, `97` as 3 kW, and `87` as
2 kW. Signal `88`, labelled 1 kW in the catalogue, never asserted, so this
test did not validate a 1 kW selection.

Signals `113`, `114`, and `115` were a misleading candidate for selected
power. Their on bits never asserted: `113` remained `128`; `114` and `115`
alternated between `0` and `128`. They may describe UI availability, but that
role is an inference. The selected-setting mapping must use the observed
`89`/`87`/`97` indications, with `88` retaining its untested catalogue status.
Electric priority `98` is also insufficient: gas priority `99` stayed selected
through the test while electric heating was enabled.

## Shore allowance and battery behaviour

Signal `204` used value type `6`. For example,
`[204,0,0,6,184,11,0,0]` decodes to raw `3000`, or 3 A; the final limit frame
`[204,0,0,6,128,62,0,0]` decodes to raw `16000`, or 16 A. Intermediate values
were 3.1, 5, 10, and 15 A. The increases followed press/release status for
signal `202`; this does not establish the browser's command write shape.

Input/output voltages `205`/`208` used type `27` and raw/1000 volts.
Input/output currents `206`/`209` and battery current `212` used type `6` and
raw/1000 amperes. Power signals `221`/`222`/`223` used type `0`; raw/1000
watts agrees with the recorded voltage/current products. The power scaling
is capture-derived corroboration. Negative battery current/power denotes
discharge in the existing service and matches the observed transitions here.

| Snapshot, BST | Alde selected | Limit | Input voltage | Reported input power | Reported output power | Battery current |
| --- | --- | --- | --- | --- | --- | --- |
| 08:44:10 | 3 kW | 3.0 A | 223 V | 2,899 W | 2,899 W | −8.0 A |
| 08:45:20 | 3 kW | 3.1 A | 0 V | 0 W | 3,220 W | −331.5 A |
| 08:45:55 | 3 kW | 3.1 A | 234 V | 702 W | 3,510 W | −287.9 A |
| 08:46:56 | 2 kW | 3.1 A | 233 V | 699 W | 2,330 W | −143.7 A |
| 08:47:50 | 3 kW | 16 A | 220 V | 3,300 W | 2,860 W | +22.7 A |

The 08:44:10 output-power payload was flagged unavailable in charger-only
mode. At 08:45:20, input voltage/power were unavailable zero payloads during
the reported unplug. Input/output/battery values in the three snapshots from
08:45:55 onward had clear availability flags when received.

For the 08:46:56 snapshot, voltage/current/power came from records
3262–3274; battery current was updated at record 3467. For 08:47:50,
input limit/current were last reported at records 3910/3917, voltages and
AC powers at 4116–4119, and battery current at 4161.

At 233 V, the configured 3.1 A allowance corresponds to about 722 W, below
both selected settings. Raising the limit to 16 A was followed by charging,
which continued with 3 kW selected. This is the low-shore-limit case the
original unplug-only warning would miss.

The initial 3 A setting did not cap the reported input current: before the
unplug, it reached 13 A with charger-only signal `199` on and charger/inverter
signal `200` off. Later, with `200` on, input current was about 3 A until the
limit increase. The capture establishes this discrepancy, not its cause.
The configured limit must not be presented as measured shore draw or a
measurement of the physical socket/cable capacity.

Victron documents that PowerAssist can supplement a limited AC supply using
battery power. That general mechanism is consistent with the post-reconnect
observations; it does not establish this vehicle's full configuration.
Source: [Victron's inverter/charger guide](https://www.victronenergy.com/inverters-chargers/help-me-choose).

## Revised warning requirement

Rog chose a warning based on the selected Alde electric setting, even while
the thermostat is temporarily idle, rather than waiting for battery discharge.
The 60-second delay remains part of the requested behaviour.

The proposed comparison is selected electric watts against configured input
limit amperes multiplied by valid input voltage, with zero shore allowance
when input AC is confirmed absent. It is a prediction that the selected
setting exceeds the shore allowance, not a claim that the heater currently
draws that power or that the battery is necessarily discharging. Other loads
can reduce the available allowance further; this initial comparison gives all
of the configured allowance to the heater and does not model those loads.

The warning needs heater-on state and a known electric setting. Unknown or
malformed telemetry, a lost Garmin connection, transition flags, and retained
state after reconnect need explicit handling before implementation. The
unavailable flag on shore indication `28` must not alone be interpreted as
confirmed unplugging. The tested absence pattern combined an off/unavailable
shore indication, unavailable zero input payloads, continuing other telemetry,
and Rog's reported unplug. Alde
`101=129` transitions occurred during setting changes, including a roughly
40-second transition to 2 kW, so a decoder must distinguish those from a
confirmed off state.

The analysis itself changed no deployed behaviour. The approved warning was
subsequently implemented in `service/notifications/alde_power.go`, with the
decoder in `service/adapters/garmin/electric_supply.go` and a Settings toggle.
The 1 kW setting remains the only selected-power mapping not exercised by
this live test; its decoder mapping is covered by a synthetic test.
