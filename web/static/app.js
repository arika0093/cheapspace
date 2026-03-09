const onReady = (callback) => {
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", callback, { once: true });
  } else {
    callback();
  }
};

onReady(() => {
  setupSourcePanels();
  setupPasswordReveal();
  setupJobDetailPolling();
  setupWorkspaceDetailPolling();
});

function setupSourcePanels() {
  const selector = document.querySelector("[data-source-selector]");
  const form = document.querySelector("[data-workspace-form]");
  if (!selector || !form) {
    return;
  }

  const sourceRef = form.querySelector("textarea[name='source_ref']");
  const refresh = () => {
    const help = selector.value;
    if (!sourceRef) return;
    switch (help) {
      case "builtin_image":
        sourceRef.placeholder = "Built-in image selected. Leave blank.";
        break;
      case "image_ref":
        sourceRef.placeholder = "ghcr.io/acme/dev:latest";
        break;
      case "dockerfile":
        sourceRef.placeholder = "FROM ubuntu:24.04\nRUN apt-get update && apt-get install -y git";
        break;
      case "nixpacks":
        sourceRef.placeholder = "Optional notes for Nixpacks build.";
        break;
      default:
        sourceRef.placeholder = "";
    }
  };

  selector.addEventListener("change", refresh);
  refresh();
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

function setupJobDetailPolling() {
  const root = document.querySelector("[data-job-detail]");
  const output = document.getElementById("job-log-output");
  const statusBadge = document.getElementById("job-status");
  if (!root || !output || !statusBadge) {
    return;
  }

  const jobId = root.dataset.jobDetail;
  let lastSequence = Number(root.dataset.lastSequence || "0");

  const poll = async () => {
    try {
      const response = await fetch(`/api/jobs/${jobId}?after=${lastSequence}`);
      if (!response.ok) {
        return;
      }
      const payload = await response.json();
      statusBadge.textContent = payload.job.Status;
      statusBadge.className = `inline-flex rounded-full px-3 py-1 text-xs font-medium ${statusClassForClient(payload.job.Status)}`;
      if (Array.isArray(payload.logs) && payload.logs.length > 0) {
        const lines = payload.logs.map((entry) => `[${entry.Sequence}] ${entry.Message}`).join("\n");
        if (output.textContent.trim() === "Waiting for logs...") {
          output.textContent = "";
        }
        output.textContent += (output.textContent.endsWith("\n") || output.textContent === "" ? "" : "\n") + lines + "\n";
        output.scrollTop = output.scrollHeight;
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
    state.className = `inline-flex rounded-full px-3 py-1 text-xs font-medium ${statusClassForClient(payload.workspace.State)}`;
    if (payload.workspace.SSHPort > 0) {
      ssh.textContent = `ssh -p ${payload.workspace.SSHPort} codespace@${payload.workspace.SSHHost}`;
    }
    hostname.textContent = payload.workspace.PublicHostname || "Not configured";
    lastError.textContent = payload.workspace.LastError || "";
    if (["running", "failed", "deleted"].includes(payload.workspace.State)) {
      window.clearInterval(intervalId);
    }
  };

  const intervalId = window.setInterval(poll, 2000);
}

function statusClassForClient(value) {
  switch (value) {
    case "running":
    case "done":
      return "bg-emerald-500/15 text-emerald-300 ring-1 ring-inset ring-emerald-400/40";
    case "queued":
    case "pending":
    case "building":
    case "provisioning":
      return "bg-sky-500/15 text-sky-300 ring-1 ring-inset ring-sky-400/40";
    case "deleting":
      return "bg-amber-500/15 text-amber-300 ring-1 ring-inset ring-amber-400/40";
    case "failed":
    case "cancelled":
      return "bg-rose-500/15 text-rose-300 ring-1 ring-inset ring-rose-400/40";
    default:
      return "bg-slate-500/15 text-slate-300 ring-1 ring-inset ring-slate-400/40";
  }
}

