/* global XturaNavigation */
class XturaApi {
  async getLightsState() {
    return this.request("/v1/lights/state");
  }

  async flashExteriorLights(count) {
    return this.request("/v1/lights/external/flash", {
      method: "POST",
      body: { count },
    });
  }

  async getWaterState() {
    return this.request("/v1/water/state");
  }

  async openGreyValve() {
    return this.request("/v1/water/grey-valve/open", { method: "POST" });
  }

  async closeGreyValve() {
    return this.request("/v1/water/grey-valve/close", { method: "POST" });
  }

  async scheduleGreyValve(targetTime, durationMinutes) {
    return this.request("/v1/water/grey-valve/schedule", {
      method: "POST",
      body: { target_time: targetTime, duration_minutes: durationMinutes },
    });
  }

  async cancelGreyValveSchedule() {
    return this.request("/v1/water/grey-valve/schedule/cancel", { method: "POST" });
  }

  async getHeatingMode() {
    return this.request("/v1/heating/mode");
  }

  async setHeatingModeSchedule() {
    return this.request("/v1/heating/mode/schedule", { method: "POST" });
  }

  async setHeatingModeOff() {
    return this.request("/v1/heating/mode/off", { method: "POST" });
  }

  async setHeatingModeManual(targetCelsius) {
    return this.request("/v1/heating/mode/manual", {
      method: "POST",
      body: { target_celsius: targetCelsius },
    });
  }

  async setHeatingModeBoost(targetCelsius, durationMinutes) {
    return this.request("/v1/heating/mode/boost", {
      method: "POST",
      body: { target_celsius: targetCelsius, duration_minutes: durationMinutes },
    });
  }

  async cancelHeatingModeBoost() {
    return this.request("/v1/heating/mode/boost/cancel", { method: "POST" });
  }

  async getHeatingSchedule() {
    return this.request("/v1/automation/heating-schedule");
  }

  async getBuildInfo() {
    return this.request("/v1/build");
  }

  async getPiStatus() {
    return this.request("/v1/pi/state");
  }

  async saveHeatingSchedule(document) {
    return this.request("/v1/automation/heating-schedule", {
      method: "PUT",
      body: document,
    });
  }

  async startRecording(waitFor, durationMinutes) {
    return this.request("/v1/recording/start", {
      method: "POST",
      body: { wait_for: waitFor, duration_minutes: durationMinutes },
    });
  }

  async stopRecording() {
    return this.request("/v1/recording/stop", { method: "POST" });
  }

  async recordingState() {
    return this.request("/v1/recording/state");
  }

  async trackingSettings() {
    return this.request("/v1/tracking/settings");
  }

  async updateTrackingSettings(settings) {
    return this.request("/v1/tracking/settings", {
      method: "PUT",
      body: settings,
    });
  }

  async trackingState() {
    return this.request("/v1/tracking/state");
  }

  async startTracking() {
    return this.request("/v1/tracking/start", { method: "POST" });
  }

  async stopTracking() {
    return this.request("/v1/tracking/stop", { method: "POST" });
  }

  async trackList() {
    return this.request("/v1/tracks");
  }

  async trackDownload(name) {
    return this.request(`/v1/tracks/${encodeURIComponent(name)}`);
  }

  async trackDelete(name) {
    return this.request(`/v1/tracks/${encodeURIComponent(name)}`, {
      method: "DELETE",
    });
  }

  async request(path, options = {}) {
    const init = {
      method: options.method || "GET",
      headers: {},
    };
    if (options.body !== undefined) {
      init.headers["Content-Type"] = "application/json";
      init.body = JSON.stringify(options.body);
    }
    const response = await fetch(path, init);
    const text = await response.text();
    const payload = text ? JSON.parse(text) : null;
    if (!response.ok) {
      const error = new Error(formatError(payload, response.status));
      error.status = response.status;
      error.payload = payload;
      throw error;
    }
    return payload;
  }
}

const allDays = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"];
const allDaysKey = [...allDays].sort().join(",");
const scheduleSlotCount = 4;
const minimumSlotMinutes = 5;
const minutesPerDay = 24 * 60;
const fallbackVisibleSlots = [
  { start: "05:30", mode: "heat", target_celsius: 18 },
  { start: "08:00", mode: "off" },
  { start: "17:30", mode: "heat", target_celsius: 21 },
  { start: "22:30", mode: "off" },
];
const api = new XturaApi();
const state = {
  route: { screen: "overview", section: null },
  sections: { controls: "heating", more: "system" },
  build: null,
  lights: null,
  water: null,
  heatingMode: null,
  heatingState: null,
  recording: null,
  trackingSettings: null,
  tracking: null,
  tracks: null,
  piStatus: null,
  schedule: null,
  scheduleEditable: false,
  scheduleRenderSignature: "",
  requestInFlight: false,
  countdownRefresh: null,
};

function byId(id) {
  return document.getElementById(id);
}

function formatError(payload, status) {
  if (!payload) {
    return `Request failed (${status})`;
  }
  if (payload.error === "validation_failed" && Array.isArray(payload.details)) {
    return payload.details.map((detail) => detail.message).join("; ");
  }
  return payload.error || `Request failed (${status})`;
}

function formatCelsius(value) {
  if (value === null || value === undefined || Number.isNaN(Number(value))) {
    return "-";
  }
  return `${Number(value).toFixed(1)}C`;
}

function clampTarget(value) {
  return Math.min(24.5, Math.max(5, Math.round(Number(value) * 2) / 2));
}

function clampInteger(value, min, max) {
  const parsed = Number.parseInt(value, 10);
  if (!Number.isFinite(parsed)) {
    return min;
  }
  return Math.min(max, Math.max(min, parsed));
}

function setStatus(message, tone = "normal") {
  const element = byId("statusMessage");
  element.textContent = message;
  element.dataset.tone = tone;
}

