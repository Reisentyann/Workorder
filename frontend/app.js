const API = "/api/v1";
let currentTicketId = null;
let currentAnalysisId = null;
let pollTimer = null;

/* ---------- 工具 ---------- */
function toast(msg, isError = false) {
  let el = document.querySelector(".toast");
  if (!el) {
    el = document.createElement("div");
    el.className = "toast";
    document.body.appendChild(el);
  }
  el.textContent = msg;
  el.className = "toast show" + (isError ? " error" : "");
  clearTimeout(el._t);
  el._t = setTimeout(() => (el.className = "toast"), 2200);
}

async function api(path, opts = {}) {
  const res = await fetch(API + path, {
    headers: { "Content-Type": "application/json" },
    ...opts,
    body: opts.body ? JSON.stringify(opts.body) : undefined,
  });
  if (!res.ok) {
    let msg = res.statusText;
    try { msg = (await res.json()).error || msg; } catch (_) {}
    throw new Error(msg);
  }
  return res.status === 204 ? null : res.json();
}

const statusText = {
  pending: "待处理",
  analyzing: "分析中",
  needs_review: "待确认",
  resolved: "已处理",
  needs_info: "需补充信息",
  canceled: "已取消",
  escalated: "已升级人工",
};

function fmtTime(s) {
  if (!s) return "-";
  return new Date(s).toLocaleString("zh-CN", { hour12: false });
}

/* ---------- 工单列表 ---------- */
async function loadTickets() {
  const tickets = await api("/tickets");
  const ul = document.getElementById("ticket-list");
  ul.innerHTML = "";
  if (!tickets.length) {
    ul.innerHTML = '<li class="ticket-item"><div class="t-meta">暂无工单</div></li>';
    return;
  }
  for (const t of tickets) {
    const li = document.createElement("li");
    li.className = "ticket-item" + (t.id === currentTicketId ? " active" : "");
    li.innerHTML = `
      <div class="t-title"></div>
      <div class="t-meta">
        <span class="badge ${t.status}">${statusText[t.status] || t.status}</span>
        <span>${t.category || "未分类"}</span>
        <span>${t.priority || "-"}</span>
      </div>`;
    li.querySelector(".t-title").textContent = t.title;
    li.onclick = () => openTicket(t.id);
    ul.appendChild(li);
  }
}

/* ---------- 新建工单 ---------- */
function setupNewDialog() {
  const dialog = document.getElementById("new-dialog");
  const closeDialog = () => dialog.close();
  document.getElementById("btn-new").onclick = () => dialog.showModal();
  document.getElementById("new-close").onclick = closeDialog;
  document.getElementById("new-cancel").onclick = closeDialog;
  dialog.addEventListener("click", (e) => { if (e.target === dialog) dialog.close(); });
  document.getElementById("new-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const title = document.getElementById("n-title").value.trim();
    const content = document.getElementById("n-content").value.trim();
    if (!title || !content) return;
    const t = await api("/tickets", { method: "POST", body: { title, content } });
    dialog.close();
    document.getElementById("new-form").reset();
    await loadTickets();
    openTicket(t.id);
    toast("工单已创建");
  });
}

/* ---------- 详情 ---------- */
async function openTicket(id) {
  currentTicketId = id;
  document.getElementById("detail-empty").hidden = true;
  document.getElementById("detail").hidden = false;
  await Promise.all([loadTickets(), renderDetail()]);
}

async function renderDetail() {
  if (!currentTicketId) return;
  const t = await api("/tickets/" + currentTicketId);
  document.getElementById("d-title").textContent = t.title;
  document.getElementById("d-id").textContent = t.id;
  const badge = document.getElementById("d-status");
  badge.textContent = statusText[t.status] || t.status;
  badge.className = "badge " + t.status;
  document.getElementById("d-content").textContent = t.content;
  document.getElementById("s-status").value = t.status;

  const analyzing = t.status === "analyzing";
  document.getElementById("btn-analyze").disabled = analyzing;
  document.getElementById("btn-cancel").hidden = !analyzing;
  document.getElementById("analyze-status").textContent = analyzing ? "分析中…" : "";

  renderHistory(t.analysis || []);

  const latest = (t.analysis || []).filter(a => a.status === "done").slice(-1)[0];
  if (latest) {
    currentAnalysisId = latest.id;
    renderAnalysis(latest);
    if (analyzing) startPolling();
    else stopPolling();
  } else {
    currentAnalysisId = null;
    document.getElementById("analysis-box").hidden = true;
    document.getElementById("review-box").hidden = true;
  }
}

