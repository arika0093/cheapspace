const storageKeys = {
  theme: "cheapspace.theme",
  proxyPAC: "cheapspace.proxyPacUrl",
  logDetailed: "cheapspace.log.detailed",
  logTime: "cheapspace.log.time",
  logWrap: "cheapspace.log.wrap",
};

const onReady = (callback) => {
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", callback, { once: true });
  } else {
    callback();
  }
};

onReady(() => {
  setupThemeToggle();
  setupAutoSubmit();
  setupSourcePanels();
  setupWorkspaceFormBehavior();
  setupCopyButtons();
  setupPasswordReveal();
  setupJobLogToggles();
  setupJobDetailPolling();
  setupWorkspaceDetailPolling();
});

function setupThemeToggle() {
  const button = document.querySelector("[data-theme-toggle]");
  if (!button) {
    return;
  }

  const root = document.documentElement;
  const apply = (theme, persist = true) => {
    const nextTheme = theme === "dark" ? "dark" : "light";
    root.dataset.theme = nextTheme;
    button.textContent = nextTheme === "dark" ? "Light mode" : "Dark mode";
    button.setAttribute("aria-pressed", String(nextTheme === "dark"));
    if (persist) {
      try {
        localStorage.setItem(storageKeys.theme, nextTheme);
      } catch {}
    }
  };

  apply(root.dataset.theme || "light", false);
  button.addEventListener("click", () => {
    apply(root.dataset.theme === "dark" ? "light" : "dark");
  });
}

function setupAutoSubmit() {
  document.querySelectorAll("[data-auto-submit]").forEach((input) => {
    input.addEventListener("change", () => {
      input.form?.requestSubmit();
    });
  });
}

function setupSourcePanels() {
  const selector = document.querySelector("[data-source-selector]");
  const form = document.querySelector("[data-workspace-form]");
  if (!selector || !form) {
    return;
  }

  const panel = form.querySelector("[data-source-panel='source_ref']");
  const sourceRef = form.querySelector("textarea[name='source_ref']");
  const label = form.querySelector("[data-source-ref-label]");
  const help = form.querySelector("[data-source-ref-help]");
  const refresh = () => {
    const value = selector.value;
    if (!sourceRef || !panel || !label || !help) {
      return;
    }
    panel.classList.toggle("hidden", value === "builtin_image");
    switch (value) {
      case "builtin_image":
        label.textContent = "Built-in image";
        help.textContent = "No extra input is required.";
        sourceRef.placeholder = "";
        break;
      case "image_ref":
        label.textContent = "Image reference";
        help.textContent = "Example: ghcr.io/acme/dev:latest";
        sourceRef.placeholder = "ghcr.io/acme/dev:latest";
        break;
      case "dockerfile":
        label.textContent = "Dockerfile contents";
        help.textContent = "Paste the Dockerfile text that should be built.";
        sourceRef.placeholder = "FROM ubuntu:24.04\nRUN apt-get update && apt-get install -y git";
        break;
      case "nixpacks":
        label.textContent = "Nixpacks notes";
        help.textContent = "The repository URL is required; notes are optional.";
        sourceRef.placeholder = "Optional notes for the Nixpacks build.";
        break;
      default:
        label.textContent = "Source details";
        help.textContent = "";
        sourceRef.placeholder = "";
    }
  };

  selector.addEventListener("change", refresh);
  refresh();
}

function setupWorkspaceFormBehavior() {
  const form = document.querySelector("[data-workspace-form]");
  if (!form) {
    return;
  }

  const sshKeys = form.querySelector("[data-ssh-keys]");
  const passwordNote = form.querySelector("[data-password-note]");
  const httpProxy = form.querySelector("[data-http-proxy]");
  const httpsProxy = form.querySelector("[data-https-proxy]");
  const proxyPAC = form.querySelector("[data-proxy-pac]");

  if (sshKeys && passwordNote) {
    const refreshPasswordNote = () => {
      passwordNote.textContent = sshKeys.value.trim()
        ? "SSH keys detected. No password will be generated."
        : "No SSH keys provided. A one-time initial password will be generated automatically.";
    };
    sshKeys.addEventListener("input", refreshPasswordNote);
    refreshPasswordNote();
  }

  if (httpProxy && httpsProxy) {
    let lastMirrored = httpsProxy.value.trim();
    const mirrorProxy = () => {
      const httpValue = httpProxy.value.trim();
      const httpsValue = httpsProxy.value.trim();
      if (!httpValue) {
        return;
      }
      if (httpsValue === "" || httpsValue === lastMirrored) {
        httpsProxy.value = httpProxy.value;
        lastMirrored = httpProxy.value.trim();
      }
    };
    httpProxy.addEventListener("input", mirrorProxy);
    form.addEventListener("submit", () => {
      if (httpProxy.value.trim() && !httpsProxy.value.trim()) {
        httpsProxy.value = httpProxy.value;
      }
    });
  }

  if (proxyPAC) {
    try {
      if (!proxyPAC.value.trim()) {
        proxyPAC.value = localStorage.getItem(storageKeys.proxyPAC) || "";
      }
    } catch {}
    proxyPAC.addEventListener("input", () => {
      try {
        const value = proxyPAC.value.trim();
        if (value) {
          localStorage.setItem(storageKeys.proxyPAC, value);
        } else {
          localStorage.removeItem(storageKeys.proxyPAC);
        }
      } catch {}
    });
  }
}

