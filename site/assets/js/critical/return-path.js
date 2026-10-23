// return-path.js keeps the page a reader asked for across a sign-in.
//
// The path is saved in the demo_return_to cookie in two places: the Go server
// sets it when it redirects an unauthenticated request to /login/, and this
// script sets it when mod-hanko reports that a session expired mid-read. After
// sign-in, mod-hanko sends the browser to /post-login/, where this script reads
// the cookie back, clears it, and redirects.
//
// Hinode bundles everything under assets/js/critical/ into a script that loads
// on every page, which is why it lives here and not inline on /post-login/.
(function () {
  'use strict'

  const COOKIE = 'demo_return_to'
  const MAX_AGE = 600 // seconds, the same lifetime the server uses

  // Only same-origin paths, and never the sign-in pages themselves, which
  // would send the reader round in a loop.
  function isSafeTarget (p) {
    return !!p && p.length <= 2048 &&
      p.charAt(0) === '/' && p.charAt(1) !== '/' && p.charAt(1) !== '\\' &&
      !p.startsWith('/login') && !p.startsWith('/post-login')
  }

  function readCookie () {
    const m = document.cookie.match(/(?:^|;\s*)demo_return_to=([^;]*)/)
    if (!m) return ''
    try {
      return decodeURIComponent(m[1])
    } catch {
      return ''
    }
  }

  function clearCookie () {
    document.cookie = COOKIE + '=; path=/; max-age=0; samesite=lax'
  }

  function saveCurrentPath () {
    const path = location.pathname + location.search
    if (!isSafeTarget(path)) return
    document.cookie = COOKIE + '=' + encodeURIComponent(path) +
      '; path=/; max-age=' + MAX_AGE + '; samesite=lax'
  }

  function restore () {
    if (!location.pathname.startsWith('/post-login')) return
    const target = readCookie()
    clearCookie()
    location.replace(isSafeTarget(target) ? target : '/')
  }

  document.addEventListener('hanko:sessionExpired', saveCurrentPath)
  document.addEventListener('hanko:afterLogout', clearCookie)
  restore()
})()