/* ---------- AI 分析 ---------- */
async function triggerAnalyze(instruction) {
  if (!currentTicketId) return;
  stopPolling();
  document.getElementById("btn-analyze").disabled = true;
  document.getElementById("btn-cancel").hidden = false;
  document.getElementById("analyze-status").textContent = "分析中…";
  document.getElementById("analysis-box").hidden = true;
  document.getElementById("review-box").hidden = true;
  try {
    await api(`/tickets/${currentTicketId}/analyze`, {
      method: "POST",
      body: instruction ? { instruction } : {},
    });
    startPolling();
  } catch (e) {
    document.getElementById("btn-analyze").disabled = false;
    document.getElementById("btn-cancel").hidden = true;
    document.getElementById("analyze-status").textContent = "";
    toast(e.message, true);
  }
}

async function cancelAnalyze() {
  if (!currentTicketId) return;
  try {
    await api(`/tickets/${currentTicketId}/analyze/cancel`, { method: "POST" });
    stopPolling();
    toast("已打断分析");
  } catch (e) {
    toast(e.message, true);
  }
}

function startPolling() {
  stopPolling();
  pollTimer = setInterval(async () => {
    try {
      const t = await api("/tickets/" + currentTicketId);
      const analyzing = t.status === "analyzing";
      document.getElementById("btn-analyze").disabled = analyzing;
      document.getElementById("btn-cancel").hidden = !analyzing;
      document.getElementById("analyze-status").textContent = analyzing ? "分析中…" : "";
      const latest = (t.analysis || []).filter(a => a.status === "done").slice(-1)[0];
      if (!analyzing) {
        stopPolling();
        renderHistory(t.analysis || []);
        if (latest) { currentAnalysisId = latest.id; renderAnalysis(latest); }
        document.getElementById("d-status").textContent = statusText[t.status] || t.status;
        document.getElementById("d-status").className = "badge " + t.status;
        await loadTickets();
      }
    } catch (_) {}
  }, 800);
}

function stopPolling() {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
}

/* ---------- 渲染 AI 结果 ---------- */
function renderAnalysis(a) {
  const box = document.getElementById("analysis-box");
  box.hidden = false;

  const conf = Math.round(a.confidence * 100);
  const confCls = conf >= 80 ? "conf-high" : conf >= 50 ? "conf-mid" : "conf-low";
  const evidence = (a.evidence || []).map(e => `<li>${escapeHtml(e)}</li>`).join("");

  let callout = "";
  if (a.refused) {
    callout = `<div class="callout manual"><b>⛔ 拒答（不硬给结论）：</b>${escapeHtml(a.refusal_summary || a.takeover_reason || "")}</div>`;
  } else if (a.auto_fixable) {
    callout = `<div class="callout auto"><b>✓ 建议自动处理：</b>${escapeHtml(a.auto_fix_suggestion || "")}</div>`;
  } else if (a.needs_more_info || a.human_takeover) {
    callout = `<div class="callout manual"><b>⚠ 信息不足 / 需人工接管：</b>${escapeHtml(a.supplement_suggestion || a.takeover_reason || "")}</div>`;
  }

  box.innerHTML = `
    <h3>AI 分析结果</h3>
    <div class="kv">
      <div class="k">工单分类</div><div class="v">${escapeHtml(a.category || "-")}</div>
      <div class="k">优先级</div><div class="v">${escapeHtml(a.priority || "-")}</div>
      <div class="k">摘要</div><div class="v">${escapeHtml(a.summary || "-")}</div>
      <div class="k">置信度</div><div class="v"><span class="${confCls}">${conf}%</span></div>
      <div class="k">判断依据</div><div class="v"><ul class="evidence-list">${evidence || "<li>-</li>"}</ul></div>
      <div class="k">建议处理角色</div><div class="v">${escapeHtml(a.suggested_assignee || "-")}</div>
      <div class="k">是否适合自动处理</div><div class="v">${a.auto_fixable ? "是" : "否"}</div>
    </div>
    ${callout}
  `;

  // 人工确认 / 修改
  const review = document.getElementById("review-box");
  review.hidden = false;
  document.getElementById("r-category").value = a.category || "";
  document.getElementById("r-priority").value = a.priority || "中";
  document.getElementById("r-summary").value = a.summary || "";
  document.getElementById("r-assignee").value = a.suggested_assignee || "";
}

