import { execFileSync } from 'node:child_process'
import { mkdir, readdir, readFile, writeFile, stat, chmod } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const args = process.argv.slice(2)
const option = (name, fallback) => args.includes(name) ? args[args.indexOf(name) + 1] : fallback
const go = option('--go', 'go')
const out = path.resolve(option('--out', path.join(root, 'dist/licenses')))
if (!out.startsWith(path.join(root, 'dist') + path.sep)) throw new Error('License output must stay under project dist')
await mkdir(out, { recursive: true })
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
const manifest = []
async function copyNotice(source, destination) {
  // Go's module cache is read-only. Do not inherit that attribute into release
  // outputs: it would make a second Windows build fail with EPERM.
  await chmod(destination, 0o644).catch(error => { if (error.code !== 'ENOENT') throw error })
  await writeFile(destination, await readFile(source))
}
async function collect(name, version, dir) {
  const entries = await readdir(dir, { withFileTypes: true })
  const licenses = entries.filter(e => e.isFile() && /^(licen[cs]e|copying|notice|unlicen[cs]e)/i.test(e.name))
  const files = []
  for (const entry of licenses) {
    const destination = (name + '@' + version + '-' + entry.name).replace(/[^a-zA-Z0-9._@-]/g, '_')
    await copyNotice(path.join(dir, entry.name), path.join(out, destination))
    files.push(destination)
  }
  if (!files.length) throw new Error('Missing license file: ' + name)
  manifest.push({ name, version, files })
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
  for (const dependency of Object.keys(metadata.dependencies || {})) await npmPackage(dependency, dir)
}
await npmPackage('vue', path.join(root, 'web'))
const goroot = execFileSync(go, ['env', 'GOROOT'], { cwd: root, encoding: 'utf8' }).trim()
await copyNotice(path.join(goroot, 'LICENSE'), path.join(out, 'Go-LICENSE.txt'))
await writeFile(path.join(out, 'manifest.json'), JSON.stringify(manifest, null, 2))
console.log('Collected license texts for ' + manifest.length + ' dependency modules/packages and Go.')
