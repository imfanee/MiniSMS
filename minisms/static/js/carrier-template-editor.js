/* Architected and Developed by :- Faisal Hanif | imfanee@gmail.com. */
(function () {
  var VAR_RE = /\{\{[a-z0-9_]+\}\}/g;
  var VAR_TOKEN_RE = /\{\{[a-z0-9_]+\}\}/;

  var GUIDELINES = {
    "application/json": {
      title: "JSON template",
      lines: [
        "Use valid JSON. Variables go inside quoted strings.",
        "Example body: {\"to\":\"{{to}}\",\"text\":\"{{message}}\"}",
        "Query (if used): same JSON rules when Content-Type is JSON.",
      ],
    },
    "text/xml": {
      title: "XML template",
      lines: [
        "Use well-formed XML. Variables in element text or attributes.",
        "Example: <submit><to>{{to}}</to><text>{{message}}</text></submit>",
      ],
    },
    "application/xml": {
      title: "XML template",
      lines: [
        "Use well-formed XML. Variables in element text or attributes.",
        "Example: <submit><to>{{to}}</to><text>{{message}}</text></submit>",
      ],
    },
    "application/x-www-form-urlencoded": {
      title: "Form / query string template",
      lines: [
        "Body: use the key/value table below.",
        "Query: use key=value pairs joined with & (e.g. to={{to}}&text={{message}}).",
      ],
    },
  };

  function replaceVars(s) {
    if (window.minismsReplaceTemplateVars) return window.minismsReplaceTemplateVars(s || "");
    return s || "";
  }

  function substVarsForValidate(s) {
    if (window.minismsReplaceTemplateVars) return window.minismsReplaceTemplateVars(s || "");
    return (s || "").replace(VAR_RE, '"0"');
  }

  function isFormType(ct) {
    return ct === "application/x-www-form-urlencoded";
  }

  function baseModeSpec(ct) {
    switch (ct) {
      case "application/json":
        return { name: "application/json" };
      case "text/xml":
      case "application/xml":
        return { name: "xml", htmlMode: true };
      case "application/x-www-form-urlencoded":
        return { name: "minisms/urlquery" };
      default:
        return { name: "application/json" };
    }
  }

  function defineOverlayMode() {
    if (window._minismsOverlayDefined) return;
    window._minismsOverlayDefined = true;
    CodeMirror.defineMode("overlay-template-vars", function () {
      return {
        token: function (stream) {
          if (stream.match(VAR_TOKEN_RE)) return "template-variable";
          stream.next();
          return null;
        },
      };
    });
    CodeMirror.defineMode("minisms/urlquery", function () {
      return {
        token: function (stream) {
          if (stream.match(VAR_TOKEN_RE)) return "template-variable";
          if (stream.match(/^[^&=\s]+(?==)/)) return "attribute";
          if (stream.match(/[&]/)) return "operator";
          if (stream.match(/=/)) return "operator";
          if (stream.sol() || stream.string.charAt(stream.pos - 1) === "&") {
            stream.eatWhile(/[^=&\s]/);
            return "string";
          }
          stream.next();
          return null;
        },
      };
    });
  }

  var queryTemplateCT = "application/x-www-form-urlencoded";

  function validateQueryText(text) {
    return validateText(queryTemplateCT, text);
  }

  function validateText(ct, text) {
    var errors = [];
    text = text || "";
    if (!text.trim()) return errors;

    if (ct === "application/json") {
      try {
        JSON.parse(substVarsForValidate(text));
      } catch (e) {
        var m = /position (\d+)/i.exec(e.message);
        var pos = m ? parseInt(m[1], 10) : 0;
        var line = 0;
        var ch = pos;
        var lines = text.split("\n");
        var acc = 0;
        for (var i = 0; i < lines.length; i++) {
          if (acc + lines[i].length >= pos) {
            line = i;
            ch = pos - acc;
            break;
          }
          acc += lines[i].length + 1;
        }
        errors.push({
          line: line,
          ch: Math.max(0, ch),
          len: 1,
          message:
            (e.message || "Invalid JSON") +
            " — put each {{variable}} inside double quotes, e.g. \"to\":\"{{to}}\".",
        });
      }
      return errors;
    }

    if (ct === "text/xml" || ct === "application/xml") {
      var parser = new DOMParser();
      var doc = parser.parseFromString(substVarsForValidate(text), "application/xml");
      if (doc.getElementsByTagName("parsererror").length) {
        errors.push({ line: 0, ch: 0, len: 1, message: "Invalid XML structure" });
      }
      return errors;
    }

    if (ct === "application/x-www-form-urlencoded") {
      var offset = 0;
      text.split("&").forEach(function (part, idx, arr) {
        if (!part && idx === arr.length - 1) return;
        if (!part) {
          errors.push({ line: 0, ch: offset, len: 1, message: "Empty segment (double &&)" });
        } else if (!/^[^=]+=/.test(part)) {
          errors.push({ line: 0, ch: offset, len: part.length, message: "Expected key=value" });
        }
        offset += part.length + 1;
      });
      return errors;
    }

    return errors;
  }

  function makeLintFn(ct) {
    return function (text) {
      return validateText(ct, text || "").map(function (e) {
        return {
          from: CodeMirror.Pos(e.line, e.ch),
          to: CodeMirror.Pos(e.line, e.ch + (e.len || 1)),
          message: e.message,
          severity: "error",
        };
      });
    };
  }

  function makeQueryLintFn() {
    return function (text) {
      return validateQueryText(text || "").map(function (e) {
        return {
          from: CodeMirror.Pos(e.line, e.ch),
          to: CodeMirror.Pos(e.line, e.ch + (e.len || 1)),
          message: e.message,
          severity: "error",
        };
      });
    };
  }

  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  function escapeAttr(s) {
    return escapeHtml(s).replace(/'/g, "&#39;");
  }

  function destroyEditor(editor, ta) {
    if (editor) {
      try {
        editor.toTextArea();
      } catch (e) { /* ignore */ }
    }
    if (ta) {
      delete ta.dataset.cmInit;
      var wrap = ta.closest(".minisms-cm-wrap");
      if (wrap) wrap.classList.remove("cm-ready");
    }
  }

  function hiddenField(panel, role) {
    return panel.querySelector(role === "query" ? "#q_h" : "#body_h");
  }

  function syncRoleToHidden(panel, role, value) {
    var h = hiddenField(panel, role);
    var ta = panel.querySelector(role === "query" ? "#q_t" : "#body_t");
    value = value == null ? "" : String(value);
    if (h) h.value = value;
    if (ta) ta.value = value;
  }

  function createEditor(ta, panel, role, ct, height) {
    if (!ta || !window.CodeMirror) return null;
    defineOverlayMode();

    var wrap = ta.closest(".minisms-cm-wrap");
    var h = hiddenField(panel, role);
    var isQuery = role === "query";
    var editor = CodeMirror.fromTextArea(ta, {
      readOnly: false,
      lineNumbers: true,
      lineWrapping: true,
      indentUnit: 2,
      tabSize: 2,
      theme: "dracula",
      mode: isQuery ? baseModeSpec(queryTemplateCT) : baseModeSpec(ct),
      gutters: ["CodeMirror-lint-markers", "CodeMirror-linenumbers"],
      lint: isQuery ? makeQueryLintFn() : makeLintFn(ct),
      extraKeys: {
        Tab: function (cm) {
          cm.replaceSelection("  ");
        },
      },
    });

    editor.addOverlay("overlay-template-vars");
    editor.setSize("100%", height);
    if (wrap) wrap.classList.add("cm-ready");

    editor.on("focus", function () {
      panel._focusedEditor = role;
    });
    editor.on("cursorActivity", function () {
      panel._focusedEditor = role;
    });
    editor.on("change", function () {
      syncRoleToHidden(panel, role, editor.getValue());
      editor.save();
      refreshPreview(panel);
    });

    ta.dataset.cmInit = "1";
    return editor;
  }

  function getFocusedRole(panel) {
    var el = document.activeElement;
    if (el) {
      if (el.closest && el.closest(".minisms-query-cm")) return "query";
      if (el.closest && el.closest(".minisms-body-cm")) return "body";
      if (el.classList && el.classList.contains("CodeMirror-scroll")) {
        var wrap = el.closest(".minisms-query-cm, .minisms-body-cm");
        if (wrap && wrap.classList.contains("minisms-query-cm")) return "query";
        if (wrap && wrap.classList.contains("minisms-body-cm")) return "body";
      }
    }
    var method = (panel.getAttribute("data-http-method") || "POST").toUpperCase();
    if (method === "POST") return "body";
    return panel._focusedEditor || "query";
  }

  function renderGuidelines(panel, ct) {
    var box = panel.querySelector("#body-guidelines");
    var qbox = panel.querySelector("#query-guidelines");
    var g = GUIDELINES[ct] || GUIDELINES["application/json"];
    var html =
      "<strong>" + escapeHtml(g.title) + "</strong><ul class=\"mb-0 ps-3\">" +
      g.lines.map(function (line) { return "<li>" + escapeHtml(line) + "</li>"; }).join("") +
      "</ul>";
    if (box) box.innerHTML = html;
    if (qbox) {
      qbox.innerHTML =
        "<strong>Query template (URL query string)</strong> — use <code>key=value&amp;…</code> " +
        "(e.g. <code>to={{to}}&amp;from={{from}}</code>). Independent of body Content-Type.";
    }
  }

  function updateMethodNotice(panel) {
    var method = (panel.getAttribute("data-http-method") || "POST").toUpperCase();
    var isPost = method === "POST";
    var isGet = method === "GET";
    var getNotice = panel.querySelector("#get-method-notice");
    var postQueryNotice = panel.querySelector("#post-method-query-notice");
    var bodyWrap = panel.querySelector("#body-editor-wrap");
    var queryWrap = panel.querySelector("#query-editor-wrap");
    var queryPreviewWrap = panel.querySelector("#query-preview-wrap");
    if (getNotice) getNotice.classList.toggle("d-none", !isGet);
    if (postQueryNotice) postQueryNotice.classList.toggle("d-none", !isPost);
    if (bodyWrap) bodyWrap.classList.toggle("opacity-75", isGet);
    if (queryWrap) queryWrap.classList.toggle("d-none", isPost);
    if (queryPreviewWrap) queryPreviewWrap.classList.toggle("d-none", isPost);
    if (isPost && panel._focusedEditor === "query") {
      panel._focusedEditor = "body";
    }
  }

  function refreshPreview(panel) {
    var bodyH = panel.querySelector("#body_h");
    var queryH = panel.querySelector("#q_h");
    if (isFormType(panel.querySelector("#ctsel")?.value || "")) {
      syncFormToTextarea(panel);
    }
    syncEditorsToForm(panel);
    var bp = panel.querySelector("#body-preview");
    var qp = panel.querySelector("#query-preview");
    if (bp && bodyH) bp.textContent = replaceVars(bodyH.value);
    if (qp && queryH) qp.textContent = replaceVars(queryH.value);
  }

  function parseFormBody(s) {
    s = (s || "").trim();
    if (!s) return [{ key: "", value: "" }];
    return s.split("&").map(function (part) {
      var i = part.indexOf("=");
      if (i < 0) return { key: part, value: "" };
      return { key: part.slice(0, i), value: part.slice(i + 1) };
    });
  }

  function serializeFormBody(rows) {
    return rows
      .filter(function (r) { return (r.key || "").trim() !== ""; })
      .map(function (r) { return r.key.trim() + "=" + (r.value || ""); })
      .join("&");
  }

  function formRowEl(row) {
    var tr = document.createElement("tr");
    tr.innerHTML =
      "<td><input type=\"text\" class=\"form-control form-control-sm font-monospace fk\" value=\"" + escapeAttr(row.key) + "\" /></td>" +
      "<td><input type=\"text\" class=\"form-control form-control-sm font-monospace fv\" value=\"" + escapeAttr(row.value) + "\" /></td>" +
      "<td class=\"text-nowrap\"><button type=\"button\" class=\"btn btn-sm btn-outline-secondary insert-var-row\">{{}}</button> " +
      "<button type=\"button\" class=\"btn btn-sm btn-outline-danger remove-row\">×</button></td>";
    return tr;
  }

  function syncFormToTextarea(panel) {
    var tbody = panel.querySelector("#form-kv-body");
    if (!tbody) return;
    var rows = [];
    tbody.querySelectorAll("tr").forEach(function (tr) {
      rows.push({
        key: (tr.querySelector(".fk") || {}).value || "",
        value: (tr.querySelector(".fv") || {}).value || "",
      });
    });
    syncRoleToHidden(panel, "body", serializeFormBody(rows));
  }

  function initFormEditor(panel) {
    var bodyH = panel.querySelector("#body_h");
    var tbody = panel.querySelector("#form-kv-body");
    if (!tbody) return;
    tbody.innerHTML = "";
    parseFormBody(bodyH ? bodyH.value : "").forEach(function (row) {
      tbody.appendChild(formRowEl(row));
    });
    if (!tbody.querySelector("tr")) tbody.appendChild(formRowEl({ key: "", value: "" }));

    if (tbody.dataset.bound !== "1") {
      tbody.dataset.bound = "1";
      tbody.addEventListener("input", function () {
        syncFormToTextarea(panel);
        refreshPreview(panel);
      });
      tbody.addEventListener("click", function (e) {
        if (e.target.classList.contains("remove-row")) {
          var tr = e.target.closest("tr");
          if (tr) tr.remove();
          if (!tbody.querySelector("tr")) tbody.appendChild(formRowEl({ key: "", value: "" }));
          syncFormToTextarea(panel);
          refreshPreview(panel);
        }
        if (e.target.classList.contains("insert-var-row")) {
          panel._focusedEditor = "body";
          insertVar(panel);
        }
      });
    }
    panel.querySelector("#form-kv-add")?.addEventListener("click", function () {
      tbody.appendChild(formRowEl({ key: "", value: "" }));
    });
  }

  function insertAtInput(inp, text) {
    if (!inp) return;
    var start = inp.selectionStart || 0;
    var end = inp.selectionEnd || 0;
    inp.value = inp.value.slice(0, start) + text + inp.value.slice(end);
    inp.focus();
    inp.selectionStart = inp.selectionEnd = start + text.length;
  }

  function insertVar(panel) {
    var sel = panel.querySelector("#var-insert-select");
    var v = (sel && sel.value) ? sel.value : "message";
    var token = "{{" + v + "}}";
    var ct = panel.querySelector("#ctsel")?.value || "";
    var role = getFocusedRole(panel);

    if (role === "body" && isFormType(ct)) {
      var target =
        panel.querySelector("#form-kv-body .fv:focus") ||
        panel.querySelector("#form-kv-body .fk:focus") ||
        panel.querySelector("#form-kv-body tr:last-child .fv");
      insertAtInput(target, token);
      syncFormToTextarea(panel);
      refreshPreview(panel);
      return;
    }

    var ed = role === "query" ? panel._minismsQueryEditor : panel._minismsBodyEditor;
    if (ed) {
      ed.replaceSelection(token);
      syncRoleToHidden(panel, role, ed.getValue());
      ed.focus();
      ed.save();
      refreshPreview(panel);
      return;
    }

    var ta = role === "query" ? panel.querySelector("#q_t") : panel.querySelector("#body_t");
    insertAtInput(ta, token);
    syncRoleToHidden(panel, role, ta.value);
    refreshPreview(panel);
  }

  function refreshQueryEditor(panel) {
    var qta = panel.querySelector("#q_t");
    var qh = panel.querySelector("#q_h");
    if (!qta) return;

    var value = qh ? qh.value : qta.value;
    if (panel._minismsQueryEditor) {
      value = panel._minismsQueryEditor.getValue();
      syncRoleToHidden(panel, "query", value);
      destroyEditor(panel._minismsQueryEditor, qta);
      panel._minismsQueryEditor = null;
    }
    syncRoleToHidden(panel, "query", value);
    panel._minismsQueryEditor = createEditor(qta, panel, "query", queryTemplateCT, "180px");
    if (panel._minismsQueryEditor) {
      panel._minismsQueryEditor.refresh();
      panel._minismsQueryEditor.focus();
      panel._focusedEditor = "query";
    }
  }

  function switchBodyMode(panel) {
    var sel = panel.querySelector("#ctsel");
    var bodyTa = panel.querySelector("#body_t");
    var bodyH = panel.querySelector("#body_h");
    if (!sel || !bodyTa) return;

    var ct = sel.value;
    renderGuidelines(panel, ct);
    if (bodyH && bodyH.value && bodyTa.value !== bodyH.value) {
      bodyTa.value = bodyH.value;
    }

    if (panel._minismsBodyEditor) {
      syncRoleToHidden(panel, "body", panel._minismsBodyEditor.getValue());
      destroyEditor(panel._minismsBodyEditor, bodyTa);
      panel._minismsBodyEditor = null;
    }

    var cmWrap = panel.querySelector("#body-editor-cm");
    var formWrap = panel.querySelector("#body-editor-form");

    if (isFormType(ct)) {
      if (cmWrap) cmWrap.classList.add("d-none");
      if (formWrap) formWrap.classList.remove("d-none");
      initFormEditor(panel);
    } else {
      if (formWrap) {
        syncFormToTextarea(panel);
        formWrap.classList.add("d-none");
      }
      if (cmWrap) cmWrap.classList.remove("d-none");
      panel._minismsBodyEditor = createEditor(bodyTa, panel, "body", ct, "280px");
    }
    refreshPreview(panel);
  }

  function syncEditorsToForm(panel) {
    if (!panel) return;
    var sel = panel.querySelector("#ctsel");
    if (isFormType(sel && sel.value ? sel.value : "")) {
      syncFormToTextarea(panel);
      return;
    }
    if (panel._minismsBodyEditor) {
      syncRoleToHidden(panel, "body", panel._minismsBodyEditor.getValue());
      panel._minismsBodyEditor.save();
    }
    if (panel._minismsQueryEditor) {
      syncRoleToHidden(panel, "query", panel._minismsQueryEditor.getValue());
      panel._minismsQueryEditor.save();
    }
  }

  function decodeTemplateB64(b64) {
    if (!b64) return "";
    try {
      if (window.utf8FromB64) return window.utf8FromB64(b64);
      return atob(b64);
    } catch (e) {
      return "";
    }
  }

  function readStoredTemplate(panel, role) {
    var attr = role === "query" ? "data-query-b64" : "data-body-b64";
    var b64 = panel.getAttribute(attr);
    if (b64) {
      var decoded = decodeTemplateB64(b64);
      if (decoded) return decoded;
    }
    var pre = panel.querySelector(role === "query" ? "#query-data" : "#body-data");
    return pre ? pre.textContent || "" : "";
  }

  function hydrateTemplateFields(panel) {
    syncRoleToHidden(panel, "body", readStoredTemplate(panel, "body"));
    syncRoleToHidden(panel, "query", readStoredTemplate(panel, "query"));
  }

  function applyHydratedToEditors(panel) {
    if (!panel) return;
    var bodyVal = readStoredTemplate(panel, "body");
    var queryVal = readStoredTemplate(panel, "query");
    syncRoleToHidden(panel, "body", bodyVal);
    syncRoleToHidden(panel, "query", queryVal);
    if (panel._minismsBodyEditor) {
      panel._minismsBodyEditor.setValue(bodyVal);
      syncRoleToHidden(panel, "body", panel._minismsBodyEditor.getValue());
    }
    if (panel._minismsQueryEditor) {
      panel._minismsQueryEditor.setValue(queryVal);
      syncRoleToHidden(panel, "query", panel._minismsQueryEditor.getValue());
    }
    refreshPreview(panel);
  }

  function bindPanel(panel) {
    if (!panel) return;
    if (panel.dataset.editorInit === "1") {
      applyHydratedToEditors(panel);
      return;
    }
    panel.dataset.editorInit = "1";
    panel._focusedEditor = "query";

    hydrateTemplateFields(panel);
    updateMethodNotice(panel);

    panel.querySelector(".minisms-query-cm")?.addEventListener("mousedown", function () {
      panel._focusedEditor = "query";
    });
    panel.querySelector(".minisms-body-cm")?.addEventListener("mousedown", function () {
      panel._focusedEditor = "body";
    });

    var sel = panel.querySelector("#ctsel");
    if (sel) {
      sel.addEventListener("change", function () {
        switchBodyMode(panel);
        refreshQueryEditor(panel);
      });
    }

    panel.querySelector("#var-insert-btn")?.addEventListener("click", function (e) {
      e.preventDefault();
      insertVar(panel);
    });

    var form = panel.querySelector("form");
    if (form) {
      form.addEventListener("htmx:beforeRequest", function () {
        syncEditorsToForm(panel);
      });
      form.addEventListener("submit", function () {
        syncEditorsToForm(panel);
      });
    }

    loadCodeMirror(function () {
      hydrateTemplateFields(panel);
      updateMethodNotice(panel);
      switchBodyMode(panel);
      if ((panel.getAttribute("data-http-method") || "POST").toUpperCase() !== "POST") {
        refreshQueryEditor(panel);
      }
      applyHydratedToEditors(panel);
    });
  }

  function loadCodeMirror(cb) {
    if (window.CodeMirror && window.CodeMirror.lint) {
      cb();
      return;
    }
    if (!window._minismsCMLoading) window._minismsCMLoading = [];
    window._minismsCMLoading.push(cb);
    if (window._minismsCMLoading.length > 1) return;

    var assets = [
      { tag: "link", href: "https://cdnjs.cloudflare.com/ajax/libs/codemirror/5.65.18/codemirror.min.css" },
      { tag: "link", href: "https://cdnjs.cloudflare.com/ajax/libs/codemirror/5.65.18/theme/dracula.min.css" },
      { tag: "link", href: "https://cdnjs.cloudflare.com/ajax/libs/codemirror/5.65.18/addon/lint/lint.min.css" },
      { tag: "script", href: "https://cdnjs.cloudflare.com/ajax/libs/codemirror/5.65.18/codemirror.min.js" },
      // application/json mode is bundled in javascript.min.js (no separate file on cdnjs)
      { tag: "script", href: "https://cdnjs.cloudflare.com/ajax/libs/codemirror/5.65.18/mode/javascript/javascript.min.js" },
      { tag: "script", href: "https://cdnjs.cloudflare.com/ajax/libs/codemirror/5.65.18/mode/xml/xml.min.js" },
      { tag: "script", href: "https://cdnjs.cloudflare.com/ajax/libs/codemirror/5.65.18/addon/lint/lint.min.js" },
    ];

    var i = 0;
    function next() {
      if (i >= assets.length) {
        var cbs = window._minismsCMLoading || [];
        window._minismsCMLoading = null;
        cbs.forEach(function (fn) { fn(); });
        return;
      }
      var a = assets[i++];
      if (a.tag === "link") {
        if (!document.querySelector("link[href=\"" + a.href + "\"]")) {
          var l = document.createElement("link");
          l.rel = "stylesheet";
          l.href = a.href;
          document.head.appendChild(l);
        }
        next();
        return;
      }
      if (document.querySelector("script[src=\"" + a.href + "\"]")) {
        next();
        return;
      }
      var s = document.createElement("script");
      s.src = a.href;
      s.crossOrigin = "anonymous";
      s.onload = next;
      s.onerror = next;
      document.head.appendChild(s);
    }
    next();
  }

  window.minismsInitTemplateEditor = function (panel) {
    panel = panel || document.getElementById("template-panel");
    if (!panel) return;
    bindPanel(panel);
  };

  function scanPanels() {
    document.querySelectorAll("#template-panel").forEach(function (panel) {
      if (panel.dataset.editorInit !== "1") bindPanel(panel);
    });
  }

  function templatePanelFromEvent(evt) {
    var elt = evt.detail && evt.detail.elt;
    if (!elt) return null;
    var form = elt.tagName === "FORM" ? elt : elt.closest("form");
    if (!form || !form.closest("#template-panel")) return null;
    return document.getElementById("template-panel");
  }

  document.body.addEventListener("htmx:configRequest", function (evt) {
    var panel = templatePanelFromEvent(evt);
    if (!panel) return;
    syncEditorsToForm(panel);
  });

  document.body.addEventListener("htmx:beforeRequest", function (evt) {
    var panel = templatePanelFromEvent(evt);
    if (panel) syncEditorsToForm(panel);
  });

  document.body.addEventListener("htmx:afterSwap", function (evt) {
    var target = evt.detail && (evt.detail.target || evt.detail.elt);
    if (target && (target.id === "http-interconnect-body" || target.querySelector("#template-panel"))) {
      setTimeout(scanPanels, 0);
    } else {
      setTimeout(scanPanels, 50);
    }
  });

  document.body.addEventListener("htmx:afterSettle", function () {
    scanPanels();
  });

  document.body.addEventListener("carrierHttpMethodChanged", function () {
    var panel = document.getElementById("template-panel");
    if (!panel || !panel.dataset.carrierId || !window.htmx) return;
    window.htmx.ajax("GET", "/admin/carriers/" + panel.dataset.carrierId + "/template", {
      target: "#template-panel",
      swap: "outerHTML",
    });
  });

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", scanPanels);
  } else {
    scanPanels();
  }
})();
