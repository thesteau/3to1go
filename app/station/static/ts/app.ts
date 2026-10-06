let _appStarted = false;

function startStationApp(): void {
  if (_appStarted) return;
  if (!currentUser) {
    openLoginDialog();
    return;
  }
  if (currentUser.must_change_password) {
    openPasswordDialog(true);
    return;
  }
  _appStarted = true;
  loadOverview({ force: true });
  window.setInterval(() => loadOverview({ silent: true, notifyNewSnapshots: true }), STATION_REFRESH_MS);
}

applyTheme("dark");
document.getElementById("hook_pre_command")?.addEventListener("input", () => {
  _hookDraftDirty.pre = true;
});
document.getElementById("hook_post_command")?.addEventListener("input", () => {
  _hookDraftDirty.post = true;
});

let connecting = false;
async function connectApp(): Promise<void> {
  if (connecting) return;
  connecting = true;
  const status = document.getElementById("connection-status")!;
  status.textContent = "Connecting...";
  try {
    const user = await refreshSession();
    status.textContent = "";
    if (!user) {
      openLoginDialog();
      return;
    }
    if (user.must_change_password) {
      openPasswordDialog(true);
      return;
    }
    startStationApp();
  } catch {
    status.innerHTML = 'Could not connect. <button type="button" onclick="connectApp()">Retry connection</button>';
  } finally {
    connecting = false;
  }
}
connectApp();
