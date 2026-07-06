(function () {
  "use strict";

  const toastTimers = new Map();

  // Setup toast when it appears
  function setupToast(toast) {
    if (!toast || toastTimers.has(toast)) return;

    const duration = parseInt(toast.dataset.tuiToastDuration || "3000");
    const progress = toast.querySelector(".toast-progress");

    // Initialize timer state
    const state = {
      timer: null,
      startTime: Date.now(),
      remaining: duration,
      paused: false,
      progressWidth: null,
    };
    toastTimers.set(toast, state);

    // Animate progress bar if present
    if (progress && duration > 0) {
      progress.style.width = "100%";
      void progress.offsetWidth;
      progress.style.transition = `width ${duration}ms linear`;
      progress.style.width = "0px";
    }

    // Auto-dismiss after duration
    if (duration > 0) {
      state.timer = setTimeout(() => dismissToast(toast), duration);
    }

    // Pause on hover
    toast.addEventListener("mouseenter", () => {
      const state = toastTimers.get(toast);
      if (!state || state.paused) return;

      // Clear the dismiss timer
      clearTimeout(state.timer);

      // Calculate remaining time
      state.remaining = state.remaining - (Date.now() - state.startTime);
      state.paused = true;

      // Pause progress animation
      if (progress) {
        state.progressWidth = getComputedStyle(progress).width;
        progress.style.transition = "none";
        progress.style.width = state.progressWidth;
      }
    });

    // Resume on mouse leave
    toast.addEventListener("mouseleave", () => {
      const state = toastTimers.get(toast);
      if (!state || !state.paused || state.remaining <= 0) return;

      // Resume timer with remaining time
      state.startTime = Date.now();
      state.paused = false;
      state.timer = setTimeout(() => dismissToast(toast), state.remaining);

      // Resume progress animation
      if (progress) {
        progress.style.width = state.progressWidth;
        void progress.offsetWidth;
        progress.style.transition = `width ${state.remaining}ms linear`;
        progress.style.width = "0px";
      }
    });
  }

  // Dismiss toast with fade out
  function dismissToast(toast) {
    // Clean up timer state, cancelling a pending auto-dismiss so a manual
    // dismiss doesn't fire the timer again on the detached node.
    const state = toastTimers.get(toast);
    if (state) clearTimeout(state.timer);
    toastTimers.delete(toast);

    // Add transition for smooth fade out
    toast.style.transition = "opacity 300ms, transform 300ms";
    toast.style.opacity = "0";
    toast.style.transform = "translateY(1rem)";

    // Remove after animation
    setTimeout(() => toast.remove(), 300);
  }

  // Handle dismiss button clicks
  document.addEventListener("click", (e) => {
    const dismissBtn = e.target.closest("[data-tui-toast-dismiss]");
    if (dismissBtn) {
      const toast = dismissBtn.closest("[data-tui-toast]");
      if (toast) dismissToast(toast);
    }
  });

  function initializeToasts(root) {
    if (!root) return;

    if (root.matches?.("[data-tui-toast]")) {
      setupToast(root);
    }

    root.querySelectorAll?.("[data-tui-toast]").forEach(setupToast);
  }

  // Cancel a toast's pending auto-dismiss and drop its Map entry. Used when a
  // toast leaves the DOM by some path other than dismissToast — e.g. an htmx
  // swap that replaces a region containing it — so the timer doesn't fire on a
  // detached node and the Map doesn't retain it.
  function cleanupToast(toast) {
    const state = toastTimers.get(toast);
    if (!state) return;
    clearTimeout(state.timer);
    toastTimers.delete(toast);
  }

  function cleanupToasts(root) {
    if (!root || root.nodeType !== 1) return;
    if (root.matches?.("[data-tui-toast]")) {
      cleanupToast(root);
    }
    root.querySelectorAll?.("[data-tui-toast]").forEach(cleanupToast);
  }

  // Initialize pre-rendered toasts
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", () =>
      initializeToasts(document),
    );
  } else {
    initializeToasts(document);
  }

  // Watch for new toasts
  new MutationObserver((mutations) => {
    mutations.forEach((m) => {
      m.addedNodes.forEach((node) => {
        if (node.nodeType === 1) {
          initializeToasts(node);
        }
      });
      m.removedNodes.forEach((node) => {
        if (node.nodeType === 1) {
          cleanupToasts(node);
        }
      });
    });
  }).observe(document.body, { childList: true, subtree: true });
})();
