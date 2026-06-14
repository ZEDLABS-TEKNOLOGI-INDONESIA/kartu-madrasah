let currentJobId = null;
let currentOrientation = "portrait";
let pollTimer = null;
let allFiles = [];
let selectedFiles = new Set();
let currentPage = 1;
const PAGE_SIZE = 50;

function onFileSelect(input, zoneId, nameId) {
  const name = input.files[0]?.name || "";
  document.getElementById(nameId).textContent = name;
  document.getElementById(zoneId).classList.toggle("drag", !!name);
}

["dzCsv", "dzSvg"].forEach((id) => {
  const el = document.getElementById(id);
  el.addEventListener("dragover", (e) => {
    e.preventDefault();
    el.classList.add("drag");
  });
  el.addEventListener("dragleave", () => el.classList.remove("drag"));
  el.addEventListener("drop", (e) => {
    e.preventDefault();
    const input = el.querySelector("input[type=file]");
    const nameId = el.id === "dzCsv" ? "csvName" : "svgName";
    input.files = e.dataTransfer.files;
    onFileSelect(input, el.id, nameId);
  });
});

function setOrientation(el) {
  currentOrientation = el.value;
  document.querySelectorAll(".result-item").forEach((i) => {
    i.classList.toggle("landscape", currentOrientation === "landscape");
  });
}

async function generateConfig() {
  const svg = document.getElementById("svgInput").files[0];
  if (!svg) {
    showAlert("Upload file SVG terlebih dahulu.", "error");
    return;
  }
  const fd = new FormData();
  fd.append("svg", svg);
  const res = await fetch("/api/extract-config", { method: "POST", body: fd });
  if (!res.ok) {
    showAlert("Gagal generate config.", "error");
    return;
  }
  const blob = await res.blob();
  dlBlob(blob, "config.yaml");
  showAlert("Config berhasil di-generate dan diunduh.", "success");
}

async function startGenerate() {
  const csv = document.getElementById("csvInput").files[0];
  const svg = document.getElementById("svgInput").files[0];
  if (!csv || !svg) {
    showAlert("Upload file CSV dan SVG terlebih dahulu.", "error");
    return;
  }

  const fd = new FormData();
  fd.append("csv", csv);
  fd.append("svg", svg);
  fd.append("orientation", currentOrientation);

  const logo = document.getElementById("logoInput").files[0];
  if (logo) fd.append("logo_instansi", logo);
  Array.from(document.getElementById("fotoInput").files).forEach((f) => fd.append("foto_user", f));
  Array.from(document.getElementById("qrInput").files).forEach((f) => fd.append("qr_user", f));

  document.getElementById("btnGenerate").disabled = true;
  document.getElementById("progressWrap").style.display = "block";
  document.getElementById("resultCard").style.display = "none";
  document.getElementById("resultGrid").innerHTML = "";
  setProgress(-1, 0, 0, "Mengirim data...");
  hideAlert();

  try {
    const res = await fetch("/api/generate", { method: "POST", body: fd });
    const data = await res.json();
    if (!res.ok) {
      showAlert(data.error || "Gagal memulai generate.", "error");
      resetBtn();
      return;
    }
    currentJobId = data.job_id;
    pollStatus();
  } catch (e) {
    showAlert("Koneksi error: " + e.message, "error");
    resetBtn();
  }
}

function pollStatus() {
  if (pollTimer) clearInterval(pollTimer);
  pollTimer = setInterval(async () => {
    const res = await fetch("/api/status?job_id=" + currentJobId);
    const data = await res.json();
    if (data.status === "processing" || data.status === "pending") {
      setProgress(50, "Memproses kartu...");
    } else if (data.status === "done") {
      clearInterval(pollTimer);
      setProgress(100, "Selesai. " + data.total + " kartu berhasil dibuat.");
      loadResults();
      resetBtn();
    } else if (data.status === "error") {
      clearInterval(pollTimer);
      showAlert("Error: " + data.error, "error");
      resetBtn();
    }
  }, 1000);
}

async function loadResults() {
  const res = await fetch("/api/list?job_id=" + currentJobId);
  const data = await res.json();
  allFiles = data.files || [];
  selectedFiles = new Set();
  currentPage = 1;
  document.getElementById("resultTotal").textContent = allFiles.length + " kartu";
  document.getElementById("resultCard").style.display = "block";
  renderPage();
}

