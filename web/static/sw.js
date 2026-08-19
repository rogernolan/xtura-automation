self.addEventListener("push", (event) => {
  const data = event.data ? event.data.json() : {};
  const side = data.side === "offline" ? "Offline" : data.side === "high" ? "High" : "Low";
  const body = data.side === "offline"
    ? `${data.sensor_name || data.sensor_id} has been offline for more than 30 minutes.`
    : `${data.sensor_name || data.sensor_id}: ${Number(data.temperature_c).toFixed(1)}C (limit ${Number(data.limit_celsius).toFixed(1)}C)`;
  event.waitUntil(self.registration.showNotification(`${data.alert_name || "Temperature alert"}: ${side}`, {
    body,
    tag: `xtura-temperature-${data.alert_id || "alert"}`,
    data: { url: "/#/more/settings" },
  }));
});
self.addEventListener("notificationclick", (event) => { event.notification.close(); event.waitUntil(clients.matchAll({ type: "window", includeUncontrolled: true }).then((windows) => { if (windows.length) { windows[0].focus(); windows[0].navigate(event.notification.data.url); } else { clients.openWindow(event.notification.data.url); } })); });
