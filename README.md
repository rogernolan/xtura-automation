# EmpirBus Live CLI

## Setup

```bash
cd /Users/rog/Development/empirebus-tests
python3 -m venv .venv
. .venv/bin/activate
python -m pip install --upgrade pip websocket-client
```

## Main Usage

Run the live analyzer against the Garmin websocket:

```bash
cd /Users/rog/Development/empirebus-tests
. .venv/bin/activate
python empirbus_filter.py -f lights
```

Default websocket:

```text
ws://192.168.1.1:8888/ws
```

The CLI:
- connects to the websocket
- replays built-in Garmin startup and heartbeat messages before filtering live traffic
- learns noisy frame families for 30 seconds
- prints compressed learning output during that window
- prints matching sent control frames immediately
- summarizes suppressed noisy receive bursts after learning
- then prints full decoded frames for the selected group

## Filter Groups

Choose exactly one:

```text
lights
heating
fuses
water
options
power
```

Examples:

```bash
python empirbus_filter.py -f power
python empirbus_filter.py -f water
python empirbus_filter.py -f fuses
```

## Optional Overrides

Override the websocket URL:

```bash
python empirbus_filter.py -f lights --ws-url ws://192.168.1.1:8888/ws
```

Replay from a saved capture instead of connecting live:

```bash
python empirbus_filter.py -f lights --source capture-live.ndjson
python empirbus_filter.py -f lights --source 192.168.1.1.har
```

Use a different signal dictionary:

```bash
python empirbus_filter.py -f power --signals signal-info.json
```

Override the built-in startup sequence from a HAR if you want to compare against a browser capture:

```bash
python empirbus_filter.py -f lights --bootstrap-from-har capture.har
```

Show each matching send and then a short burst of likely related receive frames:

```bash
python empirbus_filter.py -f lights --correlate-sends
```

## Output

Learning phase output is intentionally compressed:

```text
learning 30s [lights] ws://192.168.1.1:8888/ws
.!....!........!..
```

After learning completes, surviving frames are printed like:

```text
15:42:18.204 receive type=16 cmd=0 signal=48 label="All Exterior Lights Off" data=[48, 0, 1]
```

Matching send frames stay visible even during learning:

```text
15:42:10.100 send type=16 cmd=0 signal=47 label="All Exterior Lights On" data=[47, 0, 1]
```

Repeated noisy receive traffic is collapsed into a burst summary:

```text
burst suppressed=12 families=2 span=3.421s
```

If `--correlate-sends` is enabled, receive frames for the same signal or an immediate neighbor in the next short window are shown even if that family is otherwise noisy. This is useful for seeing the likely response right after a browser-generated command.

## Recorder

`capture_ws.py` is still available as a separate recorder if you want to save a raw capture:

```bash
. .venv/bin/activate
python capture_ws.py --ws-url ws://192.168.1.1:8888/ws --out capture.ndjson
```

Send the known browser-captured exterior-lights-on action once, then disconnect:

```bash
. .venv/bin/activate
python send_exterior_lights_on.py
```

Use debug mode to print the startup frames, toggle write, and the first returned frames after the send:

```bash
. .venv/bin/activate
python send_exterior_lights_on.py --debug --read-count-after-send 10
```

## Help

```bash
python empirbus_filter.py --help
python capture_ws.py --help
```
