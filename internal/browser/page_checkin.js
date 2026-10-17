(selectors, origin) => {
  const controls = 'button,a,[role="button"],input[type="submit"],input[type="button"]';
  const notices = '[role="status"],[role="alert"],[aria-live="polite"],[aria-live="assertive"],.semi-toast-content,.ant-message-notice-content,.el-message__content';
  const visible = el => !el.closest('[hidden],[inert],[aria-hidden="true"]') &&
    el.checkVisibility({checkOpacity: true, checkVisibilityCSS: true});
  const find = query => query ? [...document.querySelectorAll(query)].filter(visible) : [];
  const label = el => (el.getAttribute('aria-label') || el.innerText || el.value || '').replace(/\s+/g, '').toLowerCase();
  const actionable = el => !el.matches(':disabled') && !el.closest('[aria-disabled="true"]') &&
    (!el.href || new URL(el.href, location.href).origin === origin);
  const candidates = () => (selectors.checkin ? find(selectors.checkin) : find(controls).filter(el =>
    /^(签到|立即签到|每日签到|今日签到|点击签到|签到领取奖励|check-?in(now)?|dailycheck-?in)$/.test(label(el))
  )).filter(actionable);
  const inspect = () => {
    if (location.origin !== origin) return 'foreign';
    try {
      // Validate all configured selectors before considering a result or click.
      for (const query of [selectors.checkin, selectors.success, selectors.already]) {
        if (query) document.querySelector(query);
      }
      if (find(selectors.already).length) return 'already';
      if (find(selectors.success).length) return 'success';
      if (!selectors.success && !selectors.already) {
        for (const el of find(controls + ',' + notices)) {
          const text = label(el);
          if (/^(今日已签到|今天已签到|今日已经签到|今天已经签到|已签到|alreadychecked-?in(today)?|checked-?intoday)[！!。.]?$/.test(text)) return 'already';
          if (/^(签到成功|今日签到成功|已成功签到|check-?insuccessful|successfullychecked-?in)([！!。.,，:：]|$)/.test(text)) return 'success';
        }
      }
      const matches = candidates();
      return matches.length > 1 ? 'ambiguous' : matches.length === 1 ? 'ready' : '';
    } catch {
      return 'invalid';
    }
  };
  return {
    inspect,
    click() {
      const state = inspect();
      if (state !== 'ready') return state;
      candidates()[0].click();
      return 'clicked';
    },
  };
}
