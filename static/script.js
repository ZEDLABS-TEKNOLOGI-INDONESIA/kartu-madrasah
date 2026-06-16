var currentJobId = null;
var currentOrientation = "portrait";
var pollTimer = null;
var allFiles = [];
var selectedFiles = new Set();
var currentPage = 1;
var PAGE_SIZE = 50;

function onFileSelect(input, zoneId, nameId) {
  var name = input.files[0] ? input.files[0].name : "";
  document.getElementById(nameId).textContent = name;
  document.getElementById(zoneId).classList.toggle("drag", !!name);
}

["dzCsv", "dzSvg"].forEach(function (id) {
  var el = document.getElementById(id);
  el.addEventListener("dragover", function (e) {
    e.preventDefault();
    el.classList.add("drag");
  });
  el.addEventListener("dragleave", function () {
    el.classList.remove("drag");
  });
  el.addEventListener("drop", function (e) {
    e.preventDefault();
    var input = el.querySelector("input[type=file]");
    var nameId = el.id === "dzCsv" ? "csvName" : "svgName";
    input.files = e.dataTransfer.files;
    onFileSelect(input, el.id, nameId);
  });
});

function setOrientation(el) {
  currentOrientation = el.value;
  document.querySelectorAll(".result-item").forEach(function (i) {
    i.classList.toggle("landscape", currentOrientation === "landscape");
  });
}

async function generateConfig() {
  var svg = document.getElementById("svgInput").files[0];
  if (!svg) {
    showAlert("Upload file SVG terlebih dahulu.", "error");
    return;
  }
  var fd = new FormData();
  fd.append("svg", svg);
  var res = await fetch("/api/extract-config", { method: "POST", body: fd });
  if (!res.ok) {
    showAlert("Gagal generate config.", "error");
    return;
  }
  dlBlob(await res.blob(), "config.yaml");
  showAlert("Config berhasil di-generate dan diunduh.", "success");
}