function renderPage() {
  const start = (currentPage - 1) * PAGE_SIZE;
  const end = Math.min(start + PAGE_SIZE, allFiles.length);
  const pageFiles = allFiles.slice(start, end);
  const grid = document.getElementById("resultGrid");
  grid.innerHTML = "";

  pageFiles.forEach((f) => {
    const div = document.createElement("div");
    div.className = "result-item" + (currentOrientation === "landscape" ? " landscape" : "");
    div.dataset.file = f;

    const cb = document.createElement("input");
    cb.type = "checkbox";
    cb.title = "Pilih";
    cb.checked = selectedFiles.has(f);
    cb.addEventListener("change", () => {
      if (cb.checked) selectedFiles.add(f);
      else selectedFiles.delete(f);
      updateCount();
    });

    const img = document.createElement("img");
    img.src = "/api/download?job_id=" + currentJobId + "&file=" + f;
    img.loading = "lazy";
    img.alt = f;

    const label = document.createElement("div");
    label.className = "result-label";
    label.textContent = f;

    const link = document.createElement("a");
    link.href = "/api/download?job_id=" + currentJobId + "&file=" + f;
    link.download = f;
    link.textContent = "Unduh";

    div.append(cb, img, label, link);
    grid.appendChild(div);
  });

  renderPagination();
  updateCount();
}

function renderPagination() {
  const total = Math.ceil(allFiles.length / PAGE_SIZE);
  const pg = document.getElementById("pagination");
  pg.innerHTML = "";
  if (total <= 1) return;

  const mkBtn = (label, page, disabled, active) => {
    const b = document.createElement("button");
    b.className = "page-btn" + (active ? " active" : "");
    b.textContent = label;
    b.disabled = disabled;
    if (!disabled && !active)
      b.addEventListener("click", () => {
        currentPage = page;
        renderPage();
        pg.scrollIntoView({ behavior: "smooth", block: "nearest" });
      });
    return b;
  };

  pg.appendChild(mkBtn("«", 1, currentPage === 1, false));
  pg.appendChild(mkBtn("‹", currentPage - 1, currentPage === 1, false));

  const range = pageRange(currentPage, total);
  let last = 0;
  range.forEach((p) => {
    if (p - last > 1) {
      const dots = document.createElement("span");
      dots.className = "page-info";
      dots.textContent = "...";
      pg.appendChild(dots);
    }
    pg.appendChild(mkBtn(p, p, false, p === currentPage));
    last = p;
  });

  pg.appendChild(mkBtn("›", currentPage + 1, currentPage === total, false));
  pg.appendChild(mkBtn("»", total, currentPage === total, false));

  const info = document.createElement("span");
  info.className = "page-info";
  info.textContent = "Hal " + currentPage + " / " + total;
  pg.appendChild(info);
}

function pageRange(cur, total) {
  const delta = 2;
  const pages = new Set([1, total]);
  for (let i = Math.max(2, cur - delta); i <= Math.min(total - 1, cur + delta); i++) pages.add(i);
  return [...pages].sort((a, b) => a - b);
}

async function downloadZip(mode) {
  let files = [];
  if (mode === "selected") {
    files = [...selectedFiles];
    if (!files.length) {
      showAlert("Pilih minimal 1 kartu terlebih dahulu.", "error");
      return;
    }
  }
  const res = await fetch("/api/download-zip", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ job_id: currentJobId, files }),
  });
  const blob = await res.blob();
  dlBlob(blob, "kartu.zip");
}

function selectAll(checked) {
  if (checked) allFiles.forEach((f) => selectedFiles.add(f));
  else selectedFiles.clear();
  document
    .querySelectorAll(".result-item input[type=checkbox]")
    .forEach((cb) => (cb.checked = checked));
  updateCount();
}

function updateCount() {
  document.getElementById("selectedCount").textContent = selectedFiles.size + " dipilih";
}

function setProgress(pct, done, total, text) {
  const bar = document.getElementById("progressBar");
  const pctEl = document.getElementById("progressPct");
  const fracEl = document.getElementById("progressFraction");
  document.getElementById("statusText").textContent = text || "";

  if (pct < 0) {
    bar.classList.add("indeterminate");
    pctEl.textContent = "";
    fracEl.textContent = "";
  } else {
    bar.classList.remove("indeterminate");
    bar.style.width = pct + "%";
    pctEl.textContent = pct + "%";
    fracEl.textContent = total > 0 ? done + " / " + total + " kartu" : "";
  }
}

function resetBtn() {
  document.getElementById("btnGenerate").disabled = false;
}

function showAlert(msg, type) {
  const el = document.getElementById("alertBox");
  el.className = "alert show alert-" + type;
  el.textContent = msg;
}

function hideAlert() {
  document.getElementById("alertBox").className = "alert";
}

function dlBlob(blob, name) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}