function setConnection(message, tone = "normal") {
  const element = byId("connectionStatus");
  element.textContent = message;
  element.dataset.tone = tone;
}

const screenTitles = { overview: "Overview", controls: "Controls", location: "Location", more: "More" };

function applyRoute(route) {
  state.route = route;
  if (route.section) {
    state.sections[route.screen] = route.section;
  }
  const title = screenTitles[route.screen] || "Xtura";
  byId("pageTitle").textContent = title;
  document.title = title;
  ["overview", "controls", "location", "more"].forEach((screen) => {
    byId(`${screen}Nav`).classList.toggle("is-active", route.screen === screen);
    byId(`${screen}Panel`).hidden = route.screen !== screen;
  });
  document.querySelectorAll(".section-switch-item").forEach((button) => {
    button.classList.toggle(
      "is-active",
      button.dataset.sectionGroup === route.screen && button.dataset.section === route.section,
    );
  });
  document.querySelectorAll(".section-panel").forEach((panel) => {
    panel.hidden = !(panel.dataset.sectionGroup === route.screen && panel.dataset.section === route.section);
  });
}

function navigate(screen, section = null) {
  const hash = XturaNavigation.toHash({ screen, section });
  if (window.location.hash === hash) applyRoute(XturaNavigation.parse(hash));
  else window.location.hash = hash;
}

function render() {
  renderLights();
  renderWater();
  renderHeating();
  renderSchedule();
  renderBuild();
  renderRecording();
  renderTracking();
  renderPiStatus();
  syncCountdownRefresh();
}

function renderBuild() {
  const element = byId("buildInfo");
  const build = state.build;
  if (!build) {
    element.textContent = "";
    return;
  }
  const parts = [];
  if (build.deployed_at) {
    const date = new Date(build.deployed_at);
    if (!Number.isNaN(date.getTime())) {
      parts.push(`Deployed ${date.toLocaleString([], { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" })}`);
    }
  }
  parts.push(build.git_sha || "dev");
  element.textContent = parts.join(" · ");
}

function renderRecording() {
  const recording = state.recording;
  const panel = byId("recordingPanel");
  const waitFor = byId("recordingWaitFor");
  const duration = byId("recordingDuration");
  const button = byId("recordingButton");
  if (!recording) {
    panel.setAttribute("aria-busy", "true");
    byId("recordingState").textContent = "Loading";
    byId("recordingDetail").textContent = "Waiting for recording state.";
    waitFor.disabled = true;
    duration.disabled = true;
    button.textContent = "Loading";
    button.disabled = true;
    return;
  }
  const idle = recording.status === "idle";
  const active = recording.status === "armed" || recording.status === "recording";
  panel.setAttribute("aria-busy", String(state.requestInFlight));
  if (active && ["immediate", "engine_on", "heating_on", "victron_on"].includes(recording.wait_for)) {
    waitFor.value = recording.wait_for;
    duration.value = String(recording.duration_minutes || 0);
  }
  byId("recordingState").textContent = recordingStateText(recording.status);
  waitFor.disabled = state.requestInFlight || !idle;
  duration.disabled = state.requestInFlight || !idle;
  button.textContent = idle ? "Start recording" : active ? "Stop recording" : "Unavailable";
  button.disabled = state.requestInFlight || !idle && !active;
  byId("recordingDetail").textContent = recordingDetailText(recording);
}

function recordingStateText(status) {
  if (status === "armed") {
    return "Armed";
  }
  if (status === "recording") {
    return "Recording";
  }
  return status === "idle" ? "Idle" : "Unavailable";
}

function recordingDetailText(recording) {
  let detail;
  if (recording.status === "armed") {
    detail = `Waiting for ${recordingWaitText(recording.wait_for)}; it will record ${recordingDurationText(recording.duration_minutes)}.`;
  } else if (recording.status === "recording") {
    const fileName = recording.file_name ? ` to ${recording.file_name}` : "";
    detail = `Recording${fileName} ${recordingRemainingText(recording)}.`;
  } else {
    detail = recording.last_file_name ? `Last recording: ${recording.last_file_name}.` : "No recording is active.";
  }
  return recording.error ? `${detail} Error: ${recording.error}` : detail;
}

function recordingWaitText(waitFor) {
  const labels = {
    engine_on: "engine on",
    heating_on: "heating on",
    victron_on: "Victron inverter on",
  };
  return labels[waitFor] || "the selected condition";
}

function recordingDurationText(durationMinutes) {
  const minutes = Number(durationMinutes) || 0;
  if (minutes === 0) {
    return "until stopped";
  }
  return `for ${minutes} minute${minutes === 1 ? "" : "s"}`;
}

function recordingRemainingText(recording) {
  const minutes = Number(recording.duration_minutes) || 0;
  if (minutes === 0) {
    return "until stopped";
  }
  const startedAt = new Date(recording.started_at).getTime();
  if (!Number.isFinite(startedAt)) {
    return recordingDurationText(minutes);
  }
  const remainingSeconds = Math.ceil((startedAt + minutes * 60 * 1000 - Date.now()) / 1000);
  if (remainingSeconds <= 0) {
    return "finishing";
  }
  return `${formatRemainingSeconds(remainingSeconds)} remaining`;
}

