// Runs before first paint so the page never flashes the wrong theme. Dark is the default.
try {
  if (localStorage.getItem('mechon-theme') === 'light') document.documentElement.classList.remove('dark')
} catch {}
