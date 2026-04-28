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

  async saveHeatingSchedule(document) {
    return this.request("/v1/automation/heating-schedule", {
      method: "PUT",
      body: document,
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
  activeTab: "lighting",
  lights: null,
  heatingMode: null,
  heatingState: null,
  schedule: null,
  scheduleEditable: false,
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

function setActiveTab(tab) {
  state.activeTab = tab;
  byId("lightingTab").classList.toggle("is-active", tab === "lighting");
  byId("heatingTab").classList.toggle("is-active", tab === "heating");
  byId("lightingPanel").hidden = tab !== "lighting";
  byId("heatingPanel").hidden = tab !== "heating";
}

function render() {
  renderLights();
  renderHeating();
  renderSchedule();
  syncCountdownRefresh();
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
  const totalSeconds = Math.ceil(remainingMs / 1000);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes >= 60) {
    const hours = Math.floor(minutes / 60);
    const remainder = minutes % 60;
    return `${hours}h ${remainder}m remaining`;
  }
  return `${minutes}:${String(seconds).padStart(2, "0")} remaining`;
}

function syncCountdownRefresh() {
  const hasActiveBoost = Boolean(state.heatingMode && state.heatingMode.mode === "boost" && state.heatingMode.boost);
  if (hasActiveBoost && state.countdownRefresh === null) {
    state.countdownRefresh = window.setInterval(() => {
      if (state.heatingMode && state.heatingMode.mode === "boost" && state.heatingMode.boost) {
        byId("boostRemaining").textContent = boostRemainingText(state.heatingMode.boost.expires_at);
      }
    }, 1000);
  }
  if (!hasActiveBoost && state.countdownRefresh !== null) {
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
  slots.innerHTML = "";
  const schedule = state.schedule;
  if (!schedule) {
    byId("scheduleState").textContent = "Loading";
    byId("scheduleDetail").textContent = "Waiting for schedule.";
    byId("saveSchedule").disabled = true;
    return;
  }
  const program = editableProgram(schedule);
  state.scheduleEditable = Boolean(program);
  if (!program) {
    byId("scheduleState").textContent = "Unsupported";
    byId("scheduleDetail").textContent = "This editor only supports one enabled all-days schedule.";
    byId("saveSchedule").disabled = true;
    return;
  }
  byId("scheduleState").textContent = "Every day";
  byId("scheduleDetail").textContent = "Each slot ends when the next one starts. The final slot ends at midnight.";
  byId("saveSchedule").disabled = state.requestInFlight;
  visiblePeriods(program).forEach((period, index) => {
    slots.appendChild(scheduleSlot(period, index));
  });
  updateScheduleEndTimes();
}

function scheduleSlot(period, index) {
  const row = document.createElement("div");
  row.className = "schedule-slot";
  row.innerHTML = `
    <div class="field">
      <label for="slotStart${index}">Start</label>
      <input id="slotStart${index}" name="start" type="time" value="${period.start || ""}">
    </div>
    <div class="field">
      <label>End</label>
      <div class="end-time" data-end-time>--:--</div>
    </div>
    <div class="field">
      <label for="slotMode${index}">Mode</label>
      <select id="slotMode${index}" name="mode">
        <option value="heat">Heat</option>
        <option value="off">Off</option>
      </select>
    </div>
    <div class="field">
      <label for="slotTarget${index}">Target</label>
      <input id="slotTarget${index}" name="target" type="number" min="5" max="24.5" step="0.5" value="${period.target_celsius || 18}">
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
  const [lights, mode, schedule] = await Promise.all([
    api.getLightsState(),
    api.getHeatingMode(),
    api.getHeatingSchedule(),
  ]);
  state.lights = lights;
  state.heatingMode = mode;
  state.schedule = schedule;
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
}

function bindActions() {
  byId("lightingTab").addEventListener("click", () => setActiveTab("lighting"));
  byId("heatingTab").addEventListener("click", () => setActiveTab("heating"));
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
  setActiveTab("lighting");
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
