let _appStarted = false;

const DIALOG_FRAGMENTS = [
  "app-dialog",
  "login-dialog",
  "credential-dialog",
  "password-dialog",
  "users-dialog",
  "settings-dialog",
  "ntfy-dialog",
  "hooks-dialog",
  "hook-view-dialog",
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

function startCentralApp() {
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
  window.setInterval(() => loadOverview({ silent: true, notifyNewSnapshots: true }), CENTRAL_REFRESH_MS);
}

applyTheme("dark");
loadDialogs().then(() => {
  document.getElementById("hook_pre_command")?.addEventListener("input", () => {
    _hookDraftDirty.pre = true;
  });
  document.getElementById("hook_post_command")?.addEventListener("input", () => {
    _hookDraftDirty.post = true;
  });

  refreshSession().then((user) => {
    if (!user) {
      openLoginDialog();
      return;
    }
    if (user.must_change_password) {
      openPasswordDialog(true);
      return;
    }
    startCentralApp();
  });
});
