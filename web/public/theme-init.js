// Applies the remembered theme before first paint. A classic script in the head, not inline, because the
// daemon's content security policy allows only same-origin scripts. System sets nothing: the tokens follow the OS.
try {
  var t = localStorage.getItem('sh-theme')
  if (t === 'light' || t === 'dark') document.documentElement.setAttribute('data-theme', t)
} catch (e) {}
