let _appStarted = false;

const DIALOG_FRAGMENTS = [
  "login-dialog",
  "password-dialog",
  "users-dialog",
  "settings-dialog",
  "ntfy-dialog",
  "hooks-dialog",
  "hook-view-dialog",
  "job-dialog",
  "recover-dialog",
  "app-dialog",
];

async function loadDialogs() {
  const fragments = await Promise.all(
    DIALOG_FRAGMENTS.map((name) =>
      fetch(`/static/html/${name}.html`).then((r) => r.text()),
    ),
  );
  const container = document.createElement("div");
  container.innerHTML = fragments.join("");
  Array.from(container.children).forEach((el) => document.body.appendChild(el));
}

function startEdgeApp() {
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
  resetForm();
  initializeFieldHelp(EDGE_SETTINGS_HELP);
  document.getElementById("settings_cron_schedule")?.addEventListener("input", updateCronScheduleHint);
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) {
      loadData({ silent: true, includeKey: false });
    }
  });
  initMeta();
  _edgeAutoRefreshStarted = true;
  loadData();
}

applyTheme("dark");
loadDialogs().then(() => {
  document.getElementById("hook_pre_command")?.addEventListener("input", () => {
    hookDraftDirty.pre = true;
  });
  document.getElementById("hook_post_command")?.addEventListener("input", () => {
    hookDraftDirty.post = true;
  });
  document.getElementById("recover-fingerprint")?.addEventListener("input", resetRecoverPreview);

  refreshSession().then((user) => {
    if (!user) {
      openLoginDialog();
      return;
    }
    if (user.must_change_password) {
      openPasswordDialog(true);
      return;
    }
    startEdgeApp();
  });
});
