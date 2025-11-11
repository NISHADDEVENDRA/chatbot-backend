(function () {
  function qs(sel) { return document.querySelector(sel); }
  
  async function postJSON(url, payload) {
    const res = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify(payload)
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      const msg = data?.message || 'Request failed';
      throw new Error(msg);
    }
    return data;
  }
  
  async function setCookie(token, expiresAt) {
    const res = await fetch('/auth/set-cookie', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ token, expires_at: expiresAt })
    });
    if (!res.ok) throw new Error('Failed to set auth cookie');
  }
  
  function getFormData(form) {
    const fd = new FormData(form);
    const obj = {};
    fd.forEach((v, k) => { obj[k] = v; });
    return obj;
  }
  
  function redirect(url) {
    if (url && typeof url === 'string') window.location.href = url;
    else window.location.href = '/';
  }

  const loginForm = qs('#loginForm');
  if (loginForm) {
    loginForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      const data = getFormData(loginForm);
      const payload = { username: data.username, password: data.password };
      try {
        const resp = await postJSON('/auth/login', payload);
        await setCookie(resp.token, resp.expires_at);
        redirect(data.return_url || '/embed/chatframe/' + (data.client_id || ''));
      } catch (err) {
        alert(err.message || 'Login failed');
      }
    });
  }

  const registerForm = qs('#registerForm');
  if (registerForm) {
    registerForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      const data = getFormData(registerForm);
      const payload = {
        username: data.username,
        name: data.name,
        password: data.password,
        role: data.role,
        client_id: data.client_id || ''
      };
      try {
        const resp = await postJSON('/auth/register', payload);
        await setCookie(resp.token, resp.expires_at);
        redirect(data.return_url || '/embed/chatframe/' + (data.client_id || ''));
      } catch (err) {
        alert(err.message || 'Registration failed');
      }
    });
  }
})();