function setupPasswordReveal() {
  const button = document.querySelector("[data-reveal-password]");
  if (!button) {
    return;
  }

  const output = document.getElementById("password-output");
  const error = document.getElementById("password-error");

  button.addEventListener("click", async () => {
    button.disabled = true;
    error?.classList.add("hidden");
    try {
      const response = await fetch(button.dataset.url, { method: "POST" });
      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error || "Unable to reveal password");
      }
      output.textContent = payload.password;
      output.classList.remove("hidden");
      button.classList.add("hidden");
    } catch (err) {
      if (error) {
        error.textContent = err.message;
        error.classList.remove("hidden");
      }
      button.disabled = false;
    }
  });
}

function setupCopyButtons() {
  document.querySelectorAll("[data-copy-source]").forEach((button) => {
    const defaultLabel = button.dataset.copyDefault || button.textContent.trim();
    const successLabel = button.dataset.copySuccess || "Copied";

    button.addEventListener("click", async () => {
      const source = document.querySelector(button.dataset.copySource || "");
      const value = source?.textContent?.trim() || "";
      if (!value || /^Pending/i.test(value)) {
        return;
      }

      try {
        await copyText(value);
        button.textContent = successLabel;
      } catch {
        button.textContent = "Copy failed";
      }

      window.setTimeout(() => {
        button.textContent = defaultLabel;
      }, 1600);
    });
  });
}

function setupJobLogToggles() {
  const root = document.querySelector("[data-job-detail]");
  if (!root) {
    return;
  }

  const detailedToggle = document.querySelector("[data-log-toggle='detailed']");
  const timeToggle = document.querySelector("[data-log-toggle='time']");
  const wrapToggle = document.querySelector("[data-log-toggle='wrap']");

  const readStoredBoolean = (key, fallback) => {
    try {
      const stored = localStorage.getItem(key);
      if (stored === null) {
        return fallback;
      }
      return stored === "true";
    } catch {
      return fallback;
    }
  };

  const apply = () => {
    root.dataset.logDetailed = String(detailedToggle?.checked ?? true);
    root.dataset.logTime = String(timeToggle?.checked ?? false);
    root.dataset.logWrap = String(wrapToggle?.checked ?? true);
    try {
      localStorage.setItem(storageKeys.logDetailed, String(detailedToggle?.checked ?? true));
      localStorage.setItem(storageKeys.logTime, String(timeToggle?.checked ?? false));
      localStorage.setItem(storageKeys.logWrap, String(wrapToggle?.checked ?? true));
    } catch {}
  };

  if (detailedToggle) {
    detailedToggle.checked = readStoredBoolean(storageKeys.logDetailed, true);
    detailedToggle.addEventListener("change", apply);
  }
  if (timeToggle) {
    timeToggle.checked = readStoredBoolean(storageKeys.logTime, false);
    timeToggle.addEventListener("change", apply);
  }
  if (wrapToggle) {
    wrapToggle.checked = readStoredBoolean(storageKeys.logWrap, true);
    wrapToggle.addEventListener("change", apply);
  }
  apply();
}

