function toggleSettingSwitch(btn: HTMLElement): void {
  const on = btn.getAttribute("aria-checked") !== "true";
  btn.setAttribute("aria-checked", on ? "true" : "false");
  btn.classList.toggle("toggle-on", on);
}

function setToggle(id: string, on: boolean): void {
  const btn = document.getElementById(id);
  if (!btn) return;
  btn.setAttribute("aria-checked", on ? "true" : "false");
  btn.classList.toggle("toggle-on", on);
}

function getToggle(id: string): boolean {
  return document.getElementById(id)?.getAttribute("aria-checked") === "true";
}

async function cancelSettings(): Promise<void> {
  if (_settingsSnapshot !== null && JSON.stringify(collectSettingsPayload()) !== _settingsSnapshot) {
    const confirmed = await confirmApp({
      title: "Unsaved Changes",
      message: "You have unsaved changes. Use the Save button to apply them, or discard and close.",
      confirmLabel: "Discard & Close",
    });
    if (!confirmed) return;
  }
  _settingsSnapshot = null;
  closeDialog("settings-dialog");
}
