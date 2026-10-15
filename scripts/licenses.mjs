import { execFileSync } from 'node:child_process'
import { mkdir, readdir, readFile, writeFile, stat, chmod } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const args = process.argv.slice(2)
const option = (name, fallback) => args.includes(name) ? args[args.indexOf(name) + 1] : fallback
const go = option('--go', 'go')
const out = path.resolve(option('--out', path.join(root, 'dist/THIRD_PARTY_NOTICES.md')))
const relativeOut = path.relative(path.join(root, 'dist'), out)
if (!relativeOut || relativeOut === '..' || relativeOut.startsWith('..' + path.sep) || path.isAbsolute(relativeOut)) {
  throw new Error('License output must stay under project dist')
}
function jsonStream(text) {
  const values = []
  let depth = 0, quoted = false, escaped = false, start = 0
  for (let i = 0; i < text.length; i++) {
    const c = text[i]
    if (quoted) { if (escaped) escaped = false; else if (c === '\\') escaped = true; else if (c === '"') quoted = false; continue }
    if (c === '"') quoted = true
    else if (c === '{') { if (depth++ === 0) start = i }
    else if (c === '}' && --depth === 0) values.push(JSON.parse(text.slice(start, i + 1)))
  }
  return values
}
const modules = jsonStream(execFileSync(go, ['list', '-m', '-json', 'all'], { cwd: root, encoding: 'utf8' }))
const components = []
async function collect(name, version, dir) {
  const entries = await readdir(dir, { withFileTypes: true })
  const licenses = entries
    .filter(e => e.isFile() && /^(licen[cs]e|copying|notice|unlicen[cs]e)/i.test(e.name))
    .sort((a, b) => a.name.localeCompare(b.name, 'en'))
  const files = []
  for (const entry of licenses) {
    files.push({ name: entry.name, text: await readFile(path.join(dir, entry.name), 'utf8') })
  }
  if (!files.length) throw new Error('Missing license file: ' + name)
  components.push({ name, version, files })
}
for (const module of modules) {
  if (!module.Main && module.Dir) await collect(module.Path, module.Version, module.Dir)
}
const visited = new Set()
async function findPackage(from, name) {
  let current = from
  while (true) {
    const dir = path.join(current, 'node_modules', name)
    if (await stat(path.join(dir, 'package.json')).then(() => true, () => false)) return dir
    const parent = path.dirname(current)
    if (parent === current) throw new Error('Cannot locate npm dependency ' + name)
    current = parent
  }
}
async function npmPackage(name, from) {
  const dir = await findPackage(from, name)
  if (visited.has(dir)) return
  visited.add(dir)
  const metadata = JSON.parse(await readFile(path.join(dir, 'package.json'), 'utf8'))
  await collect(name, metadata.version, dir)
  for (const dependency of Object.keys(metadata.dependencies || {}).sort()) await npmPackage(dependency, dir)
}
await npmPackage('vue', path.join(root, 'web'))
const goEnv = JSON.parse(execFileSync(go, ['env', '-json', 'GOROOT', 'GOVERSION'], { cwd: root, encoding: 'utf8' }))
await collect('Go', goEnv.GOVERSION, goEnv.GOROOT)
components.sort((a, b) => a.name.localeCompare(b.name, 'en') || a.version.localeCompare(b.version, 'en'))
const parts = [
  await readFile(path.join(root, 'THIRD_PARTY_NOTICES.md'), 'utf8'),
  '\n## Bundled dependency licenses\n\nGenerated from the dependencies used by this build. Component versions and complete license/notice texts follow.\n',
]
for (const component of components) {
  parts.push('\n### ' + component.name + ' — ' + component.version + '\n')
  for (const file of component.files) {
    // A license may itself contain Markdown fences. Keep its full text literal.
    const fence = '`'.repeat(Math.max(3, ...Array.from(file.text.matchAll(/`+/g), match => match[0].length + 1)))
    parts.push('\n#### ' + file.name + '\n\n' + fence + 'text\n' + file.text + (file.text.endsWith('\n') ? '' : '\n') + fence + '\n')
  }
}
await mkdir(path.dirname(out), { recursive: true })
// Do not inherit read-only module-cache attributes; repeated Windows builds must work.
await chmod(out, 0o644).catch(error => { if (error.code !== 'ENOENT') throw error })
await writeFile(out, parts.join(''), 'utf8')
console.log('Consolidated complete license texts for ' + components.length + ' components into ' + out)
