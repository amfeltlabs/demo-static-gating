// Checks that the module list in config/_default/hugo.toml still mirrors
// Hinode's own import list, minus the modules this site leaves out on purpose.
//
// The site imports Hinode with ignoreImports and lists Hinode's modules itself
// (see the comment in hugo.toml). When a Hinode update adds, drops or bumps a
// module, the two lists drift apart without an error. This script turns that
// drift into a failed build, so an automated module update cannot merge it.
//
// Run it from site/ after the modules have been downloaded, e.g. after a build.

import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync } from 'node:fs'
import { join } from 'node:path'

const HINODE = 'github.com/gethinode/hinode/v3'

// Imported by this site but not by Hinode.
const SITE_ONLY = new Set([
  HINODE,
  'github.com/gethinode/mod-blocks/v2',
  'github.com/gethinode/mod-hanko/v3'
])

// Imported by Hinode but left out here on purpose.
const LEFT_OUT = new Set([
  'github.com/gethinode/mod-mermaid/v5' // ~42 MB of static bundles, no diagrams on this site
])

function importPaths (toml) {
  // Only [[module.imports]] entries carry a `path = "github.com/..."` line.
  return [...toml.matchAll(/^\s*path\s*=\s*"(github\.com\/[^"]+)"/gm)].map(m => m[1])
}

// `hugo config mounts` prints one JSON object per module, with its directory.
const out = execFileSync('npx', ['--no', 'hugo', 'config', 'mounts'], {
  encoding: 'utf8',
  stdio: ['ignore', 'pipe', 'pipe'] // keep Hugo's warnings out of the output
})
const dir = out.split(/\n(?=\{)/)
  .map(chunk => { try { return JSON.parse(chunk) } catch { return null } })
  .find(m => m && m.path === HINODE)?.dir
if (!dir) {
  console.error(`check-hinode-imports: ${HINODE} not found in 'hugo config mounts'`)
  process.exit(1)
}

const hinodeConfig = ['config/_default/hugo.toml', 'config.toml', 'hugo.toml']
  .map(f => join(dir, f))
  .find(f => existsSync(f) && importPaths(readFileSync(f, 'utf8')).length > 0)
if (!hinodeConfig) {
  console.error(`check-hinode-imports: no module imports found in ${dir}`)
  process.exit(1)
}

const expected = importPaths(readFileSync(hinodeConfig, 'utf8')).filter(p => !LEFT_OUT.has(p))
const actual = importPaths(readFileSync('config/_default/hugo.toml', 'utf8')).filter(p => !SITE_ONLY.has(p))

const missing = expected.filter(p => !actual.includes(p))
const extra = actual.filter(p => !expected.includes(p))
if (missing.length || extra.length) {
  console.error(`The module list in config/_default/hugo.toml no longer matches ${hinodeConfig}.`)
  for (const p of missing) console.error(`  missing: ${p}  (Hinode imports it)`)
  for (const p of extra) console.error(`  extra:   ${p}  (Hinode no longer imports it)`)
  console.error('Update the list, or LEFT_OUT in this script if a module is dropped on purpose.')
  process.exit(1)
}
console.log(`check-hinode-imports: ${actual.length} modules match Hinode's list`)