function renderTracking() {
  const settings = state.trackingSettings;
  const tracking = state.tracking;
  const engineOnly = byId("trackingEngineOnly");
  const manualControls = byId("trackingManualControls");
  const startButton = byId("trackingStartButton");
  const stopButton = byId("trackingStopButton");
  const interval = byId("trackingInterval");
  if (!settings || !tracking) {
    byId("trackingPanel").setAttribute("aria-busy", "true");
    byId("trackingState").textContent = "Loading";
    byId("trackingDetail").textContent = "Waiting for tracking state.";
    engineOnly.disabled = true;
    interval.disabled = true;
    return;
  }
  byId("trackingPanel").setAttribute("aria-busy", String(state.requestInFlight));
  byId("trackingState").textContent = trackingStateText(tracking);
  engineOnly.checked = settings.when_engine_on;
  if (interval !== document.activeElement) {
    interval.value = String(settings.sample_interval_seconds || 5);
  }
  engineOnly.disabled = state.requestInFlight;
  interval.disabled = state.requestInFlight;
  manualControls.hidden = settings.when_engine_on;
  startButton.disabled = state.requestInFlight || (tracking.tracking === true);
  stopButton.disabled = state.requestInFlight || (tracking.tracking !== true);
  byId("trackingDetail").textContent = trackingDetailText(tracking);
  renderTrackList();
}

function trackingStateText(tracking) {
  if (tracking.tracking) {
    return "Tracking";
  }
  if (tracking.when_engine_on && tracking.engine_known && !tracking.engine_on) {
    return "Engine off";
  }
  return "Idle";
}

function trackingDetailText(tracking) {
  const parts = [];
  if (tracking.tracking) {
    parts.push(`Tracking now (${tracking.point_count} point${tracking.point_count === 1 ? "" : "s"}).`);
  } else if (tracking.when_engine_on) {
    if (!tracking.engine_known) {
      parts.push("Waiting to see whether the engine is on.");
    } else if (tracking.engine_on) {
      parts.push("Engine is on; the next sample starts the track.");
    } else {
      parts.push("Engine is off; not tracking.");
    }
  } else {
    parts.push("Press Start recording to begin a track.");
  }
  if (tracking.current_file) {
    parts.push(`Writing ${tracking.current_file}.`);
  }
  if (tracking.last_error) {
    parts.push(`Last error: ${tracking.last_error}`);
  }
  return parts.join(" ") || "No tracking is active.";
}

function currentTrackingSettings() {
  const settings = state.trackingSettings || {};
  return {
    when_engine_on: Boolean(settings.when_engine_on),
    sample_interval_seconds: Number(settings.sample_interval_seconds) || 5,
  };
}

function renderTrackList() {
  const container = byId("trackList");
  container.innerHTML = "";
  const tracks = state.tracks;
  if (!Array.isArray(tracks) || tracks.length === 0) {
    container.appendChild(emptyTrackNote());
    return;
  }
  tracks.forEach((track) => {
    container.appendChild(trackRow(track));
  });
}

function emptyTrackNote() {
  const note = document.createElement("p");
  note.className = "detail-text";
  note.textContent = "No tracks yet.";
  return note;
}

function trackRow(track) {
  const row = document.createElement("div");
  row.className = "track-row";
  const name = document.createElement("span");
  name.className = "track-name";
  name.textContent = track.name;
  name.title = track.name;
  const meta = document.createElement("span");
  meta.className = "track-meta";
  meta.textContent = trackMetaText(track);
  const actions = document.createElement("span");
  actions.className = "track-actions";
  const download = document.createElement("a");
  download.className = "track-action track-download";
  download.href = `/v1/tracks/${encodeURIComponent(track.name)}`;
  download.setAttribute("download", "");
  download.textContent = "Download";
  const remove = document.createElement("button");
  remove.className = "track-action track-delete";
  remove.type = "button";
  remove.textContent = "Delete";
  remove.disabled = state.requestInFlight;
  remove.addEventListener("click", async () => {
    try {
      await withRequest(() => api.trackDelete(track.name), `Deleting ${track.name}`);
      await refreshTracks();
    } catch (_) {
      return;
    }
  });
  actions.append(download, remove);
  row.append(name, meta, actions);
  return row;
}

function trackMetaText(track) {
  const parts = [];
  if (Number.isFinite(track.point_count) && track.point_count > 0) {
    parts.push(`${track.point_count} point${track.point_count === 1 ? "" : "s"}`);
  }
  if (Number.isFinite(track.bytes) && track.bytes > 0) {
    parts.push(formatBytes(track.bytes));
  }
  return parts.join(" · ");
}

