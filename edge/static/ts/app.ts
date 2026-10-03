let _appStarted = false;

function startEdgeApp(): void {
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
  if (window.location.protocol === "http:") {
    showToast(
      "Edge is running over plain HTTP. Credentials sent to Central are not encrypted in transit. Consider setting up HTTPS.",
      "warning",
      { duration: 12000 },
    );
  }
  resetForm();
  initializeFieldHelp(EDGE_SETTINGS_HELP);
  document.getElementById("settings_cron_schedule")?.addEventListener("input", updateCronScheduleHint);
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) {
      loadData({ silent: true, includeKey: false });
    }
  });
  _edgeAutoRefreshStarted = true;
  // Show the last jobs and folders from this tab at once, then load fresh data.
  restoreEdgeView();
  loadData();
}

applyTheme("dark");
initMeta();
document.getElementById("hook_pre_command")?.addEventListener("input", () => {
  hookDraftDirty.pre = true;
});
document.getElementById("hook_post_command")?.addEventListener("input", () => {
  hookDraftDirty.post = true;
});
document.getElementById("recover-fingerprint")?.addEventListener("input", resetRecoverPreview);

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
    startEdgeApp();
  } catch {
    status.innerHTML = 'Could not connect. <button type="button" onclick="connectApp()">Retry connection</button>';
  } finally {
    connecting = false;
  }
}
connectApp();
