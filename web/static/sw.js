self.addEventListener("push", (event) => {
  const data = event.data ? event.data.json() : {};
  const side = data.side === "high" ? "High" : "Low";
  event.waitUntil(self.registration.showNotification(`${data.alert_name || "Temperature alert"}: ${side}`, {
    body: `${data.sensor_name || data.sensor_id}: ${Number(data.temperature_c).toFixed(1)}C (limit ${Number(data.limit_celsius).toFixed(1)}C)`,
    tag: `xtura-temperature-${data.alert_id || "alert"}`,
    data: { url: "/#/more/settings" },
  }));
});
self.addEventListener("notificationclick", (event) => { event.notification.close(); event.waitUntil(clients.matchAll({ type: "window", includeUncontrolled: true }).then((windows) => { if (windows.length) { windows[0].focus(); windows[0].navigate(event.notification.data.url); } else { clients.openWindow(event.notification.data.url); } })); });