function renderHistory(analysis) {
  const box = document.getElementById("analysis-history");
  if (!analysis.length) { box.innerHTML = '<div class="muted">暂无分析记录</div>'; return; }
  box.innerHTML = "";
  for (const a of [...analysis].reverse()) {
    const div = document.createElement("div");
    div.className = "history-item";
    div.innerHTML = `<div class="h-meta">${fmtTime(a.created_at)} · ${escapeHtml(a.category || "-")} · 置信度 ${Math.round(a.confidence * 100)}% · ${a.confirmed ? "已确认" : "未确认"}</div>
      <div>${escapeHtml(a.summary || "")}</div>`;
    box.appendChild(div);
  }
}

/* ---------- 人工确认 / 修改 ---------- */
async function confirmAnalysis() {
  if (!currentTicketId || !currentAnalysisId) return;
  const body = {
    category: document.getElementById("r-category").value.trim(),
    priority: document.getElementById("r-priority").value,
    summary: document.getElementById("r-summary").value.trim(),
    suggested_assignee: document.getElementById("r-assignee").value.trim(),
  };
  try {
    await api(`/tickets/${currentTicketId}/analysis/${currentAnalysisId}/confirm`, {
      method: "PUT",
      body,
    });
    toast("已确认，工单状态更新");
    await renderDetail();
    await loadTickets();
  } catch (e) {
    toast(e.message, true);
  }
}

/* ---------- 状态流转 ---------- */
async function updateStatus() {
  if (!currentTicketId) return;
  const status = document.getElementById("s-status").value;
  try {
    await api(`/tickets/${currentTicketId}/status`, { method: "PUT", body: { status } });
    toast("状态已更新");
    await renderDetail();
    await loadTickets();
  } catch (e) {
    toast(e.message, true);
  }
}

/* ---------- 删除（懒标记） ---------- */
async function softDelete() {
  if (!currentTicketId) return;
  if (!confirm("确认删除该工单？（软删除，可恢复）")) return;
  try {
    await api(`/tickets/${currentTicketId}`, { method: "DELETE" });
    currentTicketId = null;
    document.getElementById("detail").hidden = true;
    document.getElementById("detail-empty").hidden = false;
    await loadTickets();
    toast("已删除（懒标记）");
  } catch (e) {
    toast(e.message, true);
  }
}

/* ---------- 快速操作 ---------- */
async function loadQuickActions() {
  const actions = await api("/actions");
  const box = document.getElementById("quick-actions");
  box.innerHTML = "";
  for (const a of actions) {
    const btn = document.createElement("button");
    btn.className = "quick-btn";
    btn.innerHTML = `<span class="kbd">${escapeHtml(a.shortcut || "·")}</span>${escapeHtml(a.name)}`;
    btn.onclick = () => runAction(a.id);
    box.appendChild(btn);
  }
  quickActions = actions;
}

async function runAction(actionId) {
  if (!currentTicketId) { toast("请先选择工单", true); return; }
  const a = quickActions.find(x => x.id === actionId);
  try {
    await api(`/tickets/${currentTicketId}/actions/${actionId}`, { method: "POST" });
    toast(`已执行：${a ? a.name : ""}`);
    startPolling();
  } catch (e) {
    toast(e.message, true);
  }
}

