(function () {
  'use strict';

  // ---- helpers ------------------------------------------------------------
  function $(sel) { return document.querySelector(sel); }
  function toQS(obj) {
    const parts = [];
    Object.keys(obj || {}).forEach(k => {
      const v = obj[k];
      if (v !== undefined && v !== null && v !== '') {
        parts.push(encodeURIComponent(k) + '=' + encodeURIComponent(String(v)));
      }
    });
    return parts.length ? '?' + parts.join('&') : '';
  }
  function getCurrentScript() {
    return document.currentScript ||
           (function () {
             const scripts = document.getElementsByTagName('script');
             return scripts[scripts.length - 1];
           })();
  }
  function parseBool(v, def = false) {
    if (v == null) return def;
    if (typeof v === 'boolean') return v;
    const s = String(v).toLowerCase().trim();
    return s === '1' || s === 'true' || s === 'yes';
  }
  function parsePosition(v) {
    if (!v) return { bottom: '70px', right: '20px' };
    try {
      if (v.trim().startsWith('{')) return JSON.parse(v);
    } catch {}
    const p = String(v).toLowerCase();
    const map = {
      'bottom-right': { bottom: '70px', right: '20px' },
      'bottom-left':  { bottom: '70px', left: '20px' },
      'top-right':    { top: '20px', right: '20px' },
      'top-left':     { top: '20px', left: '20px' }
    };
    return map[p] || { bottom: '70px', right: '20px' };
  }

  // ✅ NEW: Helper to create gradient from single color
  function hexToRgb(hex) {
    const result = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex || '');
    return result ? {
      r: parseInt(result[1], 16),
      g: parseInt(result[2], 16),
      b: parseInt(result[3], 16)
    } : { r: 102, g: 126, b: 234 };
  }

  function createGradient(color) {
    const { r, g, b } = hexToRgb(color);
    // Create lighter version (20% lighter)
    const r2 = Math.min(255, Math.round(r + (255 - r) * 0.2));
    const g2 = Math.min(255, Math.round(g + (255 - g) * 0.2));
    const b2 = Math.min(255, Math.round(b + (255 - b) * 0.2));
    // Create darker version (15% darker)
    const r3 = Math.round(r * 0.85);
    const g3 = Math.round(g * 0.85);
    const b3 = Math.round(b * 0.85);
    
    return `linear-gradient(135deg, rgb(${r2}, ${g2}, ${b2}) 0%, ${color} 50%, rgb(${r3}, ${g3}, ${b3}) 100%)`;
  }

  // ---- read attributes ----------------------------------------------------
  const script = getCurrentScript();
  if (!script) return;

  const ATTR = (name, fallback = null) => script.getAttribute(name) ?? fallback;

  // Only require client-id, all other attributes have sensible defaults
  const clientId    = ATTR('data-chatbot-client-id') || ATTR('data-client-id');
  const baseUrlAttr = ATTR('data-base-url');
  const secret      = ATTR('data-secret');
  const theme       = ATTR('data-theme', 'light');
  const themeColor  = ATTR('data-theme-color', '#3B82F6');
  const position    = parsePosition(ATTR('data-position', 'bottom-right'));
  const widthAttr   = ATTR('data-width', '360px');
  const heightAttr  = ATTR('data-height', '520px');
  const autoOpen    = parseBool(ATTR('data-auto-open', 'false'));
  const embedMode   = ATTR('data-embed-mode', 'widget'); // 'widget' or 'fullscreen'
  const greetDelay  = parseInt(ATTR('data-greeting-delay', '300'), 10);
  const showPowered = parseBool(ATTR('data-show-powered-by', 'true'));

  const version     = Date.now();

  const showLogo      = parseBool(ATTR('data-show-logo', 'true'));
  const showWelcome   = parseBool(ATTR('data-show-welcome', 'true'));
  const showQuestions = parseBool(ATTR('data-show-questions', 'true'));

  // Launcher configuration
  const launcherColor = ATTR('data-launcher-color', themeColor);
  const launcherText  = ATTR('data-launcher-text', '');
  const launcherIcon  = ATTR('data-launcher-icon', 'chat');
  const launcherImage = ATTR('data-launcher-image', '');
  const launcherVideo = ATTR('data-launcher-video', '');
  const launcherIconColor = ATTR('data-launcher-icon-color', '#FFFFFF');
  const cancelIcon    = ATTR('data-cancel-icon', 'close');
  const cancelImage   = ATTR('data-cancel-image', '');
  const cancelIconColor = ATTR('data-cancel-icon-color', '#FFFFFF');

  if (!clientId) {
    console.error('[SaaS Chatbot] data-chatbot-client-id is required.');
    return;
  }

  const scriptSrc = script.src ? new URL(script.src, window.location.href) : null;
  const APP_BASE  = baseUrlAttr || (scriptSrc ? (scriptSrc.origin) : window.location.origin);
  const APP_ORIGIN = new URL(APP_BASE).origin;

  // ✅ FULLSCREEN MODE: Check if we're in fullscreen mode
  const isFullscreenMode = embedMode === 'fullscreen';

  // ✅ Helper functions for icons
  function getIconSVG(iconType) {
    const icons = {
      'chat': '<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />',
      'message': '<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />',
      'smile': '<circle cx="12" cy="12" r="10"/><path d="M8 14s1.5 2 4 2 4-2 4-2"/><line x1="9" y1="9" x2="9.01" y2="9"/><line x1="15" y1="9" x2="15.01" y2="9"/>',
      'help': '<circle cx="12" cy="12" r="10"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><line x1="12" y1="17" x2="12.01" y2="17"/>',
      'star': '<polygon points="12,2 15.09,8.26 22,9.27 17,14.14 18.18,21.02 12,17.77 5.82,21.02 7,14.14 2,9.27 8.91,8.26"/>',
      'headphones': '<path d="M3 14v3a2 2 0 0 0 2 2h1a2 2 0 0 0 2-2v-3a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2z"/><path d="M21 14v3a2 2 0 0 1-2 2h-1a2 2 0 0 1-2-2v-3a2 2 0 0 1 2-2h1a2 2 0 0 1 2 2z"/><path d="M3 14h3v3a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-3z"/><path d="M21 14h-3v3a2 2 0 0 0 2 2h1a2 2 0 0 0 2-2v-3z"/>',
      'robot': '<rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><circle cx="12" cy="5" r="2"/><path d="M12 7v4"/><line x1="8" y1="16" x2="8" y2="16"/><line x1="16" y1="16" x2="16" y2="16"/>',
      'heart': '<path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"/>',
      'lightbulb': '<path d="M9 21c0 .55.45 1 1 1h4c.55 0 1-.45 1-1v-1H9v1z"/><path d="M12 2C8.14 2 5 5.14 5 9c0 2.38 1.19 4.47 3 5.74V17c0 .55.45 1 1 1h6c.55 0 1-.45 1-1v-2.26c1.81-1.27 3-3.36 3-5.74 0-3.86-3.14-7-7-7z"/>',
      'phone': '<path d="M22 16.92v3a2 2 0 0 1-2.18 2 19.79 19.79 0 0 1-8.63-3.07 19.5 19.5 0 0 1-6-6 19.79 19.79 0 0 1-3.07-8.67A2 2 0 0 1 4.11 2h3a2 2 0 0 1 2 1.72 12.84 12.84 0 0 0 .7 2.81 2 2 0 0 1-.45 2.11L8.09 9.91a16 16 0 0 0 6 6l1.27-1.27a2 2 0 0 1 2.11-.45 12.84 12.84 0 0 0 2.81.7A2 2 0 0 1 22 16.92z"/>'
    };
    return icons[iconType] || icons['chat'];
  }

  function getCancelIconSVG(iconType) {
    const cancelIcons = {
      'close': '<line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line>',
      'times': '<line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line>',
      'arrow-left': '<line x1="19" y1="12" x2="5" y2="12"></line><polyline points="12,19 5,12 12,5"></polyline>',
      'arrow-down': '<line x1="12" y1="5" x2="12" y2="19"></line><polyline points="19,12 12,19 5,12"></polyline>',
      'minus': '<line x1="5" y1="12" x2="19" y2="12"></line>',
      'circle-x': '<circle cx="12" cy="12" r="10"></circle><line x1="15" y1="9" x2="9" y2="15"></line><line x1="9" y1="9" x2="15" y2="15"></line>',
      'square-x': '<rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect><line x1="9" y1="9" x2="15" y2="15"></line><line x1="15" y1="9" x2="9" y2="15"></line>',
      'hand': '<path d="M18 11V6a2 2 0 0 0-2-2v0a2 2 0 0 0-2 2v0"></path><path d="M14 10V4a2 2 0 0 0-2-2v0a2 2 0 0 0-2 2v2"></path><path d="M10 10.5V6a2 2 0 0 0-2-2v0a2 2 0 0 0-2 2v8"></path><path d="M18 8a2 2 0 1 1 4 0v6a8 8 0 0 1-8 8h-2c-2.8 0-4.5-.86-5.99-2.34l-3.6-3.6a2 2 0 0 1 2.83-2.82L7 15"></path>',
      'stop': '<rect x="6" y="6" width="12" height="12" rx="2" ry="2"></rect>',
      'exit': '<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"></path><polyline points="16,17 21,12 16,7"></polyline><line x1="21" y1="12" x2="9" y2="12"></line>'
    };
    return cancelIcons[iconType] || cancelIcons['close'];
  }

  // ---- container ----------------------------------------------------------
  const container = document.createElement('div');
  container.id = 'saas-chatbot-widget';
  
  // ✅ FULLSCREEN MODE: Different styling for fullscreen vs widget
  if (isFullscreenMode) {
    // Fullscreen mode: container takes entire viewport, no launcher
    Object.assign(container.style, {
      position: 'fixed',
      zIndex: 2147483647,
      width: '100vw',
      height: '100vh',
      top: '0',
      left: '0',
      right: '0',
      bottom: '0',
      borderRadius: '0',
      margin: '0',
      boxShadow: 'none',
      overflow: 'hidden',
      fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif',
      backgroundColor: 'transparent'
    });
  } else {
    // Widget mode: original floating widget behavior
    Object.assign(container.style, {
      position: 'fixed',
      zIndex: 2147483647,
      width: widthAttr,
      height: heightAttr,
      boxShadow: 'rgba(0, 0, 0, 0.12) 0px 10px 25px',
      borderRadius: '12px',
      overflow: 'hidden',
      fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif',
      transition: 'transform 0.3s cubic-bezier(0.4, 0, 0.2, 1), opacity 0.3s cubic-bezier(0.4, 0, 0.2, 1)',
      transform: 'scale(0.96)',
      opacity: '0',
      pointerEvents: 'none',
      background: 'transparent',
      display: 'flex',
      flexDirection: 'column',
      border: 'medium',
      bottom: '90px',
      right: '20px',
      marginBottom: '10px'
    });
    Object.keys(position).forEach(k => (container.style[k] = position[k]));
  }

  function applyResponsive() {
    // ✅ FULLSCREEN MODE: No responsive adjustments needed
    if (isFullscreenMode) return;
    
    const isSmall = Math.min(window.innerWidth, window.innerHeight) < 500;
    if (isSmall) {
      container.style.width = '92vw';
      container.style.height = '70vh';
    } else {
      container.style.width = widthAttr;
      container.style.height = heightAttr;
    }
  }
  applyResponsive();
  window.addEventListener('resize', applyResponsive);

  // ---- iframe -------------------------------------------------------------
  const iframe = document.createElement('iframe');
  const qs = toQS({
    theme,
    color: themeColor || undefined,
    v: version,
    secret: secret || undefined,
    powered: showPowered ? 1 : 0,
    show_logo: showLogo ? 1 : 0,
    show_welcome: showWelcome ? 1 : 0,
    show_questions: showQuestions ? 1 : 0,
    embed_mode: isFullscreenMode ? 'fullscreen' : 'widget'
  });
  iframe.src = APP_BASE.replace(/\/+$/, '') + '/embed/chatframe/' + encodeURIComponent(clientId) + qs;
  Object.assign(iframe.style, {
    width: '100%',
    height: '100%',
    border: '0',
    borderRadius: isFullscreenMode ? '0' : '12px',
    background: 'transparent'
  });
  iframe.setAttribute('title', 'SaaS Chatbot Widget');
  iframe.setAttribute('allow', 'clipboard-write;');
  container.appendChild(iframe);

  // ---- launcher button - ONLY for widget mode ----------------------------
  let launcher = null;
  if (!isFullscreenMode) {
    launcher = document.createElement('button');
    launcher.id = 'saas-chatbot-launcher';
    launcher.setAttribute('aria-label', 'Open chat support');
    
    // ✅ Calculate dynamic gradient background
    const launcherBackground = launcherColor
      ? createGradient(launcherColor)
      : (themeColor ? createGradient(themeColor) : 'linear-gradient(135deg,#667eea 0%,#764ba2 100%)');

    Object.assign(launcher.style, {
      position: 'fixed',
      zIndex: 2147483647,
      width: '56px',
      height: '56px',
      borderRadius: '9999px',
      border: '0',
      cursor: 'pointer',
      background: launcherBackground,
      color: '#fff',
      boxShadow: '0 8px 22px rgba(0,0,0,.18)',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      transition: 'transform .18s ease, box-shadow .18s ease',
      fontSize: '24px'
    });
    
    const launcherPosition = { bottom: '20px', right: '20px' };
    Object.keys(launcherPosition).forEach(k => (launcher.style[k] = launcherPosition[k]));

    // ✅ Set launcher content based on configuration
    if (launcherVideo) {
      launcher.innerHTML = `<video width="32" height="32" style="border-radius: 50%; object-fit: cover;" autoplay loop muted><source src="${launcherVideo}" type="video/mp4"></video>`;
    } else if (launcherImage) {
      launcher.innerHTML = `<img width="32" height="32" style="border-radius: 50%; object-fit: cover;" src="${launcherImage}" alt="Launcher" />`;
    } else if (launcherText) {
      launcher.innerHTML = `<span style="font-size: 12px; font-weight: bold; color: ${launcherIconColor};">${launcherText}</span>`;
    } else {
      launcher.innerHTML = `<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="color: ${launcherIconColor};">${getIconSVG(launcherIcon)}</svg>`;
    }

    launcher.addEventListener('mouseenter', () => {
      launcher.style.transform = 'scale(1.06)';
      launcher.style.boxShadow = '0 10px 28px rgba(0,0,0,.22)';
    });
    launcher.addEventListener('mouseleave', () => {
      launcher.style.transform = 'scale(1)';
      launcher.style.boxShadow = '0 8px 22px rgba(0,0,0,.18)';
    });
  }

  let isOpen = false;
  let isExpanded = false;
  const originalWidth = widthAttr;
  const originalHeight = heightAttr;
  
  function open() {
    if (isOpen || isFullscreenMode) return; // Fullscreen is always "open"
    isOpen = true;
    container.style.pointerEvents = 'auto';
    container.style.opacity = '1';
    container.style.transform = 'scale(1)';
    
    if (launcher) {
      // ✅ Set cancel icon based on configuration
      if (cancelImage) {
        launcher.innerHTML = `<img width="24" height="24" style="object-fit: cover;" src="${cancelImage}" alt="Close" />`;
      } else {
        launcher.innerHTML = `<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="color: ${cancelIconColor};">${getCancelIconSVG(cancelIcon)}</svg>`;
      }
      launcher.setAttribute('aria-label', 'Close chat support');
    }
    
    setTimeout(() => { try { iframe.contentWindow && iframe.contentWindow.focus(); } catch (_) {} }, 250);
    postToFrame('widget-toggled', { open: true });
  }
  
  function close() {
    if (!isOpen || isFullscreenMode) return; // Can't close fullscreen
    isOpen = false;
    container.style.pointerEvents = 'none';
    container.style.opacity = '0';
    container.style.transform = 'scale(.96)';
    
    if (launcher) {
      // ✅ Restore original launcher content based on configuration
      if (launcherVideo) {
        launcher.innerHTML = `<video width="32" height="32" style="border-radius: 50%; object-fit: cover;" autoplay loop muted><source src="${launcherVideo}" type="video/mp4"></video>`;
      } else if (launcherImage) {
        launcher.innerHTML = `<img width="32" height="32" style="border-radius: 50%; object-fit: cover;" src="${launcherImage}" alt="Launcher" />`;
      } else if (launcherText) {
        launcher.innerHTML = `<span style="font-size: 12px; font-weight: bold; color: ${launcherIconColor};">${launcherText}</span>`;
      } else {
        launcher.innerHTML = `<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="color: ${launcherIconColor};">${getIconSVG(launcherIcon)}</svg>`;
      }
      launcher.setAttribute('aria-label', 'Open chat support');
    }
    
    postToFrame('widget-toggled', { open: false });
  }
  
  function toggle() { isOpen ? close() : open(); }
  
  function expand() {
    if (!isOpen || isExpanded || isFullscreenMode) return;
    isExpanded = true;
    
    const expandedWidth = Math.min(window.innerWidth * 0.8, 800);
    const expandedHeight = Math.min(window.innerHeight * 0.8, 900);
    
    container.style.transition = 'width 0.4s cubic-bezier(0.4, 0, 0.2, 1), height 0.4s cubic-bezier(0.4, 0, 0.2, 1), transform 0.4s cubic-bezier(0.4, 0, 0.2, 1)';
    container.style.transform = 'scale(1.02)';
    container.style.width = expandedWidth + 'px';
    container.style.height = expandedHeight + 'px';
    
    setTimeout(() => {
      container.style.transform = 'scale(1)';
    }, 200);
    
    postToFrame('widget-expanded', { expanded: true, width: expandedWidth, height: expandedHeight });
  }
  
  function collapse() {
    if (!isOpen || !isExpanded || isFullscreenMode) return;
    isExpanded = false;
    
    container.style.transition = 'width 0.4s cubic-bezier(0.4, 0, 0.2, 1), height 0.4s cubic-bezier(0.4, 0, 0.2, 1), transform 0.4s cubic-bezier(0.4, 0, 0.2, 1)';
    container.style.transform = 'scale(0.98)';
    container.style.width = originalWidth;
    container.style.height = originalHeight;
    
    setTimeout(() => {
      container.style.transform = 'scale(1)';
    }, 200);
    
    postToFrame('widget-expanded', { expanded: false, width: originalWidth, height: originalHeight });
  }
  
  function toggleExpand() { isExpanded ? collapse() : expand(); }

  // Only add launcher event listeners for widget mode
  if (launcher) {
    launcher.addEventListener('click', toggle);
    launcher.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggle(); }
    });
  }

  function postToFrame(action, data) {
    try {
      iframe.contentWindow.postMessage(
        { type: 'saas-chatbot-parent', action, data },
        APP_ORIGIN
      );
    } catch (_) {}
  }

  window.addEventListener('message', (ev) => {
    if (ev.origin !== APP_ORIGIN) return;
    const msg = ev.data || {};
    if (msg.type !== 'saas-chatbot-embed') return;

    switch (msg.action) {
      case 'widget-initialized':
        if (isFullscreenMode) {
          // Fullscreen mode: no need to do anything, container is already visible
        } else if (autoOpen) {
          setTimeout(open, Math.max(0, isNaN(greetDelay) ? 300 : greetDelay));
        }
        break;
      case 'close': close(); break;
      case 'open': open(); break;
      case 'resize':
        if (!isFullscreenMode) {
          const h = Math.min(Math.max(msg.height || 0, 300), 700);
          container.style.height = h + 'px';
        }
        break;
      case 'message-sent':
        if (window.gtag) {
          window.gtag('event', 'chat_message_sent', {
            client_id: clientId,
            session_id: (msg.data && msg.data.session_id) || undefined
          });
        }
        break;
      case 'widget-expanded':
        if (msg.data && msg.data.expanded) {
          expand();
        } else {
          collapse();
        }
        break;
      case 'widget-error':
        console.error('[SaaS Chatbot]', msg.data && msg.data.error);
        break;
    }
  });

  document.body.appendChild(container);
  if (launcher) document.body.appendChild(launcher);

  window.SaasChatbotWidget = {
    version,
    clientId,
    open, close, toggle,
    expand, collapse, toggleExpand,
    isOpen: () => isOpen || isFullscreenMode,
    isExpanded: () => isExpanded || isFullscreenMode,
    sendMessage: (message) => {
      postToFrame('send-message', { message });
    }
  };
})();
