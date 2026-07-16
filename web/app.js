/* global Terminal, FitAddon, GOWEBSSH_THEMES */
(function () {
  "use strict";
  const STORAGE_KEY = "gowebssh.ui";
  const $ = (id) => document.getElementById(id);
  const I18N = {
    en: {
      signInContinue: "Sign in to continue",
      username: "Username",
      password: "Password",
      login: "Login",
      rememberLogin: "Keep me signed in for 30 days",
      home: "Home",
      logout: "Logout",
      settings: "Settings",
      confirm: "Confirm",
      savedConnections: "Saved connections",
      savedConnectionsHint: "Manage encrypted SSH connection profiles.",
      addConnection: "Add connection",
      searchConnections: "Search connections",
      noConnections: "No saved connections",
      noConnectionsHint: "Add a connection to get started.",
      server: "Server",
      disconnect: "Disconnect",
      reconnect: "Reconnect",
      memory: "Memory",
      networkDown: "Network ↓",
      networkUp: "Network ↑",
      load: "Load",
      uptime: "Uptime",
      latency: "Latency",
      pane: "Pane",
      splitRight: "Split right",
      splitDown: "Split down",
      closePane: "Close pane",
      closeOtherPanes: "Close other panes",
      maxPanes: "A terminal tab supports up to 4 panes.",
      copy: "Copy",
      paste: "Paste",
      selectAll: "Select all",
      clearTerminal: "Clear terminal",
      confirmPaste: "Confirm multi-line paste",
      pasteSummary: "Paste {lines} lines into the active terminal?",
      clipboardDenied: "Clipboard access was denied by the browser.",
      confirmMultilinePaste: "Confirm multi-line paste",
      autoCopySelection: "Automatically copy selected text",
      controlCharsWarning: "Warning: the content contains control characters.",
      host: "Host",
      port: "Port",
      connectionSettings: "Connection settings",
      securityNotice: "Security notice:",
      securityText:
        "The private key is sent to this WebSSH server and is only saved when you explicitly choose encrypted storage.",
      warning: "Warning:",
      hostKeyWarning:
        "Host key verification is disabled. Man-in-the-middle attacks are possible.",
      connectionName: "Connection name",
      connectionNamePlaceholder: "Production server",
      privateKey: "Private Key",
      uploadKey: "Upload key file",
      clearKey: "Clear private key",
      passphraseOptional: "Passphrase (optional)",
      terminalType: "Terminal type",
      useTmux: "Recover terminal with tmux after disconnect",
      cancel: "Cancel",
      save: "Save",
      connect: "Connect",
      saveConnect: "Save & Connect",
      appearance: "Appearance",
      automatic: "Automatic",
      dark: "Dark",
      light: "Light",
      terminalTheme: "Terminal theme",
      font: "Font",
      customFont: "Custom font stack",
      customFontNames: "Local font names (fallback order)",
      customFontPlaceholder: "Maple Mono, Sarasa Mono SC, monospace",
      customFontHint:
        "Separate fonts with commas. The first available local font is used.",
      fontSize: "Font size",
      lineHeight: "Line height",
      cursorBlink: "Cursor blink",
      done: "Done",
      edit: "Edit",
      delete: "Delete",
      status: "Status",
      updated: "Updated",
      tmuxRecovery: "tmux recovery",
      enabled: "Enabled",
      disabled: "Disabled",
      hasPassphrase: "Passphrase saved",
      passphrase: "Passphrase",
      newConnection: "New connection",
      confirmDelete: "Delete this saved connection?",
      confirmLogout: "Logout and close all active SSH sessions?",
      confirmClose: "Close this SSH session?",
      requiredFields: "Host, username and private key are required.",
      invalidPort: "Port must be between 1 and 65535.",
      nameRequired: "Connection name is required.",
      saveFailed: "Unable to save credential.",
      loadFailed: "Unable to load credential.",
      deleteFailed: "Unable to delete credential.",
      connectionLost: "Connection lost; reconnecting in {seconds}s",
      sessionClosed: "Session closed",
      manuallyDisconnected: "Disconnected by user",
      connecting: "Connecting",
      connected: "Connected",
      reconnecting: "Reconnecting",
      disconnected: "Disconnected",
      toggleLight: "Switch to light mode",
      toggleDark: "Switch to dark mode",
      noMetrics: "Waiting for server metrics",
      active: "Active",
      openSession: "Open session",
      signingIn: "Signing in…",
      invalidCredentials: "Invalid username or password.",
      loginFailed: "Login failed.",
      networkDenied: "Target host is blocked by network policy.",
      privateKeyInvalid: "Private key is invalid or unsupported.",
      passphraseInvalid: "Private key passphrase is incorrect.",
      hostKeyFailed: "Host key verification failed.",
      sshAuthFailed: "SSH authentication failed.",
      connectTimeout: "SSH connection timed out.",
      sessionLimit: "Maximum concurrent sessions exceeded.",
      connectionFailed: "SSH connection failed.",
      reconnectExhausted: "Automatic reconnect stopped after 5 attempts.",
    },
    "zh-CN": {
      signInContinue: "登录以继续",
      username: "用户名",
      password: "密码",
      login: "登录",
      rememberLogin: "保持登录状态 30 天",
      home: "主页",
      logout: "退出登录",
      settings: "设置",
      confirm: "确认",
      savedConnections: "已保存的连接",
      savedConnectionsHint: "管理加密保存的 SSH 连接信息。",
      addConnection: "添加连接",
      searchConnections: "搜索连接",
      noConnections: "暂无保存的连接",
      noConnectionsHint: "添加一个连接以开始使用。",
      server: "服务器",
      disconnect: "断开连接",
      reconnect: "重新连接",
      memory: "内存",
      networkDown: "网络下载 ↓",
      networkUp: "网络上传 ↑",
      load: "系统负载",
      uptime: "运行时间",
      latency: "延迟",
      pane: "窗格",
      splitRight: "向右分屏",
      splitDown: "向下分屏",
      closePane: "关闭窗格",
      closeOtherPanes: "关闭其他窗格",
      maxPanes: "每个终端标签最多支持 4 个窗格。",
      copy: "复制",
      paste: "粘贴",
      selectAll: "全选",
      clearTerminal: "清空终端",
      confirmPaste: "确认多行粘贴",
      pasteSummary: "是否将 {lines} 行内容粘贴到当前终端？",
      clipboardDenied: "浏览器拒绝了剪贴板访问。",
      confirmMultilinePaste: "粘贴多行内容前确认",
      autoCopySelection: "选中文字后自动复制",
      controlCharsWarning: "警告：粘贴内容包含控制字符。",
      host: "主机",
      port: "端口",
      connectionSettings: "连接设置",
      securityNotice: "安全提示：",
      securityText:
        "私钥会发送到当前 WebSSH 服务端；只有明确选择加密保存时才会写入凭据库。",
      warning: "警告：",
      hostKeyWarning: "Host Key 校验已关闭，存在中间人攻击风险。",
      connectionName: "连接名称",
      connectionNamePlaceholder: "生产服务器",
      privateKey: "私钥",
      uploadKey: "上传私钥文件",
      clearKey: "清空私钥",
      passphraseOptional: "私钥密码（可选）",
      terminalType: "终端类型",
      useTmux: "断线后使用 tmux 恢复终端",
      cancel: "取消",
      save: "保存",
      connect: "连接",
      saveConnect: "保存并连接",
      appearance: "界面外观",
      automatic: "跟随系统",
      dark: "夜间",
      light: "日间",
      terminalTheme: "终端主题",
      font: "字体",
      customFont: "自定义字体组",
      customFontNames: "本机字体名称（按回退顺序）",
      customFontPlaceholder: "霞鹜文楷等宽, Sarasa Mono SC, monospace",
      customFontHint: "使用逗号分隔；浏览器会使用本机第一个可用字体。",
      fontSize: "字号",
      lineHeight: "行高",
      cursorBlink: "光标闪烁",
      done: "完成",
      edit: "编辑",
      delete: "删除",
      status: "状态",
      updated: "更新时间",
      tmuxRecovery: "tmux 恢复",
      enabled: "已启用",
      disabled: "未启用",
      hasPassphrase: "已保存私钥密码",
      passphrase: "私钥密码",
      newConnection: "新建连接",
      confirmDelete: "确定删除这个已保存的连接吗？",
      confirmLogout: "退出登录并关闭所有活动 SSH 会话吗？",
      confirmClose: "确定关闭这个 SSH 会话吗？",
      requiredFields: "Host、用户名和私钥不能为空。",
      invalidPort: "端口必须在 1 到 65535 之间。",
      nameRequired: "连接名称不能为空。",
      saveFailed: "无法保存凭据。",
      loadFailed: "无法加载凭据。",
      deleteFailed: "无法删除凭据。",
      connectionLost: "连接已中断，{seconds} 秒后重连",
      sessionClosed: "会话已关闭",
      manuallyDisconnected: "已手动断开",
      connecting: "正在连接",
      connected: "已连接",
      reconnecting: "正在重连",
      disconnected: "已断开",
      toggleLight: "切换到日间模式",
      toggleDark: "切换到夜间模式",
      noMetrics: "等待服务器监控数据",
      active: "活动中",
      openSession: "打开会话",
      signingIn: "正在登录…",
      invalidCredentials: "用户名或密码错误。",
      loginFailed: "登录失败。",
      networkDenied: "目标地址被网络策略拒绝。",
      privateKeyInvalid: "私钥格式错误或不受支持。",
      passphraseInvalid: "私钥密码不正确。",
      hostKeyFailed: "Host Key 校验失败。",
      sshAuthFailed: "SSH 认证失败。",
      connectTimeout: "SSH 连接超时。",
      sessionLimit: "已达到最大并发会话数。",
      connectionFailed: "SSH 连接失败。",
      reconnectExhausted: "自动重连已达到 5 次，现已停止。",
    },
  };
  const views = {
    login: $("view-login"),
    home: $("view-home"),
    terminal: $("view-terminal"),
  };
  const e = {
    header: $("workspace-header"),
    loginForm: $("login-form"),
    loginUser: $("login-username"),
    loginPass: $("login-password"),
    loginRemember: $("login-remember"),
    loginBtn: $("login-btn"),
    loginError: $("login-error"),
    authUser: $("auth-user"),
    userMenuButton: $("user-menu-btn"),
    userMenu: $("user-menu"),
    logout: $("logout-btn"),
    langLogin: $("language-login"),
    langWorkspace: $("language-workspace"),
    modeLogin: $("mode-login"),
    modeWorkspace: $("mode-workspace"),
    settingsBtn: $("settings-btn"),
    settingsConfirm: $("settings-confirm-btn"),
    homeTab: $("home-tab"),
    tabs: $("terminal-tabs"),
    newConnection: $("new-connection-btn"),
    homeAdd: $("home-add-btn"),
    search: $("connection-search"),
    list: $("connection-list"),
    empty: $("connection-empty"),
    connectionModal: $("connection-modal"),
    settingsModal: $("settings-modal"),
    form: $("connect-form"),
    name: $("credential-name"),
    host: $("ssh-host"),
    port: $("ssh-port"),
    user: $("ssh-username"),
    key: $("ssh-private-key"),
    keyFile: $("ssh-key-file"),
    pass: $("ssh-passphrase"),
    termType: $("ssh-term"),
    useTmux: $("ssh-use-tmux"),
    clearKey: $("clear-key-btn"),
    save: $("save-credential-btn"),
    connectOnly: $("connect-only-btn"),
    saveConnect: $("save-connect-btn"),
    connectError: $("connect-error"),
    hostWarning: $("hostkey-warning"),
    ui: $("setting-ui"),
    theme: $("setting-theme"),
    font: $("setting-font"),
    customFont: $("setting-custom-font"),
    customFontRow: $("custom-font-row"),
    fontSize: $("setting-fontsize"),
    lineHeight: $("setting-lineheight"),
    cursorBlink: $("setting-cursor-blink"),
    confirmPasteSetting: $("setting-confirm-paste"),
    autoCopy: $("setting-auto-copy"),
    stack: $("terminal-stack"),
    error: $("term-error"),
    errorText: $("term-error-text"),
    errorClose: $("term-error-close"),
    panel: $("metrics-panel"),
    resizer: $("panel-resizer"),
    sideStatus: $("sidebar-status"),
    sideDisconnect: $("sidebar-disconnect"),
    sideReconnect: $("sidebar-reconnect"),
    metricTarget: $("metric-target"),
    metricCPU: $("metric-cpu"),
    metricCPUBar: $("metric-cpu-bar"),
    metricMemory: $("metric-memory"),
    metricMemoryBar: $("metric-memory-bar"),
    metricRX: $("metric-rx"),
    metricTX: $("metric-tx"),
    metricLoad: $("metric-load"),
    metricUptime: $("metric-uptime"),
    metricLatency: $("metric-latency"),
    paneCount: $("pane-count"),
    splitRight: $("split-right-btn"),
    splitDown: $("split-down-btn"),
    closePane: $("close-pane-btn"),
    contextMenu: $("terminal-context-menu"),
    pasteModal: $("paste-modal"),
    pasteSummary: $("paste-summary"),
    pastePreview: $("paste-preview"),
    confirmPaste: $("confirm-paste-btn"),
  };
  let uiConfig = {},
    credentials = [],
    activeId = null,
    serial = 0,
    currentPage = "login";
  const sessions = new Map();
  const groups = new Map();
  let activeGroupId = null;
  let contextSession = null,
    pendingPaste = "";
  let settingsSnapshot = null;
  function defaults() {
    const light = matchMedia?.("(prefers-color-scheme: light)").matches;
    return {
      uiMode: "auto",
      theme: light ? "github-light" : "catppuccin-mocha",
      fontFamily:
        '"JetBrains Mono", "Fira Code", "Cascadia Code", Menlo, Monaco, Consolas, "Noto Sans Mono CJK SC", monospace',
      fontSize: 14,
      lineHeight: 1.2,
      cursorBlink: true,
      confirmMultilinePaste: true,
      autoCopySelection: false,
      language: navigator.language.toLowerCase().startsWith("zh")
        ? "zh-CN"
        : "en",
      panelWidth: 210,
    };
  }
  function loadSettings() {
    try {
      return {
        ...defaults(),
        ...JSON.parse(localStorage.getItem(STORAGE_KEY) || "{}"),
      };
    } catch (_) {
      return defaults();
    }
  }
  let settings = loadSettings();
  const t = (key, vars = {}) => {
    let value = (I18N[settings.language] || I18N.en)[key] || key;
    Object.entries(vars).forEach(
      ([k, v]) => (value = value.replace("{" + k + "}", v)),
    );
    return value;
  };
  function saveSettings() {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({
        uiMode: settings.uiMode,
        theme: settings.theme,
        fontFamily: settings.fontFamily,
        fontSize: settings.fontSize,
        lineHeight: settings.lineHeight,
        cursorBlink: settings.cursorBlink,
        confirmMultilinePaste: settings.confirmMultilinePaste,
        autoCopySelection: settings.autoCopySelection,
        language: settings.language,
        panelWidth: settings.panelWidth,
      }),
    );
  }
  async function api(path, options = {}) {
    const res = await fetch(path, {
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...(options.headers || {}),
      },
      ...options,
    });
    const text = await res.text();
    let data;
    try {
      data = text ? JSON.parse(text) : null;
    } catch (_) {
      data = null;
    }
    return { res, data };
  }
  function errorMessage(data, fallback) {
    const code = data?.error?.code;
    const map = { AUTH_REQUIRED: "login", INVALID_PORT: "invalidPort" };
    return code && map[code]
      ? t(map[code])
      : data?.error?.message || t(fallback);
  }
  function wsError(data) {
    const map = {
      NETWORK_DENIED: "networkDenied",
      INVALID_HOST: "networkDenied",
      PRIVATE_KEY_INVALID: "privateKeyInvalid",
      PRIVATE_KEY_PASSPHRASE_REQUIRED: "passphraseInvalid",
      PRIVATE_KEY_PASSPHRASE_INVALID: "passphraseInvalid",
      HOST_KEY_UNTRUSTED: "hostKeyFailed",
      HOST_KEY_CHANGED: "hostKeyFailed",
      SSH_AUTH_FAILED: "sshAuthFailed",
      SSH_CONNECT_TIMEOUT: "connectTimeout",
      SESSION_LIMIT_EXCEEDED: "sessionLimit",
      SSH_CONNECT_FAILED: "connectionFailed",
      SSH_SESSION_FAILED: "connectionFailed",
    };
    return t(map[data?.code] || "connectionFailed");
  }
  function applyLanguage() {
    document.documentElement.lang = settings.language;
    e.langLogin.value = e.langWorkspace.value = settings.language;
    document
      .querySelectorAll("[data-i18n]")
      .forEach((n) => (n.textContent = t(n.dataset.i18n)));
    document
      .querySelectorAll("[data-i18n-placeholder]")
      .forEach((n) => (n.placeholder = t(n.dataset.i18nPlaceholder)));
    document.querySelectorAll("[data-i18n-title]").forEach((n) => {
      n.title = t(n.dataset.i18nTitle);
      n.setAttribute("aria-label", t(n.dataset.i18nTitle));
    });
    renderHome();
    sessions.forEach(updateTab);
    updateSidebar();
  }
  function effectiveLight() {
    return (
      settings.uiMode === "light" ||
      (settings.uiMode === "auto" &&
        matchMedia?.("(prefers-color-scheme: light)").matches)
    );
  }
  function applyMode() {
    document.documentElement.classList.remove("ui-auto", "ui-dark", "ui-light");
    document.documentElement.classList.add("ui-" + settings.uiMode);
    e.ui.value = settings.uiMode;
    const light = effectiveLight(),
      icon = light ? "☀" : "☾",
      label = light ? t("toggleDark") : t("toggleLight");
    [e.modeLogin, e.modeWorkspace].forEach((b) => {
      b.textContent = icon;
      b.title = label;
      b.setAttribute("aria-label", label);
    });
  }
  function toggleMode() {
    settings.uiMode = effectiveLight() ? "dark" : "light";
    saveSettings();
    applyMode();
  }
  function showPage(name) {
    currentPage = name;
    Object.entries(views).forEach(([k, n]) =>
      n.classList.toggle("hidden", k !== name),
    );
    e.header.classList.toggle("hidden", name === "login");
    e.homeTab.classList.toggle("active", name === "home");
    document.body.classList.toggle("terminal-active", name === "terminal");
    setTimeout(fitAll, 60);
  }
  function openModal(node) {
    node.classList.remove("hidden");
    document.body.classList.add("modal-open");
    setTimeout(() => node.querySelector("input,select,button")?.focus(), 20);
  }
  function closeModal(node) {
    node.classList.add("hidden");
    if (
      e.connectionModal.classList.contains("hidden") &&
      e.settingsModal.classList.contains("hidden") &&
      e.pasteModal.classList.contains("hidden")
    )
      document.body.classList.remove("modal-open");
  }
  function populateThemes() {
    e.theme.innerHTML = "";
    (uiConfig.themes || Object.keys(GOWEBSSH_THEMES)).forEach((name) =>
      e.theme.add(new Option(name, name)),
    );
  }
  function terminalTheme() {
    return (
      GOWEBSSH_THEMES[settings.theme] || GOWEBSSH_THEMES["catppuccin-mocha"]
    );
  }
  function applyTerminalSettings(s) {
    const theme = terminalTheme();
    s.container.style.setProperty("--terminal-background", theme.background);
    s.pane.style.setProperty("--terminal-background", theme.background);
    s.term.options = {
      theme: { ...theme },
      fontFamily: settings.fontFamily,
      fontSize: Number(settings.fontSize),
      lineHeight: Number(settings.lineHeight),
      cursorBlink: !!settings.cursorBlink,
      letterSpacing: 1,
      rescaleOverlappingGlyphs: true,
    };
    s.term.clearTextureAtlas();
    s.term.refresh(0, s.term.rows - 1);
    setTimeout(() => fit(s), 40);
  }
  function applySettings() {
    applyMode();
    e.theme.value = settings.theme;
    const preset = [...e.font.options].some(
      (option) =>
        option.value !== "custom" && option.value === settings.fontFamily,
    );
    e.font.value = preset ? settings.fontFamily : "custom";
    e.customFont.value = preset ? "" : settings.fontFamily;
    e.customFontRow.classList.toggle("hidden", preset);
    e.fontSize.value = settings.fontSize;
    e.lineHeight.value = settings.lineHeight;
    e.cursorBlink.checked = settings.cursorBlink;
    e.confirmPasteSetting.checked = settings.confirmMultilinePaste;
    e.autoCopy.checked = settings.autoCopySelection;
    e.panel.style.width = settings.panelWidth + "px";
    e.panel.style.flexBasis = settings.panelWidth + "px";
    sessions.forEach(applyTerminalSettings);
  }
  function readSettingsPreview() {
    settings.uiMode = e.ui.value;
    settings.theme = e.theme.value;
    if (e.font.value !== "custom") settings.fontFamily = e.font.value;
    else settings.fontFamily = normalizeFontStack(e.customFont.value);
    settings.fontSize = Number(e.fontSize.value) || 14;
    settings.lineHeight = Number(e.lineHeight.value) || 1.2;
    settings.cursorBlink = e.cursorBlink.checked;
    settings.confirmMultilinePaste = e.confirmPasteSetting.checked;
    settings.autoCopySelection = e.autoCopy.checked;
    applyMode();
    sessions.forEach(applyTerminalSettings);
  }
  function openSettings() {
    settingsSnapshot = JSON.parse(JSON.stringify(settings));
    applySettings();
    openModal(e.settingsModal);
  }
  function cancelSettings() {
    if (settingsSnapshot) settings = settingsSnapshot;
    settingsSnapshot = null;
    applySettings();
    closeModal(e.settingsModal);
  }
  function confirmSettings() {
    readSettingsPreview();
    saveSettings();
    settingsSnapshot = null;
    closeModal(e.settingsModal);
  }

  async function refreshCredentials() {
    if (!uiConfig.credentialStorage) return;
    const { res, data } = await api("/api/credentials");
    if (res.ok) {
      credentials = data.credentials || [];
      renderHome();
    }
  }
  function activeForCredential(id) {
    return [...sessions.values()].find((s) => s.savedId === id);
  }
  function renderHome() {
    if (!e.list) return;
    const q = (e.search.value || "").toLowerCase();
    e.list.innerHTML = "";
    const shown = credentials.filter((c) =>
      (c.name + " " + c.host + " " + c.username).toLowerCase().includes(q),
    );
    e.empty.classList.toggle("hidden", shown.length > 0);
    shown.forEach((c) => {
      const active = activeForCredential(c.id);
      const card = document.createElement("article");
      card.className = "connection-card";
      card.innerHTML =
        '<div class="connection-card-head"><div><h3></h3><div class="connection-host"></div></div><span class="connection-badge"></span></div><dl><div><dt>' +
        t("port") +
        "</dt><dd></dd></div><div><dt>" +
        t("terminalType") +
        "</dt><dd></dd></div><div><dt>" +
        t("tmuxRecovery") +
        "</dt><dd></dd></div><div><dt>" +
        t("passphrase") +
        "</dt><dd></dd></div><div><dt>" +
        t("updated") +
        '</dt><dd></dd></div></dl><div class="connection-card-actions"><button class="btn primary auto-width" data-action="connect"></button><button class="btn ghost" data-action="edit"></button><button class="btn danger" data-action="delete"></button></div>';
      card.querySelector("h3").textContent = c.name;
      card.querySelector(".connection-host").textContent =
        c.username + "@" + c.host;
      const badge = card.querySelector(".connection-badge");
      badge.textContent = active ? t("active") : "";
      badge.classList.toggle("hidden", !active);
      const dd = card.querySelectorAll("dd");
      dd[0].textContent = c.port;
      dd[1].textContent = c.term;
      dd[2].textContent = c.useTmux ? t("enabled") : t("disabled");
      dd[3].textContent = c.hasPassphrase ? t("enabled") : t("disabled");
      dd[4].textContent = new Date(c.updatedAt).toLocaleString(
        settings.language,
      );
      card.querySelector('[data-action="connect"]').textContent = active
        ? t("openSession")
        : t("connect");
      card.querySelector('[data-action="edit"]').textContent = t("edit");
      card.querySelector('[data-action="delete"]').textContent = t("delete");
      card.addEventListener("click", (ev) => handleCardAction(ev, c));
      e.list.appendChild(card);
    });
  }
  async function handleCardAction(event, c) {
    const action = event.target.closest("button")?.dataset.action;
    if (!action) return;
    if (action === "connect") {
      const active = activeForCredential(c.id);
      if (active) {
        activatePane(active.id);
        return;
      }
      const full = await getCredential(c.id);
      if (full) createSession({ ...full, savedId: c.id });
    } else if (action === "edit") {
      const full = await getCredential(c.id);
      if (full) openConnection(full);
    } else if (action === "delete") {
      if (!confirm(t("confirmDelete"))) return;
      const { res, data } = await api(
        "/api/credentials/" + encodeURIComponent(c.id),
        { method: "DELETE" },
      );
      if (!res.ok) showError(errorMessage(data, "deleteFailed"));
      await refreshCredentials();
    }
  }
  async function getCredential(id) {
    const { res, data } = await api(
      "/api/credentials/" + encodeURIComponent(id),
    );
    if (!res.ok) {
      showError(errorMessage(data, "loadFailed"));
      return null;
    }
    return data;
  }
  function resetConnectionForm() {
    e.form.reset();
    e.port.value = 22;
    e.termType.value = "xterm-256color";
    e.name.dataset.id = "";
    e.connectError.textContent = "";
  }
  function openConnection(item = null) {
    resetConnectionForm();
    if (item) {
      e.name.dataset.id = item.id || "";
      e.name.value = item.name || "";
      e.host.value = item.host || "";
      e.port.value = item.port || 22;
      e.user.value = item.username || "";
      e.key.value = item.privateKey || "";
      e.pass.value = item.passphrase || "";
      e.termType.value = item.term || "xterm-256color";
      e.useTmux.checked = !!item.useTmux;
    }
    openModal(e.connectionModal);
  }
  function formCredential() {
    return {
      id: e.name.dataset.id || "",
      name: e.name.value.trim(),
      host: e.host.value.trim(),
      port: Number(e.port.value) || 22,
      username: e.user.value.trim(),
      privateKey: e.key.value,
      passphrase: e.pass.value,
      term: e.termType.value.trim() || "xterm-256color",
      useTmux: e.useTmux.checked,
    };
  }
  function validate(c, requireName = false) {
    if (!c.host || !c.username || !c.privateKey) return t("requiredFields");
    if (c.port < 1 || c.port > 65535) return t("invalidPort");
    if (requireName && !c.name) return t("nameRequired");
    return "";
  }
  async function persistCredential(c) {
    const error = validate(c, true);
    if (error) {
      e.connectError.textContent = error;
      return null;
    }
    const { res, data } = await api("/api/credentials", {
      method: "POST",
      body: JSON.stringify(c),
    });
    if (!res.ok) {
      e.connectError.textContent = errorMessage(data, "saveFailed");
      return null;
    }
    await refreshCredentials();
    return data;
  }
  async function connectionAction(kind) {
    const c = formCredential(),
      needsSave = kind !== "connect",
      error = validate(c, needsSave);
    if (error) {
      e.connectError.textContent = error;
      return;
    }
    let savedId = c.id;
    if (needsSave) {
      const saved = await persistCredential(c);
      if (!saved) return;
      savedId = saved.id;
    }
    closeModal(e.connectionModal);
    if (kind !== "save") {
      createSession({ ...c, savedId });
      e.pass.value = "";
    }
  }

  function wsURL() {
    return (
      (location.protocol === "https:" ? "wss://" : "ws://") +
      location.host +
      "/api/ws/ssh"
    );
  }
  function send(s, type, data) {
    if (s.socket?.readyState === WebSocket.OPEN)
      s.socket.send(JSON.stringify({ type, data }));
  }
  function fit(s) {
    if (!s || s.pane?.offsetParent === null) return;
    try {
      s.fit.fit();
      send(s, "resize", { cols: s.term.cols, rows: s.term.rows });
    } catch (_) {}
  }
  function fitAll() {
    sessions.forEach(fit);
  }
  function createSession(credential) {
    const id = "group-" + ++serial;
    const container = document.createElement("section");
    container.className = "terminal-group hidden";
    container.dataset.groupId = id;
    e.stack.appendChild(container);
    const tab = document.createElement("div");
    tab.className = "terminal-tab";
    tab.tabIndex = 0;
    tab.setAttribute("role", "tab");
    tab.innerHTML =
      '<i class="terminal-tab-state"></i><span class="terminal-tab-label"></span><span class="terminal-tab-panes"></span><button type="button" class="terminal-tab-close" aria-label="Close">×</button>';
    e.tabs.appendChild(tab);
    const group = {
      id,
      credential: { ...credential },
      savedId: credential.savedId || "",
      container,
      tab,
      panes: [],
      orientation: "right",
      colRatio: 50,
      rowRatio: 50,
      metrics: null,
      latency: null,
    };
    groups.set(id, group);
    tab.addEventListener("click", (ev) => {
      if (!ev.target.closest(".terminal-tab-close")) activateGroup(id);
    });
    tab.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter" || ev.key === " ") activateGroup(id);
    });
    tab.addEventListener("contextmenu", (ev) => {
      activateGroup(id);
      openContextMenu(ev, sessions.get(activeId) || group.panes[0]);
    });
    tab.querySelector(".terminal-tab-close").addEventListener("click", (ev) => {
      ev.stopPropagation();
      if (
        group.panes.some((p) => p.state === "connected") &&
        !confirm(t("confirmClose"))
      )
        return;
      removeGroup(id);
    });
    const pane = createPane(group);
    activatePane(pane.id);
    renderHome();
    return pane;
  }
  function createPane(group) {
    if (!group || group.panes.length >= 4) return null;
    const id = "pane-" + ++serial;
    const pane = document.createElement("div");
    pane.className = "terminal-pane";
    pane.dataset.paneId = id;
    const badge = document.createElement("span");
    badge.className = "pane-badge";
    pane.appendChild(badge);
    const container = document.createElement("div");
    container.className = "terminal-container";
    pane.appendChild(container);
    group.container.appendChild(pane);
    const term = new Terminal({
        cursorBlink: settings.cursorBlink,
        fontFamily: settings.fontFamily,
        fontSize: Number(settings.fontSize),
        lineHeight: Number(settings.lineHeight),
        letterSpacing: 1,
        rescaleOverlappingGlyphs: true,
        theme: { ...terminalTheme() },
        scrollback: 5000,
        convertEol: false,
        allowProposedApi: true,
      }),
      fitAddon = new FitAddon.FitAddon();
    term.loadAddon(fitAddon);
    term.open(container);
    const s = {
      id,
      groupId: group.id,
      pane,
      badge,
      credential: { ...group.credential },
      savedId: group.savedId,
      container,
      term,
      fit: fitAddon,
      socket: null,
      state: "connecting",
      manual: false,
      suppressReconnect: false,
      retry: 0,
      retryTimer: null,
      pingTimer: null,
      latency: null,
      tmuxSession: "",
      metrics: null,
    };
    sessions.set(id, s);
    group.panes.push(s);
    term.onData((data) => send(s, "input", data));
    term.onSelectionChange(() => {
      if (settings.autoCopySelection && term.hasSelection()) copySelection(s);
    });
    s.observer = new ResizeObserver(() => fit(s));
    s.observer.observe(container);
    pane.addEventListener("pointerdown", () => activatePane(id));
    pane.addEventListener("focusin", () => activatePane(id));
    pane.addEventListener("contextmenu", (ev) => openContextMenu(ev, s));
    updateGroupLayout(group);
    updateTab(s);
    connectSession(s);
    return s;
  }
  function splitActive(direction) {
    const group = groups.get(activeGroupId);
    if (!group || group.panes.length >= 4) {
      showError(t("maxPanes"));
      return;
    }
    if (group.panes.length === 1) group.orientation = direction;
    const pane = createPane(group);
    if (pane) activatePane(pane.id);
  }
  function updateGroupLayout(group) {
    const count = group.panes.length;
    group.container.dataset.count = String(count);
    group.container.dataset.orientation =
      count === 2 ? group.orientation : "grid";
    group.container.style.setProperty("--pane-col", group.colRatio + "%");
    group.container.style.setProperty("--pane-row", group.rowRatio + "%");
    group.panes.forEach((p, i) => (p.badge.textContent = String(i + 1)));
    group.tab.querySelector(".terminal-tab-panes").textContent =
      count > 1 ? "·" + count : "";
    installGroupSplitters(group);
    setTimeout(() => group.panes.forEach(fit), 40);
    updateSidebar();
  }
  function installGroupSplitters(group) {
    group.container
      .querySelectorAll(".pane-splitter")
      .forEach((n) => n.remove());
    if (group.panes.length < 2) return;
    const directions =
      group.panes.length === 2 ? [group.orientation] : ["right", "down"];
    directions.forEach((direction) => {
      const bar = document.createElement("div");
      bar.className = "pane-splitter splitter-" + direction;
      bar.addEventListener("pointerdown", (ev) => {
        bar.setPointerCapture(ev.pointerId);
        const move = (eve) => {
          const rect = group.container.getBoundingClientRect();
          if (direction === "right") {
            group.colRatio = Math.min(
              75,
              Math.max(25, ((eve.clientX - rect.left) / rect.width) * 100),
            );
            group.container.style.setProperty(
              "--pane-col",
              group.colRatio + "%",
            );
          } else {
            group.rowRatio = Math.min(
              75,
              Math.max(25, ((eve.clientY - rect.top) / rect.height) * 100),
            );
            group.container.style.setProperty(
              "--pane-row",
              group.rowRatio + "%",
            );
          }
          group.panes.forEach(fit);
        };
        bar.addEventListener("pointermove", move);
        bar.addEventListener("pointerup", () => group.panes.forEach(fit), {
          once: true,
        });
      });
      group.container.appendChild(bar);
    });
  }
  function updateTab(s) {
    const group = groups.get(s.groupId);
    if (!group) return;
    const states = group.panes.map((p) => p.state);
    const state = states.includes("connected")
      ? "connected"
      : states.includes("connecting")
        ? "connecting"
        : states.includes("reconnecting")
          ? "reconnecting"
          : "disconnected";
    group.tab.dataset.state = state;
    group.tab.querySelector(".terminal-tab-label").textContent =
      s.credential.name || s.credential.username + "@" + s.credential.host;
    group.tab.title = t(state) || state;
  }
  function setState(s, state) {
    s.state = state;
    updateTab(s);
    if (activeId === s.id) updateSidebar();
    renderHome();
  }
  function connectSession(s) {
    clearTimeout(s.retryTimer);
    if (s.socket) {
      try {
        s.socket.close();
      } catch (_) {}
    }
    s.manual = false;
    s.suppressReconnect = false;
    setState(s, s.retry ? "reconnecting" : "connecting");
    const socket = new WebSocket(wsURL());
    s.socket = socket;
    socket.onopen = () => {
      try {
        s.fit.fit();
      } catch (_) {}
      send(s, "connect", {
        ...s.credential,
        passphrase: s.credential.passphrase || undefined,
        cols: s.term.cols,
        rows: s.term.rows,
        tmuxSession: s.tmuxSession || undefined,
      });
    };
    socket.onmessage = (event) => {
      let msg;
      try {
        msg = JSON.parse(event.data);
      } catch (_) {
        return;
      }
      if (msg.type === "connected") {
        s.retry = 0;
        s.tmuxSession = msg.data.tmuxSession || s.tmuxSession;
        setState(s, "connected");
        s.term.focus();
        fit(s);
        sendPing(s);
        clearInterval(s.pingTimer);
        s.pingTimer = setInterval(() => sendPing(s), 5000);
      } else if (msg.type === "output") s.term.write(msg.data);
      else if (msg.type === "pong") {
        s.latency = Math.max(
          0,
          Math.round(performance.now() - Number(msg.data)),
        );
        const group = groups.get(s.groupId);
        if (group) group.latency = s.latency;
        if (activeGroupId === s.groupId) renderLatency(group);
      } else if (msg.type === "metrics") {
        s.metrics = msg.data;
        const group = groups.get(s.groupId);
        if (group) group.metrics = msg.data;
        if (activeGroupId === s.groupId) renderMetrics(group);
      } else if (msg.type === "error") {
        s.suppressReconnect = true;
        setState(s, "disconnected");
        showError(wsError(msg.data));
      } else if (msg.type === "closed") {
        s.suppressReconnect = true;
        setState(s, "disconnected");
        s.term.writeln("\r\n[" + t("sessionClosed") + "]");
      }
    };
    socket.onerror = () => setState(s, "disconnected");
    socket.onclose = () => {
      if (s.socket !== socket) return;
      s.socket = null;
      clearInterval(s.pingTimer);
      s.pingTimer = null;
      s.latency = null;
      if (!s.manual && !s.suppressReconnect && sessions.has(s.id))
        scheduleReconnect(s);
    };
  }
  function sendPing(s) {
    if (s.state === "connected") send(s, "ping", performance.now());
  }
  function scheduleReconnect(s) {
    if (s.retry >= 5) {
      s.suppressReconnect = true;
      setState(s, "disconnected");
      s.term.writeln("\r\n[" + t("reconnectExhausted") + "]");
      return;
    }
    s.retry++;
    const delay = Math.min(15000, 1000 * Math.pow(2, Math.min(s.retry - 1, 4)));
    setState(s, "reconnecting");
    s.term.writeln(
      "\r\n[" +
        t("connectionLost", { seconds: Math.round(delay / 1000) }) +
        "]",
    );
    s.retryTimer = setTimeout(() => connectSession(s), delay);
  }
  function disconnectSession(s) {
    if (!s) return;
    s.manual = true;
    s.suppressReconnect = true;
    clearTimeout(s.retryTimer);
    clearInterval(s.pingTimer);
    s.pingTimer = null;
    s.latency = null;
    send(s, "disconnect");
    try {
      s.socket?.close();
    } catch (_) {}
    s.socket = null;
    setState(s, "disconnected");
    s.term.writeln("\r\n[" + t("manuallyDisconnected") + "]");
  }
  function reconnectSession(s) {
    if (!s) return;
    s.manual = false;
    s.suppressReconnect = false;
    s.retry = 0;
    connectSession(s);
  }
  function disposePane(s) {
    if (!s) return;
    disconnectSession(s);
    s.observer.disconnect();
    s.term.dispose();
    s.pane.remove();
    s.credential.privateKey = s.credential.passphrase = "";
    sessions.delete(s.id);
  }
  function removeSession(id) {
    const s = sessions.get(id);
    if (!s) return;
    const group = groups.get(s.groupId);
    disposePane(s);
    group.panes = group.panes.filter((p) => p.id !== id);
    if (!group.panes.length) {
      removeGroup(group.id);
      return;
    }
    updateGroupLayout(group);
    activatePane(group.panes[0].id);
    renderHome();
  }
  function removeGroup(id) {
    const group = groups.get(id);
    if (!group) return;
    [...group.panes].forEach(disposePane);
    group.container.remove();
    group.tab.remove();
    groups.delete(id);
    if (activeGroupId === id) {
      const next = groups.values().next().value;
      if (next) activateGroup(next.id);
      else {
        activeGroupId = null;
        activeId = null;
        showPage("home");
        resetMetrics();
      }
    }
    renderHome();
  }
  function activateGroup(id) {
    const group = groups.get(id);
    if (!group) return;
    groups.forEach((g) => {
      g.container.classList.toggle("hidden", g.id !== id);
      g.tab.classList.toggle("active", g.id === id);
    });
    const pane = group.panes.find((p) => p.id === activeId) || group.panes[0];
    activatePane(pane.id);
  }
  function activatePane(id) {
    const s = sessions.get(id);
    if (!s) return;
    const groupChanged = activeGroupId !== s.groupId;
    activeGroupId = s.groupId;
    activeId = id;
    groups.forEach((g) => {
      g.container.classList.toggle("hidden", g.id !== s.groupId);
      g.tab.classList.toggle("active", g.id === s.groupId);
    });
    sessions.forEach((x) => x.pane.classList.toggle("active", x.id === id));
    showPage("terminal");
    if (groupChanged) renderMetrics(groups.get(s.groupId));
    updateSidebar();
    setTimeout(() => {
      fit(s);
      s.term.focus();
    }, 30);
  }
  function updateSidebar() {
    const s = sessions.get(activeId);
    if (!s) {
      e.sideStatus.textContent = "—";
      e.sideDisconnect.disabled = e.sideReconnect.disabled = true;
      e.paneCount.textContent = "—";
      return;
    }
    e.sideStatus.textContent = t(s.state);
    e.sideStatus.className = "status status-" + s.state;
    e.sideDisconnect.disabled =
      s.state !== "connected" &&
      s.state !== "reconnecting" &&
      s.state !== "connecting";
    e.sideReconnect.disabled =
      s.state === "connected" ||
      s.state === "connecting" ||
      s.state === "reconnecting";
    const group = groups.get(s.groupId);
    e.paneCount.textContent =
      group.panes.indexOf(s) + 1 + "/" + group.panes.length;
    e.splitRight.disabled = e.splitDown.disabled = group.panes.length >= 4;
    e.closePane.disabled = group.panes.length <= 1;
  }
  function hideContextMenu() {
    e.contextMenu.classList.add("hidden");
    contextSession = null;
  }
  function openContextMenu(ev, s) {
    if (!s) return;
    ev.preventDefault();
    activatePane(s.id);
    contextSession = s;
    const count = groups.get(s.groupId).panes.length;
    e.contextMenu.querySelector('[data-context-action="copy"]').disabled =
      !s.term.hasSelection();
    e.contextMenu.querySelector('[data-context-action="close-pane"]').disabled =
      count <= 1;
    e.contextMenu.querySelector(
      '[data-context-action="close-other-panes"]',
    ).disabled = count <= 1;
    e.contextMenu
      .querySelectorAll('[data-context-action^="split-"]')
      .forEach((n) => (n.disabled = count >= 4));
    e.contextMenu.classList.remove("hidden");
    const rect = e.contextMenu.getBoundingClientRect();
    e.contextMenu.style.left =
      Math.max(8, Math.min(ev.clientX, innerWidth - rect.width - 8)) + "px";
    e.contextMenu.style.top =
      Math.max(8, Math.min(ev.clientY, innerHeight - rect.height - 8)) + "px";
  }
  async function copySelection(s) {
    if (!s?.term.hasSelection()) return;
    try {
      await navigator.clipboard.writeText(s.term.getSelection());
    } catch (_) {
      showError(t("clipboardDenied"));
    }
  }
  async function pasteFromClipboard(s) {
    try {
      await requestPaste(s, await navigator.clipboard.readText());
    } catch (_) {
      showError(t("clipboardDenied"));
    }
  }
  async function requestPaste(s, text) {
    if (!s || !text) return;
    activatePane(s.id);
    const lines = text.replace(/\r/g, "").split("\n").length;
    const hasControls = /[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/.test(text);
    if ((lines > 1 && settings.confirmMultilinePaste) || hasControls) {
      pendingPaste = text;
      e.pasteSummary.textContent =
        t("pasteSummary", { lines }) +
        (hasControls ? " " + t("controlCharsWarning") : "");
      e.pastePreview.textContent = text.slice(0, 4000);
      openModal(e.pasteModal);
    } else s.term.paste(text);
  }
  function contextAction(action) {
    const s = contextSession || sessions.get(activeId);
    hideContextMenu();
    if (!s) return;
    if (action === "copy") copySelection(s);
    else if (action === "paste") pasteFromClipboard(s);
    else if (action === "select-all") s.term.selectAll();
    else if (action === "clear") s.term.clear();
    else if (action === "split-right") splitActive("right");
    else if (action === "split-down") splitActive("down");
    else if (action === "close-pane" && groups.get(s.groupId).panes.length > 1)
      removeSession(s.id);
    else if (action === "close-other-panes") {
      const group = groups.get(s.groupId);
      group.panes
        .filter((p) => p.id !== s.id)
        .forEach((p) => removeSession(p.id));
      activatePane(s.id);
    }
  }
  function humanBytes(v) {
    if (!Number.isFinite(v)) return "—";
    const units = ["B", "KB", "MB", "GB", "TB"];
    let n = v,
      i = 0;
    while (n >= 1024 && i < units.length - 1) {
      n /= 1024;
      i++;
    }
    return n.toFixed(i ? 1 : 0) + " " + units[i];
  }
  function renderMetrics(group) {
    if (!group?.panes.length) return;
    const s = group.panes[0];
    e.metricTarget.textContent =
      s.credential.username + "@" + s.credential.host + ":" + s.credential.port;
    const m = group.metrics;
    if (!m) {
      resetMetricValues();
      renderLatency(group);
      return;
    }
    renderLatency(group);
    e.metricCPU.textContent = m.cpuPercent.toFixed(1) + "%";
    e.metricCPUBar.style.width = Math.min(100, m.cpuPercent) + "%";
    e.metricMemory.textContent =
      m.memoryPercent.toFixed(1) + "% · " + humanBytes(m.memoryUsed);
    e.metricMemoryBar.style.width = Math.min(100, m.memoryPercent) + "%";
    e.metricRX.textContent = humanBytes(m.networkRxPerSec) + "/s";
    e.metricTX.textContent = humanBytes(m.networkTxPerSec) + "/s";
    e.metricLoad.textContent = Number(m.load1).toFixed(2);
    e.metricUptime.textContent =
      Math.floor(m.uptimeSeconds / 86400) +
      (settings.language === "zh-CN" ? "天 " : "d ") +
      Math.floor((m.uptimeSeconds % 86400) / 3600) +
      (settings.language === "zh-CN" ? "小时" : "h");
  }
  function renderLatency(group) {
    e.metricLatency.textContent = Number.isFinite(group.latency)
      ? group.latency + " ms"
      : "—";
  }
  function resetMetricValues() {
    [
      e.metricCPU,
      e.metricMemory,
      e.metricRX,
      e.metricTX,
      e.metricLoad,
      e.metricUptime,
    ].forEach((n) => (n.textContent = "—"));
    e.metricCPUBar.style.width = e.metricMemoryBar.style.width = "0";
  }
  function resetMetrics() {
    e.metricTarget.textContent = "—";
    e.metricLatency.textContent = e.paneCount.textContent = "—";
    resetMetricValues();
  }
  let errorTimer = null;
  function hideError() {
    clearTimeout(errorTimer);
    errorTimer = null;
    e.errorText.textContent = "";
    e.error.classList.add("hidden");
  }
  function scheduleErrorDismiss() {
    clearTimeout(errorTimer);
    errorTimer = setTimeout(hideError, 8000);
  }
  function showError(message) {
    clearTimeout(errorTimer);
    errorTimer = null;
    e.errorText.textContent = message || "";
    e.error.classList.toggle("hidden", !message);
    if (message) scheduleErrorDismiss();
  }
  function initResizer() {
    let dragging = false,
      startX = 0,
      startWidth = 0,
      raf = 0;
    e.resizer.addEventListener("pointerdown", (ev) => {
      if (innerWidth <= 620) return;
      dragging = true;
      startX = ev.clientX;
      startWidth = e.panel.getBoundingClientRect().width;
      e.resizer.setPointerCapture(ev.pointerId);
      document.body.classList.add("resizing-panel");
    });
    e.resizer.addEventListener("pointermove", (ev) => {
      if (!dragging) return;
      const max = Math.max(260, innerWidth * 0.4),
        width = Math.min(max, Math.max(180, startWidth + ev.clientX - startX));
      settings.panelWidth = Math.round(width);
      e.panel.style.width = e.panel.style.flexBasis =
        settings.panelWidth + "px";
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(fitAll);
    });
    const stop = () => {
      if (!dragging) return;
      dragging = false;
      document.body.classList.remove("resizing-panel");
      saveSettings();
      fitAll();
    };
    e.resizer.addEventListener("pointerup", stop);
    e.resizer.addEventListener("pointercancel", stop);
    e.resizer.addEventListener("dblclick", () => {
      settings.panelWidth = 210;
      saveSettings();
      applySettings();
    });
  }

  e.loginForm.addEventListener("submit", async (ev) => {
    ev.preventDefault();
    e.loginError.textContent = "";
    e.loginBtn.disabled = true;
    const old = e.loginBtn.textContent;
    e.loginBtn.textContent = t("signingIn");
    try {
      const { res } = await api("/api/login", {
        method: "POST",
        body: JSON.stringify({
          username: e.loginUser.value,
          password: e.loginPass.value,
          remember: e.loginRemember.checked,
        }),
      });
      if (!res.ok) {
        e.loginError.textContent = t("invalidCredentials");
        return;
      }
      e.loginPass.value = "";
      e.authUser.textContent = e.loginUser.value;
      showPage("home");
      await refreshCredentials();
    } catch (_) {
      e.loginError.textContent = t("loginFailed");
    } finally {
      e.loginBtn.disabled = false;
      e.loginBtn.textContent = old;
    }
  });
  e.logout.addEventListener("click", async () => {
    if (groups.size && !confirm(t("confirmLogout"))) return;
    [...groups.keys()].forEach(removeGroup);
    await api("/api/logout", { method: "POST", body: "{}" });
    e.userMenu.classList.add("hidden");
    e.userMenuButton.setAttribute("aria-expanded", "false");
    showPage("login");
  });
  e.userMenuButton.addEventListener("click", (ev) => {
    ev.stopPropagation();
    const open = e.userMenu.classList.toggle("hidden") === false;
    e.userMenuButton.setAttribute("aria-expanded", String(open));
  });
  document.addEventListener("click", (ev) => {
    if (!ev.target.closest(".user-menu-wrap")) {
      e.userMenu.classList.add("hidden");
      e.userMenuButton.setAttribute("aria-expanded", "false");
    }
  });
  e.homeTab.addEventListener("click", () => showPage("home"));
  e.newConnection.addEventListener("click", () => openConnection());
  e.homeAdd.addEventListener("click", () => openConnection());
  e.search.addEventListener("input", renderHome);
  e.clearKey.addEventListener("click", () => {
    e.key.value = e.pass.value = e.keyFile.value = "";
  });
  e.keyFile.addEventListener("change", async () => {
    const file = e.keyFile.files?.[0];
    if (file) e.key.value = await file.text();
  });
  e.save.addEventListener("click", () => connectionAction("save"));
  e.connectOnly.addEventListener("click", () => connectionAction("connect"));
  e.saveConnect.addEventListener("click", () =>
    connectionAction("save-connect"),
  );
  e.form.addEventListener("submit", (ev) => {
    ev.preventDefault();
    connectionAction("connect");
  });
  e.settingsBtn.addEventListener("click", openSettings);
  e.settingsConfirm.addEventListener("click", confirmSettings);
  document
    .querySelectorAll("[data-close-modal]")
    .forEach((n) =>
      n.addEventListener("click", () =>
        n.dataset.closeModal === "settings"
          ? cancelSettings()
          : closeModal(
              n.dataset.closeModal === "connection"
                ? e.connectionModal
                : e.pasteModal,
            ),
      ),
    );
  document.addEventListener("keydown", (ev) => {
    if (ev.key === "Escape") {
      closeModal(e.connectionModal);
      if (!e.settingsModal.classList.contains("hidden")) cancelSettings();
      closeModal(e.pasteModal);
      hideContextMenu();
    }
  });
  window.addEventListener(
    "keydown",
    (ev) => {
      if (
        currentPage !== "terminal" ||
        !e.settingsModal.classList.contains("hidden") ||
        !e.pasteModal.classList.contains("hidden")
      )
        return;
      const s = sessions.get(activeId);
      if (!s) return;
      const mac = navigator.platform.includes("Mac");
      const copyKey = mac
        ? ev.metaKey && ev.code === "KeyC"
        : ev.ctrlKey && ev.shiftKey && ev.code === "KeyC";
      const pasteKey = mac
        ? ev.metaKey && ev.code === "KeyV"
        : ev.ctrlKey && ev.shiftKey && ev.code === "KeyV";
      if (!copyKey && !pasteKey && !(ev.shiftKey && ev.code === "Insert"))
        return;
      ev.preventDefault();
      ev.stopImmediatePropagation();
      if (copyKey) copySelection(s);
      else pasteFromClipboard(s);
    },
    true,
  );
  [e.langLogin, e.langWorkspace].forEach((n) =>
    n.addEventListener("change", () => {
      settings.language = n.value;
      saveSettings();
      applyLanguage();
      applyMode();
    }),
  );
  [e.modeLogin, e.modeWorkspace].forEach((n) =>
    n.addEventListener("click", toggleMode),
  );
  e.errorClose.addEventListener("click", hideError);
  e.error.addEventListener("mouseenter", () => {
    clearTimeout(errorTimer);
    errorTimer = null;
  });
  e.error.addEventListener("mouseleave", () => {
    if (!e.error.classList.contains("hidden")) scheduleErrorDismiss();
  });
  e.ui.addEventListener("change", () => {
    readSettingsPreview();
  });
  function settingsChanged() {
    readSettingsPreview();
  }
  function normalizeFontStack(value) {
    const generic = new Set(["monospace", "serif", "sans-serif", "system-ui"]);
    const fonts = value
      .split(",")
      .map((name) => name.trim().replace(/^['\"]|['\"]$/g, ""))
      .filter(Boolean)
      .map((name) =>
        generic.has(name.toLowerCase()) ? name : `"${name.replace(/"/g, "")}"`,
      );
    if (!fonts.some((name) => generic.has(name.toLowerCase())))
      fonts.push("monospace");
    return fonts.join(", ");
  }
  e.font.addEventListener("change", () => {
    e.customFontRow.classList.toggle("hidden", e.font.value !== "custom");
    if (e.font.value === "custom") e.customFont.focus();
    else settingsChanged();
  });
  e.customFont.addEventListener("input", settingsChanged);
  [
    e.theme,
    e.fontSize,
    e.lineHeight,
    e.cursorBlink,
    e.confirmPasteSetting,
    e.autoCopy,
  ].forEach((n) => n.addEventListener("change", settingsChanged));
  e.sideDisconnect.addEventListener("click", () =>
    disconnectSession(sessions.get(activeId)),
  );
  e.sideReconnect.addEventListener("click", () =>
    reconnectSession(sessions.get(activeId)),
  );
  e.splitRight.addEventListener("click", () => splitActive("right"));
  e.splitDown.addEventListener("click", () => splitActive("down"));
  e.closePane.addEventListener("click", () => {
    const group = groups.get(activeGroupId);
    if (group?.panes.length > 1) removeSession(activeId);
  });
  e.contextMenu
    .querySelectorAll("[data-context-action]")
    .forEach((n) =>
      n.addEventListener("click", () => contextAction(n.dataset.contextAction)),
    );
  e.confirmPaste.addEventListener("click", () => {
    const s = sessions.get(activeId),
      text = pendingPaste;
    pendingPaste = "";
    closeModal(e.pasteModal);
    if (s && text) s.term.paste(text);
  });
  document.addEventListener("pointerdown", (ev) => {
    if (!ev.target.closest("#terminal-context-menu")) hideContextMenu();
  });
  window.addEventListener("resize", fitAll);
  initResizer();
  async function bootstrap() {
    const [{ data: session }, { data: config }] = await Promise.all([
      api("/api/session"),
      api("/api/config/ui"),
    ]);
    uiConfig = config || {};
    populateThemes();
    applySettings();
    applyLanguage();
    if (uiConfig.hostKeyPolicy === "insecure-ignore")
      e.hostWarning.classList.remove("hidden");
    if (session?.authenticated) {
      e.authUser.textContent = session.username;
      showPage("home");
      await refreshCredentials();
    } else showPage("login");
  }
  bootstrap().catch((err) => {
    console.error(err);
    showPage("login");
    e.loginError.textContent = "Failed to initialize UI";
  });
})();