async function addQuickAction() {
  const name = document.getElementById("qa-name").value.trim();
  const instruction = document.getElementById("qa-instruction").value.trim();
  const shortcut = document.getElementById("qa-shortcut").value.trim();
  if (!name || !instruction) { toast("请填写按钮名和指令", true); return; }
  try {
    await api("/actions", { method: "POST", body: { name, instruction, shortcut: shortcut || null } });
    document.getElementById("qa-name").value = "";
    document.getElementById("qa-instruction").value = "";
    document.getElementById("qa-shortcut").value = "";
    await loadQuickActions();
    toast("按钮已添加");
  } catch (e) {
    toast(e.message, true);
  }
}

/* ---------- 快捷键 ---------- */
let quickActions = [];

function isTyping() {
  const el = document.activeElement;
  return el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.tagName === "SELECT");
}

document.addEventListener("keydown", (e) => {
  if (isTyping()) return;
  if (e.key === "Escape") {
    if (document.getElementById("btn-cancel").hidden === false) cancelAnalyze();
    return;
  }
  if (e.ctrlKey || e.metaKey || e.altKey) return;
  if (e.key === "n" || e.key === "N") {
    document.getElementById("new-dialog").showModal();
    return;
  }
  if (e.key === "a" || e.key === "A") {
    if (currentTicketId && !document.getElementById("btn-analyze").disabled) triggerAnalyze();
    return;
  }
  const a = quickActions.find(x => x.shortcut && x.shortcut.toLowerCase() === e.key.toLowerCase());
  if (a && currentTicketId) runAction(a.id);
});