function formatBytes(bytes) {
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

async function refreshTracks() {
  try {
    state.tracks = await api.trackList();
  } catch (_) {
    state.tracks = [];
  }
  render();
}

async function applyTrackingSettings(next) {
  try {
    const settings = await withRequest(
      () => api.updateTrackingSettings(next),
      "Saving tracking settings",
    );
    state.trackingSettings = settings;
    if (state.tracking) {
      state.tracking = {
        ...state.tracking,
        when_engine_on: settings.when_engine_on,
        sample_interval_seconds: settings.sample_interval_seconds,
      };
    }
    await refreshTracks();
  } catch (_) {
    return;
  }
}

function renderPiStatus() {
  const status = state.piStatus;
  const panel = byId("piStatusPanel");
  if (!status) {
    panel.setAttribute("aria-busy", "true");
    byId("piPowerState").textContent = "Loading";
    byId("piDetail").textContent = "Waiting for Pi status.";
    byId("piStats").innerHTML = "";
    return;
  }
  panel.setAttribute("aria-busy", "false");
  byId("piPowerState").textContent = piPowerStateText(status);
  byId("piDetail").textContent = status.last_error
    ? `Last error: ${status.last_error}`
    : "Host metrics update every few seconds.";
  renderPiStats(status);
}

function piPowerStateText(status) {
  if (!status.power) {
    return "Unknown";
  }
  if (status.power.status === "ok") {
    return "OK";
  }
  if (status.power.status === "warning") {
    return "Warning";
  }
  return "Unavailable";
}

function renderPiStats(status) {
  const container = byId("piStats");
  container.innerHTML = "";
  if (!status.sampled_at) {
    container.appendChild(piStatRow("Status", "No sample yet"));
    return;
  }
  if (status.model || status.cores > 0) {
    container.appendChild(piStatRow("CPU", status.cores > 0 ? `${status.model} · ${status.cores} cores` : status.model));
  }
  container.appendChild(piStatRow("Load", formatLoad(status.load)));
  container.appendChild(piStatRow("Memory", formatMemory(status.memory)));
  (status.disk || []).forEach((disk) => {
    container.appendChild(piStatRow(`Disk ${disk.mount}`, formatPercent(disk.used_percent)));
  });
  container.appendChild(piStatRow("Temperature", status.temperature_c === undefined ? "Unavailable" : `${Number(status.temperature_c).toFixed(1)}C`));
  container.appendChild(piStatRow("Uptime", formatUptime(status.uptime_seconds)));
  container.appendChild(piStatRow("Power", formatPower(status.power)));
}

function piStatRow(label, value) {
  const row = document.createElement("div");
  row.className = "pi-stat-row";
  const key = document.createElement("span");
  key.className = "pi-stat-label";
  key.textContent = label;
  const val = document.createElement("span");
  val.className = "pi-stat-value";
  val.textContent = value;
  row.append(key, val);
  return row;
}

function formatLoad(load) {
  if (!Array.isArray(load) || load.length === 0) {
    return "Unavailable";
  }
  return load.map((value) => Number(value).toFixed(2)).join(" / ");
}

function formatMemory(memory) {
  if (!memory || !memory.total_bytes) {
    return "Unavailable";
  }
  return `${Math.round(Number(memory.used_percent))}% used · ${formatBytes(memory.available_bytes)} free`;
}

function formatPercent(value) {
  if (!Number.isFinite(Number(value))) {
    return "Unavailable";
  }
  return `${Math.round(Number(value))}%`;
}

function formatUptime(seconds) {
  const total = Math.floor(Number(seconds) || 0);
  if (total <= 0) {
    return "Unavailable";
  }
  const days = Math.floor(total / 86400);
  const hours = Math.floor((total % 86400) / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  if (days > 0) {
    return `${days}d ${hours}h ${minutes}m`;
  }
  if (hours > 0) {
    return `${hours}h ${minutes}m`;
  }
  return `${minutes}m`;
}

const powerIssueLabels = {
  under_voltage: "under-voltage",
  frequency_capped: "frequency capped",
  throttled: "throttled",
  soft_temp_limit: "soft temp limit",
};

function formatPower(power) {
  if (!power || power.status === "unavailable") {
    return "Unavailable";
  }
  if (power.status === "ok") {
    return "OK";
  }
  const parts = [];
  if (power.under_voltage) {
    parts.push("under-voltage now");
  }
  if (power.frequency_capped) {
    parts.push("frequency capped now");
  }
  if (power.throttled) {
    parts.push("throttled now");
  }
  if (power.soft_temp_limit) {
    parts.push("soft temp limit now");
  }
  (power.occurred_since_boot || []).forEach((token) => {
    const label = powerIssueLabels[token] || token;
    parts.push(`${label} since boot`);
  });
  return parts.length > 0 ? parts.join("; ") : "Warning";
}

function renderLights() {
  const lights = state.lights;
  const flashButton = byId("flashLights");
  if (!lights) {
    byId("lightsState").textContent = "Loading";
    byId("lightsDetail").textContent = "Waiting for light state.";
    flashButton.disabled = true;
    return;
  }
  const knownText = lights.external_known ? (lights.external_on ? "On" : "Off") : "Unknown";
  byId("lightsState").textContent = lights.flash_in_progress ? "Flashing" : knownText;
  byId("lightsDetail").textContent = lights.last_command_error
    ? `Last command error: ${lights.last_command_error}`
    : lights.external_known
      ? `Exterior lights are ${knownText.toLowerCase()}.`
      : "Exterior light state has not been observed yet.";
  byId("flashCount").disabled = state.requestInFlight || lights.flash_in_progress;
  flashButton.disabled = state.requestInFlight || lights.flash_in_progress;
}

function renderWater() {
  const water = state.water;
  const openButton = byId("openGreyValve");
  const closeButton = byId("closeGreyValve");
  const scheduleTime = byId("greyScheduleTime");
  const scheduleDuration = byId("greyScheduleDuration");
  const scheduleButton = byId("greyScheduleButton");
  if (!water) {
    byId("waterState").textContent = "Loading";
    byId("waterDetail").textContent = "Waiting for water state.";
    byId("greyScheduleMessage").textContent = "";
    openButton.disabled = true;
    closeButton.disabled = true;
    scheduleTime.disabled = true;
    scheduleDuration.disabled = true;
    scheduleButton.disabled = true;
    return;
  }
  const moving = water.command_in_progress || water.valve_moving;
  const direction = water.valve_direction === "closing" ? "Closing" : "Opening";
  const scheduled = water.scheduled_opening;
  byId("waterState").textContent = moving ? direction : (water.valve_known ? "Idle" : "Unknown");
  byId("waterDetail").textContent = water.last_command_error
    ? `Last command error: ${water.last_command_error}`
    : moving
      ? "Holding the valve control for five seconds."
      : "Grey water valve is ready.";
  openButton.disabled = state.requestInFlight || water.command_in_progress;
  closeButton.disabled = state.requestInFlight || water.command_in_progress;
  if (scheduled) {
    scheduleTime.value = scheduled.local_time || scheduleTime.value || "03:00";
    scheduleDuration.value = String(scheduled.duration_minutes || 30);
    scheduleButton.textContent = "Cancel";
    byId("greyScheduleMessage").textContent = `Scheduled for ${formatScheduledOpen(scheduled.open_at)} for ${scheduled.duration_minutes} minutes.`;
  } else {
    scheduleButton.textContent = "Schedule";
    byId("greyScheduleMessage").textContent = water.last_schedule_message || "";
  }
  scheduleTime.disabled = state.requestInFlight || Boolean(scheduled);
  scheduleDuration.disabled = state.requestInFlight || Boolean(scheduled);
  scheduleButton.disabled = state.requestInFlight;
}

function formatScheduledOpen(openAt) {
  const date = new Date(openAt);
  if (Number.isNaN(date.getTime())) {
    return "the scheduled time";
  }
  return date.toLocaleString([], {
    weekday: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function renderHeating() {
  const mode = state.heatingMode;
  const boostButton = byId("boostButton");
  const boostRunning = byId("boostRunning");
  const cancelBoostButton = byId("cancelBoostButton");
  if (!mode) {
    byId("modeState").textContent = "Loading";
    byId("targetState").textContent = "Set point";
    byId("modeDetail").textContent = "Waiting for heating mode.";
    updateModeSwitch("");
    boostButton.disabled = true;
    boostButton.hidden = false;
    boostRunning.hidden = true;
    updateTargetValue(18);
    return;
  }
  byId("modeState").textContent = mode.mode || "Unknown";
  byId("targetState").textContent = "Set point";
  updateModeSwitch(mode.mode);
  const target = currentTarget();
  updateTargetValue(target);
  if (mode.mode === "boost" && mode.boost) {
    byId("modeDetail").textContent = "Boost overrides the schedule until it is cancelled or expires.";
    byId("boostTarget").textContent = formatCelsius(mode.boost.target_celsius);
    byId("boostRemaining").textContent = boostRemainingText(mode.boost.expires_at);
    boostButton.hidden = true;
    boostRunning.hidden = false;
  } else if (mode.mode === "manual") {
    byId("modeDetail").textContent = `Manual target ${formatCelsius(mode.manual_target_celsius)}.`;
    boostButton.hidden = false;
    boostRunning.hidden = true;
  } else if (mode.mode === "schedule") {
    byId("modeDetail").textContent = state.heatingState && state.heatingState.target_temperature_known
      ? `Following schedule. Current target ${formatCelsius(state.heatingState.target_temperature_c)}.`
      : "Following schedule.";
    boostButton.hidden = false;
    boostRunning.hidden = true;
  } else {
    byId("modeDetail").textContent = "Heating is forced off.";
    boostButton.hidden = false;
    boostRunning.hidden = true;
  }
  boostButton.disabled = state.requestInFlight;
  cancelBoostButton.disabled = state.requestInFlight;
}

function updateModeSwitch(mode) {
  byId("modeOn").classList.toggle("is-active", mode === "manual" || mode === "boost");
  byId("modeSchedule").classList.toggle("is-active", mode === "schedule");
  byId("modeOff").classList.toggle("is-active", mode === "off");
  byId("modeOn").disabled = state.requestInFlight;
  byId("modeSchedule").disabled = state.requestInFlight;
  byId("modeOff").disabled = state.requestInFlight;
}

function boostRemainingText(expiresAt) {
  const remainingMs = new Date(expiresAt).getTime() - Date.now();
  if (!Number.isFinite(remainingMs) || remainingMs <= 0) {
    return "expires soon";
  }
  return `${formatRemainingSeconds(Math.ceil(remainingMs / 1000))} remaining`;
}

function formatRemainingSeconds(totalSeconds) {
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes >= 60) {
    const hours = Math.floor(minutes / 60);
    const remainder = minutes % 60;
    return `${hours}h ${remainder}m`;
  }
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

function syncCountdownRefresh() {
  const hasActiveBoost = Boolean(state.heatingMode && state.heatingMode.mode === "boost" && state.heatingMode.boost);
  const hasTimedRecording = Boolean(state.recording && state.recording.status === "recording" && Number(state.recording.duration_minutes) > 0);
  if ((hasActiveBoost || hasTimedRecording) && state.countdownRefresh === null) {
    state.countdownRefresh = window.setInterval(() => {
      if (state.heatingMode && state.heatingMode.mode === "boost" && state.heatingMode.boost) {
        byId("boostRemaining").textContent = boostRemainingText(state.heatingMode.boost.expires_at);
      }
      if (state.recording && state.recording.status === "recording") {
        renderRecording();
      }
    }, 1000);
  }
  if (!hasActiveBoost && !hasTimedRecording && state.countdownRefresh !== null) {
    window.clearInterval(state.countdownRefresh);
    state.countdownRefresh = null;
  }
}

function currentTarget() {
  const mode = state.heatingMode;
  if (mode && mode.mode === "boost" && mode.boost) {
    return clampTarget(mode.boost.target_celsius);
  }
  if (mode && mode.mode === "manual" && mode.manual_target_celsius !== undefined) {
    return clampTarget(mode.manual_target_celsius);
  }
  if (state.heatingState && state.heatingState.target_temperature_known) {
    return clampTarget(state.heatingState.target_temperature_c);
  }
  return 18;
}

function updateTargetValue(target) {
  byId("targetValue").textContent = formatCelsius(clampTarget(target));
}

function editableProgram(schedule) {
  if (!schedule || !Array.isArray(schedule.programs)) {
    return null;
  }
  const enabled = schedule.programs.filter((program) => program.enabled);
  if (enabled.length === 0 && schedule.programs.length === 0) {
    return {
      id: "everyday-default",
      enabled: true,
      days: allDays,
      periods: [{ start: "00:00", mode: "off" }],
    };
  }
  if (enabled.length !== 1) {
    return null;
  }
  const program = enabled[0];
  const dayKey = [...program.days].sort().join(",");
  if (dayKey !== allDaysKey) {
    return null;
  }
  return program;
}

function visiblePeriods(program) {
  const periods = program.periods || [];
  const visible = periods
    .filter((period) => !(period.start === "00:00" && period.mode === "off"))
    .map((period) => ({ ...period }))
    .slice(0, scheduleSlotCount);
  const usedStarts = new Set(visible.map((period) => period.start));
  for (const fallback of fallbackVisibleSlots) {
    if (visible.length >= scheduleSlotCount) {
      break;
    }
    if (!usedStarts.has(fallback.start)) {
      visible.push({ ...fallback });
      usedStarts.add(fallback.start);
    }
  }
  visible.sort((a, b) => a.start.localeCompare(b.start));
  while (visible.length < scheduleSlotCount) {
    const previous = visible[visible.length - 1];
    const nextStart = previous ? addMinutes(previous.start, 60) : "06:00";
    visible.push({ start: nextStart, mode: "off" });
  }
  const normalizedStarts = normalizeSlotStarts(visible.map((period) => timeToMinutes(period.start)), 0);
  normalizedStarts.forEach((minutes, index) => {
    visible[index].start = minutesToTime(minutes);
  });
  return visible;
}

function addMinutes(start, minutes) {
  return minutesToTime(timeToMinutes(start) + minutes);
}

function timeToMinutes(time) {
  const [hour, minute] = String(time || "00:00").split(":").map(Number);
  if (!Number.isFinite(hour) || !Number.isFinite(minute)) {
    return 0;
  }
  return Math.min(minutesPerDay - 1, Math.max(0, hour * 60 + minute));
}

function minutesToTime(minutes) {
  const total = ((Math.round(minutes) % minutesPerDay) + minutesPerDay) % minutesPerDay;
  return `${String(Math.floor(total / 60)).padStart(2, "0")}:${String(total % 60).padStart(2, "0")}`;
}

function minStartForSlot(index) {
  return (index + 1) * minimumSlotMinutes;
}

function maxStartForSlot(index) {
  return minutesPerDay - ((scheduleSlotCount - index) * minimumSlotMinutes);
}

function normalizeSlotStarts(starts, editedIndex) {
  const normalized = starts.map((start, index) => {
    const fallback = timeToMinutes(fallbackVisibleSlots[index]?.start || addMinutes("06:00", index * 60));
    const minutes = Number.isFinite(start) ? start : fallback;
    return Math.min(maxStartForSlot(index), Math.max(minStartForSlot(index), minutes));
  });
  normalized[editedIndex] = Math.min(
    maxStartForSlot(editedIndex),
    Math.max(minStartForSlot(editedIndex), normalized[editedIndex]),
  );
  for (let index = editedIndex - 1; index >= 0; index -= 1) {
    normalized[index] = Math.min(normalized[index], normalized[index + 1] - minimumSlotMinutes);
    normalized[index] = Math.max(normalized[index], minStartForSlot(index));
  }
  for (let index = editedIndex + 1; index < scheduleSlotCount; index += 1) {
    normalized[index] = Math.max(normalized[index], normalized[index - 1] + minimumSlotMinutes);
    normalized[index] = Math.min(normalized[index], maxStartForSlot(index));
  }
  return normalized;
}

function renderSchedule() {
  const slots = byId("scheduleSlots");
  const schedule = state.schedule;
  if (!schedule) {
    byId("scheduleState").textContent = "Loading";
    byId("scheduleDetail").textContent = "Waiting for schedule.";
    byId("saveSchedule").disabled = true;
    if (!hasActiveScheduleControl()) {
      slots.innerHTML = "";
      state.scheduleRenderSignature = "";
    }
    return;
  }
  const program = editableProgram(schedule);
  state.scheduleEditable = Boolean(program);
  if (!program) {
    byId("scheduleState").textContent = "Unsupported";
    byId("scheduleDetail").textContent = "This editor only supports one enabled all-days schedule.";
    byId("saveSchedule").disabled = true;
    if (!hasActiveScheduleControl()) {
      slots.innerHTML = "";
      state.scheduleRenderSignature = "";
    }
    return;
  }
  byId("scheduleState").textContent = "Every day";
  byId("scheduleDetail").textContent = "Each slot ends when the next one starts. The final slot ends at midnight.";
  byId("saveSchedule").disabled = state.requestInFlight;
  const signature = scheduleRenderSignature(schedule, program);
  if (slots.children.length > 0 && (state.scheduleRenderSignature === signature || hasActiveScheduleControl())) {
    updateScheduleEndTimes();
    return;
  }
  slots.innerHTML = "";
  visiblePeriods(program).forEach((period, index) => {
    slots.appendChild(scheduleSlot(period, index));
  });
  state.scheduleRenderSignature = signature;
  updateScheduleEndTimes();
}

function scheduleRenderSignature(schedule, program) {
  return JSON.stringify({
    timezone: schedule.timezone || "",
    revision: schedule.revision || "",
    id: program.id || "",
    enabled: Boolean(program.enabled),
    days: program.days || [],
    periods: program.periods || [],
  });
}

function hasActiveScheduleControl() {
  const form = byId("scheduleForm");
  return Boolean(form && form.contains(document.activeElement));
}

function scheduleSlot(period, index) {
  const row = document.createElement("div");
  row.className = "schedule-slot";
  row.innerHTML = `
    <div class="field field-start">
      <label for="slotStart${index}">Start</label>
      <input id="slotStart${index}" name="start" type="time" value="${period.start || ""}">
    </div>
    <div class="field field-mode">
      <label for="slotMode${index}">Mode</label>
      <select id="slotMode${index}" name="mode">
        <option value="heat">Heat</option>
        <option value="off">Off</option>
      </select>
    </div>
    <div class="field field-target">
      <label for="slotTarget${index}">Target</label>
      <input id="slotTarget${index}" name="target" type="number" min="5" max="24.5" step="0.5" value="${period.target_celsius || 18}">
    </div>
    <div class="field field-end">
      <label>Ends</label>
      <div class="end-time" data-end-time>--:--</div>
    </div>
  `;
  const start = row.querySelector("[name='start']");
  const mode = row.querySelector("[name='mode']");
  const target = row.querySelector("[name='target']");
  mode.value = period.mode || "off";
  start.addEventListener("change", () => enforceScheduleStarts(index));
  applyScheduleTargetMode(mode, target);
  mode.addEventListener("change", () => applyScheduleTargetMode(mode, target));
  return row;
}

function applyScheduleTargetMode(mode, target) {
  if (mode.value === "off") {
    target.dataset.heatValue = target.type === "number" ? target.value : target.dataset.heatValue || "18";
    target.type = "text";
    target.value = "-";
    target.disabled = true;
    return;
  }
  target.disabled = false;
  target.type = "number";
  target.min = "5";
  target.max = "24.5";
  target.step = "0.5";
  target.value = target.dataset.heatValue || target.value || "18";
}

function scheduleRows() {
  return Array.from(document.querySelectorAll(".schedule-slot"));
}

function enforceScheduleStarts(editedIndex) {
  const rows = scheduleRows();
  const starts = rows.map((row) => timeToMinutes(row.querySelector("[name='start']").value));
  normalizeSlotStarts(starts, editedIndex).forEach((minutes, index) => {
    rows[index].querySelector("[name='start']").value = minutesToTime(minutes);
  });
  updateScheduleEndTimes();
}

function updateScheduleEndTimes() {
  const rows = scheduleRows();
  rows.forEach((row, index) => {
    const end = index < rows.length - 1
      ? rows[index + 1].querySelector("[name='start']").value
      : "00:00";
    row.querySelector("[data-end-time]").textContent = end || "--:--";
  });
}

function scheduleFromForm() {
  const originalProgram = editableProgram(state.schedule);
  if (!originalProgram) {
    throw new Error("Unsupported schedule shape.");
  }
  const periods = [{ start: "00:00", mode: "off" }];
  enforceScheduleStarts(0);
  scheduleRows().forEach((row) => {
    const start = row.querySelector("[name='start']").value;
    const mode = row.querySelector("[name='mode']").value;
    const period = { start, mode };
    if (mode === "heat") {
      const target = clampTarget(row.querySelector("[name='target']").value);
      period.target_celsius = target;
    }
    periods.push(period);
  });
  periods.sort((a, b) => a.start.localeCompare(b.start));
  const deduped = periods.filter((period, index, list) => index === 0 || period.start !== list[index - 1].start);
  return {
    timezone: state.schedule.timezone || "Europe/London",
    revision: state.schedule.revision,
    programs: [{
      id: originalProgram.id || "everyday-default",
      enabled: true,
      days: allDays,
      periods: deduped,
    }],
  };
}

async function withRequest(action, busyMessage) {
  state.requestInFlight = true;
  setStatus(busyMessage);
  render();
  try {
    const result = await action();
    setStatus("Saved");
    return result;
  } catch (error) {
    if (error.status === 409) {
      setStatus("Busy", "warning");
    } else {
      setStatus(error.message, "error");
    }
    throw error;
  } finally {
    state.requestInFlight = false;
    render();
  }
}

async function loadInitialState() {
  const [lights, water, mode, schedule, build, recording, trackingSettings, tracking, tracks, piStatus] = await Promise.all([
    api.getLightsState(),
    api.getWaterState(),
    api.getHeatingMode(),
    api.getHeatingSchedule(),
    api.getBuildInfo(),
    api.recordingState(),
    api.trackingSettings(),
    api.trackingState(),
    api.trackList(),
    api.getPiStatus(),
  ]);
  state.lights = lights;
  state.water = water;
  state.heatingMode = mode;
  state.schedule = schedule;
  state.build = build;
  state.recording = recording;
  state.trackingSettings = trackingSettings;
  state.tracking = tracking;
  state.tracks = tracks;
  state.piStatus = piStatus;
  setStatus("Loaded");
  render();
}

function connectEvents() {
  const events = new EventSource("/v1/events");
  events.onopen = () => setConnection("Online");
  events.onerror = () => setConnection("Reconnecting", "warning");
  events.addEventListener("lights.state_changed", (event) => {
    state.lights = JSON.parse(event.data).payload;
    render();
  });
  events.addEventListener("water.state_changed", (event) => {
    state.water = JSON.parse(event.data).payload;
    render();
  });
  events.addEventListener("heating.mode_changed", (event) => {
    state.heatingMode = JSON.parse(event.data).payload;
    render();
  });
  events.addEventListener("heating.state_changed", (event) => {
    state.heatingState = JSON.parse(event.data).payload;
    render();
  });
  events.addEventListener("automation.schedule_updated", (event) => {
    state.schedule = JSON.parse(event.data).payload;
    render();
  });
  events.addEventListener("recording.state_changed", (event) => {
    state.recording = JSON.parse(event.data).payload;
    render();
  });
  events.addEventListener("tracking.state_changed", (event) => {
    state.tracking = JSON.parse(event.data).payload;
    render();
  });
  events.addEventListener("pi.state_changed", (event) => {
    state.piStatus = JSON.parse(event.data).payload;
    render();
  });
}

function bindActions() {
  document.querySelectorAll("[data-screen]").forEach((button) => {
    button.addEventListener("click", () => navigate(button.dataset.screen, state.sections[button.dataset.screen] || null));
  });
  document.querySelectorAll("[data-section-group]").forEach((button) => {
    button.addEventListener("click", () => navigate(button.dataset.sectionGroup, button.dataset.section));
  });
  byId("flashLights").addEventListener("click", async () => {
    try {
      const count = flashCount();
      const lights = await withRequest(() => api.flashExteriorLights(count), `Flashing exterior lights ${count} time${count === 1 ? "" : "s"}`);
      state.lights = lights;
    } catch (_) {
      return;
    }
  });
  byId("flashCount").addEventListener("change", () => {
    byId("flashCount").value = String(flashCount());
  });
  byId("openGreyValve").addEventListener("click", async () => {
    try {
      const water = await withRequest(() => api.openGreyValve(), "Opening grey water valve");
      state.water = water;
    } catch (_) {
      return;
    }
  });
  byId("closeGreyValve").addEventListener("click", async () => {
    try {
      const water = await withRequest(() => api.closeGreyValve(), "Closing grey water valve");
      state.water = water;
    } catch (_) {
      return;
    }
  });
  byId("greyScheduleButton").addEventListener("click", async () => {
    try {
      const scheduled = byId("greyScheduleButton").textContent === "Cancel";
      const water = scheduled
        ? await withRequest(() => api.cancelGreyValveSchedule(), "Cancelling grey water schedule")
        : await withRequest(
          () => api.scheduleGreyValve(greyScheduleTime(), greyScheduleDuration()),
          "Scheduling grey water valve",
        );
      state.water = water;
    } catch (_) {
      return;
    }
  });
  byId("greyScheduleDuration").addEventListener("change", () => {
    byId("greyScheduleDuration").value = String(greyScheduleDuration());
  });
  byId("recordingButton").addEventListener("click", async () => {
    try {
      const recording = state.recording && state.recording.status === "idle"
        ? await withRequest(
          () => api.startRecording(byId("recordingWaitFor").value, recordingDuration()),
          "Starting recording",
        )
        : await withRequest(() => api.stopRecording(), "Stopping recording");
      state.recording = recording;
      render();
    } catch (_) {
      return;
    }
  });
  byId("recordingDuration").addEventListener("change", () => {
    byId("recordingDuration").value = String(recordingDuration());
  });
  byId("trackingEngineOnly").addEventListener("change", () => {
    applyTrackingSettings({ ...currentTrackingSettings(), when_engine_on: byId("trackingEngineOnly").checked });
  });
  byId("trackingStartButton").addEventListener("click", async () => {
    try {
      state.tracking = await withRequest(() => api.startTracking(), "Starting tracking");
      render();
    } catch (_) {
      return;
    }
  });
  byId("trackingStopButton").addEventListener("click", async () => {
    try {
      state.tracking = await withRequest(() => api.stopTracking(), "Stopping tracking");
      render();
    } catch (_) {
      return;
    }
  });
  byId("trackingInterval").addEventListener("change", () => {
    const input = byId("trackingInterval");
    const seconds = clampInteger(input.value, 1, 3600);
    input.value = String(seconds);
    if (seconds === Number(currentTrackingSettings().sample_interval_seconds)) {
      return;
    }
    applyTrackingSettings({ ...currentTrackingSettings(), sample_interval_seconds: seconds });
  });
  document.querySelectorAll("[data-target]").forEach((button) => {
    button.addEventListener("click", async () => {
      try {
        const mode = await withRequest(() => api.setHeatingModeManual(Number(button.dataset.target)), "Setting target");
        state.heatingMode = mode;
      } catch (_) {
        return;
      }
    });
  });
  byId("modeOn").addEventListener("click", async () => {
    try {
      const mode = await withRequest(() => api.setHeatingModeManual(currentTarget()), "Turning heating on");
      state.heatingMode = mode;
    } catch (_) {
      return;
    }
  });
  byId("modeSchedule").addEventListener("click", async () => {
    try {
      const mode = await withRequest(() => api.setHeatingModeSchedule(), "Resuming schedule");
      state.heatingMode = mode;
    } catch (_) {
      return;
    }
  });
  byId("modeOff").addEventListener("click", async () => {
    try {
      const mode = await withRequest(() => api.setHeatingModeOff(), "Turning heating off");
      state.heatingMode = mode;
    } catch (_) {
      return;
    }
  });
  byId("targetDown").addEventListener("click", () => adjustTarget(-0.5));
  byId("targetUp").addEventListener("click", () => adjustTarget(0.5));
  byId("boostButton").addEventListener("click", async () => {
    try {
      const mode = await withRequest(() => api.setHeatingModeBoost(21, 60), "Starting boost");
      state.heatingMode = mode;
    } catch (_) {
      return;
    }
  });
  byId("cancelBoostButton").addEventListener("click", async () => {
    try {
      const mode = await withRequest(() => api.cancelHeatingModeBoost(), "Cancelling boost");
      state.heatingMode = mode;
    } catch (_) {
      return;
    }
  });
  byId("scheduleForm").addEventListener("submit", async (event) => {
    event.preventDefault();
    try {
      const next = scheduleFromForm();
      const schedule = await withRequest(() => api.saveHeatingSchedule(next), "Saving schedule");
      state.schedule = schedule;
    } catch (error) {
      setStatus(error.message, "error");
    }
  });
}

function flashCount() {
  const input = byId("flashCount");
  const count = clampInteger(input.value, 1, 5);
  input.value = String(count);
  return count;
}

function greyScheduleTime() {
  const input = byId("greyScheduleTime");
  if (!/^\d{2}:\d{2}$/.test(input.value)) {
    input.value = "03:00";
  }
  return input.value;
}

function greyScheduleDuration() {
  const input = byId("greyScheduleDuration");
  const duration = clampInteger(input.value, 1, 1440);
  input.value = String(duration);
  return duration;
}

function recordingDuration() {
  const input = byId("recordingDuration");
  const duration = Number.parseInt(input.value, 10);
  if (!Number.isFinite(duration) || duration < 0) {
    input.value = "0";
    return 0;
  }
  input.value = String(duration);
  return duration;
}

async function adjustTarget(delta) {
  const next = clampTarget(currentTarget() + delta);
  try {
    const mode = await withRequest(() => api.setHeatingModeManual(next), "Setting target");
    state.heatingMode = mode;
  } catch (_) {
    return;
  }
}

async function boot() {
  bindActions();
  window.addEventListener("hashchange", () => applyRoute(XturaNavigation.parse(window.location.hash)));
  applyRoute(XturaNavigation.parse(window.location.hash));
  render();
  try {
    await loadInitialState();
    connectEvents();
  } catch (error) {
    setConnection("Offline", "error");
    setStatus(error.message, "error");
  }
}

document.addEventListener("DOMContentLoaded", boot);