async function startGenerate() {
  var csv = document.getElementById("csvInput").files[0];
  var svg = document.getElementById("svgInput").files[0];
  if (!csv || !svg) {
    showAlert("Upload file CSV dan SVG terlebih dahulu.", "error");
    return;
  }
  var fd = new FormData();
  fd.append("csv", csv);
  fd.append("svg", svg);
  fd.append("orientation", currentOrientation);
  var logo = document.getElementById("logoInput").files[0];
  if (logo) fd.append("logo_instansi", logo);
  Array.from(document.getElementById("fotoInput").files).forEach(function (f) {
    fd.append("foto_user", f);
  });
  Array.from(document.getElementById("qrInput").files).forEach(function (f) {
    fd.append("qr_user", f);
  });
  document.getElementById("btnGenerate").disabled = true;
  document.getElementById("progressWrap").style.display = "block";
  document.getElementById("resultCard").style.display = "none";
  document.getElementById("resultGrid").innerHTML = "";
  setProgress(-1, 0, 0, "Mengirim data...");
  hideAlert();
  try {
    var res = await fetch("/api/generate", { method: "POST", body: fd });
    var data = await res.json();
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
  pollTimer = setInterval(async function () {
    var res = await fetch("/api/status?job_id=" + currentJobId);
    var data = await res.json();
    if (data.status === "processing" || data.status === "pending") {
      var pct = data.total > 0 ? Math.round((data.done / data.total) * 100) : 0;
      setProgress(pct, data.done, data.total, "Memproses kartu...");
    } else if (data.status === "done") {
      clearInterval(pollTimer);
      var msg = "Selesai. " + data.total + " kartu diproses";
      if (data.failed > 0) {
        msg += ", " + data.failed + " gagal";
      }
      msg += ".";
      setProgress(100, data.total, data.total, msg);
      if (data.failed > 0 && data.error) {
        showAlert("Beberapa kartu gagal dirender:\n" + data.error, "error");
      }
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
  var res = await fetch("/api/list?job_id=" + currentJobId);
  var data = await res.json();
  allFiles = data.files || [];
  selectedFiles = new Set();
  currentPage = 1;
  var totalText = allFiles.length + " kartu";
  if (data.failed > 0) {
    totalText += " (" + data.failed + " gagal)";
  }
  document.getElementById("resultTotal").textContent = totalText;
  document.getElementById("resultCard").style.display = "block";
  renderPage();
}

function renderPage() {
  var start = (currentPage - 1) * PAGE_SIZE;
  var end = Math.min(start + PAGE_SIZE, allFiles.length);
  var pageFiles = allFiles.slice(start, end);
  var grid = document.getElementById("resultGrid");
  grid.innerHTML = "";
  pageFiles.forEach(function (f) {
    var div = document.createElement("div");
    div.className = "result-item" + (currentOrientation === "landscape" ? " landscape" : "");
    div.dataset.file = f;
    var cb = document.createElement("input");
    cb.type = "checkbox";
    cb.title = "Pilih";
    cb.checked = selectedFiles.has(f);
    cb.addEventListener("change", function () {
      if (cb.checked) selectedFiles.add(f);
      else selectedFiles.delete(f);
      updateCount();
    });
    var img = document.createElement("img");
    img.src = "/api/download?job_id=" + currentJobId + "&file=" + encodeURIComponent(f);
    img.loading = "lazy";
    img.alt = f;
    var label = document.createElement("div");
    label.className = "result-label";
    label.textContent = f;
    var link = document.createElement("a");
    link.href = "/api/download?job_id=" + currentJobId + "&file=" + encodeURIComponent(f);
    link.download = f;
    link.textContent = "Unduh";
    div.append(cb, img, label, link);
    grid.appendChild(div);
  });
  renderPagination();
  updateCount();
}

function renderPagination() {
  var total = Math.ceil(allFiles.length / PAGE_SIZE);
  var pg = document.getElementById("pagination");
  pg.innerHTML = "";
  if (total <= 1) return;
  function mkBtn(label, page, disabled, active) {
    var b = document.createElement("button");
    b.className = "page-btn" + (active ? " active" : "");
    b.textContent = label;
    b.disabled = disabled;
    if (!disabled && !active) {
      b.addEventListener("click", function () {
        currentPage = page;
        renderPage();
        pg.scrollIntoView({ behavior: "smooth", block: "nearest" });
      });
    }
    return b;
  }
  pg.appendChild(mkBtn("«", 1, currentPage === 1, false));
  pg.appendChild(mkBtn("‹", currentPage - 1, currentPage === 1, false));
  var range = pageRange(currentPage, total);
  var last = 0;
  range.forEach(function (p) {
    if (p - last > 1) {
      var dots = document.createElement("span");
      dots.className = "page-info";
      dots.textContent = "...";
      pg.appendChild(dots);
    }
    pg.appendChild(mkBtn(p, p, false, p === currentPage));
    last = p;
  });
  pg.appendChild(mkBtn("›", currentPage + 1, currentPage === total, false));
  pg.appendChild(mkBtn("»", total, currentPage === total, false));
  var info = document.createElement("span");
  info.className = "page-info";
  info.textContent = "Hal " + currentPage + " / " + total;
  pg.appendChild(info);
}

function pageRange(cur, total) {
  var delta = 2;
  var pages = new Set([1, total]);
  for (var i = Math.max(2, cur - delta); i <= Math.min(total - 1, cur + delta); i++) pages.add(i);
  return Array.from(pages).sort(function (a, b) {
    return a - b;
  });
}

async function downloadZip(mode) {
  var files = [];
  if (mode === "selected") {
    files = Array.from(selectedFiles);
    if (!files.length) {
      showAlert("Pilih minimal 1 kartu terlebih dahulu.", "error");
      return;
    }
  }
  var res = await fetch("/api/download-zip", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ job_id: currentJobId, files: files }),
  });
  dlBlob(await res.blob(), "kartu.zip");
}

function selectAll(checked) {
  if (checked)
    allFiles.forEach(function (f) {
      selectedFiles.add(f);
    });
  else selectedFiles.clear();
  document.querySelectorAll(".result-item input[type=checkbox]").forEach(function (cb) {
    cb.checked = checked;
  });
  updateCount();
}

function updateCount() {
  document.getElementById("selectedCount").textContent = selectedFiles.size + " dipilih";
}

function setProgress(pct, done, total, text) {
  var bar = document.getElementById("progressBar");
  var pctEl = document.getElementById("progressPct");
  var fracEl = document.getElementById("progressFraction");
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
  var el = document.getElementById("alertBox");
  el.className = "alert show alert-" + type;
  el.textContent = msg;
}

function hideAlert() {
  document.getElementById("alertBox").className = "alert";
}

function dlBlob(blob, name) {
  var url = URL.createObjectURL(blob);
  var a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}
