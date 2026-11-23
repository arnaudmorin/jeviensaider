// Small progressive enhancements; every feature also works without JS.
(function () {
  "use strict";

  // Stand descriptions in the overview are cut after two lines: mark the cut
  // ones, so that their "Voir plus" link shows.
  function markClamped(root) {
    root.querySelectorAll(".stand-desc:not(.checked)").forEach(function (d) {
      d.classList.add("checked");
      if (d.scrollHeight > d.clientHeight + 1) d.classList.add("clamped");
    });
  }
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", function () { markClamped(document); });
  } else {
    markClamped(document);
  }
  document.addEventListener("htmx:afterSettle", function (e) { markClamped(e.detail.elt || document); });

  // Sign-up form loaded by htmx into #modal: open it as a modal dialog.
  document.addEventListener("htmx:afterSwap", function (e) {
    if (e.detail.target && e.detail.target.id === "modal") {
      var d = e.detail.target.querySelector("dialog");
      if (d && !d.open) {
        d.addEventListener("close", function () { d.remove(); });
        d.showModal();
        var first = d.querySelector("input:not([type=hidden]):not([tabindex='-1'])");
        if (first) first.focus();
      }
    }
  });

  // copyText copies a field's value and confirms on the button.
  function copyText(input, button) {
    var done = function () {
      // Keep the button's own label (it may hold an icon) across clicks.
      if (button.dataset.label === undefined) button.dataset.label = button.innerHTML;
      clearTimeout(button.copiedTimer);
      button.textContent = "Copié ✓";
      button.copiedTimer = setTimeout(function () { button.innerHTML = button.dataset.label; }, 1500);
    };
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(input.value).then(done);
    } else {
      input.select();
      try { document.execCommand("copy"); done(); } catch (_) {}
    }
  }

  document.addEventListener("click", function (e) {
    var t = e.target;
    if (!(t instanceof Element)) return;

    // Close button inside a dialog.
    var close = t.closest("[data-close]");
    if (close) {
      var d = close.closest("dialog");
      if (d) d.close();
      return;
    }
    // Click on the backdrop closes the dialog.
    if (t.tagName === "DIALOG" && t.open) {
      var r = t.getBoundingClientRect();
      if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom) t.close();
      return;
    }

    // Copy-to-clipboard buttons: data-copy="#input-id".
    var copy = t.closest("[data-copy]");
    if (copy) {
      var input = document.querySelector(copy.getAttribute("data-copy"));
      if (input) copyText(input, copy);
      return;
    }

    // Share a prepared message (phone share sheet), else copy it:
    // data-share="#textarea-id".
    var share = t.closest("[data-share]");
    if (share) {
      var src = document.querySelector(share.getAttribute("data-share"));
      if (!src) return;
      if (navigator.share) {
        navigator.share({ text: src.value }).catch(function () {});
      } else {
        copyText(src, share);
      }
      return;
    }

    if (t.closest("[data-back]")) { history.back(); return; }

    // The drag handle sits in the head of a stand: a click on it must not
    // open or close the card.
    if (t.closest(".drag-handle")) { e.preventDefault(); return; }

    // "Annuler" in a form panel: put the form back as it was and close the
    // panel. The focus goes back to the button that opened it.
    var cancel = t.closest("[data-close-panel]");
    if (cancel) {
      var form = cancel.form, panel = cancel.closest("details");
      if (form) {
        form.reset();
        var first = form.querySelector("input, select, textarea");
        if (first) first.dispatchEvent(new Event("input", { bubbles: true }));
      }
      if (panel) {
        panel.open = false;
        var summary = panel.querySelector("summary");
        if (summary) summary.focus();
      }
      return;
    }
  });

  // Confirmation for destructive forms: <form data-confirm="Sûr ?">.
  document.addEventListener("submit", function (e) {
    var f = e.target;
    var msg = f.getAttribute && f.getAttribute("data-confirm");
    if (msg && !window.confirm(msg)) e.preventDefault();
  }, true);

  // Slot generator: preview the slots the form will create, the same way
  // the server cuts them (internal/slotgen): consecutive slots of the chosen
  // length, the last one shorter if the range is not a multiple of it.
  var MAX_SLOTS = 96;
  function hm(m) { var h = Math.floor(m / 60), r = m % 60; return h + "h" + (r ? (r < 10 ? "0" : "") + r : ""); }
  function minutes(v) { var p = /^(\d{1,2}):(\d{2})/.exec(v || ""); return p ? +p[1] * 60 + +p[2] : null; }
  function previewSlots(form) {
    var box = form.querySelector(".slot-preview");
    var submit = form.querySelector("[data-preview-submit]");
    if (!box) return;
    var from = minutes(form.elements.from.value), to = minutes(form.elements.to.value);
    var stepInput = form.querySelector("input[name=step]:checked");
    var step = stepInput ? +stepInput.value : 60;
    var min = parseInt(form.elements.min.value, 10);
    box.hidden = false;
    box.textContent = "";
    box.classList.remove("error");
    var fail = function (msg) { box.classList.add("error"); box.textContent = msg; if (submit) submit.textContent = "Créer les créneaux"; };
    if (from === null || to === null) { box.hidden = true; return; }
    if (to <= from) { fail("L'heure de fin doit être après l'heure de début."); return; }
    if (!step || step > to - from) step = to - from;
    var slots = [];
    for (var t = from; t < to; t += step) slots.push([t, Math.min(t + step, to)]);
    if (slots.length > MAX_SLOTS) { fail("Trop de créneaux (" + MAX_SLOTS + " au maximum) : choisissez une durée plus longue."); return; }
    var n = slots.length;
    var title = document.createElement("strong");
    title.textContent = "Cela crée " + n + " créneau" + (n > 1 ? "x" : "") +
      (min > 0 ? ", " + n * min + " place" + (n * min > 1 ? "s" : "") + " à remplir" : "");
    var list = document.createElement("ul");
    slots.forEach(function (s) {
      var li = document.createElement("li");
      li.textContent = hm(s[0]) + " – " + hm(s[1]);
      list.appendChild(li);
    });
    box.appendChild(title);
    box.appendChild(list);
    if (submit) submit.textContent = n > 1 ? "Créer les " + n + " créneaux" : "Créer le créneau";
  }
  function initPreviews(root) {
    root.querySelectorAll("form[data-slot-preview]").forEach(previewSlots);
  }
  document.addEventListener("input", function (e) {
    var f = e.target.form;
    if (f && f.hasAttribute("data-slot-preview")) previewSlots(f);
  });
  document.addEventListener("change", function (e) {
    var f = e.target.form;
    if (f && f.hasAttribute("data-slot-preview")) previewSlots(f);
  });
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", function () { initPreviews(document); });
  } else {
    initPreviews(document);
  }

  // In the form of a stand or a moment, the box "Sans couleur" and the box
  // that fills the row or the column cannot both be ticked, and picking a
  // color means that the organizer wants one. The server reads the form the
  // same way, so this only makes the screen show what will be saved.
  function syncColorBoxes(e) {
    var t = e.target, f = t.form;
    if (!f || !f.elements.nocolor) return;
    var noColor = f.elements.nocolor;
    var fill = f.elements.fill || f.elements.fill_row;
    if (t.name === "color") noColor.checked = false;
    else if (t === noColor && noColor.checked && fill) fill.checked = false;
    else if (t === fill && fill.checked) noColor.checked = false;
  }
  document.addEventListener("input", syncColorBoxes);
  document.addEventListener("change", syncColorBoxes);

  // A link to a stand of the organiser page (#stand-<id>) opens its card.
  function openTarget() {
    var id = location.hash.slice(1);
    var el = id && document.getElementById(id);
    if (el && el.tagName === "DETAILS") el.open = true;
  }
  window.addEventListener("hashchange", openTarget);
  openTarget();

  // Organiser page: drag a stand by its handle to change the order. Pointer
  // events work the same with a mouse, a pen and a finger. The new order is
  // saved as soon as the stand is dropped; the arrows stay for the keyboard
  // and for pages without the script.
  function initStandSort(list) {
    var cards = function () { return Array.prototype.filter.call(list.children, function (c) { return c.classList.contains("stand"); }); };
    if (cards().length < 2) return;
    list.classList.add("sortable");
    list.querySelectorAll(".drag-handle").forEach(function (h) { h.hidden = false; });

    // While dragging, the card follows the pointer (a translation from its
    // place in the list) and the other cards slide out of its way.
    var card = null, before = null, lastY = 0, scrolling = 0;
    var startY = 0, startTop = 0, startScroll = 0;
    var order = function () { return cards().map(function (c) { return c.id.replace("stand-", ""); }).join(","); };
    function shiftY(c) {
      var t = getComputedStyle(c).transform;
      return t && t !== "none" ? new DOMMatrix(t).m42 : 0;
    }
    function follow() {
      // Where the card should be drawn, in the coordinates of offsetTop.
      var top = startTop + (lastY - startY) + (window.scrollY - startScroll);
      // Its new place: before the first other card whose middle is below
      // the middle of the dragged card.
      var middle = top + card.offsetHeight / 2, next = null;
      var others = cards().filter(function (c) { return c !== card; });
      others.some(function (c) {
        if (middle < c.offsetTop + c.offsetHeight / 2) { next = c; return true; }
        return false;
      });
      if (card.nextElementSibling !== next) {
        // Move it in the list, then make each card that changed place slide
        // from where it was drawn to its new place.
        var drawn = others.map(function (c) { return c.offsetTop + shiftY(c); });
        list.insertBefore(card, next);
        others.forEach(function (c, i) {
          var d = drawn[i] - c.offsetTop;
          if (!d) return;
          c.style.transition = "none";
          c.style.transform = "translateY(" + d + "px)";
          void c.offsetHeight;
          c.style.transition = "";
          c.style.transform = "";
        });
      }
      card.style.transform = "translateY(" + (top - card.offsetTop) + "px)";
    }
    function autoScroll() {
      // Near the top or the bottom of the screen, scroll the page so that
      // a stand can go far up or down, even on a phone.
      if (!card) { scrolling = 0; return; }
      var edge = 60, h = window.innerHeight, dy = 0;
      if (lastY < edge) dy = -Math.ceil((edge - lastY) / 4);
      else if (lastY > h - edge) dy = Math.ceil((lastY - (h - edge)) / 4);
      if (dy) { window.scrollBy(0, dy); follow(); }
      scrolling = requestAnimationFrame(autoScroll);
    }
    function updateArrows() {
      var all = cards();
      all.forEach(function (c, i) {
        var up = c.querySelector(".row-actions input[value=up] + button");
        var down = c.querySelector(".row-actions input[value=down] + button");
        if (up) up.disabled = i === 0;
        if (down) down.disabled = i === all.length - 1;
      });
    }
    function save() {
      var body = new URLSearchParams();
      cards().forEach(function (c) { body.append("id", c.id.replace("stand-", "")); });
      fetch(list.getAttribute("data-reorder"), { method: "POST", body: body, credentials: "same-origin" })
        .then(function (r) { if (!r.ok) throw new Error(r.status); })
        // A stand added or deleted elsewhere: show the page as it is now.
        .catch(function () { location.reload(); });
    }
    function end(e) {
      if (!card || e.pointerId !== card.pointerId) return;
      var moved = order() !== before, dropped = card;
      cancelAnimationFrame(scrolling);
      list.classList.remove("dragging");
      // The card glides into its place, then puts its shadow down.
      dropped.classList.remove("drag-active");
      dropped.style.transform = "";
      var land = function () { if (dropped !== card) dropped.classList.remove("drag-source"); };
      dropped.addEventListener("transitionend", land, { once: true });
      setTimeout(land, 300);
      card = null;
      if (moved) { updateArrows(); save(); }
    }

    list.addEventListener("pointerdown", function (e) {
      var h = e.target instanceof Element && e.target.closest(".drag-handle");
      if (!h || card || (e.pointerType === "mouse" && e.button !== 0)) return;
      e.preventDefault();
      card = h.closest(".stand");
      card.pointerId = e.pointerId;
      before = order();
      lastY = startY = e.clientY;
      startScroll = window.scrollY;
      startTop = card.offsetTop + shiftY(card);
      h.setPointerCapture(e.pointerId);
      card.classList.add("drag-source", "drag-active");
      list.classList.add("dragging");
      follow();
      scrolling = requestAnimationFrame(autoScroll);
    });
    // Moving the card in the page ends the pointer capture of its handle,
    // so the rest of the drag is followed on the whole document.
    document.addEventListener("pointermove", function (e) {
      if (!card || e.pointerId !== card.pointerId) return;
      lastY = e.clientY;
      follow();
    });
    document.addEventListener("pointerup", end);
    document.addEventListener("pointercancel", end);
  }
  document.querySelectorAll("[data-reorder]").forEach(initStandSort);

  // Select-on-focus for read-only link fields.
  document.addEventListener("focusin", function (e) {
    var t = e.target;
    if (t.tagName === "INPUT" && t.readOnly) t.select();
  });
})();
