(selectors, method, origin, loginPath) => {
  const controls = 'button,a,[role="button"],input[type="submit"]';
  const visible = el => !el.closest('[hidden],[inert],[aria-hidden="true"]') &&
    el.checkVisibility({checkOpacity: true, checkVisibilityCSS: true});
  const usable = el => visible(el) && !el.matches(':disabled') && !el.closest('[aria-disabled="true"]');
  const find = query => query ? [...document.querySelectorAll(query)].filter(usable) : [];
  const label = el => (el.getAttribute('aria-label') || el.innerText || el.value || '').replace(/\s+/g, '');
  const provider = method === 'github' ? /github/i : /linux.?do/i;
  const other = method === 'github' ? /linux.?do/i : /github/i;
  const entry = () => {
    const configured = find(selectors[method]).filter(el => !other.test(label(el)));
    if (configured.length) return configured;
    const matches = find(controls).filter(el => provider.test(label(el)) && !other.test(label(el)));
    // Provider buttons take precedence over project/source links in the footer.
    const buttons = matches.filter(el => el.matches('button,[role="button"],input'));
    return buttons.length ? buttons : matches.filter(el => /登录|继续|登入|login|signin|continue/i.test(label(el)));
  };
  const consent = () => {
    const github = method === 'github' && location.origin === 'https://github.com' && location.pathname === '/login/oauth/authorize';
    const linuxdo = method === 'linuxdo' && location.origin === 'https://connect.linux.do' && location.pathname === '/oauth2/authorize';
    if (!github && !linuxdo) return [];
    const candidates = find(github ? '#js-oauth-authorize-btn,button[name="authorize"][value="1"],input[name="authorize"][value="1"]' : controls);
    return candidates.filter(el => {
      if (linuxdo && !/^(允许|授权|同意|允许授权|同意授权|authorize|allow|approve)(应用)?$/i.test(label(el))) return false;
      const form = el.form || el.closest('form');
      if (!form || new URL(form.action, location.href).origin !== location.origin) return false;
      const params = new URL(location.href).searchParams;
      const redirects = [params.get('redirect_uri'), form.querySelector('[name="redirect_uri"]')?.value].filter(Boolean);
      try {
        if (redirects.length) return redirects.every(value => new URL(value).origin === origin);
      } catch { return false; }
      // GitHub can use an application's registered callback without exposing
      // redirect_uri. In that case require the form to match the current OAuth
      // transaction, including its CSRF state, before continuing.
      return !!params.get('client_id') && !!params.get('state') &&
        form.querySelector('[name="client_id"]')?.value === params.get('client_id') &&
        form.querySelector('[name="state"]')?.value === params.get('state');
    });
  };
  const inspect = kind => {
    if (method !== 'github' && method !== 'linuxdo') return 'unsupported';
    if (kind === 'entry' && location.href === 'about:blank') return '';
    if (kind === 'entry' && (location.origin !== origin || location.pathname.replace(/\/$/, '') !== loginPath.replace(/\/$/, ''))) return 'left';
    try {
      const matches = kind === 'entry' ? entry() : consent();
      return matches.length > 1 ? 'ambiguous' : matches.length === 1 ? 'ready' : '';
    } catch { return 'invalid'; }
  };
  return {
    inspect,
    click(kind) {
      const state = inspect(kind);
      if (state !== 'ready') return state;
      (kind === 'entry' ? entry() : consent())[0].click();
      return 'clicked';
    },
  };
}
