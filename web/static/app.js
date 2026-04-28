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
const api = new XturaApi();
const state = {
  activeTab: "lighting",
  lights: null,
  heatingMode: null,
  heatingState: null,
  schedule: null,
  scheduleEditable: false,
  requestInFlight: false,
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
  flashButton.disabled = state.requestInFlight || lights.flash_in_progress;
}

function renderHeating() {
  const mode = state.heatingMode;
  const boostButton = byId("boostButton");
  const cancelBoostButton = byId("cancelBoostButton");
  if (!mode) {
    byId("modeState").textContent = "Loading";
    byId("modeDetail").textContent = "Waiting for heating mode.";
    boostButton.disabled = true;
    cancelBoostButton.hidden = true;
    updateTargetValue(18);
    return;
  }
  byId("modeState").textContent = mode.mode || "Unknown";
  const target = currentTarget();
  updateTargetValue(target);
  if (mode.mode === "boost" && mode.boost) {
    const expires = new Date(mode.boost.expires_at);
    byId("modeDetail").textContent = `Boosting to ${formatCelsius(mode.boost.target_celsius)} until ${expires.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}.`;
    cancelBoostButton.hidden = false;
  } else if (mode.mode === "manual") {
    byId("modeDetail").textContent = `Manual target ${formatCelsius(mode.manual_target_celsius)}.`;
    cancelBoostButton.hidden = true;
  } else if (mode.mode === "schedule") {
    byId("modeDetail").textContent = state.heatingState && state.heatingState.target_temperature_known
      ? `Following schedule. Current target ${formatCelsius(state.heatingState.target_temperature_c)}.`
      : "Following schedule.";
    cancelBoostButton.hidden = true;
  } else {
    byId("modeDetail").textContent = "Heating is forced off.";
    cancelBoostButton.hidden = true;
  }
  boostButton.disabled = state.requestInFlight;
  cancelBoostButton.disabled = state.requestInFlight;
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
    .sort((a, b) => a.start.localeCompare(b.start))
    .slice(0, 4);
  while (visible.length < 4) {
    visible.push({ start: "", mode: "off" });
  }
  return visible;
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
  byId("scheduleDetail").textContent = "Four visible slots. Midnight coverage is saved automatically.";
  byId("saveSchedule").disabled = state.requestInFlight;
  visiblePeriods(program).forEach((period, index) => {
    slots.appendChild(scheduleSlot(period, index));
  });
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
  const mode = row.querySelector("[name='mode']");
  const target = row.querySelector("[name='target']");
  mode.value = period.mode || "off";
  target.disabled = mode.value === "off";
  mode.addEventListener("change", () => {
    target.disabled = mode.value === "off";
  });
  return row;
}

function scheduleFromForm() {
  const originalProgram = editableProgram(state.schedule);
  if (!originalProgram) {
    throw new Error("Unsupported schedule shape.");
  }
  const periods = [{ start: "00:00", mode: "off" }];
  document.querySelectorAll(".schedule-slot").forEach((row) => {
    const start = row.querySelector("[name='start']").value;
    const mode = row.querySelector("[name='mode']").value;
    const target = clampTarget(row.querySelector("[name='target']").value);
    if (!start) {
      return;
    }
    const period = { start, mode };
    if (mode === "heat") {
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
      const lights = await withRequest(() => api.flashExteriorLights(1), "Flashing exterior lights");
      state.lights = lights;
    } catch (_) {
      return;
    }
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