function setupJobDetailPolling() {
  const root = document.querySelector("[data-job-detail]");
  const output = document.getElementById("job-log-output");
  const statusBadge = document.getElementById("job-status");
  if (!root || !output || !statusBadge) {
    return;
  }

  const stopAction = document.querySelector("[data-job-action='stop']");
  const resumeAction = document.querySelector("[data-job-action='resume']");
  const deleteAction = document.querySelector("[data-job-action='delete']");
  const jobId = root.dataset.jobDetail;
  let lastSequence = Number(root.dataset.lastSequence || "0");

  const updateJobActions = (status) => {
    stopAction?.classList.toggle("hidden", !["running", "queued"].includes(status));
    resumeAction?.classList.toggle("hidden", !["failed", "cancelled"].includes(status));
    deleteAction?.classList.toggle("hidden", status === "running");
  };

  const poll = async () => {
    try {
      const response = await fetch(`/api/jobs/${jobId}?after=${lastSequence}`);
      if (!response.ok) {
        return;
      }
      const payload = await response.json();
      statusBadge.textContent = payload.job.Status;
      statusBadge.className = `status-pill ${statusClassForClient(payload.job.Status)}`;
      updateJobActions(payload.job.Status);
      if (Array.isArray(payload.logs) && payload.logs.length > 0) {
        appendJobLogEntries(output, payload.logs);
        lastSequence = payload.logs[payload.logs.length - 1].Sequence;
        root.dataset.lastSequence = String(lastSequence);
      }
      if (["done", "failed", "cancelled"].includes(payload.job.Status)) {
        window.clearInterval(intervalId);
      }
    } catch {
      window.clearInterval(intervalId);
    }
  };

  updateJobActions(statusBadge.textContent.trim());
  const intervalId = window.setInterval(poll, 1500);
}

function setupWorkspaceDetailPolling() {
  const root = document.querySelector("[data-workspace-detail]");
  if (!root) {
    return;
  }

  const workspaceId = root.dataset.workspaceDetail;
  const state = document.getElementById("workspace-state");
  const ssh = document.getElementById("workspace-ssh-command");
  const hostname = document.getElementById("workspace-public-hostname");
  const lastError = document.getElementById("workspace-last-error");

  const poll = async () => {
    const response = await fetch(`/api/workspaces/${workspaceId}`);
    if (!response.ok) {
      return;
    }
    const payload = await response.json();
    state.textContent = payload.workspace.State;
    state.className = `status-pill ${statusClassForClient(payload.workspace.State)}`;
    if (payload.workspace.SSHPort > 0 && payload.workspace.SSHHost) {
      ssh.textContent = `ssh -p ${payload.workspace.SSHPort} codespace@${payload.workspace.SSHHost}`;
      document.querySelectorAll("[data-copy-source='#workspace-ssh-command']").forEach((button) => {
        button.disabled = false;
      });
    }
    hostname.textContent = payload.workspace.PublicHostname || "Not configured";
    lastError.textContent = payload.workspace.LastError || "";
    if (["running", "failed", "deleted"].includes(payload.workspace.State)) {
      window.clearInterval(intervalId);
    }
  };

  const intervalId = window.setInterval(poll, 2000);
}

function appendJobLogEntries(output, entries) {
  output.querySelector("[data-empty-log-state]")?.remove();
  const fragment = document.createDocumentFragment();
  for (const entry of entries) {
    fragment.appendChild(createJobLogEntry(entry));
  }
  output.appendChild(fragment);
  output.scrollTop = output.scrollHeight;
}

function createJobLogEntry(entry) {
  const article = document.createElement("article");
  article.className = "log-entry";
  article.dataset.sequence = String(entry.Sequence);

  const meta = document.createElement("div");
  meta.className = "log-meta";
  meta.appendChild(createLogPill(`#${entry.Sequence}`));
  meta.appendChild(createLogPill(entry.Stream || "stdout"));
  const timestamp = createLogPill(formatLogTimestamp(entry.CreatedAt));
  timestamp.classList.add("log-timestamp");
  meta.appendChild(timestamp);

  const message = document.createElement("pre");
  message.className = "log-message";
  message.textContent = entry.Message || "";

  article.append(meta, message);
  return article;
}

function createLogPill(text) {
  const pill = document.createElement("span");
  pill.className = "log-pill";
  pill.textContent = text;
  return pill;
}

async function copyText(value) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }

  const input = document.createElement("textarea");
  input.value = value;
  input.setAttribute("readonly", "");
  input.style.position = "absolute";
  input.style.left = "-9999px";
  document.body.appendChild(input);
  input.select();
  document.execCommand("copy");
  input.remove();
}

function formatLogTimestamp(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value || "-";
  }
  return new Intl.DateTimeFormat("ja-JP", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).format(date);
}

function statusClassForClient(value) {
  switch (value) {
    case "running":
    case "done":
      return "status-running";
    case "queued":
    case "pending":
    case "building":
    case "provisioning":
      return "status-pending";
    case "deleting":
      return "status-warning";
    case "failed":
    case "cancelled":
      return "status-error";
    default:
      return "status-neutral";
  }
}