/* ---------- 绑定 ---------- */
function escapeHtml(s) {
  return String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

function resolveTarget(sel) {
  if (typeof sel === "function") return sel();
  if (typeof sel === "string") return document.querySelector(sel);
  return null;
}

/* ---------- 引导式演示 ---------- */
const guideScenes = [
  {
    id: "normal",
    name: "① 完整主流程",
    steps: [
      { tip: "点击左侧【推送消息】中的第一条反馈，直接针对它新建工单", target: "#message-list .message-item:not(.handled)", wait: "click" },
      { tip: "正在用消息内容创建工单…", wait: "condition", check: () => { const d = document.getElementById("detail"); return d && !d.hidden && currentTicketId; } },
      { tip: "工单已创建。点击【⚡ AI 分析】", target: "#btn-analyze", wait: "click" },
      { tip: "等待 AI 分析完成（约 1.5 秒）…", wait: "condition", check: () => { const b = document.getElementById("analysis-box"); return b && !b.hidden; } },
      { tip: "查看右侧 AI 结果（分类「退款申请」/优先级「高」/置信度/判断依据），看完点【✓ 确认】", target: "#btn-confirm", wait: "click" },
      { tip: "完成！推送反馈 → 建工单 → AI 分析 → 人工确认，全流程走完", wait: "done" },
    ],
  },
  {
    id: "wrong",
    name: "② AI 错误答案 + 人工修正",
    steps: [
      { tip: "（演示预设）正在模拟 AI 返回错误答案…", wait: "auto", onEnter: () => api("/mock/preset", { method: "POST", body: { delay_ms: 800, confidence: 0.9, force: { category: "登录异常", priority: "中", summary: "疑似登录问题", suggested_assignee: "技术运维", auto_fixable: true } } }) },
      { tip: "点击左侧【推送消息】中的第一条退款反馈", target: "#message-list .message-item:not(.handled)", wait: "click" },
      { tip: "正在创建工单…", wait: "condition", check: () => { const d = document.getElementById("detail"); return d && !d.hidden && currentTicketId; } },
      { tip: "点击【⚡ AI 分析】", target: "#btn-analyze", wait: "click" },
      { tip: "等待分析完成…", wait: "condition", check: () => { const b = document.getElementById("analysis-box"); return b && !b.hidden; } },
      { tip: "注意！AI 把「申请退款」错分类成「登录异常」（演示预设的错误答案）。把分类改成：退款申请", target: "#r-category", wait: "input" },
      { tip: "把优先级改成「高」", target: "#r-priority", wait: "change" },
      { tip: "点击【✓ 确认】提交修正", target: "#btn-confirm", wait: "click" },
      { tip: "完成！你修正了 AI 的错误答案", wait: "done", onEnter: () => api("/mock/reset", { method: "POST" }) },
    ],
  },
  {
    id: "lowconf",
    name: "③ 低置信度 → 拒答",
    steps: [
      { tip: "（演示预设）正在模拟 AI 低置信度…", wait: "auto", onEnter: () => api("/mock/preset", { method: "POST", body: { delay_ms: 800, confidence: 0.3, force: { category: "其他", priority: "低", summary: "无法确定类型" } } }) },
      { tip: "点击左侧【推送消息】中的任意一条反馈", target: "#message-list .message-item:not(.handled)", wait: "click" },
      { tip: "正在创建工单…", wait: "condition", check: () => { const d = document.getElementById("detail"); return d && !d.hidden && currentTicketId; } },
      { tip: "点击【⚡ AI 分析】", target: "#btn-analyze", wait: "click" },
      { tip: "等待分析完成…", wait: "condition", check: () => { const b = document.getElementById("analysis-box"); return b && !b.hidden; } },
      { tip: "看右侧：AI 置信度很低，已「拒答」并建议人工接管，附拒答摘要。看完点【放弃】", target: "#btn-discard", wait: "click" },
      { tip: "完成！AI 不确定时不硬答，转人工", wait: "done", onEnter: () => api("/mock/reset", { method: "POST" }) },
    ],
  },
  {
    id: "slow",
    name: "④ 慢分析 + 打断",
    steps: [
      { tip: "（演示预设）本次 AI 分析会卡 30 秒…", wait: "auto", onEnter: () => api("/mock/preset", { method: "POST", body: { delay_ms: 30000, confidence: 0.8, force: { category: "退款申请", priority: "高", summary: "退款" } } }) },
      { tip: "点击左侧【推送消息】中的任意一条反馈", target: "#message-list .message-item:not(.handled)", wait: "click" },
      { tip: "正在创建工单…", wait: "condition", check: () => { const d = document.getElementById("detail"); return d && !d.hidden && currentTicketId; } },
      { tip: "点击【⚡ AI 分析】", target: "#btn-analyze", wait: "click" },
      { tip: "AI 卡住了。点击【⏹ 打断分析】按钮（或按 Esc）", target: "#btn-cancel", wait: "click" },
      { tip: "完成！打断后工单状态变为「已取消」", wait: "done", onEnter: () => api("/mock/reset", { method: "POST" }) },
    ],
  },
  {
    id: "missing",
    name: "⑤ 信息不足 → 补充建议",
    steps: [
      { tip: "点击左侧【推送消息】中最后一条「我要退款，快点处理」（故意缺订单号金额）", target: () => { const items = document.querySelectorAll("#message-list .message-item:not(.handled)"); return items.length ? items[items.length - 1] : null; }, wait: "click" },
      { tip: "正在创建工单…", wait: "condition", check: () => { const d = document.getElementById("detail"); return d && !d.hidden && currentTicketId; } },
      { tip: "点击【⚡ AI 分析】", target: "#btn-analyze", wait: "click" },
      { tip: "等待分析完成…", wait: "condition", check: () => { const b = document.getElementById("analysis-box"); return b && !b.hidden; } },
      { tip: "看右侧：AI 判定信息不足，提示「需补充信息：订单号、金额」。看完点【放弃】", target: "#btn-discard", wait: "click" },
      { tip: "完成！信息不足时 AI 要求补充关键信息，而非硬给结论", wait: "done" },
    ],
  },
];

const Guide = {
  active: false,
  steps: [],
  index: 0,
  tipEl: null,
  highlightEl: null,
  cleanupFns: [],

  start(scene) {
    this.steps = scene.steps;
    this.index = 0;
    this.active = true;
    this.createTipEl();
    this.step();
  },

  createTipEl() {
    if (!this.tipEl) {
      this.tipEl = document.createElement("div");
      this.tipEl.className = "guide-tip";
      document.body.appendChild(this.tipEl);
    }
  },

  step() {
    if (!this.active || this.index >= this.steps.length) {
      this.finish();
      return;
    }
    const s = this.steps[this.index];
    this.cleanupStep();

    if (s.onEnter) {
      Promise.resolve(s.onEnter()).catch((e) => toast("演示预设失败：" + e.message + "（请以 -demo 模式启动）", true)).finally(() => {
        if (s.wait === "auto") {
          this.index++;
          this.step();
        } else {
          this.show(s);
        }
      });
    } else {
      this.show(s);
    }
  },

  show(s) {
    this.renderTip(s);
    this.highlight(s.target);

    if (s.wait === "click" && s.target) {
      const el = resolveTarget(s.target);
      if (el) {
        const handler = () => this.advance();
        el.addEventListener("click", handler, { once: true });
        this.cleanupFns.push(() => el.removeEventListener("click", handler));
      }
    } else if (s.wait === "input" && s.target) {
      const el = resolveTarget(s.target);
      if (el) {
        const handler = () => { if (el.value && el.value.trim()) this.advance(); };
        el.addEventListener("input", handler);
        this.cleanupFns.push(() => el.removeEventListener("input", handler));
      }
    } else if (s.wait === "change" && s.target) {
      const el = resolveTarget(s.target);
      if (el) {
        const handler = () => this.advance();
        el.addEventListener("change", handler);
        this.cleanupFns.push(() => el.removeEventListener("change", handler));
      }
    } else if (s.wait === "condition" && s.check) {
      const timer = setInterval(() => {
        if (s.check()) { clearInterval(timer); this.advance(); }
      }, 300);
      this.cleanupFns.push(() => clearInterval(timer));
    } else if (s.wait === "done") {
      this.tipEl.querySelector(".guide-skip").textContent = "结束引导";
    }
  },

  advance() {
    this.index++;
    this.step();
  },

  cleanupStep() {
    this.cleanupFns.forEach((fn) => fn());
    this.cleanupFns = [];
    this.unhighlight();
  },

  renderTip(s) {
    this.tipEl.innerHTML = `<div class="guide-step">引导演示 ${this.index + 1}/${this.steps.length}</div>${escapeHtml(s.tip)}<div class="guide-skip">跳过引导</div>`;
    this.tipEl.querySelector(".guide-skip").onclick = () => this.finish();
    this.positionTip(s.target);
  },

  positionTip(sel) {
    const el = resolveTarget(sel);
    const rect = el ? el.getBoundingClientRect() : { top: 80, left: 20, bottom: 80, width: 0 };
    let left = rect.left;
    let top = rect.bottom + 8;
    if (top + 140 > window.innerHeight) top = rect.top - 150;
    if (left + 340 > window.innerWidth) left = window.innerWidth - 350;
    left = Math.max(10, left);
    top = Math.max(10, top);
    this.tipEl.style.left = left + "px";
    this.tipEl.style.top = top + "px";
  },

  highlight(sel) {
    if (!sel) return;
    const el = resolveTarget(sel);
    if (el) {
      el.classList.add("guide-highlight");
      this.highlightEl = el;
    }
  },

  unhighlight() {
    if (this.highlightEl) {
      this.highlightEl.classList.remove("guide-highlight");
      this.highlightEl = null;
    }
  },

  finish() {
    this.active = false;
    this.cleanupStep();
    if (this.tipEl) { this.tipEl.remove(); this.tipEl = null; }
    toast("引导演示结束");
  },
};

function setupDemo() {
  const dialog = document.getElementById("demo-dialog");
  document.getElementById("btn-demo").onclick = () => dialog.showModal();
  document.getElementById("demo-close").onclick = () => dialog.close();
  document.getElementById("demo-cancel-guide").onclick = () => { dialog.close(); Guide.finish(); };
  dialog.addEventListener("click", (e) => { if (e.target === dialog) dialog.close(); });
  const box = document.getElementById("demo-scenes");
  box.innerHTML = "";
  for (const s of guideScenes) {
    const btn = document.createElement("button");
    btn.className = "quick-btn";
    btn.textContent = s.name;
    btn.onclick = () => { dialog.close(); Guide.start(s); };
    box.appendChild(btn);
  }
}

/* ---------- 历史工单 ---------- */
function setupHistory() {
  const dialog = document.getElementById("history-dialog");
  document.getElementById("btn-history").onclick = async () => {
    dialog.showModal();
    await loadHistory();
  };
  document.getElementById("history-close").onclick = () => dialog.close();
  dialog.addEventListener("click", (e) => { if (e.target === dialog) dialog.close(); });
}

async function loadHistory() {
  const tickets = await api("/tickets?include_deleted=true");
  const ul = document.getElementById("history-list");
  ul.innerHTML = "";
  if (!tickets.length) {
    ul.innerHTML = '<li class="ticket-item"><div class="t-meta">暂无工单</div></li>';
    return;
  }
  for (const t of tickets) {
    const li = document.createElement("li");
    li.className = "ticket-item" + (t.deleted ? " deleted-ticket" : "");
    const badge = document.createElement("div");
    badge.className = "t-meta";
    badge.innerHTML = `<span class="badge ${t.status}">${statusText[t.status] || t.status}</span>${t.deleted ? '<span>已删除</span>' : ''}<span>${t.category || "未分类"}</span>`;
    const title = document.createElement("div");
    title.className = "t-title";
    title.textContent = t.title;
    li.appendChild(title);
    li.appendChild(badge);
    li.onclick = () => loadAudit(t.id);
    ul.appendChild(li);
  }
}

async function loadAudit(ticketId) {
  const entries = await api("/audit?ticket_id=" + encodeURIComponent(ticketId));
  const box = document.getElementById("history-audit");
  box.innerHTML = "";
  if (!entries.length) {
    box.innerHTML = '<div class="muted">暂无操作记录</div>';
    return;
  }
  for (const a of entries) {
    const div = document.createElement("div");
    div.className = "audit-item";
    div.innerHTML = `<div class="a-time">${fmtTime(a.time)} · <span class="a-action">${escapeHtml(a.action)}</span></div><div>${escapeHtml(a.detail)}</div>`;
    box.appendChild(div);
  }
}

/* ---------- 推送消息（右侧窄盒） ---------- */
async function loadMessages() {
  const msgs = await api("/inbox");
  const ul = document.getElementById("message-list");
  ul.innerHTML = "";
  if (!msgs.length) {
    ul.innerHTML = '<li class="message-item"><div class="m-meta">暂无推送消息</div></li>';
    return;
  }
  for (const m of msgs) {
    const li = document.createElement("li");
    li.className = "message-item" + (m.handled ? " handled" : "");
    const content = document.createElement("div");
    content.className = "m-content";
    content.textContent = m.content;
    const meta = document.createElement("div");
    meta.className = "m-meta";
    const time = document.createElement("span");
    time.textContent = fmtTime(m.created_at);
    meta.appendChild(time);
    if (m.handled) {
      const span = document.createElement("span");
      span.textContent = "已处理";
      meta.appendChild(span);
    } else {
      li.classList.add("clickable");
      li.title = "点击针对此反馈新建工单";
      li.onclick = () => handleMessage(m);
      const span = document.createElement("span");
      span.textContent = "点击处理 →";
      meta.appendChild(span);
    }
    li.appendChild(content);
    li.appendChild(meta);
    ul.appendChild(li);
  }
}

async function handleMessage(m) {
  try {
    const t = await api("/inbox/" + m.id + "/handle", { method: "POST" });
    toast("已从推送消息创建工单");
    await loadTickets();
    await loadMessages();
    openTicket(t.id);
  } catch (e) {
    toast(e.message, true);
  }
}

document.getElementById("btn-refresh").onclick = loadTickets;
document.getElementById("btn-analyze").onclick = () => triggerAnalyze();
document.getElementById("btn-cancel").onclick = cancelAnalyze;
document.getElementById("btn-confirm").onclick = confirmAnalysis;
document.getElementById("btn-discard").onclick = () => { document.getElementById("review-box").hidden = true; };
document.getElementById("btn-status").onclick = updateStatus;
document.getElementById("btn-delete").onclick = softDelete;
document.getElementById("qa-add").onclick = addQuickAction;

async function init() {
  setupNewDialog();
  setupDemo();
  setupHistory();
  await loadQuickActions();
  await loadTickets();
  await loadMessages();
}

init();
